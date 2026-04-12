package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/redis/go-redis/v9"
	"github.com/spf13/cobra"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	clicontext "github.com/osama1998H/moca/internal/context"
	"github.com/osama1998H/moca/internal/output"
)

// DoctorCheck is the interface for health checks run by "moca doctor".
// Each check tests one aspect of the system and returns a result.
type DoctorCheck interface {
	Name() string
	Run(ctx *clicontext.CLIContext) DoctorResult
}

// DoctorStatus represents the outcome of a health check.
type DoctorStatus string

const (
	DoctorPass DoctorStatus = "pass"
	DoctorWarn DoctorStatus = "warn"
	DoctorFail DoctorStatus = "fail"
	DoctorSkip DoctorStatus = "skip"
)

// DoctorResult holds the outcome of a single health check.
type DoctorResult struct {
	Name    string       `json:"name"`
	Status  DoctorStatus `json:"status"`
	Message string       `json:"message"`
	Detail  string       `json:"detail,omitempty"`
}

// doctorFlags holds the parsed flag values for the doctor command.
type doctorFlags struct {
	site    string
	verbose bool
	fix     bool
	jsonOut bool
}

// NewDoctorCommand returns the "moca doctor" command.
func NewDoctorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose system health",
		Long:  "Run health checks to verify that the project, configuration, and external services are working correctly.",
		RunE:  runDoctor,
	}

	f := cmd.Flags()
	f.String("site", "", "Run per-site checks for a specific site")
	f.Bool("verbose", false, "Show response times and details")
	f.Bool("fix", false, "Auto-remediate fixable issues")
	f.Bool("json", false, "Output results as JSON")

	return cmd
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)
	cliCtx := clicontext.FromCommand(cmd)

	site, _ := cmd.Flags().GetString("site")
	verbose, _ := cmd.Flags().GetBool("verbose")
	fix, _ := cmd.Flags().GetBool("fix")
	jsonOut, _ := cmd.Flags().GetBool("json")

	flags := &doctorFlags{
		site:    site,
		verbose: verbose,
		fix:     fix,
		jsonOut: jsonOut,
	}

	checks := buildChecks(cliCtx, flags)
	results := make([]DoctorResult, 0, len(checks))
	for _, check := range checks {
		results = append(results, check.Run(cliCtx))
	}

	if jsonOut || w.Mode() == output.ModeJSON {
		return w.PrintJSON(results)
	}

	w.Print("System Health Check\n")

	headers := []string{"CHECK", "STATUS", "MESSAGE"}
	if verbose {
		headers = append(headers, "DETAIL")
	}
	rows := make([][]string, 0, len(results))
	for _, r := range results {
		row := []string{r.Name, formatStatus(r.Status, w.Color()), r.Message}
		if verbose {
			row = append(row, r.Detail)
		}
		rows = append(rows, row)
	}
	if err := w.PrintTable(headers, rows); err != nil {
		return err
	}

	// Exit code 1 if any check failed.
	for _, r := range results {
		if r.Status == DoctorFail {
			return output.NewCLIError("One or more health checks failed").
				WithFix("Review the failures above and resolve the issues.")
		}
	}
	return nil
}

func buildChecks(cliCtx *clicontext.CLIContext, flags *doctorFlags) []DoctorCheck {
	checks := []DoctorCheck{
		&configCheck{},
		&versionCheck{},
		&diskCheck{},
	}

	// Infrastructure checks only if project is detected.
	if cliCtx != nil && cliCtx.Project != nil {
		checks = append(checks,
			&postgresCheck{verbose: flags.verbose},
			&redisCheck{verbose: flags.verbose},
			&kafkaCheck{verbose: flags.verbose},
			&meilisearchCheck{verbose: flags.verbose},
			&minioCheck{verbose: flags.verbose},
		)
	}

	// Per-site checks.
	if flags.site != "" && cliCtx != nil && cliCtx.Project != nil {
		checks = append(checks,
			&schemaCheck{site: flags.site, verbose: flags.verbose},
			&searchIndexCheck{site: flags.site, verbose: flags.verbose},
			&queueCheck{site: flags.site, verbose: flags.verbose},
		)
	}

	return checks
}

func formatStatus(s DoctorStatus, cc *output.ColorConfig) string {
	switch s {
	case DoctorPass:
		return cc.Success("PASS")
	case DoctorWarn:
		return cc.Warning("WARN")
	case DoctorFail:
		return cc.Error("FAIL")
	case DoctorSkip:
		return cc.Muted("SKIP")
	default:
		return string(s)
	}
}

// ─── configCheck ────────────────────────────────────────────────────────────

type configCheck struct{}

func (c *configCheck) Name() string { return "Config valid" }
func (c *configCheck) Run(ctx *clicontext.CLIContext) DoctorResult {
	if ctx == nil || ctx.Project == nil {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorFail,
			Message: "No moca.yaml found in current or parent directories",
		}
	}
	return DoctorResult{
		Name:    c.Name(),
		Status:  DoctorPass,
		Message: "moca.yaml parsed and validated successfully",
		Detail:  "Project root: " + ctx.ProjectRoot,
	}
}

// ─── versionCheck ───────────────────────────────────────────────────────────

type versionCheck struct{}

func (c *versionCheck) Name() string { return "Moca version" }
func (c *versionCheck) Run(ctx *clicontext.CLIContext) DoctorResult {
	if ctx == nil || ctx.Project == nil {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorSkip,
			Message: "No project detected",
		}
	}
	return DoctorResult{
		Name:    c.Name(),
		Status:  DoctorPass,
		Message: "moca version: " + ctx.Project.Moca,
		Detail:  "project: " + ctx.Project.Project.Name + " " + ctx.Project.Project.Version,
	}
}

// ─── postgresCheck ──────────────────────────────────────────────────────────

type postgresCheck struct {
	verbose bool
}

func (c *postgresCheck) Name() string { return "PostgreSQL reachable" }
func (c *postgresCheck) Run(ctx *clicontext.CLIContext) DoctorResult {
	if ctx == nil || ctx.Project == nil {
		return DoctorResult{Name: c.Name(), Status: DoctorSkip, Message: "No project detected"}
	}
	cfg := ctx.Project.Infrastructure.Database

	connStr := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		url.PathEscape(cfg.User), url.PathEscape(cfg.Password),
		cfg.Host, cfg.Port, cfg.SystemDB)

	bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(bgCtx, connStr)
	if err != nil {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorFail,
			Message: "Cannot connect to PostgreSQL",
			Detail:  err.Error(),
		}
	}
	defer pool.Close() //nolint:errcheck

	start := time.Now()
	var version string
	if err := pool.QueryRow(bgCtx, "SELECT version()").Scan(&version); err != nil {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorFail,
			Message: "Cannot query PostgreSQL",
			Detail:  err.Error(),
		}
	}
	latency := time.Since(start)

	// Extract short version info.
	shortVersion := version
	if idx := strings.Index(version, ","); idx > 0 {
		shortVersion = version[:idx]
	}

	detail := fmt.Sprintf("%s (latency: %s)", shortVersion, latency)
	status := DoctorPass
	msg := fmt.Sprintf("Connected to %s:%d", cfg.Host, cfg.Port)

	if latency > 100*time.Millisecond {
		status = DoctorWarn
		msg += " (slow connection)"
	}

	return DoctorResult{Name: c.Name(), Status: status, Message: msg, Detail: detail}
}

// ─── redisCheck ─────────────────────────────────────────────────────────────

type redisCheck struct {
	verbose bool
}

func (c *redisCheck) Name() string { return "Redis reachable" }
func (c *redisCheck) Run(ctx *clicontext.CLIContext) DoctorResult {
	if ctx == nil || ctx.Project == nil {
		return DoctorResult{Name: c.Name(), Status: DoctorSkip, Message: "No project detected"}
	}
	cfg := ctx.Project.Infrastructure.Redis
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	dbs := []struct {
		name string
		db   int
	}{
		{"cache", cfg.DbCache},
		{"queue", cfg.DbQueue},
		{"session", cfg.DbSession},
		{"pubsub", cfg.DbPubSub},
	}

	bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var failedClients []string
	var redisVersion string

	for _, d := range dbs {
		client := redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: cfg.Password,
			DB:       d.db,
		})
		defer client.Close() //nolint:errcheck

		if err := client.Ping(bgCtx).Err(); err != nil {
			failedClients = append(failedClients, fmt.Sprintf("%s(db%d)", d.name, d.db))
			continue
		}

		// Get version from first successful client.
		if redisVersion == "" {
			info, err := client.InfoMap(bgCtx, "server").Result()
			if err == nil {
				if serverInfo, ok := info["server"]; ok {
					redisVersion = serverInfo["redis_version"]
				}
			}
		}
	}

	if len(failedClients) == len(dbs) {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorFail,
			Message: fmt.Sprintf("Cannot connect to Redis at %s", addr),
		}
	}

	if len(failedClients) > 0 {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorWarn,
			Message: fmt.Sprintf("Some Redis DBs unreachable: %s", strings.Join(failedClients, ", ")),
			Detail:  "redis_version: " + redisVersion,
		}
	}

	msg := fmt.Sprintf("All 4 Redis clients connected at %s", addr)
	detail := ""
	status := DoctorPass
	if redisVersion != "" {
		detail = "redis_version: " + redisVersion
		if strings.HasPrefix(redisVersion, "6.") || strings.HasPrefix(redisVersion, "5.") {
			status = DoctorWarn
			msg += " (version < 7 detected)"
		}
	}

	return DoctorResult{Name: c.Name(), Status: status, Message: msg, Detail: detail}
}

// ─── kafkaCheck ─────────────────────────────────────────────────────────────

type kafkaCheck struct {
	verbose bool
}

func (c *kafkaCheck) Name() string { return "Kafka reachable" }
func (c *kafkaCheck) Run(ctx *clicontext.CLIContext) DoctorResult {
	if ctx == nil || ctx.Project == nil {
		return DoctorResult{Name: c.Name(), Status: DoctorSkip, Message: "No project detected"}
	}
	cfg := ctx.Project.Infrastructure.Kafka
	if cfg.Enabled == nil || !*cfg.Enabled {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorSkip,
			Message: "Kafka not configured (disabled or not set)",
		}
	}
	if len(cfg.Brokers) == 0 {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorFail,
			Message: "Kafka enabled but no brokers configured",
		}
	}

	client, err := kgo.NewClient(kgo.SeedBrokers(cfg.Brokers...))
	if err != nil {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorFail,
			Message: "Cannot create Kafka client",
			Detail:  err.Error(),
		}
	}
	defer client.Close() //nolint:errcheck

	bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	admin := kadm.NewClient(client)
	topics, err := admin.ListTopics(bgCtx)
	if err != nil {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorFail,
			Message: "Cannot list Kafka topics",
			Detail:  err.Error(),
		}
	}

	return DoctorResult{
		Name:    c.Name(),
		Status:  DoctorPass,
		Message: fmt.Sprintf("Connected to %s (%d topics)", strings.Join(cfg.Brokers, ","), len(topics)),
	}
}

// ─── meilisearchCheck ───────────────────────────────────────────────────────

type meilisearchCheck struct {
	verbose bool
}

func (c *meilisearchCheck) Name() string { return "Meilisearch reachable" }
func (c *meilisearchCheck) Run(ctx *clicontext.CLIContext) DoctorResult {
	if ctx == nil || ctx.Project == nil {
		return DoctorResult{Name: c.Name(), Status: DoctorSkip, Message: "No project detected"}
	}
	cfg := ctx.Project.Infrastructure.Search
	if cfg.Host == "" {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorSkip,
			Message: "Meilisearch not configured",
		}
	}

	baseURL := fmt.Sprintf("http://%s:%d", cfg.Host, cfg.Port)

	// Health check.
	client := &http.Client{Timeout: 5 * time.Second}
	healthURL := baseURL + "/health"
	resp, err := client.Get(healthURL)
	if err != nil {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorFail,
			Message: "Cannot connect to Meilisearch",
			Detail:  err.Error(),
		}
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorFail,
			Message: fmt.Sprintf("Meilisearch health check returned HTTP %d", resp.StatusCode),
		}
	}

	// Version check.
	versionURL := baseURL + "/version"
	vReq, err := http.NewRequest("GET", versionURL, nil)
	if err != nil {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorPass,
			Message: fmt.Sprintf("Connected to %s", baseURL),
		}
	}
	if cfg.APIKey != "" {
		vReq.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	vResp, err := client.Do(vReq)
	if err != nil {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorPass,
			Message: fmt.Sprintf("Connected to %s (version check failed)", baseURL),
		}
	}
	defer vResp.Body.Close() //nolint:errcheck

	body, _ := io.ReadAll(vResp.Body)
	var versionInfo struct {
		PkgVersion string `json:"pkgVersion"`
	}
	_ = json.Unmarshal(body, &versionInfo)

	detail := ""
	status := DoctorPass
	if versionInfo.PkgVersion != "" {
		detail = "version: " + versionInfo.PkgVersion
		// Warn if version is older than 1.12.
		if strings.HasPrefix(versionInfo.PkgVersion, "1.") {
			parts := strings.Split(versionInfo.PkgVersion, ".")
			if len(parts) >= 2 {
				var minor int
				_, _ = fmt.Sscanf(parts[1], "%d", &minor)
				if minor < 12 {
					status = DoctorWarn
				}
			}
		} else if strings.HasPrefix(versionInfo.PkgVersion, "0.") {
			status = DoctorWarn
		}
	}

	msg := fmt.Sprintf("Connected to %s", baseURL)
	if status == DoctorWarn {
		msg += " (version < 1.12)"
	}
	return DoctorResult{Name: c.Name(), Status: status, Message: msg, Detail: detail}
}

// ─── minioCheck ─────────────────────────────────────────────────────────────

type minioCheck struct {
	verbose bool
}

func (c *minioCheck) Name() string { return "Object storage reachable" }
func (c *minioCheck) Run(ctx *clicontext.CLIContext) DoctorResult {
	if ctx == nil || ctx.Project == nil {
		return DoctorResult{Name: c.Name(), Status: DoctorSkip, Message: "No project detected"}
	}
	cfg := ctx.Project.Infrastructure.Storage
	if cfg.Driver != "s3" {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorSkip,
			Message: fmt.Sprintf("Storage driver is %q, not s3", cfg.Driver),
		}
	}
	if cfg.Endpoint == "" {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorFail,
			Message: "S3 storage configured but no endpoint set",
		}
	}

	u, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorFail,
			Message: "Invalid S3 endpoint URL",
			Detail:  err.Error(),
		}
	}
	useSSL := u.Scheme == "https"
	host := u.Host

	client, err := minio.New(host, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorFail,
			Message: "Cannot create S3 client",
			Detail:  err.Error(),
		}
	}

	bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	buckets, err := client.ListBuckets(bgCtx)
	if err != nil {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorFail,
			Message: "Cannot list S3 buckets",
			Detail:  err.Error(),
		}
	}

	// Check if configured bucket exists.
	bucketFound := false
	for _, b := range buckets {
		if b.Name == cfg.Bucket {
			bucketFound = true
			break
		}
	}

	detail := fmt.Sprintf("%d buckets found", len(buckets))
	if cfg.Bucket != "" && !bucketFound {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorWarn,
			Message: fmt.Sprintf("Connected to %s but bucket %q not found", host, cfg.Bucket),
			Detail:  detail,
		}
	}

	return DoctorResult{
		Name:    c.Name(),
		Status:  DoctorPass,
		Message: fmt.Sprintf("Connected to %s", host),
		Detail:  detail,
	}
}

// ─── diskCheck ──────────────────────────────────────────────────────────────

type diskCheck struct{}

func (c *diskCheck) Name() string { return "Disk space" }
func (c *diskCheck) Run(ctx *clicontext.CLIContext) DoctorResult {
	path := "."
	if ctx != nil && ctx.ProjectRoot != "" {
		path = ctx.ProjectRoot
	}

	available, err := diskAvailableBytes(path)
	if err != nil {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorWarn,
			Message: "Cannot check disk space",
			Detail:  err.Error(),
		}
	}
	const (
		gb100mb = 100 * 1024 * 1024
		gb1     = 1024 * 1024 * 1024
	)

	avail := formatDiskBytes(available)
	if available < gb100mb {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorFail,
			Message: fmt.Sprintf("Only %s available (< 100MB)", avail),
		}
	}
	if available < gb1 {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorWarn,
			Message: fmt.Sprintf("Only %s available (< 1GB)", avail),
		}
	}
	return DoctorResult{
		Name:    c.Name(),
		Status:  DoctorPass,
		Message: fmt.Sprintf("%s available", avail),
	}
}

func formatDiskBytes(b int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case b >= gb:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// ─── schemaCheck (per-site) ─────────────────────────────────────────────────

type schemaCheck struct {
	site    string
	verbose bool
}

func (c *schemaCheck) Name() string { return "Site schema (" + c.site + ")" }
func (c *schemaCheck) Run(ctx *clicontext.CLIContext) DoctorResult {
	if ctx == nil || ctx.Project == nil {
		return DoctorResult{Name: c.Name(), Status: DoctorSkip, Message: "No project detected"}
	}
	cfg := ctx.Project.Infrastructure.Database
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		url.PathEscape(cfg.User), url.PathEscape(cfg.Password),
		cfg.Host, cfg.Port, cfg.SystemDB)

	bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(bgCtx, connStr)
	if err != nil {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorFail,
			Message: "Cannot connect to PostgreSQL",
			Detail:  err.Error(),
		}
	}
	defer pool.Close() //nolint:errcheck

	// Build expected schema name using same logic as orm.schemaNameForSite.
	schemaName := "tenant_" + strings.ToLower(strings.NewReplacer(".", "_", "-", "_", " ", "_").Replace(c.site))

	var count int
	err = pool.QueryRow(bgCtx,
		"SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name = $1",
		schemaName).Scan(&count)
	if err != nil {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorFail,
			Message: "Cannot query schema information",
			Detail:  err.Error(),
		}
	}

	if count == 0 {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorFail,
			Message: fmt.Sprintf("Schema %q does not exist", schemaName),
			Detail:  "Run 'moca site create " + c.site + "' to create the site.",
		}
	}

	// Count tables in schema.
	var tableCount int
	err = pool.QueryRow(bgCtx,
		"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = $1",
		schemaName).Scan(&tableCount)
	if err != nil {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorWarn,
			Message: fmt.Sprintf("Schema %q exists but cannot count tables", schemaName),
			Detail:  err.Error(),
		}
	}

	return DoctorResult{
		Name:    c.Name(),
		Status:  DoctorPass,
		Message: fmt.Sprintf("Schema %q exists with %d tables", schemaName, tableCount),
	}
}

// ─── searchIndexCheck (per-site) ────────────────────────────────────────────

type searchIndexCheck struct {
	site    string
	verbose bool
}

func (c *searchIndexCheck) Name() string { return "Search indexes (" + c.site + ")" }
func (c *searchIndexCheck) Run(ctx *clicontext.CLIContext) DoctorResult {
	if ctx == nil || ctx.Project == nil {
		return DoctorResult{Name: c.Name(), Status: DoctorSkip, Message: "No project detected"}
	}
	cfg := ctx.Project.Infrastructure.Search
	if cfg.Host == "" {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorSkip,
			Message: "Meilisearch not configured",
		}
	}

	baseURL := fmt.Sprintf("http://%s:%d", cfg.Host, cfg.Port)
	client := &http.Client{Timeout: 5 * time.Second}

	req, err := http.NewRequest("GET", baseURL+"/indexes", nil)
	if err != nil {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorFail,
			Message: "Cannot build request",
			Detail:  err.Error(),
		}
	}
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorFail,
			Message: "Cannot connect to Meilisearch",
			Detail:  err.Error(),
		}
	}
	defer resp.Body.Close() //nolint:errcheck

	body, _ := io.ReadAll(resp.Body)

	var indexResp struct {
		Results []struct {
			UID string `json:"uid"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &indexResp); err != nil {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorWarn,
			Message: "Cannot parse Meilisearch response",
			Detail:  err.Error(),
		}
	}

	// Count indexes matching the site prefix.
	prefix := strings.ToLower(strings.ReplaceAll(c.site, ".", "_")) + "_"
	var matchCount int
	for _, idx := range indexResp.Results {
		if strings.HasPrefix(idx.UID, prefix) {
			matchCount++
		}
	}

	if matchCount == 0 {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorWarn,
			Message: fmt.Sprintf("No indexes found with prefix %q", prefix),
			Detail:  fmt.Sprintf("Total indexes: %d", len(indexResp.Results)),
		}
	}

	return DoctorResult{
		Name:    c.Name(),
		Status:  DoctorPass,
		Message: fmt.Sprintf("%d indexes found for site", matchCount),
		Detail:  fmt.Sprintf("Prefix: %s, total indexes: %d", prefix, len(indexResp.Results)),
	}
}

// ─── queueCheck (per-site) ──────────────────────────────────────────────────

type queueCheck struct {
	site    string
	verbose bool
}

func (c *queueCheck) Name() string { return "Queue status (" + c.site + ")" }
func (c *queueCheck) Run(ctx *clicontext.CLIContext) DoctorResult {
	if ctx == nil || ctx.Project == nil {
		return DoctorResult{Name: c.Name(), Status: DoctorSkip, Message: "No project detected"}
	}
	cfg := ctx.Project.Infrastructure.Redis
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: cfg.Password,
		DB:       cfg.DbQueue,
	})
	defer client.Close() //nolint:errcheck

	bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(bgCtx).Err(); err != nil {
		return DoctorResult{
			Name:    c.Name(),
			Status:  DoctorFail,
			Message: "Cannot connect to Redis queue",
			Detail:  err.Error(),
		}
	}

	// Check main queue stream.
	streamKey := "moca:queue:" + c.site
	streamLen, err := client.XLen(bgCtx, streamKey).Result()
	if err != nil {
		// Stream may not exist yet, which is fine.
		streamLen = 0
	}

	// Check DLQ stream.
	dlqKey := "moca:dlq:" + c.site
	dlqLen, err := client.XLen(bgCtx, dlqKey).Result()
	if err != nil {
		dlqLen = 0
	}

	detail := fmt.Sprintf("stream: %d pending, dlq: %d failed", streamLen, dlqLen)
	status := DoctorPass
	msg := "Queue healthy"

	if dlqLen > 0 {
		status = DoctorWarn
		msg = fmt.Sprintf("%d messages in DLQ", dlqLen)
	}
	if streamLen > 1000 {
		status = DoctorWarn
		msg = fmt.Sprintf("Large queue backlog (%d pending)", streamLen)
	}

	return DoctorResult{Name: c.Name(), Status: status, Message: msg, Detail: detail}
}

// Ensure all checks satisfy the DoctorCheck interface.
var _ DoctorCheck = (*configCheck)(nil)
var _ DoctorCheck = (*versionCheck)(nil)
var _ DoctorCheck = (*postgresCheck)(nil)
var _ DoctorCheck = (*redisCheck)(nil)
var _ DoctorCheck = (*kafkaCheck)(nil)
var _ DoctorCheck = (*meilisearchCheck)(nil)
var _ DoctorCheck = (*minioCheck)(nil)
var _ DoctorCheck = (*diskCheck)(nil)
var _ DoctorCheck = (*schemaCheck)(nil)
var _ DoctorCheck = (*searchIndexCheck)(nil)
var _ DoctorCheck = (*queueCheck)(nil)

// Ensure unused imports are referenced.
var _ = os.Getenv

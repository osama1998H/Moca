package deploy

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/internal/generate"
	"github.com/osama1998H/moca/pkg/backup"
)

// setupStep is a single step in the deploy setup pipeline.
type setupStep struct {
	fn          func(ctx context.Context) error
	name        string
	description string
	number      int
	optional    bool
}

// Setup runs the 14-step production deployment pipeline.
// In dry-run mode it returns the step descriptions without executing.
func Setup(ctx context.Context, opts SetupOptions, cfg *config.ProjectConfig, cmd Commander) (*DeploymentRecord, []StepResult, error) {
	record := DeploymentRecord{
		ID:          GenerateID(),
		Type:        TypeSetup,
		Status:      StatusInProgress,
		StartedAt:   time.Now(),
		Domain:      opts.Domain,
		ProxyEngine: opts.Proxy,
		ProcessMgr:  opts.Process,
	}

	steps := buildSetupSteps(ctx, opts, cfg, cmd)

	var results []StepResult
	for _, s := range steps {
		sr := StepResult{
			Number:      s.number,
			Name:        s.name,
			Description: s.description,
		}

		if s.optional && !stepEnabled(opts, s.name) {
			sr.Skipped = true
			results = append(results, sr)
			continue
		}

		if opts.DryRun {
			results = append(results, sr)
			continue
		}

		if err := s.fn(ctx); err != nil {
			record.Status = StatusFailed
			record.Error = err.Error()
			record.CompletedAt = time.Now()
			record.Duration = record.CompletedAt.Sub(record.StartedAt)
			_ = RecordDeployment(opts.ProjectRoot, record)
			return &record, results, fmt.Errorf("step %d (%s): %w", s.number, s.name, err)
		}

		results = append(results, sr)
	}

	if !opts.DryRun {
		record.Status = StatusSuccess
		record.CompletedAt = time.Now()
		record.Duration = record.CompletedAt.Sub(record.StartedAt)
		if err := RecordDeployment(opts.ProjectRoot, record); err != nil {
			return &record, results, fmt.Errorf("record deployment: %w", err)
		}
	}

	return &record, results, nil
}

func buildSetupSteps(ctx context.Context, opts SetupOptions, cfg *config.ProjectConfig, cmd Commander) []setupStep {
	return []setupStep{
		{number: 1, name: "validate", description: "Validate system requirements", fn: func(_ context.Context) error {
			return stepValidateRequirements(ctx, cfg, cmd)
		}},
		{number: 2, name: "prod-mode", description: "Switch to production mode", fn: func(_ context.Context) error {
			return stepSwitchProdMode(opts.ProjectRoot)
		}},
		{number: 3, name: "build", description: "Build frontend assets and binaries", fn: func(_ context.Context) error {
			return stepBuild(ctx, opts.ProjectRoot, cmd)
		}},
		{number: 4, name: "proxy", description: "Generate reverse proxy configuration", fn: func(_ context.Context) error {
			return stepGenerateProxy(cfg, opts)
		}},
		{number: 5, name: "process-mgr", description: "Generate process manager configuration", fn: func(_ context.Context) error {
			return stepGenerateProcessMgr(cfg, opts)
		}},
		{number: 6, name: "redis", description: "Generate Redis production configuration", fn: func(_ context.Context) error {
			return stepRedisConfig(opts.ProjectRoot, cfg)
		}},
		{number: 7, name: "logrotate", description: "Configure log rotation", optional: true, fn: func(_ context.Context) error {
			return stepLogrotate(ctx, opts, cfg, cmd)
		}},
		{number: 8, name: "backup", description: "Configure automated backups", fn: func(_ context.Context) error {
			return stepBackupSchedule(ctx, opts, cfg)
		}},
		{number: 9, name: "firewall", description: "Setup firewall rules", optional: true, fn: func(_ context.Context) error {
			return stepFirewall(ctx, cmd)
		}},
		{number: 10, name: "fail2ban", description: "Setup fail2ban intrusion detection", optional: true, fn: func(_ context.Context) error {
			return stepFail2ban(ctx, opts, cfg, cmd)
		}},
		{number: 11, name: "tls", description: "Obtain TLS certificates", fn: func(_ context.Context) error {
			return stepTLS(ctx, opts, cfg, cmd)
		}},
		{number: 12, name: "start", description: "Start all services", fn: func(_ context.Context) error {
			return stepStartServices(ctx, opts, cfg, cmd)
		}},
		{number: 13, name: "health", description: "Run health checks", fn: func(_ context.Context) error {
			return stepHealthCheck(ctx, cfg)
		}},
		{number: 14, name: "record", description: "Record deployment", fn: func(_ context.Context) error {
			return CreateSnapshot(opts.ProjectRoot, GenerateID())
		}},
	}
}

// stepEnabled returns whether an optional step should run.
func stepEnabled(opts SetupOptions, stepName string) bool {
	switch stepName {
	case "logrotate":
		return opts.Logrotate
	case "firewall":
		return opts.Firewall
	case "fail2ban":
		return opts.Fail2ban
	default:
		return true
	}
}

// Step implementations ---

func stepValidateRequirements(ctx context.Context, cfg *config.ProjectConfig, cmd Commander) error {
	checks := []struct {
		name string
		cmd  string
		fix  string
		args []string
	}{
		{name: "PostgreSQL", cmd: "pg_isready", args: []string{
			"-h", cfg.Infrastructure.Database.Host,
			"-p", fmt.Sprintf("%d", cfg.Infrastructure.Database.Port),
		}, fix: "Ensure PostgreSQL is running and accessible."},
		{name: "Redis", cmd: "redis-cli", args: []string{
			"-h", cfg.Infrastructure.Redis.Host,
			"-p", fmt.Sprintf("%d", cfg.Infrastructure.Redis.Port),
			"ping",
		}, fix: "Ensure Redis is running and accessible."},
	}

	for _, c := range checks {
		if _, err := cmd.Run(ctx, c.cmd, c.args...); err != nil {
			return fmt.Errorf("%s check failed: %s", c.name, c.fix)
		}
	}
	return nil
}

func stepSwitchProdMode(projectRoot string) error {
	envDir := filepath.Join(projectRoot, ".moca")
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(envDir, "environment"), []byte("production\n"), 0o644)
}

func stepBuild(ctx context.Context, projectRoot string, cmd Commander) error {
	// Build frontend if desk/ exists.
	deskDir := filepath.Join(projectRoot, "desk")
	if _, err := os.Stat(filepath.Join(deskDir, "package.json")); err == nil {
		if out, err := cmd.Run(ctx, "npx", "vite", "build"); err != nil {
			return fmt.Errorf("frontend build failed: %s", string(out))
		}
	}

	// Build all server binaries.
	binDir := filepath.Join(projectRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}

	binaries := []struct {
		name string
		pkg  string
	}{
		{"moca-server", "./cmd/moca-server"},
		{"moca-worker", "./cmd/moca-worker"},
		{"moca-scheduler", "./cmd/moca-scheduler"},
		{"moca-outbox", "./cmd/moca-outbox"},
		{"moca", "./cmd/moca"},
	}

	for _, b := range binaries {
		outPath := filepath.Join(binDir, b.name)
		if out, err := cmd.Run(ctx, "go", "build", "-o", outPath, b.pkg); err != nil {
			return fmt.Errorf("build %s failed: %s", b.name, string(out))
		}
	}

	return nil
}

func stepGenerateProxy(cfg *config.ProjectConfig, opts SetupOptions) error {
	switch opts.Proxy {
	case "caddy", "":
		_, err := generate.GenerateCaddy(cfg, opts.ProjectRoot, generate.CaddyOptions{
			Domain: opts.Domain,
		})
		return err
	case "nginx":
		_, err := generate.GenerateNginx(cfg, opts.ProjectRoot, generate.NginxOptions{
			Domain: opts.Domain,
		})
		return err
	default:
		return fmt.Errorf("unsupported proxy engine: %s", opts.Proxy)
	}
}

func stepGenerateProcessMgr(cfg *config.ProjectConfig, opts SetupOptions) error {
	switch opts.Process {
	case "systemd", "":
		_, err := generate.GenerateSystemd(cfg, opts.ProjectRoot, generate.SystemdOptions{})
		return err
	case "docker":
		_, err := generate.GenerateDocker(cfg, opts.ProjectRoot, generate.DockerOptions{
			Profile: "production",
		})
		return err
	default:
		return fmt.Errorf("unsupported process manager: %s", opts.Process)
	}
}

func stepRedisConfig(projectRoot string, cfg *config.ProjectConfig) error {
	configDir := filepath.Join(projectRoot, "config", "redis")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return err
	}

	content := fmt.Sprintf(`# Generated by Moca — do not edit manually.
# Redis production configuration

# Memory policy
maxmemory-policy allkeys-lru

# Persistence
save 900 1
save 300 10
save 60 10000

# Connection
bind %s
port %d
tcp-keepalive 300

# Logging
loglevel notice
`, cfg.Infrastructure.Redis.Host, cfg.Infrastructure.Redis.Port)

	return os.WriteFile(filepath.Join(configDir, "redis-production.conf"), []byte(content), 0o644)
}

func stepLogrotate(ctx context.Context, opts SetupOptions, cfg *config.ProjectConfig, cmd Commander) error {
	content := fmt.Sprintf(`# Generated by Moca — do not edit manually.
%s/.moca/logs/*.log {
    daily
    missingok
    rotate 14
    compress
    delaycompress
    notifempty
    create 0640 moca moca
    sharedscripts
    postrotate
        systemctl reload %s-server@1.service 2>/dev/null || true
    endscript
}
`, opts.ProjectRoot, cfg.Project.Name)

	tmpFile := filepath.Join(opts.ProjectRoot, ".moca", "logrotate.conf")
	if err := os.WriteFile(tmpFile, []byte(content), 0o644); err != nil {
		return err
	}

	target := fmt.Sprintf("/etc/logrotate.d/moca-%s", cfg.Project.Name)
	if _, err := cmd.RunWithSudo(ctx, "cp", tmpFile, target); err != nil {
		return fmt.Errorf("install logrotate config: %w", err)
	}
	return nil
}

func stepBackupSchedule(ctx context.Context, opts SetupOptions, cfg *config.ProjectConfig) error {
	schedule := cfg.Backup.Schedule
	if schedule == "" {
		schedule = "0 2 * * *" // default: daily at 2 AM
	}
	return backup.InstallCronSchedule(ctx, schedule, cfg.Project.Name, opts.ProjectRoot)
}

func stepFirewall(ctx context.Context, cmd Commander) error {
	ports := []string{"22/tcp", "80/tcp", "443/tcp"}
	for _, port := range ports {
		if _, err := cmd.RunWithSudo(ctx, "ufw", "allow", port); err != nil {
			return fmt.Errorf("ufw allow %s: %w", port, err)
		}
	}
	_, err := cmd.RunWithSudo(ctx, "ufw", "--force", "enable")
	return err
}

func stepFail2ban(ctx context.Context, opts SetupOptions, cfg *config.ProjectConfig, cmd Commander) error {
	content := fmt.Sprintf(`# Generated by Moca — do not edit manually.
[moca-%s]
enabled = true
port = http,https
filter = moca-%s
logpath = %s/.moca/logs/access.log
maxretry = 5
bantime = 600
findtime = 600
`, cfg.Project.Name, cfg.Project.Name, opts.ProjectRoot)

	tmpFile := filepath.Join(opts.ProjectRoot, ".moca", "fail2ban.conf")
	if err := os.WriteFile(tmpFile, []byte(content), 0o644); err != nil {
		return err
	}

	target := fmt.Sprintf("/etc/fail2ban/jail.d/moca-%s.conf", cfg.Project.Name)
	if _, err := cmd.RunWithSudo(ctx, "cp", tmpFile, target); err != nil {
		return fmt.Errorf("install fail2ban config: %w", err)
	}
	_, err := cmd.RunWithSudo(ctx, "systemctl", "restart", "fail2ban")
	return err
}

func stepTLS(ctx context.Context, opts SetupOptions, cfg *config.ProjectConfig, cmd Commander) error {
	switch opts.TLS {
	case "none":
		return nil
	case "acme", "":
		// Caddy handles ACME automatically — no extra action needed.
		if opts.Proxy == "caddy" || opts.Proxy == "" {
			return nil
		}
		// For nginx, use certbot.
		email := opts.Email
		if email == "" {
			email = cfg.Production.TLS.Email
		}
		if email == "" {
			return fmt.Errorf("--email is required for ACME TLS with nginx")
		}
		_, err := cmd.RunWithSudo(ctx, "certbot", "--nginx",
			"-d", opts.Domain,
			"--email", email,
			"--agree-tos", "--non-interactive",
		)
		return err
	case "custom":
		if opts.TLSCert == "" || opts.TLSKey == "" {
			return fmt.Errorf("--tls-cert and --tls-key are required for custom TLS")
		}
		tlsDir := filepath.Join(opts.ProjectRoot, "config", "tls")
		if err := os.MkdirAll(tlsDir, 0o755); err != nil {
			return err
		}
		if err := copyFileIfExists(opts.TLSCert, filepath.Join(tlsDir, "cert.pem")); err != nil {
			return fmt.Errorf("copy TLS cert: %w", err)
		}
		return copyFileIfExists(opts.TLSKey, filepath.Join(tlsDir, "key.pem"))
	default:
		return fmt.Errorf("unsupported TLS mode: %s", opts.TLS)
	}
}

func stepStartServices(ctx context.Context, opts SetupOptions, _ *config.ProjectConfig, cmd Commander) error {
	switch opts.Process {
	case "systemd", "":
		if _, err := cmd.RunWithSudo(ctx, "systemctl", "daemon-reload"); err != nil {
			return fmt.Errorf("systemctl daemon-reload: %w", err)
		}
		_, err := cmd.RunWithSudo(ctx, "systemctl", "start", "moca.target")
		return err
	case "docker":
		composePath := filepath.Join(opts.ProjectRoot, "config", "docker", "docker-compose.yml")
		_, err := cmd.Run(ctx, "docker", "compose", "-f", composePath, "up", "-d")
		return err
	default:
		return fmt.Errorf("unsupported process manager: %s", opts.Process)
	}
}

func stepHealthCheck(ctx context.Context, cfg *config.ProjectConfig) error {
	port := cfg.Production.Port
	if port == 0 {
		port = 8000
	}
	url := fmt.Sprintf("http://localhost:%d/api/v1/health", port)

	client := &http.Client{Timeout: 5 * time.Second}
	var lastErr error

	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
			}
		}

		resp, err := client.Get(url)
		if err != nil {
			lastErr = err
			continue
		}
		_ = resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			return nil
		}
		lastErr = fmt.Errorf("health check returned status %d", resp.StatusCode)
	}

	return fmt.Errorf("health check failed after 3 attempts: %w", lastErr)
}

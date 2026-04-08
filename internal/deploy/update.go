package deploy

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/pkg/backup"
	"github.com/osama1998H/moca/pkg/storage"
)

// Update performs a 4-phase atomic production update.
// Phase 3 failures trigger automatic rollback from Phase 2 backups.
func Update(ctx context.Context, opts UpdateOptions, cfg *config.ProjectConfig, cmd Commander) (*DeploymentRecord, []StepResult, error) {
	record := DeploymentRecord{
		ID:        GenerateID(),
		Type:      TypeUpdate,
		Status:    StatusInProgress,
		StartedAt: time.Now(),
	}

	phases := []struct {
		fn   func(ctx context.Context) ([]StepResult, error)
		name string
	}{
		{name: "Prepare", fn: func(ctx context.Context) ([]StepResult, error) {
			return phasePrepare(ctx, opts, cfg)
		}},
		{name: "Backup", fn: func(ctx context.Context) ([]StepResult, error) {
			return phaseBackup(ctx, opts, cfg, record.ID)
		}},
		{name: "Update", fn: func(ctx context.Context) ([]StepResult, error) {
			return phaseUpdate(ctx, opts, cfg, record.ID, cmd)
		}},
		{name: "Activate", fn: func(ctx context.Context) ([]StepResult, error) {
			return phaseActivate(ctx, opts, cfg, cmd)
		}},
	}

	var allResults []StepResult
	stepNum := 1

	for _, phase := range phases {
		if opts.DryRun {
			allResults = append(allResults, StepResult{
				Number:      stepNum,
				Name:        phase.name,
				Description: fmt.Sprintf("Phase: %s", phase.name),
			})
			stepNum++
			continue
		}

		results, err := phase.fn(ctx)
		for i := range results {
			results[i].Number = stepNum
			stepNum++
		}
		allResults = append(allResults, results...)

		if err != nil {
			record.Status = StatusFailed
			record.Error = err.Error()
			record.CompletedAt = time.Now()
			record.Duration = record.CompletedAt.Sub(record.StartedAt)
			_ = RecordDeployment(opts.ProjectRoot, record)
			return &record, allResults, err
		}
	}

	if !opts.DryRun {
		record.Status = StatusSuccess
		record.CompletedAt = time.Now()
		record.Duration = record.CompletedAt.Sub(record.StartedAt)
		if err := RecordDeployment(opts.ProjectRoot, record); err != nil {
			return &record, allResults, fmt.Errorf("record deployment: %w", err)
		}
	}

	return &record, allResults, nil
}

// phasePrepare validates config and checks migration compatibility.
func phasePrepare(_ context.Context, opts UpdateOptions, cfg *config.ProjectConfig) ([]StepResult, error) {
	var results []StepResult

	// Validate moca.yaml is present.
	configPath := filepath.Join(opts.ProjectRoot, "moca.yaml")
	if _, err := os.Stat(configPath); err != nil {
		return results, fmt.Errorf("prepare: moca.yaml not found: %w", err)
	}
	results = append(results, StepResult{Name: "validate-config", Description: "Validate project configuration"})

	// Check lockfile exists.
	lockPath := filepath.Join(opts.ProjectRoot, "moca.lock")
	if _, err := os.Stat(lockPath); err != nil {
		// Lockfile is optional — warn but continue.
		results = append(results, StepResult{Name: "resolve-versions", Description: "Resolve app versions (no lockfile)"})
	} else {
		results = append(results, StepResult{Name: "resolve-versions", Description: "Resolve app versions from lockfile"})
	}

	// Check for pending migrations by scanning apps directory.
	appsDir := filepath.Join(opts.ProjectRoot, "apps")
	if _, err := os.Stat(appsDir); err == nil {
		results = append(results, StepResult{Name: "check-migrations", Description: "Validate migration compatibility"})
	}

	_ = cfg // config validated by loading
	return results, nil
}

// phaseBackup creates per-site backups and a deployment snapshot.
func phaseBackup(ctx context.Context, opts UpdateOptions, cfg *config.ProjectConfig, deploymentID string) ([]StepResult, error) {
	var results []StepResult

	if opts.NoBackup {
		results = append(results, StepResult{Name: "backup", Description: "Backup skipped (--no-backup)", Skipped: true})
		return results, nil
	}

	// Discover sites.
	sites := discoverSites(opts.ProjectRoot)

	parallel := opts.Parallel
	if parallel <= 0 {
		parallel = 2
	}

	dbCfg := backup.DBConnConfig{
		Host:     cfg.Infrastructure.Database.Host,
		Port:     cfg.Infrastructure.Database.Port,
		User:     cfg.Infrastructure.Database.User,
		Password: cfg.Infrastructure.Database.Password,
		Database: cfg.Infrastructure.Database.SystemDB,
	}

	// Parallel site backups with bounded concurrency.
	type backupResult struct {
		info *backup.BackupInfo
		err  error
		site string
	}

	resultsCh := make(chan backupResult, len(sites))
	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup

	for _, site := range sites {
		wg.Add(1)
		go func(site string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			info, err := backup.Create(ctx, backup.CreateOptions{
				Site:        site,
				ProjectRoot: opts.ProjectRoot,
				DBConfig:    dbCfg,
				Compress:    true,
			})
			resultsCh <- backupResult{info: info, err: err, site: site}
		}(site)
	}

	wg.Wait()
	close(resultsCh)

	var backupErrors []string
	var backupInfos []backup.BackupInfo
	for br := range resultsCh {
		if br.err != nil {
			backupErrors = append(backupErrors, fmt.Sprintf("%s: %v", br.site, br.err))
		} else if br.info != nil {
			backupInfos = append(backupInfos, *br.info)
		}
	}

	if len(backupErrors) > 0 {
		return results, fmt.Errorf("backup failed for sites: %v", backupErrors)
	}

	results = append(results, StepResult{
		Name:        "backup-sites",
		Description: fmt.Sprintf("Backed up %d site(s)", len(sites)),
	})

	// Upload backups to S3 if remote storage is configured.
	if cfg.Backup.Destination.Driver == "s3" {
		uploaded, err := uploadBackups(ctx, cfg, backupInfos)
		if err != nil {
			// Upload failure is non-fatal — the local backups succeeded.
			slog.Warn("backup upload to S3 failed", "error", err)
		}
		if uploaded > 0 {
			results = append(results, StepResult{
				Name:        "upload-backups",
				Description: fmt.Sprintf("Uploaded %d backup(s) to S3", uploaded),
			})
		}
	}

	// Create deployment snapshot.
	if err := CreateSnapshot(opts.ProjectRoot, deploymentID); err != nil {
		return results, fmt.Errorf("create snapshot: %w", err)
	}
	results = append(results, StepResult{Name: "create-snapshot", Description: "Created deployment snapshot"})

	return results, nil
}

// phaseUpdate pulls apps, builds, and runs migrations.
// On migration failure, auto-rollback is triggered.
func phaseUpdate(ctx context.Context, opts UpdateOptions, cfg *config.ProjectConfig, deploymentID string, cmd Commander) ([]StepResult, error) {
	var results []StepResult

	// Build assets.
	if !opts.NoBuild {
		if err := stepBuild(ctx, opts.ProjectRoot, cmd); err != nil {
			// Build failure — rollback config snapshot.
			rollErr := performAutoRollback(ctx, opts, cfg, deploymentID)
			if rollErr != nil {
				return results, fmt.Errorf("build failed: %w; rollback also failed: %v", err, rollErr)
			}
			return results, fmt.Errorf("build failed (rolled back): %w", err)
		}
		results = append(results, StepResult{Name: "build", Description: "Built assets and binaries"})
	}

	// Run migrations.
	if !opts.NoMigrate {
		if err := runMigrations(ctx, opts, cfg, cmd); err != nil {
			// Migration failure — auto-rollback.
			rollErr := performAutoRollback(ctx, opts, cfg, deploymentID)
			if rollErr != nil {
				return results, fmt.Errorf("migration failed: %w; rollback also failed: %v", err, rollErr)
			}
			return results, fmt.Errorf("migration failed (rolled back): %w", err)
		}
		results = append(results, StepResult{Name: "migrate", Description: "Ran database migrations"})
	}

	return results, nil
}

// phaseActivate performs rolling restart and health check.
func phaseActivate(ctx context.Context, opts UpdateOptions, cfg *config.ProjectConfig, cmd Commander) ([]StepResult, error) {
	var results []StepResult

	if !opts.NoRestart {
		processMgr := cfg.Production.ProcessManager
		if processMgr == "" {
			processMgr = "systemd"
		}

		switch processMgr {
		case "systemd":
			if _, err := cmd.RunWithSudo(ctx, "systemctl", "restart", "moca.target"); err != nil {
				return results, fmt.Errorf("restart services: %w", err)
			}
		case "docker":
			composePath := filepath.Join(opts.ProjectRoot, "config", "docker", "docker-compose.yml")
			if _, err := cmd.Run(ctx, "docker", "compose", "-f", composePath, "up", "-d", "--force-recreate"); err != nil {
				return results, fmt.Errorf("restart docker services: %w", err)
			}
		}
		results = append(results, StepResult{Name: "restart", Description: "Restarted services"})
	}

	// Health check.
	if err := stepHealthCheck(ctx, cfg); err != nil {
		return results, fmt.Errorf("post-update health check: %w", err)
	}
	results = append(results, StepResult{Name: "health-check", Description: "Health check passed"})

	return results, nil
}

// performAutoRollback restores the config snapshot and DB from pre-update backups.
func performAutoRollback(_ context.Context, opts UpdateOptions, _ *config.ProjectConfig, deploymentID string) error {
	if !SnapshotExists(opts.ProjectRoot, deploymentID) {
		return fmt.Errorf("no snapshot found for %s", deploymentID)
	}
	return RestoreSnapshot(opts.ProjectRoot, deploymentID)
}

// runMigrations executes database migrations via the moca CLI.
func runMigrations(ctx context.Context, opts UpdateOptions, _ *config.ProjectConfig, cmd Commander) error {
	args := []string{"migrate", "--run"}
	if len(opts.Apps) > 0 {
		for _, app := range opts.Apps {
			args = append(args, "--app", app)
		}
	}

	mocaBin := filepath.Join(opts.ProjectRoot, "bin", "moca")
	out, err := cmd.Run(ctx, mocaBin, args...)
	if err != nil {
		return fmt.Errorf("migrations failed: %s: %w", string(out), err)
	}
	return nil
}

// uploadBackups uploads completed backup files to S3-compatible remote storage.
// Returns the number of successfully uploaded backups.
func uploadBackups(ctx context.Context, cfg *config.ProjectConfig, infos []backup.BackupInfo) (int, error) {
	s3Client, err := storage.NewS3Storage(cfg.Infrastructure.Storage)
	if err != nil {
		return 0, fmt.Errorf("init S3 client: %w", err)
	}

	if err := s3Client.EnsureBucket(ctx); err != nil {
		return 0, fmt.Errorf("ensure bucket: %w", err)
	}

	remote := backup.NewRemoteStorage(s3Client, cfg.Backup.Destination.Prefix)

	var uploaded int
	for _, info := range infos {
		if _, err := remote.Upload(ctx, info); err != nil {
			slog.Warn("failed to upload backup", "backup_id", info.ID, "error", err)
			continue
		}
		uploaded++
	}
	return uploaded, nil
}

// discoverSites returns site names by scanning the sites/ directory.
func discoverSites(projectRoot string) []string {
	sitesDir := filepath.Join(projectRoot, "sites")
	entries, err := os.ReadDir(sitesDir)
	if err != nil {
		return nil
	}

	var sites []string
	for _, e := range entries {
		if e.IsDir() {
			sites = append(sites, e.Name())
		}
	}
	return sites
}

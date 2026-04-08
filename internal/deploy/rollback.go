package deploy

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/pkg/backup"
)

// Rollback restores the project to a previous deployment state.
func Rollback(ctx context.Context, opts RollbackOptions, cfg *config.ProjectConfig, cmd Commander) (*DeploymentRecord, error) {
	record := DeploymentRecord{
		ID:        GenerateID(),
		Type:      TypeRollback,
		Status:    StatusInProgress,
		StartedAt: time.Now(),
	}

	// Find target deployment.
	target, err := resolveRollbackTarget(opts)
	if err != nil {
		return nil, err
	}
	record.RollbackOf = target.ID

	// Verify snapshot exists.
	if !SnapshotExists(opts.ProjectRoot, target.ID) {
		return nil, fmt.Errorf("deploy: no snapshot found for deployment %s", target.ID)
	}

	// Pre-rollback backup of current state (unless --no-backup).
	if !opts.NoBackup {
		sites := discoverSites(opts.ProjectRoot)
		dbCfg := backup.DBConnConfig{
			Host:     cfg.Infrastructure.Database.Host,
			Port:     cfg.Infrastructure.Database.Port,
			User:     cfg.Infrastructure.Database.User,
			Password: cfg.Infrastructure.Database.Password,
			Database: cfg.Infrastructure.Database.SystemDB,
		}
		for _, site := range sites {
			if _, err := backup.Create(ctx, backup.CreateOptions{
				Site:        site,
				ProjectRoot: opts.ProjectRoot,
				DBConfig:    dbCfg,
				Compress:    true,
			}); err != nil {
				return nil, fmt.Errorf("pre-rollback backup for %s: %w", site, err)
			}
		}
	}

	// Restore config snapshot.
	if err := RestoreSnapshot(opts.ProjectRoot, target.ID); err != nil {
		record.Status = StatusFailed
		record.Error = err.Error()
		record.CompletedAt = time.Now()
		record.Duration = record.CompletedAt.Sub(record.StartedAt)
		_ = RecordDeployment(opts.ProjectRoot, record)
		return &record, fmt.Errorf("restore snapshot: %w", err)
	}

	// Rebuild binaries from restored config.
	if err := stepBuild(ctx, opts.ProjectRoot, cmd); err != nil {
		record.Status = StatusFailed
		record.Error = err.Error()
		record.CompletedAt = time.Now()
		record.Duration = record.CompletedAt.Sub(record.StartedAt)
		_ = RecordDeployment(opts.ProjectRoot, record)
		return &record, fmt.Errorf("rebuild after rollback: %w", err)
	}

	// Restart services.
	processMgr := cfg.Production.ProcessManager
	if processMgr == "" {
		processMgr = "systemd"
	}
	if err := restartServices(ctx, processMgr, opts.ProjectRoot, cmd); err != nil {
		record.Status = StatusFailed
		record.Error = err.Error()
		record.CompletedAt = time.Now()
		record.Duration = record.CompletedAt.Sub(record.StartedAt)
		_ = RecordDeployment(opts.ProjectRoot, record)
		return &record, fmt.Errorf("restart after rollback: %w", err)
	}

	// Health check.
	if err := stepHealthCheck(ctx, cfg); err != nil {
		record.Status = StatusFailed
		record.Error = err.Error()
		record.CompletedAt = time.Now()
		record.Duration = record.CompletedAt.Sub(record.StartedAt)
		_ = RecordDeployment(opts.ProjectRoot, record)
		return &record, fmt.Errorf("health check after rollback: %w", err)
	}

	// Record success.
	record.Status = StatusSuccess
	record.CompletedAt = time.Now()
	record.Duration = record.CompletedAt.Sub(record.StartedAt)
	if err := RecordDeployment(opts.ProjectRoot, record); err != nil {
		return &record, fmt.Errorf("record rollback: %w", err)
	}

	return &record, nil
}

func resolveRollbackTarget(opts RollbackOptions) (*DeploymentRecord, error) {
	if opts.DeploymentID != "" {
		return FindDeployment(opts.ProjectRoot, opts.DeploymentID)
	}
	step := opts.Step
	if step <= 0 {
		step = 1
	}
	return FindByStep(opts.ProjectRoot, step)
}

// restartServices restarts all Moca services using the configured process manager.
func restartServices(ctx context.Context, processMgr, projectRoot string, cmd Commander) error {
	switch processMgr {
	case "systemd":
		if _, err := cmd.RunWithSudo(ctx, "systemctl", "daemon-reload"); err != nil {
			return err
		}
		_, err := cmd.RunWithSudo(ctx, "systemctl", "restart", "moca.target")
		return err
	case "docker":
		composePath := filepath.Join(projectRoot, "config", "docker", "docker-compose.yml")
		_, err := cmd.Run(ctx, "docker", "compose", "-f", composePath, "up", "-d", "--force-recreate")
		return err
	default:
		return fmt.Errorf("unsupported process manager: %s", processMgr)
	}
}

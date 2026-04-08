package deploy

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/pkg/backup"
)

// EnvDiff describes a difference between two environment configs.
type EnvDiff struct {
	Field    string `json:"field"`
	Source   string `json:"source"`
	Target   string `json:"target"`
	Modified bool   `json:"modified"`
}

// Promote copies a deployment from one environment to another.
func Promote(ctx context.Context, opts PromoteOptions, cfg *config.ProjectConfig, cmd Commander) (*DeploymentRecord, []EnvDiff, error) {
	record := DeploymentRecord{
		ID:           GenerateID(),
		Type:         TypePromote,
		Status:       StatusInProgress,
		StartedAt:    time.Now(),
		PromotedFrom: opts.SourceEnv,
		PromotedTo:   opts.TargetEnv,
	}

	// Compute environment diff.
	diffs := computeEnvDiff(cfg, opts.SourceEnv, opts.TargetEnv)

	if opts.DryRun {
		return &record, diffs, nil
	}

	// Backup target environment.
	if !opts.SkipBackup {
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
				return nil, nil, fmt.Errorf("pre-promote backup for %s: %w", site, err)
			}
		}
	}

	// Create snapshot before promotion.
	if err := CreateSnapshot(opts.ProjectRoot, record.ID); err != nil {
		return nil, nil, fmt.Errorf("create pre-promote snapshot: %w", err)
	}

	// Copy lockfile from source to target project root if applicable.
	srcLock := filepath.Join(opts.ProjectRoot, "moca.lock")
	if err := copyFileIfExists(srcLock, filepath.Join(opts.ProjectRoot, "moca.lock")); err != nil {
		return nil, nil, fmt.Errorf("copy lockfile: %w", err)
	}

	// Run migrations on target.
	if err := runMigrations(ctx, UpdateOptions{ProjectRoot: opts.ProjectRoot}, cfg, cmd); err != nil {
		return nil, nil, fmt.Errorf("promote migrations: %w", err)
	}

	// Restart services.
	processMgr := cfg.Production.ProcessManager
	if processMgr == "" {
		processMgr = "systemd"
	}
	if err := restartServices(ctx, processMgr, opts.ProjectRoot, cmd); err != nil {
		return nil, nil, fmt.Errorf("promote restart: %w", err)
	}

	// Health check.
	if err := stepHealthCheck(ctx, cfg); err != nil {
		return nil, nil, fmt.Errorf("promote health check: %w", err)
	}

	// Record.
	record.Status = StatusSuccess
	record.CompletedAt = time.Now()
	record.Duration = record.CompletedAt.Sub(record.StartedAt)
	if err := RecordDeployment(opts.ProjectRoot, record); err != nil {
		return &record, diffs, fmt.Errorf("record promotion: %w", err)
	}

	return &record, diffs, nil
}

// computeEnvDiff compares two environment configs and returns the differences.
func computeEnvDiff(cfg *config.ProjectConfig, sourceEnv, targetEnv string) []EnvDiff {
	src := resolveEnvConfig(cfg, sourceEnv)
	tgt := resolveEnvConfig(cfg, targetEnv)

	var diffs []EnvDiff

	addDiff := func(field, srcVal, tgtVal string) {
		diffs = append(diffs, EnvDiff{
			Field:    field,
			Source:   srcVal,
			Target:   tgtVal,
			Modified: srcVal != tgtVal,
		})
	}

	addDiff("port", fmt.Sprintf("%d", src.port), fmt.Sprintf("%d", tgt.port))
	addDiff("workers", src.workers, tgt.workers)
	addDiff("log_level", src.logLevel, tgt.logLevel)
	addDiff("tls_provider", src.tlsProvider, tgt.tlsProvider)
	addDiff("proxy_engine", src.proxyEngine, tgt.proxyEngine)
	addDiff("process_manager", src.processMgr, tgt.processMgr)

	return diffs
}

// envConfig is a flattened environment configuration for comparison.
type envConfig struct {
	workers     string
	logLevel    string
	tlsProvider string
	proxyEngine string
	processMgr  string
	port        int
}

func resolveEnvConfig(cfg *config.ProjectConfig, env string) envConfig {
	// Start with production defaults.
	ec := envConfig{
		port:        cfg.Production.Port,
		workers:     cfg.Production.Workers,
		logLevel:    cfg.Production.LogLevel,
		tlsProvider: cfg.Production.TLS.Provider,
		proxyEngine: cfg.Production.Proxy.Engine,
		processMgr:  cfg.Production.ProcessManager,
	}

	// Apply staging overrides if the environment is staging.
	if env == "staging" {
		if cfg.Staging.Port != nil {
			ec.port = *cfg.Staging.Port
		}
		if cfg.Staging.Workers != nil {
			ec.workers = *cfg.Staging.Workers
		}
		if cfg.Staging.LogLevel != nil {
			ec.logLevel = *cfg.Staging.LogLevel
		}
		if cfg.Staging.TLS != nil {
			ec.tlsProvider = cfg.Staging.TLS.Provider
		}
		if cfg.Staging.Proxy != nil {
			ec.proxyEngine = cfg.Staging.Proxy.Engine
		}
		if cfg.Staging.ProcessManager != nil {
			ec.processMgr = *cfg.Staging.ProcessManager
		}
	}

	// Apply development overrides if the environment is development.
	if env == "development" {
		ec.port = cfg.Development.Port
		ec.workers = fmt.Sprintf("%d", cfg.Development.Workers)
		ec.logLevel = "debug"
	}

	return ec
}

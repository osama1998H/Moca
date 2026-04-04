package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/term"

	"github.com/osama1998H/moca/apps/core"
	"github.com/osama1998H/moca/internal/config"
	clicontext "github.com/osama1998H/moca/internal/context"
	"github.com/osama1998H/moca/internal/drivers"
	"github.com/osama1998H/moca/internal/output"
	"github.com/osama1998H/moca/internal/process"
	"github.com/osama1998H/moca/pkg/apps"
	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/hooks"
	"github.com/osama1998H/moca/pkg/meta"
	"github.com/osama1998H/moca/pkg/orm"
	"github.com/osama1998H/moca/pkg/queue"
	"github.com/osama1998H/moca/pkg/tenancy"
	"github.com/spf13/cobra"
)

// Services holds all constructed service instances for CLI commands.
// Commands call newServices to build the full dependency graph from config,
// then defer Services.Close().
type Services struct {
	DB          *orm.DBManager
	Redis       *drivers.RedisClients
	Migrator    *meta.Migrator
	Registry    *meta.Registry
	Runner      *orm.MigrationRunner
	Sites       *tenancy.SiteManager
	Apps        *apps.AppInstaller
	DocManager  *document.DocManager
	Controllers *document.ControllerRegistry
	Logger      *slog.Logger
}

// Close releases all connections. Redis is closed before DB.
func (s *Services) Close() {
	if s.Redis != nil {
		_ = s.Redis.Close()
	}
	if s.DB != nil {
		s.DB.Close()
	}
}

// newServices constructs the full service dependency graph from project config.
// Redis connection failure is non-fatal (logged as warning, services degrade gracefully).
func newServices(ctx context.Context, cfg *config.ProjectConfig, verbose bool) (*Services, error) {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	db, err := orm.NewDBManager(ctx, cfg.Infrastructure.Database, logger)
	if err != nil {
		return nil, output.NewCLIError("Cannot connect to PostgreSQL").
			WithErr(err).
			WithCause(err.Error()).
			WithFix(fmt.Sprintf("Ensure PostgreSQL is running on %s:%d.",
				cfg.Infrastructure.Database.Host, cfg.Infrastructure.Database.Port))
	}

	redis := drivers.NewRedisClients(cfg.Infrastructure.Redis, logger)
	var redisCache = redis.Cache
	if err := redis.Ping(ctx); err != nil {
		logger.Warn("Redis unavailable, running without cache", slog.String("error", err.Error()))
		redisCache = nil
	}

	migrator := meta.NewMigrator(db, logger)
	registry := meta.NewRegistry(db, redisCache, logger)
	runner := orm.NewMigrationRunner(db, logger)

	naming := document.NewNamingEngine()
	validator := document.NewValidator()
	controllers := document.NewControllerRegistry()
	core.Initialize(controllers, hooks.NewHookRegistry())
	docManager := document.NewDocManager(registry, db, naming, validator, controllers, logger)

	redisPubSub := redis.PubSub
	if redisCache == nil {
		redisPubSub = nil
	}
	sites := tenancy.NewSiteManager(db, migrator, registry, redisCache, redisPubSub, logger, core.BootstrapCoreMeta)
	installer := apps.NewAppInstaller(db, migrator, registry, runner, redisCache, logger)

	return &Services{
		DB:          db,
		Redis:       redis,
		Migrator:    migrator,
		Registry:    registry,
		Runner:      runner,
		Sites:       sites,
		Apps:        installer,
		DocManager:  docManager,
		Controllers: controllers,
		Logger:      logger,
	}, nil
}

// requireProject extracts CLIContext from the command and returns an error
// if no project was detected.
func requireProject(cmd *cobra.Command) (*clicontext.CLIContext, error) {
	ctx := clicontext.FromCommand(cmd)
	if ctx == nil || ctx.Project == nil {
		return nil, output.NewCLIError("No Moca project found").
			WithContext("Looked for moca.yaml in current and parent directories").
			WithFix("Run 'moca init' to create a project, or cd into a project directory.")
	}
	return ctx, nil
}

// resolveSiteName determines the target site name from --site flag or CLIContext.
func resolveSiteName(cmd *cobra.Command, ctx *clicontext.CLIContext) (string, error) {
	if site, _ := cmd.Flags().GetString("site"); site != "" {
		return site, nil
	}
	if ctx.Site != "" {
		return ctx.Site, nil
	}
	return "", output.NewCLIError("No site specified").
		WithFix("Pass --site <name> or run 'moca site use <name>' to set a default.")
}

// confirmPrompt asks the user to confirm a destructive action.
// Returns true if the user enters "y" or "yes". Returns error in non-TTY contexts.
func confirmPrompt(msg string) (bool, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false, output.NewCLIError("Cannot confirm in non-interactive mode").
			WithFix("Pass --force to skip confirmation.")
	}
	fmt.Fprintf(os.Stderr, "%s [y/N]: ", msg)
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return false, nil
	}
	answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
	return answer == "y" || answer == "yes", nil
}

// readPassword prompts for a password without echoing input.
// Returns error in non-TTY contexts.
func readPassword(prompt string) (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", output.NewCLIError("Cannot prompt for password in non-interactive mode").
			WithFix("Pass --admin-password <password> on the command line.")
	}
	fmt.Fprint(os.Stderr, prompt)
	pw, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr) // newline after hidden input
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	return string(pw), nil
}

// formatBytes formats a byte count into a human-readable string.
func formatBytes(b int64) string {
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

// listActiveSites returns the names of all active sites from the system database.
// Reused by queue, events, search, monitor, and worker commands.
func listActiveSites(ctx context.Context, svc *Services) ([]string, error) {
	rows, err := svc.DB.SystemPool().Query(ctx, "SELECT name FROM sites WHERE status = 'active'")
	if err != nil {
		return nil, fmt.Errorf("list active sites: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("list active sites: scan: %w", err)
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// newQueueProducer creates a queue.Producer from the Services' Redis Queue client.
func newQueueProducer(svc *Services) *queue.Producer {
	return queue.NewProducer(svc.Redis.Queue, svc.Logger)
}

// writePIDFile writes the current process PID to {dir}/.moca/{name}.pid.
func writePIDFile(dir, name string) error {
	pidDir := filepath.Join(dir, ".moca")
	if err := os.MkdirAll(pidDir, 0o755); err != nil {
		return fmt.Errorf("create pid directory: %w", err)
	}
	path := filepath.Join(pidDir, name+".pid")
	data := []byte(strconv.Itoa(os.Getpid()) + "\n")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	return nil
}

// readPIDFile reads and parses the PID from {dir}/.moca/{name}.pid.
func readPIDFile(dir, name string) (int, error) {
	path := filepath.Join(dir, ".moca", name+".pid")
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read pid file: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("parse pid file: %w", err)
	}
	return pid, nil
}

// removePIDFile removes {dir}/.moca/{name}.pid. Returns nil if the file does not exist.
func removePIDFile(dir, name string) error {
	path := filepath.Join(dir, ".moca", name+".pid")
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove pid file: %w", err)
	}
	return nil
}

// stopProcess sends SIGTERM to the process identified by {dir}/.moca/{name}.pid.
func stopProcess(dir, name string) error {
	pid, err := readPIDFile(dir, name)
	if err != nil {
		return output.NewCLIError(fmt.Sprintf("No %s PID file found", name)).
			WithErr(err).
			WithFix(fmt.Sprintf("Is the %s running? Start it with 'moca %s start --foreground'.", name, name))
	}
	if !process.IsRunning(pid) {
		_ = removePIDFile(dir, name)
		return output.NewCLIError(fmt.Sprintf("The %s process (PID %d) is not running", name, pid)).
			WithFix(fmt.Sprintf("The PID file was stale and has been removed. Start with 'moca %s start --foreground'.", name))
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("send SIGTERM to %s (PID %d): %w", name, pid, err)
	}
	_ = removePIDFile(dir, name)
	return nil
}

// processStatus reads the PID file and checks if the process is running.
// Returns the PID, whether it's running, and any error reading the PID file.
func processStatus(dir, name string) (int, bool, error) {
	pid, err := readPIDFile(dir, name)
	if err != nil {
		return 0, false, err
	}
	return pid, process.IsRunning(pid), nil
}

// gatherMigrations scans apps and converts manifest migrations to orm.AppMigration slice.
func gatherMigrations(appsDir string) ([]orm.AppMigration, error) {
	appInfos, err := apps.ScanApps(appsDir)
	if err != nil {
		return nil, err
	}

	var migrations []orm.AppMigration
	for _, ai := range appInfos {
		if ai.Manifest == nil {
			continue
		}
		for _, m := range ai.Manifest.Migrations {
			migrations = append(migrations, orm.AppMigration{
				AppName:   ai.Name,
				Version:   m.Version,
				UpSQL:     m.Up,
				DownSQL:   m.Down,
				DependsOn: m.DependsOn,
			})
		}
	}
	return migrations, nil
}

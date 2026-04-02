package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
	"gopkg.in/yaml.v3"

	"github.com/moca-framework/moca/internal/config"
	"github.com/moca-framework/moca/internal/lockfile"
	"github.com/moca-framework/moca/internal/output"
	"github.com/moca-framework/moca/pkg/orm"
	"github.com/spf13/cobra"
)

// NewInitCommand returns the "moca init" command.
func NewInitCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init [PATH]",
		Short: "Initialize a new Moca project",
		Long:  "Create a new Moca project with moca.yaml, directory structure, and initial configuration.",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runInit,
	}

	f := cmd.Flags()
	f.String("name", "", "Project name (default: directory name)")
	f.String("db-host", "localhost", "PostgreSQL host")
	f.Int("db-port", 5432, "PostgreSQL port")
	f.String("db-user", "postgres", "PostgreSQL user")
	f.String("db-password", "", "PostgreSQL password")
	f.String("redis-host", "localhost", "Redis host")
	f.Int("redis-port", 6379, "Redis port")
	f.Bool("kafka", true, "Enable Kafka integration")
	f.Bool("no-kafka", false, "Disable Kafka (Redis pub/sub fallback)")
	f.Bool("minimal", false, "Minimal setup (PostgreSQL + Redis only)")
	f.String("template", "standard", "Project template: standard, minimal, enterprise")
	f.Bool("skip-assets", false, "Skip building frontend assets")
	f.StringSlice("apps", nil, "Apps to pre-install")

	return cmd
}

func runInit(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)
	ctx := cmd.Context()

	// 1. Resolve target directory.
	targetDir, err := resolveInitDir(args)
	if err != nil {
		return err
	}

	// Check for existing project.
	if _, err := os.Stat(filepath.Join(targetDir, "moca.yaml")); err == nil {
		return output.NewCLIError("Project already exists").
			WithContext("Found moca.yaml in " + targetDir).
			WithFix("Use a different directory or remove the existing moca.yaml.")
	}

	// 2. Determine project name.
	projectName, _ := cmd.Flags().GetString("name")
	if projectName == "" {
		projectName = filepath.Base(targetDir)
	}

	// 3. Build config from flags.
	cfg := buildInitConfig(cmd, projectName)

	// 4. Create directory structure.
	s := w.NewSpinner("Creating project structure...")
	s.Start()
	if err := createProjectDirs(targetDir); err != nil {
		s.Stop("Failed")
		return output.NewCLIError("Failed to create project directories").WithErr(err)
	}
	s.Stop("Project structure created")

	// 5. Write moca.yaml.
	s = w.NewSpinner("Writing moca.yaml...")
	s.Start()
	if err := writeMocaYAML(targetDir, cfg); err != nil {
		s.Stop("Failed")
		return output.NewCLIError("Failed to write moca.yaml").WithErr(err)
	}
	s.Stop("moca.yaml written")

	// 6. Connect to PostgreSQL and create system schema.
	s = w.NewSpinner("Connecting to PostgreSQL...")
	s.Start()
	if err := initPostgres(ctx, cfg); err != nil {
		s.Stop("Failed")
		return err // already a CLIError
	}
	s.Stop("PostgreSQL connected, moca_system schema ready")

	// 7. Connect to Redis.
	s = w.NewSpinner("Connecting to Redis...")
	s.Start()
	if err := initRedis(ctx, cfg, w); err != nil {
		// Non-fatal: warn and continue.
		s.Stop("Redis unavailable (non-fatal)")
		w.PrintWarning(fmt.Sprintf("Redis connection failed: %v", err))
		w.PrintWarning("Redis is optional for development. Site creation will still work.")
	} else {
		s.Stop("Redis connected")
	}

	// 8. Register core app in moca_system.apps.
	s = w.NewSpinner("Registering core app...")
	s.Start()
	if err := registerCoreApp(ctx, cfg); err != nil {
		s.Stop("Failed")
		return output.NewCLIError("Failed to register core app").
			WithErr(err).
			WithCause(err.Error())
	}
	s.Stop("Core app registered")

	// 9. Write moca.lock.
	s = w.NewSpinner("Generating moca.lock...")
	s.Start()
	if err := writeMocaLock(targetDir); err != nil {
		s.Stop("Failed")
		return output.NewCLIError("Failed to write moca.lock").WithErr(err)
	}
	s.Stop("moca.lock generated")

	// 10. Git init.
	s = w.NewSpinner("Initializing git repository...")
	s.Start()
	if err := initGit(targetDir); err != nil {
		s.Stop("Skipped (git not available)")
	} else {
		s.Stop("Git repository initialized")
	}

	// 11. Print summary.
	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"project":  projectName,
			"path":     targetDir,
			"db_host":  cfg.Infrastructure.Database.Host,
			"db_port":  cfg.Infrastructure.Database.Port,
			"redis":    fmt.Sprintf("%s:%d", cfg.Infrastructure.Redis.Host, cfg.Infrastructure.Redis.Port),
			"template": cfg.Project.Name,
		})
	}

	w.Print("")
	w.PrintSuccess(fmt.Sprintf("Project %q initialized at %s", projectName, targetDir))
	w.Print("")
	w.Print("Next steps:")
	w.Print("  cd %s", targetDir)
	w.Print("  moca site create <site-name> --admin-password <password>")
	w.Print("")

	return nil
}

// resolveInitDir determines the target directory for moca init.
func resolveInitDir(args []string) (string, error) {
	if len(args) > 0 {
		absPath, err := filepath.Abs(args[0])
		if err != nil {
			return "", fmt.Errorf("resolve path: %w", err)
		}
		return absPath, nil
	}
	return os.Getwd()
}

// buildInitConfig creates a ProjectConfig from init command flags.
func buildInitConfig(cmd *cobra.Command, name string) *config.ProjectConfig {
	dbHost, _ := cmd.Flags().GetString("db-host")
	dbPort, _ := cmd.Flags().GetInt("db-port")
	dbUser, _ := cmd.Flags().GetString("db-user")
	dbPassword, _ := cmd.Flags().GetString("db-password")
	redisHost, _ := cmd.Flags().GetString("redis-host")
	redisPort, _ := cmd.Flags().GetInt("redis-port")
	noKafka, _ := cmd.Flags().GetBool("no-kafka")

	kafkaEnabled := !noKafka
	return &config.ProjectConfig{
		Moca: Version,
		Project: config.ProjectInfo{
			Name:    name,
			Version: "0.1.0",
		},
		Infrastructure: config.InfrastructureConfig{
			Database: config.DatabaseConfig{
				Driver:   "postgres",
				Host:     dbHost,
				Port:     dbPort,
				User:     dbUser,
				Password: dbPassword,
				SystemDB: "postgres",
				PoolSize: 10,
			},
			Redis: config.RedisConfig{
				Host:      redisHost,
				Port:      redisPort,
				DbCache:   0,
				DbQueue:   1,
				DbSession: 2,
				DbPubSub:  3,
			},
			Kafka: config.KafkaConfig{
				Enabled: &kafkaEnabled,
				Brokers: []string{"localhost:9092"},
			},
			Search: config.SearchConfig{
				Engine: "meilisearch",
				Host:   "localhost",
				Port:   7700,
			},
			Storage: config.StorageConfig{
				Driver: "local",
			},
		},
		Apps: map[string]config.AppConfig{
			"core": {
				Source:  "builtin",
				Version: "*",
			},
		},
		Development: config.DevelopmentConfig{
			Port:       8000,
			Workers:    1,
			AutoReload: true,
		},
		Scheduler: config.SchedulerConfig{
			Enabled:      true,
			TickInterval: "60s",
		},
	}
}

// createProjectDirs creates the standard project directory structure.
func createProjectDirs(targetDir string) error {
	dirs := []string{
		"apps",
		".moca",
		"sites",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(targetDir, d), 0o755); err != nil {
			return err
		}
	}
	return nil
}

// writeMocaYAML marshals the config and writes it to moca.yaml.
func writeMocaYAML(targetDir string, cfg *config.ProjectConfig) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	content := "# Moca Framework Configuration\n# Generated by 'moca init'\n\n" + string(data)
	return os.WriteFile(filepath.Join(targetDir, "moca.yaml"), []byte(content), 0o644)
}

// initPostgres creates a temporary PG pool and ensures the moca_system schema exists.
func initPostgres(ctx context.Context, cfg *config.ProjectConfig) error {
	dsn := buildInitDSN(cfg.Infrastructure.Database)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return output.NewCLIError("Cannot connect to PostgreSQL").
			WithErr(err).
			WithCause(err.Error()).
			WithFix(fmt.Sprintf("Ensure PostgreSQL is running on %s:%d. Check user/password.",
				cfg.Infrastructure.Database.Host, cfg.Infrastructure.Database.Port))
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return output.NewCLIError("Cannot connect to PostgreSQL").
			WithErr(err).
			WithCause(err.Error()).
			WithFix(fmt.Sprintf("Ensure PostgreSQL is running on %s:%d. Check user/password.",
				cfg.Infrastructure.Database.Host, cfg.Infrastructure.Database.Port))
	}

	if err := orm.EnsureSystemSchema(ctx, pool, "moca_system"); err != nil {
		return output.NewCLIError("Failed to create system schema").
			WithErr(err).
			WithCause(err.Error())
	}
	return nil
}

// initRedis verifies Redis connectivity. Returns error on failure (non-fatal in caller).
func initRedis(ctx context.Context, cfg *config.ProjectConfig, _ *output.Writer) error {
	client := goredis.NewClient(&goredis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Infrastructure.Redis.Host, cfg.Infrastructure.Redis.Port),
		Password: cfg.Infrastructure.Redis.Password,
		DB:       0,
	})
	defer client.Close() //nolint:errcheck
	return client.Ping(ctx).Err()
}

// registerCoreApp inserts the core app record into moca_system.apps.
func registerCoreApp(ctx context.Context, cfg *config.ProjectConfig) error {
	dsn := buildInitDSN(cfg.Infrastructure.Database)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	manifest, _ := json.Marshal(map[string]any{
		"name":    "core",
		"version": "0.1.0",
		"title":   "Moca Core",
	})

	_, err = pool.Exec(ctx, `
		INSERT INTO moca_system.apps (name, version, title, description, publisher, manifest)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (name) DO NOTHING`,
		"core", "0.1.0", "Moca Core", "Core framework app", "moca-framework", manifest,
	)
	return err
}

// writeMocaLock writes the initial lockfile in YAML format.
func writeMocaLock(targetDir string) error {
	lf := &lockfile.Lockfile{
		MocaVersion: "0.1.0",
		Apps: map[string]lockfile.AppLock{
			"core": {
				Version: "0.1.0",
				Source:  "builtin",
			},
		},
	}
	return lockfile.Write(filepath.Join(targetDir, "moca.lock"), lf)
}

// initGit runs git init in the target directory.
func initGit(targetDir string) error {
	cmd := exec.Command("git", "init", targetDir)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

// buildInitDSN constructs a PostgreSQL DSN from DatabaseConfig.
func buildInitDSN(cfg config.DatabaseConfig) string {
	systemDB := cfg.SystemDB
	if systemDB == "" {
		systemDB = "postgres"
	}
	u := &url.URL{
		Scheme: "postgres",
		Host:   fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Path:   "/" + systemDB,
	}
	if cfg.User != "" || cfg.Password != "" {
		u.User = url.UserPassword(cfg.User, cfg.Password)
	}
	q := u.Query()
	q.Set("sslmode", "disable")
	u.RawQuery = q.Encode()
	return u.String()
}

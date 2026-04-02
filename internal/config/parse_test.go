package config_test

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/osama1998H/moca/internal/config"
)

// TestParse_ValidFull parses the complete fixture and checks that all fields
// round-trip correctly.
func TestParse_ValidFull(t *testing.T) {
	cfg, err := config.ParseFile("testdata/valid_full.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// project block
	if cfg.Project.Name != "my-erp" {
		t.Errorf("project.name = %q, want %q", cfg.Project.Name, "my-erp")
	}
	if cfg.Project.Version != "1.0.0" {
		t.Errorf("project.version = %q, want %q", cfg.Project.Version, "1.0.0")
	}

	// moca constraint
	if cfg.Moca != ">=1.0.0, <2.0.0" {
		t.Errorf("moca = %q, want %q", cfg.Moca, ">=1.0.0, <2.0.0")
	}

	// apps map
	if len(cfg.Apps) != 4 {
		t.Errorf("len(apps) = %d, want 4", len(cfg.Apps))
	}
	core, ok := cfg.Apps["core"]
	if !ok {
		t.Fatal("apps.core not found")
	}
	if core.Source != "builtin" {
		t.Errorf("apps.core.source = %q, want %q", core.Source, "builtin")
	}

	crm, ok := cfg.Apps["crm"]
	if !ok {
		t.Fatal("apps.crm not found")
	}
	if crm.Version != "~1.2.0" {
		t.Errorf("apps.crm.version = %q, want %q", crm.Version, "~1.2.0")
	}
	if crm.Branch != "main" {
		t.Errorf("apps.crm.branch = %q, want %q", crm.Branch, "main")
	}
	if crm.Ref != "a1b2c3d" {
		t.Errorf("apps.crm.ref = %q, want %q", crm.Ref, "a1b2c3d")
	}

	// infrastructure.database
	db := cfg.Infrastructure.Database
	if db.Host != "localhost" {
		t.Errorf("database.host = %q, want %q", db.Host, "localhost")
	}
	if db.Port != 5432 {
		t.Errorf("database.port = %d, want 5432", db.Port)
	}
	if db.Driver != "postgres" {
		t.Errorf("database.driver = %q, want %q", db.Driver, "postgres")
	}
	if db.SystemDB != "moca_system" {
		t.Errorf("database.system_db = %q, want %q", db.SystemDB, "moca_system")
	}
	if db.PoolSize != 25 {
		t.Errorf("database.pool_size = %d, want 25", db.PoolSize)
	}

	// infrastructure.redis
	redis := cfg.Infrastructure.Redis
	if redis.Host != "localhost" {
		t.Errorf("redis.host = %q, want %q", redis.Host, "localhost")
	}
	if redis.Port != 6379 {
		t.Errorf("redis.port = %d, want 6379", redis.Port)
	}
	if redis.DbQueue != 1 {
		t.Errorf("redis.db_queue = %d, want 1", redis.DbQueue)
	}
	if redis.DbSession != 2 {
		t.Errorf("redis.db_session = %d, want 2", redis.DbSession)
	}
	if redis.DbPubSub != 3 {
		t.Errorf("redis.db_pubsub = %d, want 3", redis.DbPubSub)
	}

	// infrastructure.kafka
	kafka := cfg.Infrastructure.Kafka
	if kafka.Enabled == nil {
		t.Fatal("kafka.enabled is nil, want true")
	}
	if !*kafka.Enabled {
		t.Error("kafka.enabled = false, want true")
	}
	if len(kafka.Brokers) != 1 || kafka.Brokers[0] != "localhost:9092" {
		t.Errorf("kafka.brokers = %v, want [localhost:9092]", kafka.Brokers)
	}

	// infrastructure.search
	search := cfg.Infrastructure.Search
	if search.Engine != "meilisearch" {
		t.Errorf("search.engine = %q, want %q", search.Engine, "meilisearch")
	}
	if search.Port != 7700 {
		t.Errorf("search.port = %d, want 7700", search.Port)
	}
	if search.APIKey != "supersecretkey" {
		t.Errorf("search.api_key = %q, want %q", search.APIKey, "supersecretkey")
	}

	// infrastructure.storage
	storage := cfg.Infrastructure.Storage
	if storage.Driver != "s3" {
		t.Errorf("storage.driver = %q, want %q", storage.Driver, "s3")
	}
	if storage.Bucket != "moca-files" {
		t.Errorf("storage.bucket = %q, want %q", storage.Bucket, "moca-files")
	}

	// development
	dev := cfg.Development
	if dev.Port != 8000 {
		t.Errorf("development.port = %d, want 8000", dev.Port)
	}
	if !dev.AutoReload {
		t.Error("development.auto_reload = false, want true")
	}
	if dev.DeskPort != 3000 {
		t.Errorf("development.desk_port = %d, want 3000", dev.DeskPort)
	}

	// production
	prod := cfg.Production
	if prod.Port != 443 {
		t.Errorf("production.port = %d, want 443", prod.Port)
	}
	if prod.LogLevel != "warn" {
		t.Errorf("production.log_level = %q, want %q", prod.LogLevel, "warn")
	}
	if prod.TLS.Provider != "acme" {
		t.Errorf("production.tls.provider = %q, want %q", prod.TLS.Provider, "acme")
	}
	if prod.TLS.Email != "admin@example.com" {
		t.Errorf("production.tls.email = %q, want %q", prod.TLS.Email, "admin@example.com")
	}
	if prod.Proxy.Engine != "caddy" {
		t.Errorf("production.proxy.engine = %q, want %q", prod.Proxy.Engine, "caddy")
	}

	// scheduler
	sched := cfg.Scheduler
	if !sched.Enabled {
		t.Error("scheduler.enabled = false, want true")
	}
	if sched.TickInterval != "60s" {
		t.Errorf("scheduler.tick_interval = %q, want %q", sched.TickInterval, "60s")
	}

	// backup
	bkp := cfg.Backup
	if bkp.Schedule != "0 2 * * *" {
		t.Errorf("backup.schedule = %q, want %q", bkp.Schedule, "0 2 * * *")
	}
	if bkp.Retention.Daily != 7 {
		t.Errorf("backup.retention.daily = %d, want 7", bkp.Retention.Daily)
	}
	if bkp.Destination.Bucket != "moca-backups" {
		t.Errorf("backup.destination.bucket = %q, want %q", bkp.Destination.Bucket, "moca-backups")
	}
}

// TestParse_ValidMinimal parses the minimal fixture and confirms required fields
// are set while optional fields are zero-valued.
func TestParse_ValidMinimal(t *testing.T) {
	cfg, err := config.ParseFile("testdata/valid_minimal.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Project.Name != "minimal-project" {
		t.Errorf("project.name = %q, want %q", cfg.Project.Name, "minimal-project")
	}
	if cfg.Infrastructure.Database.Port != 5432 {
		t.Errorf("database.port = %d, want 5432", cfg.Infrastructure.Database.Port)
	}
	if cfg.Infrastructure.Redis.Port != 6379 {
		t.Errorf("redis.port = %d, want 6379", cfg.Infrastructure.Redis.Port)
	}

	// Optional fields should be zero-valued.
	if cfg.Development.Port != 0 {
		t.Errorf("development.port = %d, want 0 (not set)", cfg.Development.Port)
	}
	if cfg.Scheduler.TickInterval != "" {
		t.Errorf("scheduler.tick_interval = %q, want empty (not set)", cfg.Scheduler.TickInterval)
	}
	if cfg.Infrastructure.Kafka.Enabled != nil {
		t.Error("kafka.enabled should be nil (not set)")
	}
}

// TestParse_Malformed confirms that a syntactically broken YAML file returns a
// *ConfigError.
func TestParse_Malformed(t *testing.T) {
	_, err := config.ParseFile("testdata/malformed.yaml")
	if err == nil {
		t.Fatal("expected error for malformed YAML, got nil")
	}
	var ce *config.ConfigError
	if !errors.As(err, &ce) {
		t.Errorf("expected *ConfigError, got %T: %v", err, err)
	}
}

// TestParse_NonExistentFile confirms that ParseFile returns a *ConfigError when
// the file does not exist.
func TestParse_NonExistentFile(t *testing.T) {
	_, err := config.ParseFile("testdata/does_not_exist.yaml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	var ce *config.ConfigError
	if !errors.As(err, &ce) {
		t.Errorf("expected *ConfigError, got %T: %v", err, err)
	}
}

// TestParse_FromReader confirms that Parse(io.Reader) works without a file.
func TestParse_FromReader(t *testing.T) {
	yaml := `
project:
  name: reader-project
  version: "2.0.0"
moca: "^2.0.0"
apps:
  core:
    source: builtin
infrastructure:
  database:
    host: db.local
    port: 5432
  redis:
    host: redis.local
    port: 6379
`
	cfg, err := config.Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Project.Name != "reader-project" {
		t.Errorf("project.name = %q, want %q", cfg.Project.Name, "reader-project")
	}
}

// TestParse_FromReader_MalformedYAML confirms that Parse returns an error for
// broken YAML passed as a reader.
func TestParse_FromReader_MalformedYAML(t *testing.T) {
	_, err := config.Parse(strings.NewReader(":\n  - bad: [unclosed"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestParse_FromReader_ReadError confirms that Parse returns a *ConfigError when
// the reader returns an error during ReadAll.
func TestParse_FromReader_ReadError(t *testing.T) {
	_, err := config.Parse(&errorReader{})
	if err == nil {
		t.Fatal("expected error from failing reader, got nil")
	}
	var ce *config.ConfigError
	if !errors.As(err, &ce) {
		t.Errorf("expected *ConfigError, got %T: %v", err, err)
	}
}

// errorReader is an io.Reader that always returns an error.
type errorReader struct{}

func (e *errorReader) Read(_ []byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

// TestExpandEnvVars_NoPatterns confirms that input with no ${...} patterns is
// returned unchanged.
func TestExpandEnvVars_NoPatterns(t *testing.T) {
	input := []byte("project:\n  name: simple\n")
	got, err := config.ExpandEnvVars(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != string(input) {
		t.Errorf("ExpandEnvVars changed input without patterns:\ngot:  %s\nwant: %s", got, input)
	}
}

// TestExpandEnvVars_AllPresent confirms substitution when all referenced vars
// are set.
func TestExpandEnvVars_AllPresent(t *testing.T) {
	t.Setenv("MY_KEY", "hello")
	t.Setenv("OTHER", "world")

	input := []byte("key: ${MY_KEY}\nother: ${OTHER}\n")
	got, err := config.ExpandEnvVars(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "key: hello\nother: world\n"
	if string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestExpandEnvVars_MissingVars confirms that an EnvExpandError is returned
// listing every missing variable.
func TestExpandEnvVars_MissingVars(t *testing.T) {
	// Ensure these are NOT set in the test environment.
	t.Setenv("PRESENT_VAR", "ok")

	input := []byte("a: ${MISSING_A}\nb: ${MISSING_B}\nc: ${PRESENT_VAR}\n")
	_, err := config.ExpandEnvVars(input)
	if err == nil {
		t.Fatal("expected EnvExpandError, got nil")
	}

	var ee *config.EnvExpandError
	if !errors.As(err, &ee) {
		t.Fatalf("expected *EnvExpandError, got %T: %v", err, err)
	}
	if len(ee.Missing) != 2 {
		t.Errorf("missing count = %d, want 2: %v", len(ee.Missing), ee.Missing)
	}
	// Missing list is sorted.
	if ee.Missing[0] != "MISSING_A" || ee.Missing[1] != "MISSING_B" {
		t.Errorf("missing vars = %v, want [MISSING_A MISSING_B]", ee.Missing)
	}
}

// TestExpandEnvVars_DuplicateVar confirms that a variable referenced multiple
// times is reported only once in the missing list.
func TestExpandEnvVars_DuplicateVar(t *testing.T) {
	input := []byte("a: ${DUP_VAR}\nb: ${DUP_VAR}\n")
	_, err := config.ExpandEnvVars(input)
	if err == nil {
		t.Fatal("expected EnvExpandError, got nil")
	}
	var ee *config.EnvExpandError
	if !errors.As(err, &ee) {
		t.Fatalf("expected *EnvExpandError, got %T", err)
	}
	if len(ee.Missing) != 1 {
		t.Errorf("expected 1 missing entry, got %d: %v", len(ee.Missing), ee.Missing)
	}
}

// TestParse_WithEnvVars parses the env-var fixture after setting the required
// environment variables and confirms all substitutions were applied.
func TestParse_WithEnvVars(t *testing.T) {
	t.Setenv("MOCA_MEILI_KEY", "meili-test-key")
	t.Setenv("MOCA_S3_ACCESS_KEY", "s3-access-test")
	t.Setenv("MOCA_S3_SECRET_KEY", "s3-secret-test")
	t.Setenv("MOCA_BACKUP_KEY", "backup-key-test")

	cfg, err := config.ParseFile("testdata/with_env_vars.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Infrastructure.Search.APIKey != "meili-test-key" {
		t.Errorf("search.api_key = %q, want %q", cfg.Infrastructure.Search.APIKey, "meili-test-key")
	}
	if cfg.Infrastructure.Storage.AccessKey != "s3-access-test" {
		t.Errorf("storage.access_key = %q, want %q", cfg.Infrastructure.Storage.AccessKey, "s3-access-test")
	}
	if cfg.Infrastructure.Storage.SecretKey != "s3-secret-test" {
		t.Errorf("storage.secret_key = %q, want %q", cfg.Infrastructure.Storage.SecretKey, "s3-secret-test")
	}
	if cfg.Backup.EncryptionKey != "backup-key-test" {
		t.Errorf("backup.encryption_key = %q, want %q", cfg.Backup.EncryptionKey, "backup-key-test")
	}
}

// TestParse_WithEnvVars_MissingVars confirms that parsing the env-var fixture
// without setting the required env vars returns a *ConfigError wrapping an
// *EnvExpandError.
func TestParse_WithEnvVars_MissingVars(t *testing.T) {
	// Do NOT set MOCA_MEILI_KEY, MOCA_S3_ACCESS_KEY, etc.

	_, err := config.ParseFile("testdata/with_env_vars.yaml")
	if err == nil {
		t.Fatal("expected error for missing env vars, got nil")
	}
	// ParseFile wraps EnvExpandError in ConfigError.
	var ce *config.ConfigError
	if !errors.As(err, &ce) {
		t.Errorf("expected *ConfigError, got %T: %v", err, err)
	}
}

package config

// ProjectConfig is the top-level struct for a parsed moca.yaml file.
// It maps 1:1 to the moca.yaml schema defined in MOCA_CLI_SYSTEM_DESIGN.md §3.1.
type ProjectConfig struct {
	Apps           map[string]AppConfig `yaml:"apps"`
	Notification   NotificationConfig   `yaml:"notification,omitempty"`
	Staging        StagingConfig        `yaml:"staging"`
	Project        ProjectInfo          `yaml:"project"`
	Moca           string               `yaml:"moca"`
	Production     ProductionConfig     `yaml:"production"`
	Scheduler      SchedulerConfig      `yaml:"scheduler"`
	Backup         BackupConfig         `yaml:"backup"`
	Development    DevelopmentConfig    `yaml:"development"`
	Infrastructure InfrastructureConfig `yaml:"infrastructure"`
}

// ProjectInfo holds the project-level metadata.
type ProjectInfo struct {
	// Name is the project identifier (e.g. "my-erp"). Required.
	Name string `yaml:"name"`
	// Version is the project's own semantic version (e.g. "1.0.0"). Required.
	Version string `yaml:"version"`
}

// AppConfig describes a single installed Moca application.
type AppConfig struct {
	// Source is one of: "builtin", a GitHub module path (e.g. "github.com/moca-apps/crm"),
	// or a local relative path (e.g. "./local-apps/custom-hr").
	Source string `yaml:"source"`

	// Version is a semver constraint (e.g. "~1.2.0", "^2.0.0", "*").
	Version string `yaml:"version"`

	// Branch pins the app to a specific VCS branch. Optional.
	Branch string `yaml:"branch,omitempty"`

	// Ref pins the app to an exact commit hash. Optional.
	Ref string `yaml:"ref,omitempty"`
}

// InfrastructureConfig aggregates all external service configurations.
type InfrastructureConfig struct {
	Storage  StorageConfig  `yaml:"storage"`
	Search   SearchConfig   `yaml:"search"`
	Database DatabaseConfig `yaml:"database"`
	Kafka    KafkaConfig    `yaml:"kafka"`
	Redis    RedisConfig    `yaml:"redis"`
}

// DatabaseConfig holds PostgreSQL connection settings.
type DatabaseConfig struct {
	Driver   string `yaml:"driver"`
	Host     string `yaml:"host"`
	SystemDB string `yaml:"system_db"`
	// User is the PostgreSQL username. Supports ${DB_USER} env expansion.
	User string `yaml:"user,omitempty"`
	// Password is the PostgreSQL password. Supports ${DB_PASSWORD} env expansion.
	Password string `yaml:"password,omitempty"`
	Port     int    `yaml:"port"`
	PoolSize int    `yaml:"pool_size,omitempty"`
}

// RedisConfig holds Redis connection and database-index settings.
type RedisConfig struct {
	// Host is the Redis server hostname. Required.
	Host string `yaml:"host"`
	// Password is the Redis AUTH password. Supports ${REDIS_PASSWORD} env expansion.
	// Defaults to empty string (no authentication) for development.
	Password string `yaml:"password,omitempty"`
	// Port is the Redis server TCP port. Required; valid range 1–65535.
	Port int `yaml:"port"`
	// DbCache is the Redis database index used for application cache.
	DbCache int `yaml:"db_cache"`
	// DbQueue is the Redis database index used for the job queue (Redis Streams).
	DbQueue int `yaml:"db_queue"`
	// DbSession is the Redis database index used for HTTP session storage.
	DbSession int `yaml:"db_session"`
	// DbPubSub is the Redis database index used for pub/sub messaging.
	DbPubSub int `yaml:"db_pubsub"`
}

// KafkaConfig holds Apache Kafka connection settings.
// Enabled uses a pointer so that an explicit "false" can be distinguished from
// an absent field (nil pointer = not configured, non-nil = explicitly set).
type KafkaConfig struct {
	// Enabled controls whether the Kafka integration is active. Pointer to distinguish
	// explicit false from absent.
	Enabled *bool `yaml:"enabled"`
	// Brokers is the list of Kafka broker addresses (e.g. "kafka:9092").
	Brokers []string `yaml:"brokers,omitempty"`
}

// SearchConfig holds Meilisearch connection settings.
type SearchConfig struct {
	Engine string `yaml:"engine"`
	Host   string `yaml:"host"`
	APIKey string `yaml:"api_key,omitempty"`
	Port   int    `yaml:"port"`
}

// StorageConfig holds object storage settings (S3-compatible or local).
type StorageConfig struct {
	// Driver is "s3" or "local".
	Driver string `yaml:"driver"`
	// Endpoint is the S3-compatible endpoint URL (e.g. "http://minio:9000").
	Endpoint string `yaml:"endpoint,omitempty"`
	// Bucket is the storage bucket name.
	Bucket string `yaml:"bucket,omitempty"`
	// AccessKey is the S3 access key ID.
	// Supports ${ENV_VAR} interpolation.
	AccessKey string `yaml:"access_key,omitempty"`
	// SecretKey is the S3 secret access key.
	// Supports ${ENV_VAR} interpolation.
	SecretKey string `yaml:"secret_key,omitempty"`
}

// DevelopmentConfig holds settings for the local development environment.
type DevelopmentConfig struct {
	LogDir        string `yaml:"log_dir,omitempty"`
	Port          int    `yaml:"port,omitempty"`
	Workers       int    `yaml:"workers,omitempty"`
	DeskPort      int    `yaml:"desk_port,omitempty"`
	AutoReload    bool   `yaml:"auto_reload,omitempty"`
	DeskDevServer bool   `yaml:"desk_dev_server,omitempty"`
}

// ProductionConfig holds settings for the production environment.
type ProductionConfig struct {
	TLS            TLSConfig   `yaml:"tls"`
	Workers        string      `yaml:"workers,omitempty"`
	Proxy          ProxyConfig `yaml:"proxy"`
	ProcessManager string      `yaml:"process_manager,omitempty"`
	LogLevel       string      `yaml:"log_level,omitempty"`
	Port           int         `yaml:"port,omitempty"`
}

// TLSConfig holds TLS certificate provisioning settings.
type TLSConfig struct {
	// Provider is "acme" (Let's Encrypt) or "manual".
	Provider string `yaml:"provider,omitempty"`
	// Email is the contact address for ACME certificate registration.
	Email string `yaml:"email,omitempty"`
}

// ProxyConfig holds reverse-proxy configuration.
type ProxyConfig struct {
	// Engine is "caddy" or "nginx".
	Engine string `yaml:"engine,omitempty"`
}

// StagingConfig is an optional environment section that inherits from another
// environment (typically "production") and overrides only the fields that differ.
//
// All overridable fields are pointers: a nil pointer means "inherit from parent";
// a non-nil pointer means "override with this value". This enables correct merging
// in config.ResolveInheritance (MS-01-T3) without ambiguity about zero values.
type StagingConfig struct {
	Port           *int         `yaml:"port,omitempty"`
	Workers        *string      `yaml:"workers,omitempty"`
	LogLevel       *string      `yaml:"log_level,omitempty"`
	ProcessManager *string      `yaml:"process_manager,omitempty"`
	TLS            *TLSConfig   `yaml:"tls,omitempty"`
	Proxy          *ProxyConfig `yaml:"proxy,omitempty"`
	Inherits       string       `yaml:"inherits,omitempty"`
}

// SchedulerConfig holds configuration for the moca-scheduler binary.
type SchedulerConfig struct {
	TickInterval string `yaml:"tick_interval,omitempty"`
	Enabled      bool   `yaml:"enabled"`
}

// BackupConfig holds database backup scheduling and storage settings.
type BackupConfig struct {
	Destination   BackupDestination `yaml:"destination"`
	Schedule      string            `yaml:"schedule,omitempty"`
	EncryptionKey string            `yaml:"encryption_key,omitempty"`
	Retention     RetentionConfig   `yaml:"retention"`
	Encrypt       bool              `yaml:"encrypt,omitempty"`
}

// RetentionConfig controls how many backup copies to keep per time window.
type RetentionConfig struct {
	// Daily is the number of daily backup files to retain.
	Daily int `yaml:"daily,omitempty"`
	// Weekly is the number of weekly backup files to retain.
	Weekly int `yaml:"weekly,omitempty"`
	// Monthly is the number of monthly backup files to retain.
	Monthly int `yaml:"monthly,omitempty"`
}

// BackupDestination describes the remote storage target for backup files.
type BackupDestination struct {
	// Driver is the storage backend, e.g. "s3".
	Driver string `yaml:"driver,omitempty"`
	// Bucket is the S3 bucket name.
	Bucket string `yaml:"bucket,omitempty"`
	// Prefix is the key prefix for backup objects.
	// Supports ${ENV_VAR} interpolation.
	Prefix string `yaml:"prefix,omitempty"`
}

// NotificationConfig holds notification delivery settings.
type NotificationConfig struct {
	Email EmailConfig `yaml:"email,omitempty"`
}

// EmailConfig holds email provider settings.
type EmailConfig struct {
	SES      SESConfig  `yaml:"ses,omitempty"`
	Provider string     `yaml:"provider,omitempty"`
	SMTP     SMTPConfig `yaml:"smtp,omitempty"`
}

// SMTPConfig holds SMTP connection settings for email delivery.
type SMTPConfig struct {
	// Host is the SMTP server hostname.
	Host string `yaml:"host"`
	// User is the SMTP authentication username.
	// Supports ${ENV_VAR} interpolation.
	User string `yaml:"user,omitempty"`
	// Password is the SMTP authentication password.
	// Supports ${ENV_VAR} interpolation.
	Password string `yaml:"password,omitempty"`
	// FromName is the display name in the From header.
	FromName string `yaml:"from_name,omitempty"`
	// FromAddr is the email address in the From header.
	FromAddr string `yaml:"from_addr"`
	// Port is the SMTP server port. Defaults to 587 if unset.
	Port int `yaml:"port,omitempty"`
	// UseTLS enables STARTTLS negotiation.
	UseTLS bool `yaml:"use_tls,omitempty"`
}

// SESConfig holds AWS SES settings for email delivery.
type SESConfig struct {
	// Region is the AWS region (e.g. "us-east-1").
	Region string `yaml:"region"`
	// FromAddr is the verified sender email address.
	FromAddr string `yaml:"from_addr"`
}

package generate

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/osama1998H/moca/internal/config"
)

// EnvOptions configures environment file generation.
type EnvOptions struct {
	OutputPath string
	Format     string // "dotenv", "docker", "systemd"
}

type envData struct {
	Header          string
	ProjectName     string
	ProjectVersion  string
	Workers         string
	LogLevel        string
	DBHost          string
	DBUser          string
	DBPassword      string
	DBName          string
	RedisHost       string
	RedisPassword   string
	KafkaBrokers    string
	SearchEngine    string
	SearchHost      string
	SearchAPIKey    string
	StorageDriver   string
	StorageEndpoint string
	StorageBucket   string
	StorageAccessKey string
	StorageSecretKey string
	TLSProvider     string
	TLSEmail        string
	SchedulerTick   string
	Port            int
	DBPort          int
	DBPoolSize      int
	RedisPort       int
	RedisDbCache    int
	RedisDbQueue    int
	RedisDbSession  int
	RedisDbPubSub   int
	SearchPort      int
	KafkaEnabled    bool
	SearchEnabled   bool
	StorageEnabled  bool
	SchedulerEnabled bool
}

// GenerateEnv writes an environment file in the specified format.
func GenerateEnv(cfg *config.ProjectConfig, projectRoot string, opts EnvOptions) ([]string, error) {
	if opts.OutputPath == "" {
		opts.OutputPath = filepath.Join(projectRoot, ".env")
	}
	if opts.Format == "" {
		opts.Format = "dotenv"
	}

	dbPort := cfg.Infrastructure.Database.Port
	if dbPort == 0 {
		dbPort = 5432
	}
	redisPort := cfg.Infrastructure.Redis.Port
	if redisPort == 0 {
		redisPort = 6379
	}
	searchPort := cfg.Infrastructure.Search.Port
	if searchPort == 0 {
		searchPort = 7700
	}

	dbName := cfg.Infrastructure.Database.SystemDB
	if dbName == "" {
		dbName = "moca"
	}

	data := envData{
		Header:          fileHeader(),
		ProjectName:     cfg.Project.Name,
		ProjectVersion:  cfg.Project.Version,
		Port:            defaultPort(cfg),
		Workers:         defaultWorkers(cfg),
		LogLevel:        defaultLogLevel(cfg),
		DBHost:          cfg.Infrastructure.Database.Host,
		DBPort:          dbPort,
		DBUser:          cfg.Infrastructure.Database.User,
		DBPassword:      cfg.Infrastructure.Database.Password,
		DBName:          dbName,
		DBPoolSize:      cfg.Infrastructure.Database.PoolSize,
		RedisHost:       cfg.Infrastructure.Redis.Host,
		RedisPort:       redisPort,
		RedisPassword:   cfg.Infrastructure.Redis.Password,
		RedisDbCache:    cfg.Infrastructure.Redis.DbCache,
		RedisDbQueue:    cfg.Infrastructure.Redis.DbQueue,
		RedisDbSession:  cfg.Infrastructure.Redis.DbSession,
		RedisDbPubSub:   cfg.Infrastructure.Redis.DbPubSub,
		KafkaEnabled:    KafkaEnabled(cfg.Infrastructure.Kafka),
		KafkaBrokers:    strings.Join(cfg.Infrastructure.Kafka.Brokers, ","),
		SearchEnabled:   cfg.Infrastructure.Search.Host != "",
		SearchEngine:    cfg.Infrastructure.Search.Engine,
		SearchHost:      cfg.Infrastructure.Search.Host,
		SearchPort:      searchPort,
		SearchAPIKey:    cfg.Infrastructure.Search.APIKey,
		StorageEnabled:  cfg.Infrastructure.Storage.Driver != "",
		StorageDriver:   cfg.Infrastructure.Storage.Driver,
		StorageEndpoint: cfg.Infrastructure.Storage.Endpoint,
		StorageBucket:   cfg.Infrastructure.Storage.Bucket,
		StorageAccessKey: cfg.Infrastructure.Storage.AccessKey,
		StorageSecretKey: cfg.Infrastructure.Storage.SecretKey,
		TLSProvider:     cfg.Production.TLS.Provider,
		TLSEmail:        cfg.Production.TLS.Email,
		SchedulerEnabled: cfg.Scheduler.Enabled,
		SchedulerTick:   cfg.Scheduler.TickInterval,
	}

	var tmpl string
	switch opts.Format {
	case "docker":
		tmpl = envDockerTmpl
	case "systemd":
		tmpl = envSystemdTmpl
	default:
		tmpl = envDotenvTmpl
	}

	if err := renderToFile(opts.OutputPath, tmpl, data); err != nil {
		return nil, fmt.Errorf("generate env (%s): %w", opts.Format, err)
	}
	return []string{opts.OutputPath}, nil
}

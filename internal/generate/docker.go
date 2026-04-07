package generate

import (
	"path/filepath"
	"strings"

	"github.com/osama1998H/moca/internal/config"
)

// DockerOptions configures Docker Compose file generation.
type DockerOptions struct {
	OutputDir string
	Profile   string
	Include   []string
}

type dockerData struct {
	Header        string
	ProjectName   string
	DBUser        string
	DBPassword    string
	DBName        string
	RedisPassword string
	SearchAPIKey  string
	LogLevel      string
	Port          int
	DBPort        int
	RedisPort     int
	SearchPort    int
	IncludeKafka  bool
	IncludeMeili  bool
	IncludeMinio  bool
}

// GenerateDocker writes Docker Compose files, Dockerfile, and .dockerignore.
func GenerateDocker(cfg *config.ProjectConfig, projectRoot string, opts DockerOptions) ([]string, error) {
	if opts.OutputDir == "" {
		opts.OutputDir = filepath.Join(projectRoot, "config", "docker")
	}
	if opts.Profile == "" {
		opts.Profile = "development"
	}

	includeSet := make(map[string]bool)
	for _, s := range opts.Include {
		includeSet[strings.ToLower(s)] = true
	}

	// Auto-include based on config.
	if KafkaEnabled(cfg.Infrastructure.Kafka) {
		includeSet["kafka"] = true
	}
	if cfg.Infrastructure.Search.Host != "" {
		includeSet["meilisearch"] = true
	}
	if cfg.Infrastructure.Storage.Driver == "s3" {
		includeSet["minio"] = true
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

	data := dockerData{
		Header:        fileHeader(),
		ProjectName:   cfg.Project.Name,
		Port:          defaultPort(cfg),
		DBUser:        cfg.Infrastructure.Database.User,
		DBPassword:    cfg.Infrastructure.Database.Password,
		DBName:        dbName,
		DBPort:        dbPort,
		RedisPort:     redisPort,
		RedisPassword: cfg.Infrastructure.Redis.Password,
		SearchPort:    searchPort,
		SearchAPIKey:  cfg.Infrastructure.Search.APIKey,
		LogLevel:      defaultLogLevel(cfg),
		IncludeKafka:  includeSet["kafka"],
		IncludeMeili:  includeSet["meilisearch"],
		IncludeMinio:  includeSet["minio"],
	}

	type genFile struct {
		name     string
		template string
	}

	files := []genFile{
		{"docker-compose.yml", dockerComposeTmpl},
		{"Dockerfile", dockerfileTmpl},
		{".dockerignore", dockerIgnoreTmpl},
	}
	if opts.Profile == "production" {
		files = append(files, genFile{"docker-compose.prod.yml", dockerComposeProdTmpl})
	}

	var paths []string
	for _, f := range files {
		p := filepath.Join(opts.OutputDir, f.name)
		if err := renderToFile(p, f.template, data); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}

	return paths, nil
}

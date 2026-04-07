package generate

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/osama1998H/moca/internal/config"
)

// K8sOptions configures Kubernetes manifest generation.
type K8sOptions struct {
	OutputDir string
	Namespace string
	Replicas  int
}

type k8sData struct {
	Header        string
	ProjectName   string
	Namespace     string
	ImageName     string
	Domain        string
	LogLevel      string
	TLSProvider   string
	DBHost        string
	RedisHost     string
	SearchHost    string
	KafkaBrokers  string
	Replicas      int
	MaxReplicas   int
	Port          int
	DBPort        int
	RedisPort     int
	SearchPort    int
	SearchEnabled bool
	KafkaEnabled  bool
}

// GenerateK8s writes Kubernetes manifests to the output directory.
func GenerateK8s(cfg *config.ProjectConfig, projectRoot string, opts K8sOptions) ([]string, error) {
	if opts.OutputDir == "" {
		opts.OutputDir = filepath.Join(projectRoot, "config", "k8s")
	}
	if opts.Namespace == "" {
		opts.Namespace = "moca"
	}
	if opts.Replicas <= 0 {
		opts.Replicas = 3
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

	data := k8sData{
		Header:        fileHeader(),
		ProjectName:   cfg.Project.Name,
		Namespace:     opts.Namespace,
		Replicas:      opts.Replicas,
		MaxReplicas:   opts.Replicas * 3,
		Port:          defaultPort(cfg),
		ImageName:     fmt.Sprintf("%s:latest", cfg.Project.Name),
		Domain:        cfg.Project.Name,
		LogLevel:      defaultLogLevel(cfg),
		TLSProvider:   cfg.Production.TLS.Provider,
		DBHost:        cfg.Infrastructure.Database.Host,
		DBPort:        dbPort,
		RedisHost:     cfg.Infrastructure.Redis.Host,
		RedisPort:     redisPort,
		SearchHost:    cfg.Infrastructure.Search.Host,
		SearchPort:    searchPort,
		SearchEnabled: cfg.Infrastructure.Search.Host != "",
		KafkaEnabled:  KafkaEnabled(cfg.Infrastructure.Kafka),
		KafkaBrokers:  strings.Join(cfg.Infrastructure.Kafka.Brokers, ","),
	}

	type manifest struct {
		name     string
		template string
	}

	manifests := []manifest{
		{"deployment.yaml", k8sDeploymentTmpl},
		{"service.yaml", k8sServiceTmpl},
		{"ingress.yaml", k8sIngressTmpl},
		{"configmap.yaml", k8sConfigMapTmpl},
		{"secret.yaml", k8sSecretTmpl},
		{"hpa.yaml", k8sHPATmpl},
		{"pdb.yaml", k8sPDBTmpl},
	}

	var paths []string
	for _, m := range manifests {
		p := filepath.Join(opts.OutputDir, m.name)
		if err := renderToFile(p, m.template, data); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}

	return paths, nil
}

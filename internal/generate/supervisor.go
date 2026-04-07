package generate

import (
	"path/filepath"

	"github.com/osama1998H/moca/internal/config"
)

// SupervisorOptions configures supervisord config generation.
type SupervisorOptions struct {
	OutputPath string
	User       string
}

type supervisorData struct {
	Header       string
	ProjectName  string
	ProjectRoot  string
	User         string
	LogLevel     string
	Port         int
	KafkaEnabled bool
}

// GenerateSupervisor writes a supervisord.conf file.
func GenerateSupervisor(cfg *config.ProjectConfig, projectRoot string, opts SupervisorOptions) ([]string, error) {
	if opts.OutputPath == "" {
		opts.OutputPath = filepath.Join(projectRoot, "config", "supervisor", "supervisord.conf")
	}
	if opts.User == "" {
		opts.User = "moca"
	}

	data := supervisorData{
		Header:       fileHeader(),
		ProjectName:  cfg.Project.Name,
		ProjectRoot:  projectRoot,
		User:         opts.User,
		Port:         defaultPort(cfg),
		LogLevel:     defaultLogLevel(cfg),
		KafkaEnabled: KafkaEnabled(cfg.Infrastructure.Kafka),
	}

	if err := renderToFile(opts.OutputPath, supervisorTmpl, data); err != nil {
		return nil, err
	}
	return []string{opts.OutputPath}, nil
}

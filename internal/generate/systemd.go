package generate

import (
	"path/filepath"

	"github.com/osama1998H/moca/internal/config"
)

// SystemdOptions configures systemd unit file generation.
type SystemdOptions struct {
	OutputDir string
	User      string
}

type systemdData struct {
	Header      string
	User        string
	ProjectName string
	ProjectRoot string
	LogLevel    string
	Port        int
}

type systemdTargetData struct {
	Header      string
	ProjectName string
	Wants       []string
}

// GenerateSystemd writes systemd unit files to the output directory.
// It conditionally omits outbox and search-sync units when Kafka is disabled.
func GenerateSystemd(cfg *config.ProjectConfig, projectRoot string, opts SystemdOptions) ([]string, error) {
	if opts.OutputDir == "" {
		opts.OutputDir = filepath.Join(projectRoot, "config", "systemd")
	}
	if opts.User == "" {
		opts.User = "moca"
	}

	data := systemdData{
		Header:      fileHeader(),
		User:        opts.User,
		ProjectName: cfg.Project.Name,
		ProjectRoot: projectRoot,
		Port:        defaultPort(cfg),
		LogLevel:    defaultLogLevel(cfg),
	}

	kafka := KafkaEnabled(cfg.Infrastructure.Kafka)

	type unitFile struct {
		name     string
		template string
	}

	units := []unitFile{
		{"moca-server@.service", systemdServerTmpl},
		{"moca-worker@.service", systemdWorkerTmpl},
		{"moca-scheduler.service", systemdSchedulerTmpl},
	}

	if kafka {
		units = append(units,
			unitFile{"moca-outbox.service", systemdOutboxTmpl},
			unitFile{"moca-search-sync.service", systemdSearchSyncTmpl},
		)
	}

	var paths []string
	for _, u := range units {
		p := filepath.Join(opts.OutputDir, u.name)
		if err := renderToFile(p, u.template, data); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}

	// Generate the moca.target that groups all units.
	wants := make([]string, 0, len(units))
	for _, u := range units {
		wants = append(wants, u.name)
	}

	targetData := systemdTargetData{
		Header:      fileHeader(),
		ProjectName: cfg.Project.Name,
		Wants:       wants,
	}
	targetPath := filepath.Join(opts.OutputDir, "moca.target")
	if err := renderToFile(targetPath, systemdTargetTmpl, targetData); err != nil {
		return nil, err
	}
	paths = append(paths, targetPath)

	return paths, nil
}

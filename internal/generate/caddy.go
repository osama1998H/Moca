package generate

import (
	"path/filepath"

	"github.com/osama1998H/moca/internal/config"
)

// CaddyOptions configures Caddyfile generation.
type CaddyOptions struct {
	OutputPath  string
	Domain      string
	Multitenant bool
}

type caddyData struct {
	Header      string
	Domain      string
	TLSProvider string
	TLSEmail    string
	ProjectRoot string
	ProjectName string
	Port        int
	Multitenant bool
}

// GenerateCaddy writes a Caddyfile to the specified output path.
// It returns the absolute path of the generated file.
func GenerateCaddy(cfg *config.ProjectConfig, projectRoot string, opts CaddyOptions) ([]string, error) {
	if opts.OutputPath == "" {
		opts.OutputPath = filepath.Join(projectRoot, "config", "caddy", "Caddyfile")
	}
	if opts.Domain == "" {
		opts.Domain = cfg.Project.Name
	}

	data := caddyData{
		Header:      fileHeader(),
		Domain:      opts.Domain,
		Port:        defaultPort(cfg),
		TLSProvider: cfg.Production.TLS.Provider,
		TLSEmail:    cfg.Production.TLS.Email,
		Multitenant: opts.Multitenant,
		ProjectRoot: projectRoot,
		ProjectName: cfg.Project.Name,
	}

	if err := renderToFile(opts.OutputPath, caddyTmpl, data); err != nil {
		return nil, err
	}
	return []string{opts.OutputPath}, nil
}

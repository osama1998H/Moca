package generate

import (
	"path/filepath"

	"github.com/osama1998H/moca/internal/config"
)

// NginxOptions configures NGINX config generation.
type NginxOptions struct {
	OutputPath  string
	Domain      string
	Multitenant bool
}

type nginxData struct {
	Header      string
	Domain      string
	TLSProvider string
	ProjectRoot string
	ProjectName string
	Port        int
	Multitenant bool
}

// GenerateNginx writes an NGINX config file to the specified output path.
func GenerateNginx(cfg *config.ProjectConfig, projectRoot string, opts NginxOptions) ([]string, error) {
	if opts.OutputPath == "" {
		opts.OutputPath = filepath.Join(projectRoot, "config", "nginx", "moca.conf")
	}
	if opts.Domain == "" {
		opts.Domain = cfg.Project.Name
	}

	data := nginxData{
		Header:      fileHeader(),
		Domain:      opts.Domain,
		Port:        defaultPort(cfg),
		TLSProvider: cfg.Production.TLS.Provider,
		Multitenant: opts.Multitenant,
		ProjectRoot: projectRoot,
		ProjectName: cfg.Project.Name,
	}

	if err := renderToFile(opts.OutputPath, nginxTmpl, data); err != nil {
		return nil, err
	}
	return []string{opts.OutputPath}, nil
}

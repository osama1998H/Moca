package config

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/osama1998H/moca/pkg/sitepath"
	"gopkg.in/yaml.v3"
)

// LoadSiteConfig reads sites/{site}/site_config.yaml as a raw map.
// Returns an empty map (not an error) if the file does not exist.
func LoadSiteConfig(projectRoot, site string) (map[string]any, error) {
	path, err := sitepath.Path(projectRoot, site, "site_config.yaml")
	if err != nil {
		return nil, err
	}
	return loadYAMLMap(path)
}

// SaveSiteConfig writes data as YAML to sites/{site}/site_config.yaml.
// Creates the directory structure if it does not exist.
func SaveSiteConfig(projectRoot, site string, data map[string]any) error {
	path, err := sitepath.Path(projectRoot, site, "site_config.yaml")
	if err != nil {
		return err
	}
	return saveYAMLMap(path, data)
}

// LoadCommonSiteConfig reads sites/common_site_config.yaml as a raw map.
// Returns an empty map (not an error) if the file does not exist.
func LoadCommonSiteConfig(projectRoot string) (map[string]any, error) {
	path := filepath.Join(projectRoot, "sites", "common_site_config.yaml")
	return loadYAMLMap(path)
}

// SaveCommonSiteConfig writes data as YAML to sites/common_site_config.yaml.
func SaveCommonSiteConfig(projectRoot string, data map[string]any) error {
	path := filepath.Join(projectRoot, "sites", "common_site_config.yaml")
	return saveYAMLMap(path, data)
}

// ConfigToMap converts a ProjectConfig to map[string]any via YAML round-trip.
// This enables uniform dot-path access across structured and unstructured configs.
func ConfigToMap(cfg *ProjectConfig) (map[string]any, error) {
	if cfg == nil {
		return map[string]any{}, nil
	}

	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}

	var result map[string]any
	if err := yaml.Unmarshal(raw, &result); err != nil {
		return nil, err
	}

	if result == nil {
		return map[string]any{}, nil
	}
	return result, nil
}

// LoadProjectConfigMap loads moca.yaml as a raw map[string]any (not as ProjectConfig).
// This is used by config set/remove to modify individual keys without
// losing the raw structure. Returns an error if the file cannot be read.
func LoadProjectConfigMap(projectRoot string) (map[string]any, error) {
	path := filepath.Join(projectRoot, "moca.yaml")
	return loadYAMLMap(path)
}

// SaveProjectConfigMap writes data as YAML to moca.yaml.
func SaveProjectConfigMap(projectRoot string, data map[string]any) error {
	path := filepath.Join(projectRoot, "moca.yaml")
	return saveYAMLMap(path, data)
}

func loadYAMLMap(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{}, nil
		}
		return nil, err
	}

	var data map[string]any
	if err := yaml.Unmarshal(raw, &data); err != nil {
		return nil, err
	}

	if data == nil {
		return map[string]any{}, nil
	}
	return data, nil
}

func saveYAMLMap(path string, data map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	raw, err := yaml.Marshal(data)
	if err != nil {
		return err
	}

	return os.WriteFile(path, raw, 0o644)
}

package config

import (
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

// ParseFile reads the file at path, expands environment variables, and decodes
// it into a ProjectConfig. The file path is included in any error messages.
//
// Returns an error if:
//   - The file cannot be opened or read.
//   - Any ${ENV_VAR} references in the file are not set in the environment.
//   - The YAML is malformed or does not match the ProjectConfig schema.
func ParseFile(path string) (*ProjectConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, &ConfigError{
			File:    path,
			Message: fmt.Sprintf("cannot open config file: %v", err),
			Err:     err,
		}
	}
	defer f.Close() //nolint:errcheck

	cfg, err := parse(f)
	if err != nil {
		// Re-wrap with the file path so error messages include it.
		if ce, ok := err.(*ConfigError); ok {
			ce.File = path
			return nil, ce
		}
		if ee, ok := err.(*EnvExpandError); ok {
			return nil, &ConfigError{
				File:    path,
				Message: ee.Error(),
				Err:     ee,
			}
		}
		return nil, &ConfigError{
			File:    path,
			Message: err.Error(),
			Err:     err,
		}
	}
	return cfg, nil
}

// Parse decodes a moca.yaml document from r into a ProjectConfig.
// Environment variable expansion (${VAR_NAME}) is applied to the raw bytes
// before YAML decoding. No file path context is available; error messages
// use the generic "config:" prefix.
//
// Returns an error if:
//   - r cannot be read.
//   - Any ${ENV_VAR} references are not set.
//   - The YAML is malformed or does not match the ProjectConfig schema.
func Parse(r io.Reader) (*ProjectConfig, error) {
	return parse(r)
}

// parse is the shared implementation used by ParseFile and Parse.
func parse(r io.Reader) (*ProjectConfig, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, &ConfigError{
			Message: fmt.Sprintf("cannot read config: %v", err),
			Err:     err,
		}
	}

	expanded, err := ExpandEnvVars(raw)
	if err != nil {
		return nil, err // EnvExpandError or ConfigError — pass through as-is
	}

	var cfg ProjectConfig
	if err := yaml.Unmarshal(expanded, &cfg); err != nil {
		line := extractYAMLLine(err)
		return nil, &ConfigError{
			Line:    line,
			Message: fmt.Sprintf("invalid YAML: %v", err),
			Err:     err,
		}
	}

	return &cfg, nil
}

// extractYAMLLine attempts to retrieve the source line number from a yaml.v3
// decode error. yaml.v3 wraps type errors in *yaml.TypeError, which carries
// individual error strings with line information. We extract the line from the
// underlying yaml.v3 error when possible; otherwise return 0.
func extractYAMLLine(err error) int {
	if te, ok := err.(*yaml.TypeError); ok && len(te.Errors) > 0 {
		// yaml.TypeError errors are formatted as "line N: ..." by yaml.v3.
		var line int
		if n, _ := fmt.Sscanf(te.Errors[0], "line %d:", &line); n == 1 {
			return line
		}
	}
	return 0
}

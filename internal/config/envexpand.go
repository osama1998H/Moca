package config

import (
	"os"
	"regexp"
	"sort"
)

// envVarPattern matches ${VAR_NAME} references in YAML source.
// Variable names must start with a letter or underscore, followed by letters, digits, or underscores.
var envVarPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// ExpandEnvVars replaces all ${VAR_NAME} references in src with the corresponding
// environment variable values. It operates on raw YAML bytes before decoding, so
// no custom unmarshalers are needed on individual struct fields.
//
// Errors:
//   - If one or more referenced environment variables are not set, ExpandEnvVars
//     returns an EnvExpandError listing every missing variable name.
//   - If src contains no ${...} patterns, the input bytes are returned unchanged.
func ExpandEnvVars(src []byte) ([]byte, error) {
	var missing []string
	seen := make(map[string]bool)

	// First pass: collect all missing variable names so we can report them together.
	envVarPattern.ReplaceAllFunc(src, func(match []byte) []byte {
		// Extract the variable name from the match (strip ${ and }).
		name := string(envVarPattern.FindSubmatch(match)[1])
		if _, ok := os.LookupEnv(name); !ok {
			if !seen[name] {
				seen[name] = true
				missing = append(missing, name)
			}
		}
		return match // unchanged in this pass
	})

	if len(missing) > 0 {
		sort.Strings(missing) // deterministic order for error messages
		return nil, &EnvExpandError{Missing: missing}
	}

	// Second pass: perform the actual substitution.
	expanded := envVarPattern.ReplaceAllFunc(src, func(match []byte) []byte {
		name := string(envVarPattern.FindSubmatch(match)[1])
		return []byte(os.Getenv(name))
	})

	return expanded, nil
}

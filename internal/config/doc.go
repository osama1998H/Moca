// Package config implements moca.yaml parsing, validation, and config resolution.
//
// The moca.yaml file is the single source of truth for a MOCA project.
// This package provides typed structs (ProjectConfig and sub-structs),
// YAML parsing with environment variable expansion, user-friendly validation
// with dot-path field error messages, and config inheritance/merging for
// multi-environment and multi-site setups.
//
// Implemented in MS-01-T2 and MS-01-T3.
package config

// Package output provides formatted output and rich error types for the MOCA CLI.
// It supports TTY (colored), JSON, and table output modes, plus progress indicators.
//
// Design ref: MOCA_CLI_SYSTEM_DESIGN.md §2.3 (lines 142–149) — Output Layer Architecture
package output

import (
	"io"
	"os"

	"golang.org/x/term"
)

// ColorConfig holds resolved color state for the current session.
// Color is disabled when --no-color is passed, NO_COLOR env is set,
// or stdout is not a terminal (piped/redirected).
type ColorConfig struct {
	enabled bool
}

// NewColorConfig resolves whether color output is enabled.
// Priority: noColorFlag → NO_COLOR env var → TTY detection on w.
func NewColorConfig(noColorFlag bool, w io.Writer) *ColorConfig {
	if noColorFlag {
		return &ColorConfig{enabled: false}
	}
	if os.Getenv("NO_COLOR") != "" {
		return &ColorConfig{enabled: false}
	}
	if f, ok := w.(*os.File); ok {
		return &ColorConfig{enabled: term.IsTerminal(int(f.Fd()))}
	}
	return &ColorConfig{enabled: false}
}

// NewColorConfigForTesting creates a ColorConfig with an explicit enabled state.
// Use in tests where TTY detection is unreliable.
func NewColorConfigForTesting(enabled bool) *ColorConfig {
	return &ColorConfig{enabled: enabled}
}

// Enabled returns true when ANSI color codes should be emitted.
func (c *ColorConfig) Enabled() bool {
	return c.enabled
}

// Success wraps s in green ANSI codes.
func (c *ColorConfig) Success(s string) string { return c.wrap(s, "32") }

// Warning wraps s in yellow ANSI codes.
func (c *ColorConfig) Warning(s string) string { return c.wrap(s, "33") }

// Error wraps s in red ANSI codes.
func (c *ColorConfig) Error(s string) string { return c.wrap(s, "31") }

// Info wraps s in cyan ANSI codes.
func (c *ColorConfig) Info(s string) string { return c.wrap(s, "36") }

// Muted wraps s in gray ANSI codes.
func (c *ColorConfig) Muted(s string) string { return c.wrap(s, "90") }

// Bold wraps s in bold ANSI codes.
func (c *ColorConfig) Bold(s string) string { return c.wrap(s, "1") }

func (c *ColorConfig) wrap(s, code string) string {
	if !c.enabled {
		return s
	}
	return "\033[" + code + "m" + s + "\033[0m"
}

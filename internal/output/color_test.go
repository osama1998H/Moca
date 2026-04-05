package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestColorConfig_Disabled(t *testing.T) {
	cc := &ColorConfig{enabled: false}
	if cc.Enabled() {
		t.Error("expected disabled")
	}
	if got := cc.Success("ok"); got != "ok" {
		t.Errorf("Success = %q, want plain text", got)
	}
	if got := cc.Warning("warn"); got != "warn" {
		t.Errorf("Warning = %q", got)
	}
	if got := cc.Error("err"); got != "err" {
		t.Errorf("Error = %q", got)
	}
	if got := cc.Info("info"); got != "info" {
		t.Errorf("Info = %q", got)
	}
	if got := cc.Muted("muted"); got != "muted" {
		t.Errorf("Muted = %q", got)
	}
	if got := cc.Bold("bold"); got != "bold" {
		t.Errorf("Bold = %q", got)
	}
}

func TestColorConfig_Enabled(t *testing.T) {
	cc := &ColorConfig{enabled: true}
	if !cc.Enabled() {
		t.Error("expected enabled")
	}

	// Success should wrap in green.
	got := cc.Success("ok")
	if !strings.HasPrefix(got, "\033[32m") || !strings.HasSuffix(got, "\033[0m") {
		t.Errorf("Success = %q, expected ANSI wrapping", got)
	}
	if !strings.Contains(got, "ok") {
		t.Errorf("Success should contain the original text")
	}
}

func TestColorConfig_AllColorsContainText(t *testing.T) {
	cc := &ColorConfig{enabled: true}
	text := "test_string"

	methods := []struct {
		fn   func(string) string
		name string
	}{
		{cc.Success, "Success"},
		{cc.Warning, "Warning"},
		{cc.Error, "Error"},
		{cc.Info, "Info"},
		{cc.Muted, "Muted"},
		{cc.Bold, "Bold"},
	}

	for _, m := range methods {
		t.Run(m.name, func(t *testing.T) {
			got := m.fn(text)
			if !strings.Contains(got, text) {
				t.Errorf("%s(%q) = %q, should contain original text", m.name, text, got)
			}
			if !strings.HasPrefix(got, "\033[") {
				t.Errorf("%s should start with ANSI escape", m.name)
			}
			if !strings.HasSuffix(got, "\033[0m") {
				t.Errorf("%s should end with ANSI reset", m.name)
			}
		})
	}
}

func TestNewColorConfig_NoColorFlag(t *testing.T) {
	var buf bytes.Buffer
	cc := NewColorConfig(true, &buf)
	if cc.Enabled() {
		t.Error("expected disabled when noColorFlag is true")
	}
}

func TestNewColorConfig_NoColorEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var buf bytes.Buffer
	cc := NewColorConfig(false, &buf)
	if cc.Enabled() {
		t.Error("expected disabled when NO_COLOR env is set")
	}
}

func TestNewColorConfig_NonTTYWriter(t *testing.T) {
	// bytes.Buffer is not an *os.File, so should be disabled.
	var buf bytes.Buffer
	cc := NewColorConfig(false, &buf)
	if cc.Enabled() {
		t.Error("expected disabled for non-TTY writer")
	}
}

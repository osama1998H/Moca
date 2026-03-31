package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// newTestCommand creates a cobra command with the same persistent flags as the root.
func newTestCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "test", RunE: func(cmd *cobra.Command, args []string) error { return nil }}
	pf := cmd.PersistentFlags()
	pf.Bool("json", false, "")
	pf.Bool("table", false, "")
	pf.Bool("no-color", false, "")
	pf.Bool("verbose", false, "")
	return cmd
}

func TestNewWriter_DefaultMode(t *testing.T) {
	cmd := newTestCommand()
	cmd.SetArgs([]string{})
	_ = cmd.Execute()
	w := NewWriter(cmd)
	if w.Mode() != ModeTTY {
		t.Errorf("Mode() = %d, want ModeTTY (%d)", w.Mode(), ModeTTY)
	}
}

func TestNewWriter_JSONMode(t *testing.T) {
	cmd := newTestCommand()
	cmd.SetArgs([]string{"--json"})
	_ = cmd.Execute()
	w := NewWriter(cmd)
	if w.Mode() != ModeJSON {
		t.Errorf("Mode() = %d, want ModeJSON (%d)", w.Mode(), ModeJSON)
	}
	if w.Color().Enabled() {
		t.Error("JSON mode should disable color")
	}
}

func TestNewWriter_TableMode(t *testing.T) {
	cmd := newTestCommand()
	cmd.SetArgs([]string{"--table"})
	_ = cmd.Execute()
	w := NewWriter(cmd)
	if w.Mode() != ModeTable {
		t.Errorf("Mode() = %d, want ModeTable (%d)", w.Mode(), ModeTable)
	}
}

func TestNewWriter_JSONPrecedence(t *testing.T) {
	cmd := newTestCommand()
	cmd.SetArgs([]string{"--json", "--table"})
	_ = cmd.Execute()
	w := NewWriter(cmd)
	if w.Mode() != ModeJSON {
		t.Errorf("Mode() = %d, want ModeJSON when both --json and --table set", w.Mode())
	}
}

func TestPrint_WritesToBuffer(t *testing.T) {
	var buf bytes.Buffer
	cc := &ColorConfig{enabled: false}
	w := NewWriterWithOptions(&buf, &buf, ModeTTY, cc, false)
	w.Print("hello %s", "world")
	if got := buf.String(); got != "hello world\n" {
		t.Errorf("Print output = %q, want %q", got, "hello world\n")
	}
}

func TestPrint_NoOpInJSONMode(t *testing.T) {
	var buf bytes.Buffer
	cc := &ColorConfig{enabled: false}
	w := NewWriterWithOptions(&buf, &buf, ModeJSON, cc, false)
	w.Print("should not appear")
	if buf.Len() != 0 {
		t.Errorf("Print in JSON mode should be no-op, got %q", buf.String())
	}
}

func TestPrintJSON_WritesValidJSON(t *testing.T) {
	var buf bytes.Buffer
	cc := &ColorConfig{enabled: false}
	w := NewWriterWithOptions(&buf, &buf, ModeJSON, cc, false)

	data := map[string]string{"name": "acme", "env": "production"}
	if err := w.PrintJSON(data); err != nil {
		t.Fatalf("PrintJSON error: %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if got["name"] != "acme" {
		t.Errorf("name = %q, want %q", got["name"], "acme")
	}
}

func TestPrintJSON_NoOpInTTYMode(t *testing.T) {
	var buf bytes.Buffer
	cc := &ColorConfig{enabled: false}
	w := NewWriterWithOptions(&buf, &buf, ModeTTY, cc, false)
	if err := w.PrintJSON(map[string]string{"x": "y"}); err != nil {
		t.Fatalf("PrintJSON error: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("PrintJSON in TTY mode should be no-op, got %q", buf.String())
	}
}

func TestPrintTable_FormatsCorrectly(t *testing.T) {
	var buf bytes.Buffer
	cc := &ColorConfig{enabled: false}
	w := NewWriterWithOptions(&buf, &buf, ModeTable, cc, false)

	headers := []string{"NAME", "STATUS"}
	rows := [][]string{
		{"acme.localhost", "active"},
		{"test.localhost", "inactive"},
	}
	if err := w.PrintTable(headers, rows); err != nil {
		t.Fatalf("PrintTable error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "NAME") {
		t.Error("table output missing NAME header")
	}
	if !strings.Contains(out, "acme.localhost") {
		t.Error("table output missing row data")
	}
}

func TestPrintTable_NoOpInJSONMode(t *testing.T) {
	var buf bytes.Buffer
	cc := &ColorConfig{enabled: false}
	w := NewWriterWithOptions(&buf, &buf, ModeJSON, cc, false)
	if err := w.PrintTable([]string{"A"}, [][]string{{"1"}}); err != nil {
		t.Fatalf("PrintTable error: %v", err)
	}
	if buf.Len() != 0 {
		t.Error("PrintTable in JSON mode should be no-op")
	}
}

func TestPrintSuccess_ContainsCheckmark(t *testing.T) {
	var buf bytes.Buffer
	cc := &ColorConfig{enabled: false}
	w := NewWriterWithOptions(&buf, &buf, ModeTTY, cc, false)
	w.PrintSuccess("done")
	if !strings.Contains(buf.String(), "✓") {
		t.Error("PrintSuccess should contain checkmark")
	}
	if !strings.Contains(buf.String(), "done") {
		t.Error("PrintSuccess should contain the message")
	}
}

func TestPrintWarning_ContainsPrefix(t *testing.T) {
	var buf bytes.Buffer
	cc := &ColorConfig{enabled: false}
	w := NewWriterWithOptions(&buf, &buf, ModeTTY, cc, false)
	w.PrintWarning("caution")
	if !strings.Contains(buf.String(), "!") {
		t.Error("PrintWarning should contain ! prefix")
	}
}

func TestPrintError_WritesToErrW(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cc := &ColorConfig{enabled: false}
	w := NewWriterWithOptions(&stdout, &stderr, ModeTTY, cc, false)
	w.PrintError("failure")
	if stdout.Len() != 0 {
		t.Error("PrintError should not write to stdout")
	}
	if !strings.Contains(stderr.String(), "failure") {
		t.Error("PrintError should write to stderr")
	}
}

func TestDebugf_OnlyWhenVerbose(t *testing.T) {
	var buf bytes.Buffer
	cc := &ColorConfig{enabled: false}

	// verbose=false: no output
	w := NewWriterWithOptions(&buf, &buf, ModeTTY, cc, false)
	w.Debugf("debug info")
	if buf.Len() != 0 {
		t.Error("Debugf should be no-op when verbose is false")
	}

	// verbose=true: outputs
	buf.Reset()
	w = NewWriterWithOptions(&buf, &buf, ModeTTY, cc, true)
	w.Debugf("debug info")
	if !strings.Contains(buf.String(), "debug info") {
		t.Error("Debugf should output when verbose is true")
	}
}

// --- ColorConfig tests ---

func TestColorConfig_DisabledByFlag(t *testing.T) {
	cc := NewColorConfig(true, &bytes.Buffer{})
	if cc.Enabled() {
		t.Error("color should be disabled when noColorFlag is true")
	}
}

func TestColorConfig_DisabledByEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	cc := NewColorConfig(false, &bytes.Buffer{})
	if cc.Enabled() {
		t.Error("color should be disabled when NO_COLOR env is set")
	}
}

func TestColorConfig_DisabledForNonFile(t *testing.T) {
	cc := NewColorConfig(false, &bytes.Buffer{})
	if cc.Enabled() {
		t.Error("color should be disabled for non-*os.File writers")
	}
}

func TestColorConfig_WrapsANSI(t *testing.T) {
	cc := &ColorConfig{enabled: true}
	got := cc.Success("ok")
	if !strings.HasPrefix(got, "\033[32m") {
		t.Errorf("Success should start with green ANSI code, got %q", got)
	}
	if !strings.HasSuffix(got, "\033[0m") {
		t.Errorf("Success should end with reset ANSI code, got %q", got)
	}
}

func TestColorConfig_NoopWhenDisabled(t *testing.T) {
	cc := &ColorConfig{enabled: false}
	if got := cc.Success("ok"); got != "ok" {
		t.Errorf("disabled Success(%q) = %q, want unchanged", "ok", got)
	}
	if got := cc.Error("err"); got != "err" {
		t.Errorf("disabled Error(%q) = %q, want unchanged", "err", got)
	}
}

// --- Table tests ---

func TestTable_Render(t *testing.T) {
	var buf bytes.Buffer
	cc := &ColorConfig{enabled: false}
	tbl := NewTable([]string{"ID", "NAME"}, cc)
	tbl.AddRow("1", "alpha")
	tbl.AddRow("2", "beta")
	if err := tbl.Render(&buf); err != nil {
		t.Fatalf("Render error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "ID") || !strings.Contains(out, "NAME") {
		t.Error("table should contain headers")
	}
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "beta") {
		t.Error("table should contain row data")
	}
}

func TestTable_EmptyRows(t *testing.T) {
	var buf bytes.Buffer
	cc := &ColorConfig{enabled: false}
	tbl := NewTable([]string{"COL"}, cc)
	if err := tbl.Render(&buf); err != nil {
		t.Fatalf("Render error: %v", err)
	}
	if !strings.Contains(buf.String(), "COL") {
		t.Error("table with no rows should still render headers")
	}
}

func TestTable_AddRowPanicsOnMismatch(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("AddRow should panic on column count mismatch")
		}
	}()
	cc := &ColorConfig{enabled: false}
	tbl := NewTable([]string{"A", "B"}, cc)
	tbl.AddRow("only-one")
}

// --- JSON tests ---

func TestWriteJSON_Indented(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, map[string]int{"count": 42}); err != nil {
		t.Fatalf("WriteJSON error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "  ") {
		t.Error("WriteJSON should produce indented output")
	}
	if !strings.Contains(out, `"count": 42`) {
		t.Errorf("WriteJSON output = %q", out)
	}
}

func TestWriteJSONCompact(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSONCompact(&buf, map[string]int{"count": 42}); err != nil {
		t.Fatalf("WriteJSONCompact error: %v", err)
	}
	out := strings.TrimSpace(buf.String())
	if strings.Contains(out, "\n") {
		t.Error("WriteJSONCompact should produce single-line output")
	}
}

package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteJSON_IndentedOutput(t *testing.T) {
	var buf bytes.Buffer
	data := map[string]any{"key": "value", "count": 42}
	if err := WriteJSON(&buf, data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	// Should be indented.
	if !strings.Contains(output, "  ") {
		t.Errorf("expected indented JSON, got: %s", output)
	}
	if !strings.Contains(output, `"key"`) {
		t.Errorf("expected key in output, got: %s", output)
	}
	if !strings.Contains(output, `"value"`) {
		t.Errorf("expected value in output, got: %s", output)
	}
}

func TestWriteJSON_NilValue(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "null" {
		t.Errorf("got %q, want null", got)
	}
}

func TestWriteJSON_EmptyMap(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, map[string]any{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "{}" {
		t.Errorf("got %q, want {}", got)
	}
}

func TestWriteJSON_Array(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, []string{"a", "b", "c"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, `"a"`) {
		t.Errorf("output = %q", output)
	}
}

func TestWriteJSONCompact_BasicObject(t *testing.T) {
	var buf bytes.Buffer
	data := map[string]string{"key": "value"}
	if err := WriteJSONCompact(&buf, data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	// Should NOT be indented.
	if strings.Contains(output, "  ") {
		t.Errorf("expected compact JSON, got: %s", output)
	}
	if !strings.Contains(output, `"key":"value"`) {
		t.Errorf("expected compact format, got: %s", output)
	}
}

func TestWriteJSONCompact_NilValue(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSONCompact(&buf, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "null" {
		t.Errorf("got %q, want null", got)
	}
}

func TestWriteJSON_Struct(t *testing.T) {
	type item struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	var buf bytes.Buffer
	if err := WriteJSON(&buf, item{Name: "test", Count: 5}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, `"name": "test"`) {
		t.Errorf("output = %q", output)
	}
}

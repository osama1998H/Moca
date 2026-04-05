package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewTable(t *testing.T) {
	headers := []string{"Name", "Status", "Total"}
	cc := &ColorConfig{enabled: false}
	tbl := NewTable(headers, cc)
	if tbl == nil {
		t.Fatal("expected non-nil Table")
	}
}

func TestTable_AddRow_And_Render(t *testing.T) {
	cc := &ColorConfig{enabled: false}
	tbl := NewTable([]string{"Name", "Status"}, cc)
	tbl.AddRow("SO-001", "Draft")
	tbl.AddRow("SO-002", "Open")

	var buf bytes.Buffer
	if err := tbl.Render(&buf); err != nil {
		t.Fatalf("Render error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Name") {
		t.Errorf("expected header 'Name', got: %s", output)
	}
	if !strings.Contains(output, "SO-001") {
		t.Errorf("expected row data 'SO-001', got: %s", output)
	}
	if !strings.Contains(output, "SO-002") {
		t.Errorf("expected row data 'SO-002', got: %s", output)
	}
}

func TestTable_AddRow_ColumnMismatch_Panics(t *testing.T) {
	cc := &ColorConfig{enabled: false}
	tbl := NewTable([]string{"Name", "Status"}, cc)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for column mismatch")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value = %T, want string", r)
		}
		if !strings.Contains(msg, "3 columns, expected 2") {
			t.Errorf("panic message = %q", msg)
		}
	}()

	tbl.AddRow("SO-001", "Draft", "extra") // 3 values for 2-column table
}

func TestTable_EmptyTable(t *testing.T) {
	cc := &ColorConfig{enabled: false}
	tbl := NewTable([]string{"Name", "Status"}, cc)

	var buf bytes.Buffer
	if err := tbl.Render(&buf); err != nil {
		t.Fatalf("Render error: %v", err)
	}

	output := buf.String()
	// Should have just the header row.
	if !strings.Contains(output, "Name") {
		t.Errorf("expected header even in empty table, got: %s", output)
	}
}

func TestTable_SingleColumn(t *testing.T) {
	cc := &ColorConfig{enabled: false}
	tbl := NewTable([]string{"ID"}, cc)
	tbl.AddRow("1")
	tbl.AddRow("2")

	var buf bytes.Buffer
	if err := tbl.Render(&buf); err != nil {
		t.Fatalf("Render error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "1") || !strings.Contains(output, "2") {
		t.Errorf("output = %q", output)
	}
}

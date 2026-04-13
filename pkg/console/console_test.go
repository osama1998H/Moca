package console

import "testing"

func TestConsole_NilDocManagerReturnsError(t *testing.T) {
	c := &Console{}

	_, err := c.Get("SalesOrder", "SO-001")
	if err == nil {
		t.Error("expected error when DocManager is nil")
	}

	_, _, err = c.GetList("SalesOrder")
	if err == nil {
		t.Error("expected error when DocManager is nil")
	}

	_, err = c.Insert("SalesOrder", map[string]any{})
	if err == nil {
		t.Error("expected error when DocManager is nil")
	}

	err = c.Update("SalesOrder", "SO-001", map[string]any{})
	if err == nil {
		t.Error("expected error when DocManager is nil")
	}

	err = c.Delete("SalesOrder", "SO-001")
	if err == nil {
		t.Error("expected error when DocManager is nil")
	}
}

func TestConsole_NilPoolReturnsError(t *testing.T) {
	c := &Console{}

	_, err := c.SQL("SELECT 1")
	if err == nil {
		t.Error("expected error when Pool is nil")
	}
}

func TestConsole_NilRegistryReturnsError(t *testing.T) {
	c := &Console{}

	_, err := c.Meta("SalesOrder")
	if err == nil {
		t.Error("expected error when Registry is nil")
	}
}

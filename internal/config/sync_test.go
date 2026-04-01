package config

import (
	"encoding/json"
	"testing"
)

// Unit tests for sync helpers. Integration tests with real PostgreSQL and Redis
// are in sync_integration_test.go (build-tagged).

func TestConfigJSONMarshal(t *testing.T) {
	cfg := map[string]any{
		"infrastructure": map[string]any{
			"database": map[string]any{
				"host": "localhost",
				"port": 5432,
			},
		},
		"project": map[string]any{
			"name": "test",
		},
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	val, ok := GetByPath(result, "infrastructure.database.host")
	if !ok {
		t.Fatal("expected key to exist after round-trip")
	}
	if val != "localhost" {
		t.Fatalf("expected 'localhost', got %v", val)
	}
}

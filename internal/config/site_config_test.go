package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSiteConfig_NotExists(t *testing.T) {
	dir := t.TempDir()
	data, err := LoadSiteConfig(dir, "acme.localhost")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("expected empty map, got %v", data)
	}
}

func TestSaveAndLoadSiteConfig_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	site := "acme.localhost"

	original := map[string]any{
		"infrastructure": map[string]any{
			"database": map[string]any{
				"port": 5433,
			},
		},
		"development": map[string]any{
			"auto_reload": true,
		},
	}

	if err := SaveSiteConfig(dir, site, original); err != nil {
		t.Fatalf("save error: %v", err)
	}

	// Verify file exists.
	path := filepath.Join(dir, "sites", site, "site_config.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file should exist: %v", err)
	}

	loaded, err := LoadSiteConfig(dir, site)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}

	val, ok := GetByPath(loaded, "infrastructure.database.port")
	if !ok {
		t.Fatal("expected key to exist")
	}
	if val != 5433 {
		t.Fatalf("expected 5433, got %v", val)
	}

	val, ok = GetByPath(loaded, "development.auto_reload")
	if !ok {
		t.Fatal("expected key to exist")
	}
	if val != true {
		t.Fatalf("expected true, got %v", val)
	}
}

func TestLoadCommonSiteConfig_NotExists(t *testing.T) {
	dir := t.TempDir()
	data, err := LoadCommonSiteConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("expected empty map, got %v", data)
	}
}

func TestSaveAndLoadCommonSiteConfig_RoundTrip(t *testing.T) {
	dir := t.TempDir()

	original := map[string]any{
		"infrastructure": map[string]any{
			"redis": map[string]any{
				"host": "redis.internal",
			},
		},
	}

	if err := SaveCommonSiteConfig(dir, original); err != nil {
		t.Fatalf("save error: %v", err)
	}

	loaded, err := LoadCommonSiteConfig(dir)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}

	val, ok := GetByPath(loaded, "infrastructure.redis.host")
	if !ok {
		t.Fatal("expected key to exist")
	}
	if val != "redis.internal" {
		t.Fatalf("expected 'redis.internal', got %v", val)
	}
}

func TestConfigToMap_RoundTrip(t *testing.T) {
	cfg := &ProjectConfig{
		Moca: "^1.0.0",
		Project: ProjectInfo{
			Name:    "test-project",
			Version: "1.0.0",
		},
		Infrastructure: InfrastructureConfig{
			Database: DatabaseConfig{
				Host: "localhost",
				Port: 5432,
			},
			Redis: RedisConfig{
				Host: "localhost",
				Port: 6379,
			},
		},
	}

	data, err := ConfigToMap(cfg)
	if err != nil {
		t.Fatalf("conversion error: %v", err)
	}

	val, ok := GetByPath(data, "project.name")
	if !ok {
		t.Fatal("expected project.name to exist")
	}
	if val != "test-project" {
		t.Fatalf("expected 'test-project', got %v", val)
	}

	val, ok = GetByPath(data, "infrastructure.database.host")
	if !ok {
		t.Fatal("expected infrastructure.database.host to exist")
	}
	if val != "localhost" {
		t.Fatalf("expected 'localhost', got %v", val)
	}
}

func TestConfigToMap_Nil(t *testing.T) {
	data, err := ConfigToMap(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("expected empty map, got %v", data)
	}
}

func TestLoadProjectConfigMap_NotExists(t *testing.T) {
	dir := t.TempDir()
	// moca.yaml doesn't exist — should return empty map.
	data, err := LoadProjectConfigMap(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("expected empty map, got %v", data)
	}
}

func TestSaveAndLoadProjectConfigMap_RoundTrip(t *testing.T) {
	dir := t.TempDir()

	original := map[string]any{
		"moca": "^1.0.0",
		"project": map[string]any{
			"name":    "roundtrip-test",
			"version": "0.1.0",
		},
	}

	if err := SaveProjectConfigMap(dir, original); err != nil {
		t.Fatalf("save error: %v", err)
	}

	loaded, err := LoadProjectConfigMap(dir)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}

	val, ok := GetByPath(loaded, "project.name")
	if !ok {
		t.Fatal("expected project.name to exist")
	}
	if val != "roundtrip-test" {
		t.Fatalf("expected 'roundtrip-test', got %v", val)
	}
}

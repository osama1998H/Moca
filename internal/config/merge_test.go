package config_test

import (
	"testing"

	"github.com/osama1998H/moca/internal/config"
)

// TestResolveInheritance_InheritsFromProduction confirms that nil staging
// pointer fields are populated with production values after ResolveInheritance.
func TestResolveInheritance_InheritsFromProduction(t *testing.T) {
	cfg := &config.ProjectConfig{
		Production: config.ProductionConfig{
			Port:           443,
			Workers:        "auto",
			LogLevel:       "warn",
			ProcessManager: "systemd",
			TLS:            config.TLSConfig{Provider: "acme", Email: "admin@example.com"},
			Proxy:          config.ProxyConfig{Engine: "caddy"},
		},
		Staging: config.StagingConfig{
			Inherits: "production",
			// All pointer fields nil — should be populated from production.
		},
	}

	config.ResolveInheritance(cfg)

	stg := cfg.Staging

	if stg.Port == nil {
		t.Fatal("staging.Port should not be nil after ResolveInheritance")
	}
	if *stg.Port != 443 {
		t.Errorf("staging.Port = %d, want 443 (inherited from production)", *stg.Port)
	}

	if stg.Workers == nil {
		t.Fatal("staging.Workers should not be nil")
	}
	if *stg.Workers != "auto" {
		t.Errorf("staging.Workers = %q, want %q", *stg.Workers, "auto")
	}

	if stg.LogLevel == nil {
		t.Fatal("staging.LogLevel should not be nil")
	}
	if *stg.LogLevel != "warn" {
		t.Errorf("staging.LogLevel = %q, want %q", *stg.LogLevel, "warn")
	}

	if stg.ProcessManager == nil {
		t.Fatal("staging.ProcessManager should not be nil")
	}
	if *stg.ProcessManager != "systemd" {
		t.Errorf("staging.ProcessManager = %q, want %q", *stg.ProcessManager, "systemd")
	}

	if stg.TLS == nil {
		t.Fatal("staging.TLS should not be nil")
	}
	if stg.TLS.Provider != "acme" {
		t.Errorf("staging.TLS.Provider = %q, want %q", stg.TLS.Provider, "acme")
	}
	if stg.TLS.Email != "admin@example.com" {
		t.Errorf("staging.TLS.Email = %q, want %q", stg.TLS.Email, "admin@example.com")
	}

	if stg.Proxy == nil {
		t.Fatal("staging.Proxy should not be nil")
	}
	if stg.Proxy.Engine != "caddy" {
		t.Errorf("staging.Proxy.Engine = %q, want %q", stg.Proxy.Engine, "caddy")
	}
}

// TestResolveInheritance_OverridesPreserved confirms that non-nil staging
// pointer fields are kept as-is and not replaced by production values.
func TestResolveInheritance_OverridesPreserved(t *testing.T) {
	stagingPort := 8443
	stagingLogLevel := "info"

	cfg := &config.ProjectConfig{
		Production: config.ProductionConfig{
			Port:     443,
			LogLevel: "warn",
		},
		Staging: config.StagingConfig{
			Inherits: "production",
			Port:     &stagingPort,     // explicitly set — must not be replaced
			LogLevel: &stagingLogLevel, // explicitly set — must not be replaced
		},
	}

	config.ResolveInheritance(cfg)

	if cfg.Staging.Port == nil || *cfg.Staging.Port != 8443 {
		t.Errorf("staging.Port = %v, want 8443 (override should be preserved)", cfg.Staging.Port)
	}
	if cfg.Staging.LogLevel == nil || *cfg.Staging.LogLevel != "info" {
		t.Errorf("staging.LogLevel = %v, want %q (override should be preserved)", cfg.Staging.LogLevel, "info")
	}
}

// TestResolveInheritance_NoOp confirms that ResolveInheritance is a no-op when
// Staging.Inherits is empty.
func TestResolveInheritance_NoOp(t *testing.T) {
	cfg := &config.ProjectConfig{
		Production: config.ProductionConfig{Port: 443},
		Staging:    config.StagingConfig{Inherits: ""},
	}

	config.ResolveInheritance(cfg)

	if cfg.Staging.Port != nil {
		t.Errorf("staging.Port should remain nil when Inherits is empty, got %d", *cfg.Staging.Port)
	}
}

// TestResolveInheritance_TLSNoCrossAliasing confirms that the TLS struct copied
// from production into staging is a value copy with no aliasing.
func TestResolveInheritance_TLSNoCrossAliasing(t *testing.T) {
	cfg := &config.ProjectConfig{
		Production: config.ProductionConfig{
			TLS: config.TLSConfig{Provider: "acme", Email: "prod@example.com"},
		},
		Staging: config.StagingConfig{Inherits: "production"},
	}

	config.ResolveInheritance(cfg)

	// Mutating the staging TLS must not affect production.
	cfg.Staging.TLS.Provider = "manual"
	if cfg.Production.TLS.Provider != "acme" {
		t.Error("mutating staging.TLS modified production.TLS — aliasing bug")
	}
}

// TestResolveInheritance_FromFixture parses the with_staging fixture and
// confirms that ResolveInheritance correctly populates nil staging fields from
// production.
func TestResolveInheritance_FromFixture(t *testing.T) {
	cfg, err := config.ParseFile("testdata/with_staging.yaml")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	// Before resolve: staging has port=8443, log_level=info set, rest nil.
	if cfg.Staging.Port == nil || *cfg.Staging.Port != 8443 {
		t.Fatalf("pre-resolve: staging.port should be 8443, got %v", cfg.Staging.Port)
	}

	config.ResolveInheritance(cfg)

	// After resolve: staging.Workers should come from production ("auto").
	if cfg.Staging.Workers == nil || *cfg.Staging.Workers != "auto" {
		t.Errorf("post-resolve: staging.Workers should be %q (from production), got %v", "auto", cfg.Staging.Workers)
	}
	// Staging port override should be preserved.
	if cfg.Staging.Port == nil || *cfg.Staging.Port != 8443 {
		t.Errorf("post-resolve: staging.Port = %v, want 8443", cfg.Staging.Port)
	}
	// Staging log_level override should be preserved.
	if cfg.Staging.LogLevel == nil || *cfg.Staging.LogLevel != "info" {
		t.Errorf("post-resolve: staging.LogLevel = %v, want %q", cfg.Staging.LogLevel, "info")
	}
}

// TestMergeLayers_NilLayers confirms that nil layers are skipped and the project
// layer is returned as the result.
func TestMergeLayers_NilLayers(t *testing.T) {
	project := &config.ProjectConfig{
		Project: config.ProjectInfo{Name: "base-project"},
		Infrastructure: config.InfrastructureConfig{
			Database: config.DatabaseConfig{Host: "db.base", Port: 5432},
		},
	}

	result := config.MergeLayers(project, nil, nil)

	if result.Project.Name != "base-project" {
		t.Errorf("project.name = %q, want %q", result.Project.Name, "base-project")
	}
	if result.Infrastructure.Database.Host != "db.base" {
		t.Errorf("database.host = %q, want %q", result.Infrastructure.Database.Host, "db.base")
	}
}

// TestMergeLayers_AllNil confirms that MergeLayers returns an empty
// ProjectConfig when all layers are nil.
func TestMergeLayers_AllNil(t *testing.T) {
	result := config.MergeLayers(nil, nil, nil)
	if result == nil {
		t.Fatal("MergeLayers(nil,nil,nil) returned nil, expected empty ProjectConfig")
	}
}

// TestMergeLayers_PriorityOrder confirms that site > commonSite > project.
func TestMergeLayers_PriorityOrder(t *testing.T) {
	project := &config.ProjectConfig{
		Infrastructure: config.InfrastructureConfig{
			Database: config.DatabaseConfig{Host: "project-db", Port: 5432},
			Redis:    config.RedisConfig{Host: "project-redis", Port: 6379},
		},
	}
	commonSite := &config.ProjectConfig{
		Infrastructure: config.InfrastructureConfig{
			Database: config.DatabaseConfig{Host: "common-db"}, // overrides project
		},
	}
	site := &config.ProjectConfig{
		Infrastructure: config.InfrastructureConfig{
			Database: config.DatabaseConfig{Host: "site-db"}, // overrides commonSite
		},
	}

	result := config.MergeLayers(project, commonSite, site)

	if result.Infrastructure.Database.Host != "site-db" {
		t.Errorf("database.host = %q, want %q (site wins)", result.Infrastructure.Database.Host, "site-db")
	}
	// Port from project should be carried through since commonSite/site have 0.
	if result.Infrastructure.Database.Port != 5432 {
		t.Errorf("database.port = %d, want 5432 (carried from project)", result.Infrastructure.Database.Port)
	}
	// Redis should come from project (no override in commonSite/site).
	if result.Infrastructure.Redis.Host != "project-redis" {
		t.Errorf("redis.host = %q, want %q", result.Infrastructure.Redis.Host, "project-redis")
	}
}

// TestMergeLayers_CommonSiteOverridesProject confirms that commonSite overrides
// project when site has no value.
func TestMergeLayers_CommonSiteOverridesProject(t *testing.T) {
	project := &config.ProjectConfig{
		Project: config.ProjectInfo{Name: "project-name"},
	}
	commonSite := &config.ProjectConfig{
		Project: config.ProjectInfo{Name: "common-name"},
	}

	result := config.MergeLayers(project, commonSite, nil)

	if result.Project.Name != "common-name" {
		t.Errorf("project.name = %q, want %q (commonSite wins over project)", result.Project.Name, "common-name")
	}
}

// TestMergeLayers_AppsMapMerge confirms that the apps map entries from higher-
// priority layers replace individual keys from lower-priority layers.
func TestMergeLayers_AppsMapMerge(t *testing.T) {
	project := &config.ProjectConfig{
		Apps: map[string]config.AppConfig{
			"core": {Source: "builtin"},
			"crm":  {Source: "github.com/moca-apps/crm", Version: "~1.0.0"},
		},
	}
	site := &config.ProjectConfig{
		Apps: map[string]config.AppConfig{
			"crm": {Source: "github.com/moca-apps/crm", Version: "~2.0.0"}, // override
			"hr":  {Source: "github.com/moca-apps/hr"},                     // new key
		},
	}

	result := config.MergeLayers(project, nil, site)

	if result.Apps["core"].Source != "builtin" {
		t.Errorf("apps.core.source = %q, want %q", result.Apps["core"].Source, "builtin")
	}
	if result.Apps["crm"].Version != "~2.0.0" {
		t.Errorf("apps.crm.version = %q, want %q (site wins)", result.Apps["crm"].Version, "~2.0.0")
	}
	if _, ok := result.Apps["hr"]; !ok {
		t.Error("apps.hr should be present from site layer")
	}
}

// TestMergeLayers_ResultIsIndependentCopy confirms that modifications to the
// result do not affect any of the input layers (no aliasing).
func TestMergeLayers_ResultIsIndependentCopy(t *testing.T) {
	project := &config.ProjectConfig{
		Apps: map[string]config.AppConfig{
			"core": {Source: "builtin"},
		},
		Infrastructure: config.InfrastructureConfig{
			Kafka: config.KafkaConfig{
				Brokers: []string{"kafka:9092"},
			},
		},
	}

	result := config.MergeLayers(project, nil, nil)

	// Mutate the result map and slice.
	result.Apps["core"] = config.AppConfig{Source: "modified"}
	result.Infrastructure.Kafka.Brokers[0] = "modified-broker"

	// Original must be unchanged.
	if project.Apps["core"].Source != "builtin" {
		t.Error("mutating result.Apps modified project.Apps — map aliasing bug")
	}
	if project.Infrastructure.Kafka.Brokers[0] != "kafka:9092" {
		t.Error("mutating result.Kafka.Brokers modified project.Kafka.Brokers — slice aliasing bug")
	}
}

// TestMergeLayers_KafkaEnabled confirms that KafkaConfig.Enabled (*bool) is
// correctly handled in layer merging.
func TestMergeLayers_KafkaEnabled(t *testing.T) {
	f := false
	tr := true

	project := &config.ProjectConfig{
		Infrastructure: config.InfrastructureConfig{
			Kafka: config.KafkaConfig{Enabled: &f},
		},
	}
	site := &config.ProjectConfig{
		Infrastructure: config.InfrastructureConfig{
			Kafka: config.KafkaConfig{Enabled: &tr},
		},
	}

	result := config.MergeLayers(project, nil, site)

	if result.Infrastructure.Kafka.Enabled == nil {
		t.Fatal("kafka.enabled should not be nil in result")
	}
	if !*result.Infrastructure.Kafka.Enabled {
		t.Error("kafka.enabled = false, want true (site overrides project)")
	}

	// No aliasing: mutating result must not change site.
	*result.Infrastructure.Kafka.Enabled = false
	if !*site.Infrastructure.Kafka.Enabled {
		t.Error("mutating result.kafka.enabled modified site — pointer aliasing bug")
	}
}

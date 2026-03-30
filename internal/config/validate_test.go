package config_test

import (
	"testing"

	"github.com/moca-framework/moca/internal/config"
)

// minimalValid returns a *ProjectConfig with every required field populated so
// that individual test cases can clear specific fields to trigger errors.
func minimalValid() *config.ProjectConfig {
	t := true
	return &config.ProjectConfig{
		Moca: "^1.0.0",
		Project: config.ProjectInfo{
			Name:    "test-project",
			Version: "1.0.0",
		},
		Apps: map[string]config.AppConfig{
			"core": {Source: "builtin"},
		},
		Infrastructure: config.InfrastructureConfig{
			Database: config.DatabaseConfig{
				Host: "localhost",
				Port: 5432,
			},
			Redis: config.RedisConfig{
				Host: "localhost",
				Port: 6379,
			},
			Kafka: config.KafkaConfig{
				Enabled: &t,
				Brokers: []string{"localhost:9092"},
			},
		},
	}
}

// findError returns the first ValidationError with the given field, or nil.
func findError(errs []config.ValidationError, field string) *config.ValidationError {
	for i := range errs {
		if errs[i].Field == field {
			return &errs[i]
		}
	}
	return nil
}

// TestValidate_ValidFull confirms that a fully-populated config produces no
// validation errors.
func TestValidate_ValidFull(t *testing.T) {
	cfg, err := config.ParseFile("testdata/valid_full.yaml")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	errs := config.Validate(cfg)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %d: %v", len(errs), errs)
	}
}

// TestValidate_ValidMinimal confirms that the minimal config passes validation.
func TestValidate_ValidMinimal(t *testing.T) {
	cfg, err := config.ParseFile("testdata/valid_minimal.yaml")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	errs := config.Validate(cfg)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %d: %v", len(errs), errs)
	}
}

// TestValidate_MissingMoca confirms that an empty moca field produces an error.
func TestValidate_MissingMoca(t *testing.T) {
	cfg := minimalValid()
	cfg.Moca = ""
	errs := config.Validate(cfg)
	if e := findError(errs, "moca"); e == nil {
		t.Error("expected error for moca: required, got none")
	}
}

// TestValidate_InvalidMocaConstraint confirms that a malformed semver constraint
// produces a format error.
func TestValidate_InvalidMocaConstraint(t *testing.T) {
	cfg := minimalValid()
	cfg.Moca = "not-a-constraint"
	errs := config.Validate(cfg)
	if e := findError(errs, "moca"); e == nil {
		t.Error("expected format error for moca, got none")
	}
}

// TestValidate_ValidMocaConstraints covers the accepted semver constraint forms.
func TestValidate_ValidMocaConstraints(t *testing.T) {
	cases := []string{
		"^1.0.0",
		"~2.1.0",
		">=1.0.0",
		"<=2.0.0",
		">1.0.0",
		"<2.0.0",
		"=1.0.0",
		"*",
		">=1.0.0, <2.0.0",
	}
	for _, constraint := range cases {
		cfg := minimalValid()
		cfg.Moca = constraint
		errs := config.Validate(cfg)
		if e := findError(errs, "moca"); e != nil {
			t.Errorf("constraint %q should be valid, got error: %s", constraint, e.Message)
		}
	}
}

// TestValidate_MissingProjectName confirms that an empty project.name is caught.
func TestValidate_MissingProjectName(t *testing.T) {
	cfg := minimalValid()
	cfg.Project.Name = ""
	errs := config.Validate(cfg)
	if e := findError(errs, "project.name"); e == nil {
		t.Error("expected error for project.name: required")
	}
}

// TestValidate_MissingProjectVersion confirms that an empty project.version is
// caught.
func TestValidate_MissingProjectVersion(t *testing.T) {
	cfg := minimalValid()
	cfg.Project.Version = ""
	errs := config.Validate(cfg)
	if e := findError(errs, "project.version"); e == nil {
		t.Error("expected error for project.version: required")
	}
}

// TestValidate_InvalidProjectVersion confirms that a non-semver project.version
// value produces a format error.
func TestValidate_InvalidProjectVersion(t *testing.T) {
	cfg := minimalValid()
	cfg.Project.Version = "not-semver"
	errs := config.Validate(cfg)
	if e := findError(errs, "project.version"); e == nil {
		t.Error("expected format error for project.version")
	}
}

// TestValidate_ValidSemverVersions checks that standard semver forms pass.
func TestValidate_ValidSemverVersions(t *testing.T) {
	cases := []string{"1.0.0", "v1.0.0", "0.1.0", "2.3.4-alpha.1", "1.0.0+build.123"}
	for _, ver := range cases {
		cfg := minimalValid()
		cfg.Project.Version = ver
		errs := config.Validate(cfg)
		if e := findError(errs, "project.version"); e != nil {
			t.Errorf("version %q should be valid, got error: %s", ver, e.Message)
		}
	}
}

// TestValidate_MissingCoreApp confirms that configs without an apps.core entry
// produce an error.
func TestValidate_MissingCoreApp(t *testing.T) {
	cfg := minimalValid()
	cfg.Apps = map[string]config.AppConfig{
		"crm": {Source: "github.com/moca-apps/crm"},
	}
	errs := config.Validate(cfg)
	if e := findError(errs, "apps.core"); e == nil {
		t.Error("expected error for apps.core: required")
	}
}

// TestValidate_NilAppsMap confirms that a nil apps map also triggers the core
// app error.
func TestValidate_NilAppsMap(t *testing.T) {
	cfg := minimalValid()
	cfg.Apps = nil
	errs := config.Validate(cfg)
	if e := findError(errs, "apps.core"); e == nil {
		t.Error("expected error for apps.core when apps is nil")
	}
}

// TestValidate_MissingDatabaseHost confirms that an empty database host is
// caught.
func TestValidate_MissingDatabaseHost(t *testing.T) {
	cfg := minimalValid()
	cfg.Infrastructure.Database.Host = ""
	errs := config.Validate(cfg)
	if e := findError(errs, "infrastructure.database.host"); e == nil {
		t.Error("expected error for infrastructure.database.host: required")
	}
}

// TestValidate_MissingDatabasePort confirms that a zero database port is caught.
func TestValidate_MissingDatabasePort(t *testing.T) {
	cfg := minimalValid()
	cfg.Infrastructure.Database.Port = 0
	errs := config.Validate(cfg)
	if e := findError(errs, "infrastructure.database.port"); e == nil {
		t.Error("expected error for infrastructure.database.port: required")
	}
}

// TestValidate_InvalidDatabasePort confirms that an out-of-range port is caught.
func TestValidate_InvalidDatabasePort(t *testing.T) {
	cfg := minimalValid()
	cfg.Infrastructure.Database.Port = 99999
	errs := config.Validate(cfg)
	if e := findError(errs, "infrastructure.database.port"); e == nil {
		t.Error("expected range error for infrastructure.database.port")
	}
}

// TestValidate_MissingRedisHost confirms that an empty redis host is caught.
func TestValidate_MissingRedisHost(t *testing.T) {
	cfg := minimalValid()
	cfg.Infrastructure.Redis.Host = ""
	errs := config.Validate(cfg)
	if e := findError(errs, "infrastructure.redis.host"); e == nil {
		t.Error("expected error for infrastructure.redis.host: required")
	}
}

// TestValidate_MissingRedisPort confirms that a zero redis port is caught.
func TestValidate_MissingRedisPort(t *testing.T) {
	cfg := minimalValid()
	cfg.Infrastructure.Redis.Port = 0
	errs := config.Validate(cfg)
	if e := findError(errs, "infrastructure.redis.port"); e == nil {
		t.Error("expected error for infrastructure.redis.port: required")
	}
}

// TestValidate_InvalidOptionalPorts confirms that optional port fields are only
// checked when non-zero and must be in range.
func TestValidate_InvalidOptionalPorts(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*config.ProjectConfig)
		field string
	}{
		{
			name:  "search port out of range",
			setup: func(c *config.ProjectConfig) { c.Infrastructure.Search.Port = 65536 },
			field: "infrastructure.search.port",
		},
		{
			name:  "development port out of range",
			setup: func(c *config.ProjectConfig) { c.Development.Port = -1 },
			field: "development.port",
		},
		{
			name:  "development desk_port out of range",
			setup: func(c *config.ProjectConfig) { c.Development.DeskPort = 70000 },
			field: "development.desk_port",
		},
		{
			name:  "production port out of range",
			setup: func(c *config.ProjectConfig) { c.Production.Port = 0o0 - 1 },
			field: "production.port",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := minimalValid()
			tc.setup(cfg)
			errs := config.Validate(cfg)
			if e := findError(errs, tc.field); e == nil {
				t.Errorf("expected range error for %s", tc.field)
			}
		})
	}
}

// TestValidate_ZeroOptionalPortsAreValid confirms that zero values for optional
// port fields do not trigger errors (zero = not set).
func TestValidate_ZeroOptionalPortsAreValid(t *testing.T) {
	cfg := minimalValid()
	cfg.Infrastructure.Search.Port = 0
	cfg.Development.Port = 0
	errs := config.Validate(cfg)
	if e := findError(errs, "infrastructure.search.port"); e != nil {
		t.Errorf("zero search.port should not error: %v", e)
	}
	if e := findError(errs, "development.port"); e != nil {
		t.Errorf("zero development.port should not error: %v", e)
	}
}

// TestValidate_InvalidSchedulerTickInterval confirms that a bad duration string
// produces an error.
func TestValidate_InvalidSchedulerTickInterval(t *testing.T) {
	cfg := minimalValid()
	cfg.Scheduler.TickInterval = "not-a-duration"
	errs := config.Validate(cfg)
	if e := findError(errs, "scheduler.tick_interval"); e == nil {
		t.Error("expected error for invalid tick_interval")
	}
}

// TestValidate_ValidSchedulerTickInterval confirms that a valid Go duration
// passes.
func TestValidate_ValidSchedulerTickInterval(t *testing.T) {
	cases := []string{"5m", "30s", "1h", "2h30m", "100ms"}
	for _, dur := range cases {
		cfg := minimalValid()
		cfg.Scheduler.TickInterval = dur
		errs := config.Validate(cfg)
		if e := findError(errs, "scheduler.tick_interval"); e != nil {
			t.Errorf("tick_interval %q should be valid, got: %s", dur, e.Message)
		}
	}
}

// TestValidate_EmptySchedulerTickInterval confirms that an empty tick_interval
// (field not set) does not produce an error.
func TestValidate_EmptySchedulerTickInterval(t *testing.T) {
	cfg := minimalValid()
	cfg.Scheduler.TickInterval = ""
	errs := config.Validate(cfg)
	if e := findError(errs, "scheduler.tick_interval"); e != nil {
		t.Errorf("empty tick_interval should not error: %v", e)
	}
}

// TestValidate_InvalidStagingInherits confirms that a staging.inherits value
// other than "production" is caught.
func TestValidate_InvalidStagingInherits(t *testing.T) {
	cfg := minimalValid()
	cfg.Staging.Inherits = "development"
	errs := config.Validate(cfg)
	if e := findError(errs, "staging.inherits"); e == nil {
		t.Error("expected error for invalid staging.inherits value")
	}
}

// TestValidate_ValidStagingInherits confirms that "production" and empty are
// both valid values for staging.inherits.
func TestValidate_ValidStagingInherits(t *testing.T) {
	for _, val := range []string{"production", ""} {
		cfg := minimalValid()
		cfg.Staging.Inherits = val
		errs := config.Validate(cfg)
		if e := findError(errs, "staging.inherits"); e != nil {
			t.Errorf("staging.inherits=%q should not error: %v", val, e)
		}
	}
}

// TestValidate_MultipleErrors confirms that all validation errors are
// accumulated in a single Validate call rather than short-circuiting at the
// first failure.
func TestValidate_MultipleErrors(t *testing.T) {
	cfg := &config.ProjectConfig{} // completely empty
	errs := config.Validate(cfg)

	// At minimum, expect errors for: moca, project.name, project.version,
	// apps.core, database.host, database.port, redis.host, redis.port.
	const minExpected = 8
	if len(errs) < minExpected {
		t.Errorf("expected at least %d errors for empty config, got %d: %v", minExpected, len(errs), errs)
	}

	// Confirm specific required-field errors are present.
	required := []string{
		"moca",
		"project.name",
		"project.version",
		"apps.core",
		"infrastructure.database.host",
		"infrastructure.database.port",
		"infrastructure.redis.host",
		"infrastructure.redis.port",
	}
	for _, field := range required {
		if e := findError(errs, field); e == nil {
			t.Errorf("expected error for %s in empty config, got none", field)
		}
	}
}

// TestValidate_MissingFieldsFixture parses the missing_fields fixture and
// confirms that the absent required fields are caught.
func TestValidate_MissingFieldsFixture(t *testing.T) {
	cfg, err := config.ParseFile("testdata/missing_fields.yaml")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	errs := config.Validate(cfg)

	// Fixture is missing: moca, project.name, database.host, redis.port.
	for _, field := range []string{"moca", "project.name", "infrastructure.database.host", "infrastructure.redis.port"} {
		if e := findError(errs, field); e == nil {
			t.Errorf("expected error for %s from missing_fields fixture", field)
		}
	}
}

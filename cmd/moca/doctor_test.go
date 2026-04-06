package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/osama1998H/moca/internal/config"
)

func TestDoctorCommandHealthy(t *testing.T) {
	origPostgres := doctorCheckPostgres
	origRedis := doctorCheckRedis
	origMeili := doctorCheckMeilisearch
	t.Cleanup(func() {
		doctorCheckPostgres = origPostgres
		doctorCheckRedis = origRedis
		doctorCheckMeilisearch = origMeili
	})

	doctorCheckPostgres = func(context.Context, config.DatabaseConfig) error { return nil }
	doctorCheckRedis = func(context.Context, config.RedisConfig) error { return nil }
	doctorCheckMeilisearch = func(context.Context, config.SearchConfig) error { return nil }

	stdout, _, err := executeRootCommand(t, testCLIContext(t.TempDir(), ""), NewDoctorCommand(), "doctor", "--json")
	if err != nil {
		t.Fatalf("doctor --json: %v", err)
	}

	var results []DoctorResult
	if err := json.Unmarshal([]byte(stdout), &results); err != nil {
		t.Fatalf("unmarshal doctor json: %v", err)
	}
	for _, result := range results {
		if result.Status == DoctorFail {
			t.Fatalf("unexpected failing result: %+v", result)
		}
	}
}

func TestDoctorCommandFailsWhenDependencyFails(t *testing.T) {
	origPostgres := doctorCheckPostgres
	origRedis := doctorCheckRedis
	origMeili := doctorCheckMeilisearch
	t.Cleanup(func() {
		doctorCheckPostgres = origPostgres
		doctorCheckRedis = origRedis
		doctorCheckMeilisearch = origMeili
	})

	doctorCheckPostgres = func(context.Context, config.DatabaseConfig) error { return assertErr("postgres down") }
	doctorCheckRedis = func(context.Context, config.RedisConfig) error { return nil }
	doctorCheckMeilisearch = func(context.Context, config.SearchConfig) error { return nil }

	stdout, _, err := executeRootCommand(t, testCLIContext(t.TempDir(), ""), NewDoctorCommand(), "doctor", "--json")
	if err == nil {
		t.Fatal("expected doctor failure, got nil")
	}
	if !strings.Contains(err.Error(), "health checks failed") {
		t.Fatalf("unexpected error: %v", err)
	}

	var results []DoctorResult
	if err := json.Unmarshal([]byte(stdout), &results); err != nil {
		t.Fatalf("unmarshal doctor json: %v", err)
	}
	foundFail := false
	for _, result := range results {
		if result.Name == "PostgreSQL reachable" && result.Status == DoctorFail {
			foundFail = true
		}
	}
	if !foundFail {
		t.Fatalf("expected failing PostgreSQL result, got %+v", results)
	}
}

type staticErr string

func (e staticErr) Error() string { return string(e) }

func assertErr(msg string) error { return staticErr(msg) }

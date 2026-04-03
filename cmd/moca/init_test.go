package main

import (
	"testing"

	"github.com/spf13/cobra"

	"github.com/osama1998H/moca/internal/config"
)

func TestBuildInitConfig_UsesValidMocaConstraint(t *testing.T) {
	cmd := &cobra.Command{Use: "init"}
	cmd.Flags().String("db-host", "localhost", "")
	cmd.Flags().Int("db-port", 5432, "")
	cmd.Flags().String("db-user", "postgres", "")
	cmd.Flags().String("db-password", "", "")
	cmd.Flags().String("redis-host", "localhost", "")
	cmd.Flags().Int("redis-port", 6379, "")
	cmd.Flags().Bool("no-kafka", false, "")

	cfg := buildInitConfig(cmd, "demo")

	if cfg.Moca != defaultMocaConstraint {
		t.Fatalf("Moca = %q, want %q", cfg.Moca, defaultMocaConstraint)
	}

	if errs := config.Validate(cfg); len(errs) > 0 {
		t.Fatalf("Validate(buildInitConfig) returned errors: %v", errs)
	}
}

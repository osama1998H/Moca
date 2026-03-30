package main

import (
	"errors"
	"fmt"
	"os"
	"runtime"

	"github.com/moca-framework/moca/internal/config"
)

// Build-time variables injected via -ldflags.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func main() {
	fmt.Println("MOCA Framework Worker")
	fmt.Println("=====================")
	fmt.Printf("Version:    %s\n", Version)
	fmt.Printf("Commit:     %s\n", Commit)
	fmt.Printf("Built:      %s\n", BuildDate)
	fmt.Printf("Go version: %s\n", runtime.Version())
	fmt.Printf("OS/Arch:    %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Println()

	loadAndPrintConfig()
}

func loadAndPrintConfig() {
	const configFile = "moca.yaml"

	if _, err := os.Stat(configFile); errors.Is(err, os.ErrNotExist) {
		fmt.Println("no moca.yaml found in current directory")
		os.Exit(0)
	}

	cfg, err := config.LoadAndResolve(configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	printConfig(cfg)
}

func printConfig(cfg *config.ProjectConfig) {
	fmt.Printf("Project:    %s %s\n", cfg.Project.Name, cfg.Project.Version)
	fmt.Printf("Moca:       %s\n", cfg.Moca)
	fmt.Printf("Database:   %s:%d\n", cfg.Infrastructure.Database.Host, cfg.Infrastructure.Database.Port)
	fmt.Printf("Redis:      %s:%d\n", cfg.Infrastructure.Redis.Host, cfg.Infrastructure.Redis.Port)
	fmt.Printf("Apps:       %d installed\n", len(cfg.Apps))
}

package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/osama1998H/moca/internal/config"
	clicontext "github.com/osama1998H/moca/internal/context"
	"github.com/osama1998H/moca/pkg/cli"
	"github.com/spf13/cobra"
)

func executeRootCommand(t *testing.T, ctx context.Context, cmd *cobra.Command, args ...string) (string, string, error) {
	t.Helper()

	cli.ResetForTesting()
	root := cli.RootCommand()
	root.AddCommand(cmd)
	root.PersistentPreRunE = nil
	if ctx != nil {
		root.SetContext(ctx)
	}

	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(args)

	err := root.Execute()
	return outBuf.String(), errBuf.String(), err
}

func testCLIContext(projectRoot, site string) context.Context {
	return clicontext.WithCLIContext(context.Background(), &clicontext.CLIContext{
		ProjectRoot: projectRoot,
		Project:     testCLIProjectConfig(),
		Site:        site,
		Environment: "development",
	})
}

func testCLIProjectConfig() *config.ProjectConfig {
	kafkaEnabled := false
	return &config.ProjectConfig{
		Moca: "0.1.0",
		Project: config.ProjectInfo{
			Name:    "test-erp",
			Version: "0.1.0",
		},
		Infrastructure: config.InfrastructureConfig{
			Database: config.DatabaseConfig{
				Driver:   "postgres",
				Host:     "localhost",
				Port:     5433,
				User:     "moca",
				Password: "moca_test",
				SystemDB: "moca_test",
				PoolSize: 10,
			},
			Redis: config.RedisConfig{
				Host:      "localhost",
				Port:      6380,
				DbCache:   0,
				DbQueue:   1,
				DbSession: 2,
				DbPubSub:  3,
			},
			Kafka: config.KafkaConfig{
				Enabled: &kafkaEnabled,
			},
			Search: config.SearchConfig{
				Engine: "meilisearch",
				Host:   "localhost",
				Port:   7700,
				APIKey: "moca_test",
			},
		},
		Apps: map[string]config.AppConfig{
			"core": {
				Source:  "builtin",
				Version: "*",
			},
		},
	}
}

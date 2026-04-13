package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/osama1998H/moca/internal/output"
	"github.com/osama1998H/moca/pkg/auth"
	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/tenancy"
	"github.com/osama1998H/moca/pkg/testutils/factory"
	"github.com/spf13/cobra"
)

func newTestFactoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "factory DOCTYPE [COUNT]",
		Short: "Generate test data from MetaType definitions",
		Long: `Generate realistic test data by introspecting MetaType field definitions.

Uses gofakeit to produce valid values for every field type, respecting
validation constraints (Required, MaxLength, MinValue/MaxValue, Options).
Link fields are resolved by creating referenced documents first.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: runTestFactory,
	}

	cmd.Flags().String("site", "", "Target site (required)")
	cmd.Flags().Int64("seed", 0, "Random seed for reproducibility (default: time-based)")
	cmd.Flags().Bool("with-children", true, "Generate child table data")
	cmd.Flags().Bool("dry-run", false, "Print generated data as JSON without inserting")
	cmd.Flags().Int("batch-size", 50, "Insert batch size")

	return cmd
}

func runTestFactory(cmd *cobra.Command, args []string) error {
	cliCtx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	doctype := args[0]
	count := 1
	if len(args) > 1 {
		count, err = strconv.Atoi(args[1])
		if err != nil || count < 1 {
			return output.NewCLIError("COUNT must be a positive integer").
				WithFix("Example: moca test factory SalesOrder 100")
		}
	}

	siteName, err := resolveSiteName(cmd, cliCtx)
	if err != nil {
		return err
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	withChildren, _ := cmd.Flags().GetBool("with-children")
	seed, _ := cmd.Flags().GetInt64("seed")
	batchSize, _ := cmd.Flags().GetInt("batch-size")

	if seed == 0 {
		seed = time.Now().UnixNano()
	}

	w := output.NewWriter(cmd)
	ctx := cmd.Context()

	svc, err := newServices(ctx, cliCtx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	// Create factory.
	f := factory.New(svc.Registry,
		factory.WithSeed(seed),
	)

	genOpts := []factory.GenOption{
		factory.WithChildren(withChildren),
	}

	if dryRun {
		// Generate without inserting.
		values, err := f.Generate(ctx, siteName, doctype, count, genOpts...)
		if err != nil {
			return output.NewCLIError("Failed to generate test data").WithErr(err)
		}

		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(values); err != nil {
			return fmt.Errorf("encode JSON: %w", err)
		}
		return nil
	}

	// Build a CLI adapter that implements factory.InsertEnv.
	sitePool, poolErr := svc.DB.ForSite(ctx, siteName)
	if poolErr != nil {
		return output.NewCLIError("Cannot connect to site").WithErr(poolErr)
	}

	env := &cliInsertEnv{
		siteName:   siteName,
		docManager: svc.DocManager,
		site: &tenancy.SiteContext{
			Name:     siteName,
			DBSchema: tenancy.SchemaNameForSite(siteName),
			Status:   "active",
			Pool:     sitePool,
		},
	}

	// Generate and insert in batches.
	start := time.Now()
	totalInserted := 0

	for totalInserted < count {
		batch := batchSize
		if totalInserted+batch > count {
			batch = count - totalInserted
		}

		docs, err := f.GenerateAndInsert(ctx, env, doctype, batch, genOpts...)
		if err != nil {
			return output.NewCLIError(
				fmt.Sprintf("Failed after inserting %d/%d documents", totalInserted, count),
			).WithErr(err)
		}

		totalInserted += len(docs)
		w.Print("Inserted %d/%d %s documents...", totalInserted, count, doctype)
	}

	elapsed := time.Since(start)
	w.Print("Created %d %s documents in %s (seed: %d)", count, doctype, elapsed.Round(time.Millisecond), seed)

	return nil
}

// cliInsertEnv adapts CLI services to the factory.InsertEnv interface.
type cliInsertEnv struct {
	docManager *document.DocManager
	site       *tenancy.SiteContext
	siteName   string
}

func (e *cliInsertEnv) GetDocManager() *document.DocManager { return e.docManager }

func (e *cliInsertEnv) GetDocContext() *document.DocContext {
	user := &auth.User{
		Email:    "Administrator",
		FullName: "Administrator",
		Roles:    []string{"System Manager"},
	}
	return document.NewDocContext(context.Background(), e.site, user)
}

func (e *cliInsertEnv) GetSiteName() string { return e.siteName }

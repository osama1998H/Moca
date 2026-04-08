package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/osama1998H/moca/internal/deploy"
	"github.com/osama1998H/moca/internal/output"
)

// NewDeployCommand returns the "moca deploy" command group with all subcommands.
func NewDeployCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deployment operations",
		Long:  "Set up, update, rollback, and monitor production deployments.",
	}

	cmd.AddCommand(
		newDeploySetupCmd(),
		newDeployUpdateCmd(),
		newDeployRollbackCmd(),
		newDeployPromoteCmd(),
		newDeployStatusCmd(),
		newDeployHistoryCmd(),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// deploy setup
// ---------------------------------------------------------------------------

func newDeploySetupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "One-command production setup",
		Long: `Set up a Moca project for production in a single idempotent command.
Runs a 14-step pipeline: validate requirements, build, generate proxy
and process manager configs, configure backups, TLS, and more.`,
		RunE: runDeploySetup,
	}

	f := cmd.Flags()
	f.String("domain", "", "Production domain (required)")
	f.String("email", "", "Admin email for TLS certificate registration")
	f.String("proxy", "caddy", `Reverse proxy engine: "caddy" or "nginx"`)
	f.String("process", "systemd", `Process manager: "systemd" or "docker"`)
	f.String("workers", "", "Number of HTTP worker processes")
	f.Int("background", 2, "Number of background workers")
	f.String("tls", "acme", `TLS mode: "acme", "custom", or "none"`)
	f.String("tls-cert", "", "Custom TLS certificate path (--tls=custom)")
	f.String("tls-key", "", "Custom TLS key path (--tls=custom)")
	f.Bool("firewall", false, "Configure UFW firewall rules")
	f.Bool("fail2ban", false, "Configure fail2ban intrusion detection")
	f.Bool("logrotate", true, "Configure log rotation")
	f.Bool("dry-run", false, "Print all steps without executing")
	f.Bool("yes", false, "Skip confirmation prompts")
	_ = cmd.MarkFlagRequired("domain")

	return cmd
}

func runDeploySetup(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	domain, _ := cmd.Flags().GetString("domain")
	email, _ := cmd.Flags().GetString("email")
	proxy, _ := cmd.Flags().GetString("proxy")
	process, _ := cmd.Flags().GetString("process")
	workers, _ := cmd.Flags().GetString("workers")
	background, _ := cmd.Flags().GetInt("background")
	tls, _ := cmd.Flags().GetString("tls")
	tlsCert, _ := cmd.Flags().GetString("tls-cert")
	tlsKey, _ := cmd.Flags().GetString("tls-key")
	firewall, _ := cmd.Flags().GetBool("firewall")
	fail2ban, _ := cmd.Flags().GetBool("fail2ban")
	logrotate, _ := cmd.Flags().GetBool("logrotate")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	yes, _ := cmd.Flags().GetBool("yes")

	opts := deploy.SetupOptions{
		Domain:      domain,
		Email:       email,
		Proxy:       proxy,
		Process:     process,
		Workers:     workers,
		Background:  background,
		TLS:         tls,
		TLSCert:     tlsCert,
		TLSKey:      tlsKey,
		Firewall:    firewall,
		Fail2ban:    fail2ban,
		Logrotate:   logrotate,
		DryRun:      dryRun,
		Yes:         yes,
		ProjectRoot: ctx.ProjectRoot,
	}

	if !dryRun && !yes {
		confirmed, promptErr := confirmPrompt(fmt.Sprintf("Set up production deployment for %s?", domain))
		if promptErr != nil {
			return promptErr
		}
		if !confirmed {
			w.PrintInfo("Aborted.")
			return nil
		}
	}

	if dryRun {
		w.PrintInfo("Dry run — no changes will be made.\n")
	}

	record, results, setupErr := deploy.Setup(cmd.Context(), opts, ctx.Project, deploy.DefaultCommander{})

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"record": record,
			"steps":  results,
		})
	}

	// Print step results.
	for _, r := range results {
		status := "✓"
		if r.Skipped {
			status = "–"
		}
		w.Print("  %s Step %d/%d: %s", status, r.Number, 14, r.Description)
	}

	if setupErr != nil {
		w.Print("")
		return output.NewCLIError("Deployment setup failed").
			WithErr(setupErr).
			WithFix("Fix the error above and re-run 'moca deploy setup'.")
	}

	if !dryRun {
		w.Print("")
		w.PrintSuccess(fmt.Sprintf("Production deployment complete: https://%s", domain))
		w.Print("  Deployment ID: %s", record.ID)
		w.Print("  Duration:      %s", record.Duration.Round(time.Millisecond))
	}

	return nil
}

// ---------------------------------------------------------------------------
// deploy update
// ---------------------------------------------------------------------------

func newDeployUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Production update (backup, build, migrate, restart)",
		Long: `Safe, atomic production update with four-phase execution.
Phase 1: Prepare — validate config and migration compatibility.
Phase 2: Backup — parallel per-site backup with verification.
Phase 3: Update — build assets and run database migrations.
Phase 4: Activate — rolling restart with health check.

Migration failures in Phase 3 trigger automatic rollback.`,
		RunE: runDeployUpdate,
	}

	f := cmd.Flags()
	f.StringSlice("apps", nil, "Update specific apps only (comma-separated)")
	f.Bool("no-backup", false, "Skip pre-update database backup")
	f.Bool("no-migrate", false, "Skip database migrations")
	f.Bool("no-build", false, "Skip frontend asset build")
	f.Bool("no-restart", false, "Skip process restart")
	f.Bool("dry-run", false, "Show update plan without executing")
	f.Int("parallel", 2, "Number of parallel site migrations")

	return cmd
}

func runDeployUpdate(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	apps, _ := cmd.Flags().GetStringSlice("apps")
	noBackup, _ := cmd.Flags().GetBool("no-backup")
	noMigrate, _ := cmd.Flags().GetBool("no-migrate")
	noBuild, _ := cmd.Flags().GetBool("no-build")
	noRestart, _ := cmd.Flags().GetBool("no-restart")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	parallel, _ := cmd.Flags().GetInt("parallel")

	opts := deploy.UpdateOptions{
		Apps:        apps,
		NoBackup:    noBackup,
		NoMigrate:   noMigrate,
		NoBuild:     noBuild,
		NoRestart:   noRestart,
		DryRun:      dryRun,
		Parallel:    parallel,
		ProjectRoot: ctx.ProjectRoot,
	}

	if !dryRun {
		confirmed, promptErr := confirmPrompt("Run production update?")
		if promptErr != nil {
			return promptErr
		}
		if !confirmed {
			w.PrintInfo("Aborted.")
			return nil
		}
	}

	if dryRun {
		w.PrintInfo("Dry run — no changes will be made.\n")
	}

	record, results, updateErr := deploy.Update(cmd.Context(), opts, ctx.Project, deploy.DefaultCommander{})

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"record": record,
			"steps":  results,
		})
	}

	for _, r := range results {
		status := "✓"
		if r.Skipped {
			status = "–"
		}
		w.Print("  %s %s", status, r.Description)
	}

	if updateErr != nil {
		w.Print("")
		if record != nil && record.Status == deploy.StatusRolledBack {
			return output.NewCLIError("Update failed — auto-rollback completed").
				WithErr(updateErr).
				WithFix("Fix the issue and re-run 'moca deploy update'.")
		}
		return output.NewCLIError("Update failed").
			WithErr(updateErr).
			WithFix("Fix the error above and re-run 'moca deploy update'.")
	}

	if !dryRun {
		w.Print("")
		w.PrintSuccess("Update complete.")
		w.Print("  Deployment ID: %s", record.ID)
		w.Print("  Duration:      %s", record.Duration.Round(time.Millisecond))
		w.Print("  Rollback:      moca deploy rollback %s", record.ID)
	}

	return nil
}

// ---------------------------------------------------------------------------
// deploy rollback
// ---------------------------------------------------------------------------

func newDeployRollbackCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rollback [DEPLOYMENT_ID]",
		Short: "Rollback to a previous deployment",
		Long: `Rollback to a previous deployment state by restoring the config
snapshot, rebuilding, and restarting services. If no deployment ID
is given, rolls back to the deployment before the latest.`,
		Args: cobra.MaximumNArgs(1),
		RunE: runDeployRollback,
	}

	f := cmd.Flags()
	f.Int("step", 0, "Rollback N deployments back (default: 1)")
	f.Bool("force", false, "Skip confirmation prompt")
	f.Bool("no-backup", false, "Skip pre-rollback backup of current state")

	return cmd
}

func runDeployRollback(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	step, _ := cmd.Flags().GetInt("step")
	force, _ := cmd.Flags().GetBool("force")
	noBackup, _ := cmd.Flags().GetBool("no-backup")

	opts := deploy.RollbackOptions{
		Step:        step,
		Force:       force,
		NoBackup:    noBackup,
		ProjectRoot: ctx.ProjectRoot,
	}
	if len(args) > 0 {
		opts.DeploymentID = args[0]
	}

	if !force {
		confirmed, promptErr := confirmPrompt("Rollback to previous deployment?")
		if promptErr != nil {
			return promptErr
		}
		if !confirmed {
			w.PrintInfo("Aborted.")
			return nil
		}
	}

	s := w.NewSpinner("Rolling back...")
	s.Start()

	record, rollErr := deploy.Rollback(cmd.Context(), opts, ctx.Project, deploy.DefaultCommander{})

	if rollErr != nil {
		s.Stop("Failed")
		return output.NewCLIError("Rollback failed").
			WithErr(rollErr).
			WithFix("Check the error above and try again.")
	}

	s.Stop("Done")

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(record)
	}

	w.PrintSuccess("Rollback complete.")
	w.Print("  Deployment ID: %s", record.ID)
	w.Print("  Rolled back:   %s", record.RollbackOf)
	w.Print("  Duration:      %s", record.Duration.Round(time.Millisecond))

	return nil
}

// ---------------------------------------------------------------------------
// deploy promote
// ---------------------------------------------------------------------------

func newDeployPromoteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "promote SOURCE_ENV TARGET_ENV",
		Short: "Promote one environment to another",
		Long: `Promote a deployment from one environment (e.g. staging) to another
(e.g. production). In dry-run mode, shows the configuration diff
between the two environments.`,
		Args: cobra.ExactArgs(2),
		RunE: runDeployPromote,
	}

	f := cmd.Flags()
	f.Bool("dry-run", false, "Show environment diff without executing")
	f.Bool("skip-backup", false, "Skip backup of target environment")

	return cmd
}

func runDeployPromote(cmd *cobra.Command, args []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	skipBackup, _ := cmd.Flags().GetBool("skip-backup")

	opts := deploy.PromoteOptions{
		SourceEnv:   args[0],
		TargetEnv:   args[1],
		DryRun:      dryRun,
		SkipBackup:  skipBackup,
		ProjectRoot: ctx.ProjectRoot,
	}

	record, diffs, promoteErr := deploy.Promote(cmd.Context(), opts, ctx.Project, deploy.DefaultCommander{})

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"record": record,
			"diffs":  diffs,
		})
	}

	// Show environment diff.
	if len(diffs) > 0 {
		w.Print("Environment diff (%s → %s):", args[0], args[1])
		headers := []string{"FIELD", args[0], args[1], "CHANGED"}
		var rows [][]string
		for _, d := range diffs {
			changed := ""
			if d.Modified {
				changed = "✓"
			}
			rows = append(rows, []string{d.Field, d.Source, d.Target, changed})
		}
		if err := w.PrintTable(headers, rows); err != nil {
			return err
		}
	}

	if dryRun {
		return nil
	}

	if promoteErr != nil {
		return output.NewCLIError("Promotion failed").
			WithErr(promoteErr).
			WithFix("Fix the error above and re-run 'moca deploy promote'.")
	}

	w.Print("")
	w.PrintSuccess(fmt.Sprintf("Promoted %s → %s.", args[0], args[1]))
	w.Print("  Deployment ID: %s", record.ID)
	w.Print("  Duration:      %s", record.Duration.Round(time.Millisecond))

	return nil
}

// ---------------------------------------------------------------------------
// deploy status
// ---------------------------------------------------------------------------

func newDeployStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show deployment status",
		Long:  "Show current deployment status, process states, and site count.",
		RunE:  runDeployStatus,
	}
}

func runDeployStatus(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	result, err := deploy.Status(cmd.Context(), ctx.ProjectRoot, ctx.Project)
	if err != nil {
		return output.NewCLIError("Failed to get deployment status").
			WithErr(err)
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(result)
	}

	w.Print("Deployment Status")
	w.Print("=================")
	if result.CurrentDeployment != "" {
		w.Print("  Current:  %s", result.CurrentDeployment)
		w.Print("  Uptime:   %s", result.Uptime.Round(time.Second))
	} else {
		w.Print("  Current:  (none)")
	}
	w.Print("  Sites:    %d", result.SiteCount)
	w.Print("")

	headers := []string{"PROCESS", "STATE", "PID", "UPTIME"}
	var rows [][]string
	for _, p := range result.Processes {
		pid := "–"
		if p.PID > 0 {
			pid = fmt.Sprintf("%d", p.PID)
		}
		uptime := "–"
		if p.Uptime != "" {
			uptime = p.Uptime
		}
		rows = append(rows, []string{p.Name, p.State, pid, uptime})
	}

	return w.PrintTable(headers, rows)
}

// ---------------------------------------------------------------------------
// deploy history
// ---------------------------------------------------------------------------

func newDeployHistoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "history",
		Short: "Show deployment history",
		Long:  "Display past deployments with status, duration, and apps updated.",
		RunE:  runDeployHistory,
	}

	f := cmd.Flags()
	f.Int("limit", 20, "Maximum entries to display")

	return cmd
}

func runDeployHistory(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	limit, _ := cmd.Flags().GetInt("limit")

	records, err := deploy.ListDeployments(ctx.ProjectRoot, limit)
	if err != nil {
		return output.NewCLIError("Failed to load deployment history").
			WithErr(err)
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(records)
	}

	if len(records) == 0 {
		w.PrintInfo("No deployments recorded yet.")
		w.Print("  Run 'moca deploy setup --domain <domain>' to create your first deployment.")
		return nil
	}

	headers := []string{"ID", "TYPE", "STATUS", "DURATION", "APPS", "AGE"}
	var rows [][]string
	for _, r := range records {
		apps := "–"
		if len(r.Apps) > 0 {
			names := make([]string, 0, len(r.Apps))
			for name := range r.Apps {
				names = append(names, name)
			}
			apps = strings.Join(names, ", ")
		}

		duration := "–"
		if r.Duration > 0 {
			duration = r.Duration.Round(time.Millisecond).String()
		}

		rows = append(rows, []string{
			r.ID,
			r.Type,
			r.Status,
			duration,
			apps,
			formatAge(r.StartedAt),
		})
	}

	return w.PrintTable(headers, rows)
}

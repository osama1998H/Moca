package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/osama1998H/moca/internal/config"
	clicontext "github.com/osama1998H/moca/internal/context"
	"github.com/osama1998H/moca/internal/output"
	"github.com/osama1998H/moca/pkg/notify"
)

// NewNotifyCommand returns the "moca notify" command group with all subcommands.
func NewNotifyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "notify",
		Short: "Notification management",
		Long:  "Send test emails and configure notification providers.",
	}

	cmd.AddCommand(
		newNotifyTestEmailCmd(),
		newNotifyConfigCmd(),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// notify test-email
// ---------------------------------------------------------------------------

func newNotifyTestEmailCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test-email",
		Short: "Send a test email to verify SMTP configuration",
		Long:  "Send a test email to the given recipient to verify that the notification email provider is configured correctly.",
		RunE:  runNotifyTestEmail,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site")
	f.String("to", "", "Recipient email address (required)")
	f.String("provider", "", `Email provider: "smtp" (default), "ses"`)

	return cmd
}

func runNotifyTestEmail(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	toAddr, _ := cmd.Flags().GetString("to")
	if toAddr == "" {
		return output.NewCLIError("Recipient email address required").
			WithFix("Pass --to <email> to specify the recipient.")
	}

	emailCfg := ctx.Project.Notification.Email
	if provider, _ := cmd.Flags().GetString("provider"); provider != "" {
		emailCfg.Provider = provider
	}

	sender, err := notify.NewEmailSender(emailCfg)
	if err != nil {
		return output.NewCLIError("Invalid email configuration").
			WithErr(err).
			WithCause(err.Error()).
			WithFix("Check your notification.email settings in moca.yaml.")
	}
	if sender == nil {
		return output.NewCLIError("No email provider configured").
			WithCause("No SMTP host or SES region is set in the project configuration").
			WithFix("Configure SMTP: moca notify config --set smtp.host=smtp.gmail.com --set smtp.port=587")
	}

	providerName := emailCfg.Provider
	if providerName == "" {
		providerName = "smtp"
	}

	s := w.NewSpinner("Sending test email...")
	s.Start()

	msg := notify.EmailMessage{
		To:       []string{toAddr},
		Subject:  "Moca Test Email",
		HTMLBody: "<h3>Moca Notification Test</h3><p>This is a test email from your Moca installation. If you received this message, your email configuration is working correctly.</p>",
		TextBody: "Moca Notification Test\n\nThis is a test email from your Moca installation.\nIf you received this message, your email configuration is working correctly.",
	}

	if err := sender.Send(cmd.Context(), msg); err != nil {
		s.Stop("Failed")
		return output.NewCLIError("Test email delivery failed").
			WithErr(err).
			WithCause(err.Error()).
			WithContext(fmt.Sprintf("provider: %s, to: %s", providerName, toAddr)).
			WithFix("Verify SMTP host, port, credentials, and TLS settings in moca.yaml.")
	}

	s.Stop("Email sent")

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"to":       toAddr,
			"provider": providerName,
			"status":   "sent",
		})
	}

	w.PrintSuccess(fmt.Sprintf("Test email sent to %s via %s", toAddr, providerName))
	return nil
}

// ---------------------------------------------------------------------------
// notify config
// ---------------------------------------------------------------------------

func newNotifyConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show or update notification provider settings",
		Long: `Show or update notification provider settings.

Examples:
  moca notify config --json
  moca notify config --set smtp.host=smtp.gmail.com --set smtp.port=587
  moca notify config --set provider=ses --set ses.region=us-east-1`,
		RunE: runNotifyConfig,
	}

	f := cmd.Flags()
	f.String("site", "", "Target site")
	f.StringArray("set", nil, "Set a notification config value (KEY=VALUE)")
	f.Bool("json", false, "Output current config as JSON")

	return cmd
}

func runNotifyConfig(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	setValues, _ := cmd.Flags().GetStringArray("set")

	if len(setValues) == 0 {
		return notifyConfigRead(cmd, w, ctx)
	}
	return notifyConfigWrite(cmd, w, ctx, setValues)
}

// notifyConfigRead displays the current notification configuration.
func notifyConfigRead(cmd *cobra.Command, w *output.Writer, ctx *clicontext.CLIContext) error {
	cfgMap, err := config.ConfigToMap(ctx.Project)
	if err != nil {
		return output.NewCLIError("Failed to read configuration").WithErr(err)
	}

	notifSection, ok := config.GetByPath(cfgMap, "notification")
	if !ok {
		notifSection = map[string]any{}
	}

	jsonFlag, _ := cmd.Flags().GetBool("json")
	if jsonFlag || w.Mode() == output.ModeJSON {
		return w.PrintJSON(notifSection)
	}

	// Flatten and display as key-value table.
	notifMap, ok := notifSection.(map[string]any)
	if !ok || len(notifMap) == 0 {
		w.PrintInfo("No notification settings configured.")
		w.Print("  Configure with: moca notify config --set smtp.host=smtp.gmail.com --set smtp.port=587")
		return nil
	}

	flat := config.FlattenMap(notifMap, "")
	masked := maskNotifySecrets(flat)

	keys := make([]string, 0, len(masked))
	for k := range masked {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	headers := []string{"KEY", "VALUE"}
	rows := make([][]string, 0, len(keys))
	for _, k := range keys {
		rows = append(rows, []string{k, fmt.Sprintf("%v", masked[k])})
	}

	return w.PrintTable(headers, rows)
}

// notifyConfigWrite updates notification config values in moca.yaml.
func notifyConfigWrite(_ *cobra.Command, w *output.Writer, ctx *clicontext.CLIContext, setValues []string) error {
	data, err := config.LoadProjectConfigMap(ctx.ProjectRoot)
	if err != nil {
		return output.NewCLIError("Failed to load moca.yaml").WithErr(err)
	}

	var updated []string
	for _, kv := range setValues {
		key, value, ok := strings.Cut(kv, "=")
		if !ok || key == "" {
			return output.NewCLIError(fmt.Sprintf("Invalid --set value: %q", kv)).
				WithFix("Use KEY=VALUE format, e.g., --set smtp.host=smtp.gmail.com")
		}

		// Map short keys to full config path under notification.email.
		fullKey := notifyConfigKeyPath(key)
		config.SetByPath(data, fullKey, autoDetectType(value))
		updated = append(updated, key)
	}

	if err := config.SaveProjectConfigMap(ctx.ProjectRoot, data); err != nil {
		return output.NewCLIError("Failed to write moca.yaml").
			WithErr(err).
			WithCause(err.Error())
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"updated": updated,
			"status":  "ok",
		})
	}

	w.PrintSuccess(fmt.Sprintf("Updated %d notification setting(s):", len(updated)))
	for _, k := range updated {
		w.Print("  %s", k)
	}
	return nil
}

// notifyConfigKeyPath maps a user-supplied short key to the full config path.
// Users type "smtp.host" and it maps to "notification.email.smtp.host".
// The "provider" key maps to "notification.email.provider".
func notifyConfigKeyPath(key string) string {
	return "notification.email." + key
}

// maskNotifySecrets replaces sensitive values in a notification config map.
func maskNotifySecrets(flat map[string]any) map[string]any {
	result := make(map[string]any, len(flat))
	for k, v := range flat {
		lower := strings.ToLower(k)
		if strings.Contains(lower, "password") || strings.Contains(lower, "secret") {
			if s, ok := v.(string); ok && s != "" {
				result[k] = "***"
				continue
			}
		}
		result[k] = v
	}
	return result
}


package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/osama1998H/moca/internal/config"
	"github.com/osama1998H/moca/internal/output"
	"github.com/osama1998H/moca/pkg/events"
)

// NewEventsCommand returns the "moca events" command group with all subcommands.
func NewEventsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "events",
		Short: "Kafka event management",
		Long:  "List topics, tail events, publish test events, and manage consumers.",
	}

	cmd.AddCommand(
		newEventsListTopicsCmd(),
		newEventsTailCmd(),
		newEventsPublishCmd(),
		newEventsConsumerStatusCmd(),
		newEventsReplayCmd(),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// isKafkaEnabled returns true when Kafka is explicitly enabled in config.
func isKafkaEnabled(cfg config.KafkaConfig) bool {
	return cfg.Enabled != nil && *cfg.Enabled
}

// newKafkaAdminClient creates a kadm admin client from KafkaConfig.
// The caller must close the returned kgo.Client when done.
func newKafkaAdminClient(cfg config.KafkaConfig) (*kadm.Client, *kgo.Client, error) {
	if len(cfg.Brokers) == 0 {
		return nil, nil, fmt.Errorf("kafka enabled but no brokers configured")
	}
	client, err := kgo.NewClient(kgo.SeedBrokers(cfg.Brokers...))
	if err != nil {
		return nil, nil, fmt.Errorf("create kafka client: %w", err)
	}
	admin := kadm.NewClient(client)
	return admin, client, nil
}

// knownTopicInfo describes a built-in Moca event topic.
type knownTopicInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// knownTopicList holds the 7 built-in Moca event topics from pkg/events/event.go.
var knownTopicList = []knownTopicInfo{
	{events.TopicDocumentEvents, "Document lifecycle events"},
	{events.TopicAuditLog, "Audit log entries"},
	{events.TopicMetaChanges, "MetaType schema changes"},
	{events.TopicIntegrationOutbox, "Outbound integration messages"},
	{events.TopicWorkflowTransitions, "Workflow state transitions"},
	{events.TopicNotifications, "User notifications"},
	{events.TopicSearchIndexing, "Search index updates"},
}

// readEventPayload reads event JSON from a file path or inline payload string.
func readEventPayload(file, payload string) ([]byte, error) {
	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, output.NewCLIError("Cannot read payload file").
				WithErr(err).
				WithFix(fmt.Sprintf("Check that %q exists and is readable.", file))
		}
		return data, nil
	}
	return []byte(payload), nil
}

// matchesEventFilter returns true if the event passes all active post-filters.
func matchesEventFilter(ev events.DocumentEvent, site, doctype, eventType string) bool {
	if site != "" && ev.Site != site {
		return false
	}
	if doctype != "" && ev.DocType != doctype {
		return false
	}
	if eventType != "" && ev.EventType != eventType {
		return false
	}
	return true
}

// formatEventShort returns a one-line summary of a DocumentEvent.
func formatEventShort(ev events.DocumentEvent) string {
	ts := ev.Timestamp.Format("15:04:05")
	action := strings.ToUpper(ev.Action)
	if action == "" {
		action = strings.ToUpper(ev.EventType)
	}
	return fmt.Sprintf("%s  %-8s %-20s %-20s %s", ts, action, ev.DocType, ev.DocName, ev.User)
}

// ---------------------------------------------------------------------------
// events list-topics
// ---------------------------------------------------------------------------

func newEventsListTopicsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list-topics",
		Short: "List all Kafka topics",
		Long:  "List all Moca-managed event topics. Shows live Kafka metadata when enabled, or built-in topic constants when Kafka is disabled.",
		RunE:  runEventsListTopics,
	}

	return cmd
}

func runEventsListTopics(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	kafkaCfg := ctx.Project.Infrastructure.Kafka

	if isKafkaEnabled(kafkaCfg) {
		return listTopicsKafka(cmd, w, kafkaCfg)
	}
	return listTopicsRedis(w)
}

func listTopicsKafka(cmd *cobra.Command, w *output.Writer, cfg config.KafkaConfig) error {
	admin, kgoClient, err := newKafkaAdminClient(cfg)
	if err != nil {
		return output.NewCLIError("Cannot connect to Kafka").
			WithErr(err).
			WithFix("Check kafka.brokers in moca.yaml and ensure Kafka is running.")
	}
	defer kgoClient.Close()

	topics, err := admin.ListTopics(cmd.Context())
	if err != nil {
		return output.NewCLIError("Failed to list Kafka topics").
			WithErr(err).
			WithFix("Check Kafka connectivity and broker configuration.")
	}

	// Filter to Moca-managed topics (prefix "moca.").
	type topicInfo struct {
		Name       string `json:"name"`
		Partitions int    `json:"partitions"`
		Replicas   int    `json:"replicas"`
	}

	var result []topicInfo
	for _, td := range topics.Sorted() {
		if !strings.HasPrefix(td.Topic, "moca.") {
			continue
		}
		replicas := 0
		for _, p := range td.Partitions.Sorted() {
			if len(p.Replicas) > replicas {
				replicas = len(p.Replicas)
			}
		}
		result = append(result, topicInfo{
			Name:       td.Topic,
			Partitions: len(td.Partitions),
			Replicas:   replicas,
		})
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(result)
	}

	if len(result) == 0 {
		w.PrintInfo("No Moca-managed topics found in Kafka.")
		return nil
	}

	headers := []string{"TOPIC", "PARTITIONS", "REPLICAS"}
	rows := make([][]string, 0, len(result))
	for _, t := range result {
		rows = append(rows, []string{
			t.Name,
			strconv.Itoa(t.Partitions),
			strconv.Itoa(t.Replicas),
		})
	}
	return w.PrintTable(headers, rows)
}

func listTopicsRedis(w *output.Writer) error {
	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(knownTopicList)
	}

	w.PrintWarning("Kafka is disabled. Showing known topic constants (mapped to Redis pub/sub channels).")
	w.Print("")

	headers := []string{"TOPIC", "DESCRIPTION"}
	rows := make([][]string, 0, len(knownTopicList))
	for _, t := range knownTopicList {
		rows = append(rows, []string{t.Name, t.Description})
	}
	return w.PrintTable(headers, rows)
}

// ---------------------------------------------------------------------------
// events tail TOPIC
// ---------------------------------------------------------------------------

func newEventsTailCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tail TOPIC",
		Short: "Tail events from a topic in real-time",
		Long: `Subscribe to an event topic and stream events as they arrive.
Kafka mode: consumes from the topic with a temporary consumer group.
Redis mode: subscribes to the pub/sub channel (no historical replay).`,
		Args: cobra.ExactArgs(1),
		RunE: runEventsTail,
	}

	f := cmd.Flags()
	f.String("site", "", "Filter events by site")
	f.String("doctype", "", "Filter events by doctype")
	f.String("event", "", "Filter events by event type (e.g. doc.created)")
	f.String("format", "short", "Output format: short or json")
	f.String("since", "", "Start from offset (e.g. 1h, 30m) — Kafka only")

	return cmd
}

func runEventsTail(cmd *cobra.Command, args []string) error {
	topic := args[0]
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	site, _ := cmd.Flags().GetString("site")
	doctype, _ := cmd.Flags().GetString("doctype")
	eventFilter, _ := cmd.Flags().GetString("event")
	format, _ := cmd.Flags().GetString("format")
	since, _ := cmd.Flags().GetString("since")

	kafkaCfg := ctx.Project.Infrastructure.Kafka

	if isKafkaEnabled(kafkaCfg) {
		return tailKafka(cmd, w, kafkaCfg, topic, site, doctype, eventFilter, format, since)
	}
	return tailRedis(cmd, w, ctx.Project, topic, site, doctype, eventFilter, format, since)
}

func tailKafka(cmd *cobra.Command, w *output.Writer, cfg config.KafkaConfig, topic, site, doctype, eventFilter, format, since string) error {
	if len(cfg.Brokers) == 0 {
		return output.NewCLIError("Kafka enabled but no brokers configured").
			WithFix("Set infrastructure.kafka.brokers in moca.yaml.")
	}

	opts := []kgo.Opt{
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ConsumeTopics(topic),
	}

	if since != "" {
		d, err := parseDuration(since)
		if err != nil {
			return output.NewCLIError("Invalid --since value").
				WithErr(err).
				WithFix("Use a duration like '1h', '30m', or '7d'.")
		}
		startMs := time.Now().Add(-d).UnixMilli()
		opts = append(opts, kgo.ConsumeResetOffset(kgo.NewOffset().AfterMilli(startMs)))
	} else {
		opts = append(opts, kgo.ConsumeResetOffset(kgo.NewOffset().AtEnd()))
	}

	client, err := kgo.NewClient(opts...)
	if err != nil {
		return output.NewCLIError("Cannot connect to Kafka").
			WithErr(err).
			WithFix("Check kafka.brokers in moca.yaml and ensure Kafka is running.")
	}
	defer client.Close()

	sigCtx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()

	w.PrintInfo(fmt.Sprintf("Tailing topic %q (Kafka mode). Press Ctrl+C to stop.", topic))

	out := cmd.OutOrStdout()
	for {
		fetches := client.PollFetches(sigCtx)
		if sigCtx.Err() != nil {
			return nil
		}
		if errs := fetches.Errors(); len(errs) > 0 {
			for _, e := range errs {
				w.PrintError(fmt.Sprintf("fetch error on %s/%d: %v", e.Topic, e.Partition, e.Err))
			}
			continue
		}

		fetches.EachRecord(func(r *kgo.Record) {
			var ev events.DocumentEvent
			if err := json.Unmarshal(r.Value, &ev); err != nil {
				_, _ = fmt.Fprintf(out, "[unmarshal error] %s\n", string(r.Value))
				return
			}
			if !matchesEventFilter(ev, site, doctype, eventFilter) {
				return
			}
			printEvent(out, ev, format)
		})
	}
}

func tailRedis(cmd *cobra.Command, w *output.Writer, cfg *config.ProjectConfig, topic, site, doctype, eventFilter, format, since string) error {
	if since != "" {
		w.PrintWarning("--since is not supported with Redis pub/sub (no historical replay). Ignoring.")
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), cfg, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	if svc.Redis == nil || svc.Redis.PubSub == nil {
		return output.NewCLIError("Redis is not available").
			WithFix("Ensure Redis is running and configured in moca.yaml.")
	}

	sigCtx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()

	sub := svc.Redis.PubSub.Subscribe(sigCtx, topic)
	defer func() { _ = sub.Close() }()

	w.PrintInfo(fmt.Sprintf("Tailing topic %q (Redis pub/sub mode). Press Ctrl+C to stop.", topic))
	w.PrintWarning("Redis pub/sub is fire-and-forget. Only events published after this subscription are shown.")

	ch := sub.Channel()
	out := cmd.OutOrStdout()
	for {
		select {
		case <-sigCtx.Done():
			return nil
		case msg, ok := <-ch:
			if !ok {
				return nil
			}
			var ev events.DocumentEvent
			if err := json.Unmarshal([]byte(msg.Payload), &ev); err != nil {
				_, _ = fmt.Fprintf(out, "[unmarshal error] %s\n", msg.Payload)
				continue
			}
			if !matchesEventFilter(ev, site, doctype, eventFilter) {
				continue
			}
			printEvent(out, ev, format)
		}
	}
}

func printEvent(out io.Writer, ev events.DocumentEvent, format string) {
	switch format {
	case "json":
		data, err := json.Marshal(ev)
		if err != nil {
			_, _ = fmt.Fprintf(out, "[marshal error] %v\n", err)
			return
		}
		_, _ = fmt.Fprintf(out, "%s\n", data)
	default:
		_, _ = fmt.Fprintf(out, "%s\n", formatEventShort(ev))
	}
}

// ---------------------------------------------------------------------------
// events publish TOPIC
// ---------------------------------------------------------------------------

func newEventsPublishCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "publish TOPIC",
		Short: "Publish a test event",
		Long:  "Publish a DocumentEvent to a topic. Useful for testing consumers and webhook integrations.",
		Args:  cobra.ExactArgs(1),
		RunE:  runEventsPublish,
	}

	f := cmd.Flags()
	f.String("payload", "", "JSON event payload (inline)")
	f.String("file", "", "Read payload from a JSON file")
	f.String("site", "", "Set or override the event site field")
	f.String("doctype", "", "Set or override the event doctype field")
	f.String("event", "", "Set or override the event_type field (e.g. doc.created)")

	return cmd
}

func runEventsPublish(cmd *cobra.Command, args []string) error {
	topic := args[0]
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	payload, _ := cmd.Flags().GetString("payload")
	file, _ := cmd.Flags().GetString("file")
	site, _ := cmd.Flags().GetString("site")
	doctype, _ := cmd.Flags().GetString("doctype")
	eventType, _ := cmd.Flags().GetString("event")

	if payload == "" && file == "" {
		return output.NewCLIError("No event payload specified").
			WithFix("Pass --payload '{...}' or --file path/to/event.json.")
	}
	if payload != "" && file != "" {
		return output.NewCLIError("Cannot use both --payload and --file").
			WithFix("Use one of --payload or --file, not both.")
	}

	jsonData, readErr := readEventPayload(file, payload)
	if readErr != nil {
		return readErr
	}

	var ev events.DocumentEvent
	if unmarshalErr := json.Unmarshal(jsonData, &ev); unmarshalErr != nil {
		return output.NewCLIError("Invalid JSON payload").
			WithErr(unmarshalErr).
			WithFix("Ensure the payload is valid JSON matching the DocumentEvent schema.")
	}

	// Override fields from flags.
	if site != "" {
		ev.Site = site
	}
	if doctype != "" {
		ev.DocType = doctype
	}
	if eventType != "" {
		ev.EventType = eventType
	}

	if defaultsErr := events.EnsureDocumentEventDefaults(&ev); defaultsErr != nil {
		return fmt.Errorf("fill event defaults: %w", defaultsErr)
	}

	verbose, _ := cmd.Flags().GetBool("verbose")
	svc, err := newServices(cmd.Context(), ctx.Project, verbose)
	if err != nil {
		return err
	}
	defer svc.Close()

	producer, err := events.NewProducer(ctx.Project.Infrastructure.Kafka, svc.Redis)
	if err != nil {
		return output.NewCLIError("Cannot create event producer").
			WithErr(err).
			WithFix("Check Kafka/Redis configuration in moca.yaml.")
	}
	defer func() { _ = producer.Close() }()

	if pubErr := producer.Publish(cmd.Context(), topic, ev); pubErr != nil {
		return output.NewCLIError("Failed to publish event").
			WithErr(pubErr).
			WithFix("Check connectivity to Kafka or Redis.")
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(map[string]any{
			"status":   "published",
			"topic":    topic,
			"event_id": ev.EventID,
			"event":    ev,
		})
	}

	w.PrintSuccess(fmt.Sprintf("Published event %s to topic %q", ev.EventID, topic))
	return nil
}

// ---------------------------------------------------------------------------
// events consumer-status
// ---------------------------------------------------------------------------

func newEventsConsumerStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "consumer-status",
		Short: "Show consumer group lag",
		Long:  "Display Kafka consumer group status including lag per partition. Kafka-only feature.",
		RunE:  runEventsConsumerStatus,
	}

	cmd.Flags().String("group", "", "Filter by consumer group name")

	return cmd
}

func runEventsConsumerStatus(cmd *cobra.Command, _ []string) error {
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	kafkaCfg := ctx.Project.Infrastructure.Kafka

	if !isKafkaEnabled(kafkaCfg) {
		if w.Mode() == output.ModeJSON {
			return w.PrintJSON(map[string]any{
				"status":  "unavailable",
				"message": "Consumer groups are a Kafka-only feature. Redis pub/sub has no consumer group tracking.",
			})
		}
		w.PrintInfo("Kafka is disabled. Consumer groups are a Kafka-only feature.")
		w.PrintInfo("Redis pub/sub operates in fire-and-forget mode with no consumer group tracking.")
		return nil
	}

	admin, kgoClient, err := newKafkaAdminClient(kafkaCfg)
	if err != nil {
		return output.NewCLIError("Cannot connect to Kafka").
			WithErr(err).
			WithFix("Check kafka.brokers in moca.yaml and ensure Kafka is running.")
	}
	defer kgoClient.Close()

	groupFilter, _ := cmd.Flags().GetString("group")

	// Determine which groups to query.
	var groupNames []string
	if groupFilter != "" {
		groupNames = []string{groupFilter}
	} else {
		listed, listErr := admin.ListGroups(cmd.Context())
		if listErr != nil {
			return output.NewCLIError("Failed to list consumer groups").
				WithErr(listErr).
				WithFix("Check Kafka connectivity.")
		}
		groupNames = listed.Groups()
	}

	if len(groupNames) == 0 {
		if w.Mode() == output.ModeJSON {
			return w.PrintJSON([]any{})
		}
		w.PrintInfo("No consumer groups found.")
		return nil
	}

	// Get lag for the groups (Lag does describe + offset fetch internally).
	lag, err := admin.Lag(cmd.Context(), groupNames...)
	if err != nil {
		return output.NewCLIError("Failed to get consumer group lag").
			WithErr(err).
			WithFix("Check Kafka connectivity.")
	}

	type lagEntry struct {
		Group     string `json:"group"`
		Topic     string `json:"topic"`
		Status    string `json:"status"`
		Lag       int64  `json:"lag"`
		Partition int32  `json:"partition"`
	}

	var entries []lagEntry
	for _, gl := range lag.Sorted() {
		if gl.Error() != nil {
			w.PrintError(fmt.Sprintf("Error for group %s: %v", gl.Group, gl.Error()))
			continue
		}
		for _, ml := range gl.Lag.Sorted() {
			status := "up to date"
			if ml.Lag > 100 {
				status = "lagging"
			} else if ml.Lag > 0 {
				status = "catching up"
			}
			entries = append(entries, lagEntry{
				Group:     gl.Group,
				Topic:     ml.Topic,
				Partition: ml.Partition,
				Lag:       ml.Lag,
				Status:    status,
			})
		}
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(entries)
	}

	if len(entries) == 0 {
		w.PrintInfo("Consumer groups have no topic assignments.")
		return nil
	}

	headers := []string{"GROUP", "TOPIC", "PARTITION", "LAG", "STATUS"}
	rows := make([][]string, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, []string{
			e.Group,
			e.Topic,
			strconv.FormatInt(int64(e.Partition), 10),
			strconv.FormatInt(e.Lag, 10),
			e.Status,
		})
	}
	return w.PrintTable(headers, rows)
}

// ---------------------------------------------------------------------------
// events replay TOPIC
// ---------------------------------------------------------------------------

func newEventsReplayCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "replay TOPIC",
		Short: "Replay events from a time offset",
		Long: `Replay events from a specific point in time. Useful for rebuilding
search indexes, re-triggering webhooks, or disaster recovery.
Requires Kafka — not available in Redis pub/sub mode.`,
		Args: cobra.ExactArgs(1),
		RunE: runEventsReplay,
	}

	f := cmd.Flags()
	f.String("since", "", "Start time as duration (e.g. 2h, 7d) — required")
	f.String("until", "", "End time as duration (default: now)")
	f.String("consumer", "", "Target consumer group for offset reset")
	f.Bool("dry-run", false, "Show events without replaying")
	f.Bool("force", false, "Skip confirmation prompt")

	return cmd
}

func runEventsReplay(cmd *cobra.Command, args []string) error {
	topic := args[0]
	w := output.NewWriter(cmd)

	ctx, err := requireProject(cmd)
	if err != nil {
		return err
	}

	kafkaCfg := ctx.Project.Infrastructure.Kafka

	if !isKafkaEnabled(kafkaCfg) {
		return output.NewCLIError("Event replay requires Kafka").
			WithContext("Kafka is disabled in moca.yaml.").
			WithFix("Set 'infrastructure.kafka.enabled: true' and configure brokers to enable event replay.")
	}

	sinceStr, _ := cmd.Flags().GetString("since")
	untilStr, _ := cmd.Flags().GetString("until")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	force, _ := cmd.Flags().GetBool("force")

	if sinceStr == "" {
		return output.NewCLIError("--since is required").
			WithFix("Specify a duration like --since 2h or --since 7d.")
	}

	sinceDur, err := parseDuration(sinceStr)
	if err != nil {
		return output.NewCLIError("Invalid --since value").
			WithErr(err).
			WithFix("Use a duration like '2h', '30m', or '7d'.")
	}
	sinceTime := time.Now().Add(-sinceDur)

	untilTime := time.Now()
	if untilStr != "" {
		untilDur, untilErr := parseDuration(untilStr)
		if untilErr != nil {
			return output.NewCLIError("Invalid --until value").
				WithErr(untilErr).
				WithFix("Use a duration like '1h', '30m', or '7d'.")
		}
		untilTime = time.Now().Add(-untilDur)
	}

	if sinceTime.After(untilTime) {
		return output.NewCLIError("--since must be before --until").
			WithFix("The --since duration should be larger than --until (e.g. --since 2h --until 1h).")
	}

	// Confirmation for non-dry-run.
	if !dryRun && !force {
		msg := fmt.Sprintf("Replay events from %s to %s on topic %q?",
			sinceTime.Format(time.RFC3339), untilTime.Format(time.RFC3339), topic)
		ok, confirmErr := confirmPrompt(msg)
		if confirmErr != nil {
			return confirmErr
		}
		if !ok {
			w.PrintInfo("Replay cancelled.")
			return nil
		}
	}

	if len(kafkaCfg.Brokers) == 0 {
		return output.NewCLIError("Kafka enabled but no brokers configured").
			WithFix("Set infrastructure.kafka.brokers in moca.yaml.")
	}

	// Create consumer to read events in the time range.
	consumerOpts := []kgo.Opt{
		kgo.SeedBrokers(kafkaCfg.Brokers...),
		kgo.ConsumeTopics(topic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AfterMilli(sinceTime.UnixMilli())),
	}

	consumer, err := kgo.NewClient(consumerOpts...)
	if err != nil {
		return output.NewCLIError("Cannot connect to Kafka for reading").
			WithErr(err).
			WithFix("Check kafka.brokers in moca.yaml.")
	}
	defer consumer.Close()

	// Create producer for re-publishing (only needed for non-dry-run).
	var producer events.Producer
	if !dryRun {
		p, err := events.NewProducer(kafkaCfg, nil)
		if err != nil {
			return output.NewCLIError("Cannot create event producer for replay").
				WithErr(err).
				WithFix("Check Kafka configuration.")
		}
		defer func() { _ = p.Close() }()
		producer = p
	}

	sigCtx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()

	if dryRun {
		w.PrintInfo(fmt.Sprintf("Dry-run: scanning topic %q from %s to %s...",
			topic, sinceTime.Format(time.RFC3339), untilTime.Format(time.RFC3339)))
	} else {
		sp := w.NewSpinner(fmt.Sprintf("Replaying events from %s to %s...",
			sinceTime.Format(time.RFC3339), untilTime.Format(time.RFC3339)))
		sp.Start()
		defer sp.Stop("Done.")
	}

	var count int64
	done := false
	for !done {
		fetches := consumer.PollFetches(sigCtx)
		if sigCtx.Err() != nil {
			break
		}
		if errs := fetches.Errors(); len(errs) > 0 {
			for _, e := range errs {
				w.PrintError(fmt.Sprintf("fetch error on %s/%d: %v", e.Topic, e.Partition, e.Err))
			}
		}

		fetches.EachRecord(func(r *kgo.Record) {
			if done {
				return
			}
			if r.Timestamp.After(untilTime) {
				done = true
				return
			}

			count++

			if dryRun {
				return
			}

			var ev events.DocumentEvent
			if err := json.Unmarshal(r.Value, &ev); err != nil {
				w.PrintError(fmt.Sprintf("skip malformed event: %v", err))
				return
			}
			if err := producer.Publish(sigCtx, topic, ev); err != nil {
				w.PrintError(fmt.Sprintf("replay publish failed: %v", err))
			}
		})

		// If no records returned and we are past the until time, we're done.
		if fetches.NumRecords() == 0 {
			break
		}
	}

	result := map[string]any{
		"topic":   topic,
		"from":    sinceTime.Format(time.RFC3339),
		"to":      untilTime.Format(time.RFC3339),
		"count":   count,
		"dry_run": dryRun,
	}

	if w.Mode() == output.ModeJSON {
		return w.PrintJSON(result)
	}

	if dryRun {
		w.PrintInfo(fmt.Sprintf("Would replay %d events from topic %q.", count, topic))
	} else {
		w.PrintSuccess(fmt.Sprintf("Replayed %d events on topic %q.", count, topic))
	}
	return nil
}

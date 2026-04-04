package main

import (
	"testing"

	"github.com/osama1998H/moca/internal/config"
)

// TestEventsCommandStructure verifies all 5 subcommands exist.
func TestEventsCommandStructure(t *testing.T) {
	cmd := NewEventsCommand()

	expected := []string{"list-topics", "tail", "publish", "consumer-status", "replay"}
	found := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		found[sub.Name()] = true
	}

	for _, name := range expected {
		if !found[name] {
			t.Errorf("missing subcommand %q", name)
		}
	}

	if len(cmd.Commands()) != len(expected) {
		t.Errorf("expected %d subcommands, got %d", len(expected), len(cmd.Commands()))
	}
}

// TestEventsListTopicsHasRunE verifies list-topics has a RunE handler.
func TestEventsListTopicsHasRunE(t *testing.T) {
	cmd := findSubcommand(NewEventsCommand(), "list-topics")
	if cmd == nil {
		t.Fatal("list-topics subcommand not found")
	}
	if cmd.RunE == nil {
		t.Error("list-topics should have RunE set")
	}
}

// TestEventsTailFlags verifies tail flags and args.
func TestEventsTailFlags(t *testing.T) {
	cmd := findSubcommand(NewEventsCommand(), "tail")
	if cmd == nil {
		t.Fatal("tail subcommand not found")
	}
	if cmd.RunE == nil {
		t.Error("tail should have RunE set")
	}
	if cmd.Args == nil {
		t.Error("tail should have Args validation set")
	}

	flags := []string{"site", "doctype", "event", "format", "since"}
	for _, name := range flags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("tail command missing --%s flag", name)
		}
	}
}

// TestEventsPublishFlags verifies publish flags and args.
func TestEventsPublishFlags(t *testing.T) {
	cmd := findSubcommand(NewEventsCommand(), "publish")
	if cmd == nil {
		t.Fatal("publish subcommand not found")
	}
	if cmd.RunE == nil {
		t.Error("publish should have RunE set")
	}
	if cmd.Args == nil {
		t.Error("publish should have Args validation set")
	}

	flags := []string{"payload", "file", "site", "doctype", "event"}
	for _, name := range flags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("publish command missing --%s flag", name)
		}
	}
}

// TestEventsConsumerStatusFlags verifies consumer-status flags.
func TestEventsConsumerStatusFlags(t *testing.T) {
	cmd := findSubcommand(NewEventsCommand(), "consumer-status")
	if cmd == nil {
		t.Fatal("consumer-status subcommand not found")
	}
	if cmd.RunE == nil {
		t.Error("consumer-status should have RunE set")
	}

	if cmd.Flags().Lookup("group") == nil {
		t.Error("consumer-status command missing --group flag")
	}
}

// TestEventsReplayFlags verifies replay flags and args.
func TestEventsReplayFlags(t *testing.T) {
	cmd := findSubcommand(NewEventsCommand(), "replay")
	if cmd == nil {
		t.Fatal("replay subcommand not found")
	}
	if cmd.RunE == nil {
		t.Error("replay should have RunE set")
	}
	if cmd.Args == nil {
		t.Error("replay should have Args validation set")
	}

	flags := []string{"since", "until", "consumer", "dry-run", "force"}
	for _, name := range flags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("replay command missing --%s flag", name)
		}
	}
}

// TestIsKafkaEnabled verifies the Kafka-enabled helper.
func TestIsKafkaEnabled(t *testing.T) {
	tests := []struct {
		cfg    func() *bool
		name   string
		expect bool
	}{
		{func() *bool { return nil }, "nil pointer", false},
		{func() *bool { v := false; return &v }, "explicit false", false},
		{func() *bool { v := true; return &v }, "explicit true", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.KafkaConfig{Enabled: tt.cfg()}
			if got := isKafkaEnabled(cfg); got != tt.expect {
				t.Errorf("isKafkaEnabled() = %v, want %v", got, tt.expect)
			}
		})
	}
}

// TestKnownTopicList verifies the built-in topic list has 7 entries.
func TestKnownTopicList(t *testing.T) {
	if len(knownTopicList) != 7 {
		t.Errorf("expected 7 known topics, got %d", len(knownTopicList))
	}

	for i, topic := range knownTopicList {
		if topic.Name == "" {
			t.Errorf("topic %d has empty name", i)
		}
		if topic.Description == "" {
			t.Errorf("topic %d (%s) has empty description", i, topic.Name)
		}
	}
}

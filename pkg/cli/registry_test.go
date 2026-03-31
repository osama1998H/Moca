package cli

import (
	"bytes"
	"sort"
	"testing"

	"github.com/spf13/cobra"
)

func TestRegisterCommand(t *testing.T) {
	ResetForTesting()

	cmd := &cobra.Command{Use: "test-cmd", Short: "A test command"}
	if err := RegisterCommand(cmd); err != nil {
		t.Fatalf("RegisterCommand() returned unexpected error: %v", err)
	}

	names := RegisteredCommandNames()
	if len(names) != 1 || names[0] != "test-cmd" {
		t.Fatalf("expected [test-cmd], got %v", names)
	}
}

func TestRegisterMultipleCommands(t *testing.T) {
	ResetForTesting()

	cmds := []*cobra.Command{
		{Use: "alpha", Short: "Alpha command"},
		{Use: "beta", Short: "Beta command"},
		{Use: "gamma", Short: "Gamma command"},
	}
	for _, c := range cmds {
		if err := RegisterCommand(c); err != nil {
			t.Fatalf("RegisterCommand(%s) error: %v", c.Name(), err)
		}
	}

	names := RegisteredCommandNames()
	sort.Strings(names)
	expected := []string{"alpha", "beta", "gamma"}
	if len(names) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, names)
	}
	for i, name := range names {
		if name != expected[i] {
			t.Fatalf("expected %s at index %d, got %s", expected[i], i, name)
		}
	}
}

func TestCommandNameCollisionDetection(t *testing.T) {
	ResetForTesting()

	cmd1 := &cobra.Command{Use: "dupe", Short: "First"}
	cmd2 := &cobra.Command{Use: "dupe", Short: "Second"}

	if err := RegisterCommand(cmd1); err != nil {
		t.Fatalf("first RegisterCommand() should succeed: %v", err)
	}

	err := RegisterCommand(cmd2)
	if err == nil {
		t.Fatal("second RegisterCommand() with same name should return error")
	}
	t.Logf("collision error (expected): %v", err)
}

func TestMustRegisterCommandPanicsOnCollision(t *testing.T) {
	ResetForTesting()

	cmd1 := &cobra.Command{Use: "panic-test", Short: "First"}
	MustRegisterCommand(cmd1)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("MustRegisterCommand should panic on collision")
		}
		t.Logf("panic message (expected): %v", r)
	}()

	cmd2 := &cobra.Command{Use: "panic-test", Short: "Second"}
	MustRegisterCommand(cmd2)
}

func TestRootCommandAttachesRegistered(t *testing.T) {
	ResetForTesting()

	MustRegisterCommand(&cobra.Command{Use: "sub1", Short: "Sub 1"})
	MustRegisterCommand(&cobra.Command{Use: "sub2", Short: "Sub 2"})

	root := RootCommand()

	var found []string
	for _, c := range root.Commands() {
		found = append(found, c.Name())
	}
	sort.Strings(found)

	if len(found) < 2 {
		t.Fatalf("expected at least 2 commands, got %v", found)
	}

	for _, name := range []string{"sub1", "sub2"} {
		idx := sort.SearchStrings(found, name)
		if idx >= len(found) || found[idx] != name {
			t.Errorf("expected %q in root commands, got %v", name, found)
		}
	}
}

func TestCommandsAppearInHelpOutput(t *testing.T) {
	ResetForTesting()

	MustRegisterCommand(&cobra.Command{Use: "test-alpha", Short: "Alpha"})
	MustRegisterCommand(&cobra.Command{Use: "test-beta", Short: "Beta"})

	root := RootCommand()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"--help"})
	_ = root.Execute()

	output := buf.String()
	for _, name := range []string{"test-alpha", "test-beta"} {
		if !bytes.Contains([]byte(output), []byte(name)) {
			t.Errorf("expected %q in help output, got:\n%s", name, output)
		}
	}
}

func TestResetForTesting(t *testing.T) {
	ResetForTesting()

	MustRegisterCommand(&cobra.Command{Use: "before-reset", Short: "Before"})
	if len(RegisteredCommandNames()) != 1 {
		t.Fatal("expected 1 registered command before reset")
	}

	ResetForTesting()

	if len(RegisteredCommandNames()) != 0 {
		t.Fatal("expected 0 registered commands after reset")
	}

	// Should be able to register the same name again after reset.
	MustRegisterCommand(&cobra.Command{Use: "before-reset", Short: "After"})
	if len(RegisteredCommandNames()) != 1 {
		t.Fatal("expected 1 registered command after re-registration")
	}
}

func TestRootCommandHasPersistentFlags(t *testing.T) {
	ResetForTesting()

	root := RootCommand()
	pf := root.PersistentFlags()

	flags := []string{"site", "env", "project", "json", "table", "no-color", "verbose"}
	for _, name := range flags {
		f := pf.Lookup(name)
		if f == nil {
			t.Errorf("expected persistent flag %q on root command", name)
		}
	}
}

func TestRootCommandSilencesErrorsAndUsage(t *testing.T) {
	ResetForTesting()

	root := RootCommand()
	if !root.SilenceErrors {
		t.Error("expected SilenceErrors = true")
	}
	if !root.SilenceUsage {
		t.Error("expected SilenceUsage = true")
	}
}

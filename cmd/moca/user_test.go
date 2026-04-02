package main

import (
	"testing"
)

func TestUserCommandSubcommands(t *testing.T) {
	cmd := NewUserCommand()

	expected := map[string]struct {
		flags   []string
		argsMin int
		argsMax int
	}{
		"add":                {[]string{"site", "first-name", "last-name", "password", "roles"}, 1, 1},
		"remove":             {[]string{"site", "force"}, 1, 1},
		"set-password":       {[]string{"site", "password"}, 1, 1},
		"set-admin-password": {[]string{"site", "password"}, 0, 0},
		"add-role":           {[]string{"site"}, 2, 2},
		"remove-role":        {[]string{"site"}, 2, 2},
		"list":               {[]string{"site", "role", "status"}, 0, 0},
		"disable":            {[]string{"site"}, 1, 1},
		"enable":             {[]string{"site"}, 1, 1},
		"impersonate":        {[]string{"site", "ttl"}, 1, 1},
	}

	subs := cmd.Commands()
	if len(subs) != len(expected) {
		t.Fatalf("expected %d subcommands, got %d", len(expected), len(subs))
	}

	for _, sub := range subs {
		spec, ok := expected[sub.Name()]
		if !ok {
			t.Errorf("unexpected subcommand %q", sub.Name())
			continue
		}

		// Verify RunE is set (not a placeholder).
		if sub.RunE == nil {
			t.Errorf("subcommand %q has nil RunE (still a placeholder?)", sub.Name())
		}

		// Verify flags exist.
		for _, flag := range spec.flags {
			if sub.Flags().Lookup(flag) == nil {
				t.Errorf("subcommand %q missing flag --%s", sub.Name(), flag)
			}
		}
	}
}

func TestUserCommandNotPlaceholder(t *testing.T) {
	cmd := NewUserCommand()
	for _, sub := range cmd.Commands() {
		if sub.Short == "" {
			t.Errorf("subcommand %q has empty Short description", sub.Name())
		}
		if sub.Long == "" {
			t.Errorf("subcommand %q has empty Long description", sub.Name())
		}
	}
}

func TestRoleMapsFromStrings(t *testing.T) {
	result := roleMapsFromStrings([]string{"Sales Manager", " Sales User ", "", "System Manager"})
	if len(result) != 3 {
		t.Fatalf("expected 3 role maps, got %d", len(result))
	}

	names := make([]string, 0, len(result))
	for _, r := range result {
		m, ok := r.(map[string]any)
		if !ok {
			t.Fatalf("expected map[string]any, got %T", r)
		}
		name, ok := m["role"].(string)
		if !ok {
			t.Fatalf("expected string role, got %T", m["role"])
		}
		names = append(names, name)
	}

	expected := []string{"Sales Manager", "Sales User", "System Manager"}
	for i, name := range names {
		if name != expected[i] {
			t.Errorf("role[%d] = %q, want %q", i, name, expected[i])
		}
	}
}

func TestCliAdminUser(t *testing.T) {
	user := cliAdminUser()
	if user.Email != "Administrator" {
		t.Errorf("Email = %q, want Administrator", user.Email)
	}
	if len(user.Roles) != 1 || user.Roles[0] != "System Manager" {
		t.Errorf("Roles = %v, want [System Manager]", user.Roles)
	}
}

package apps

import (
	"testing"

	"github.com/osama1998H/moca/pkg/document"
	"github.com/osama1998H/moca/pkg/hooks"
)

func TestRegisterInit(t *testing.T) {
	ResetForTesting()
	defer ResetForTesting()

	called := false
	fn := func(cr *document.ControllerRegistry, hr *hooks.HookRegistry) {
		called = true
	}

	if err := RegisterInit("testapp", fn); err != nil {
		t.Fatalf("RegisterInit: unexpected error: %v", err)
	}

	names := RegisteredInitNames()
	if len(names) != 1 || names[0] != "testapp" {
		t.Fatalf("RegisteredInitNames = %v, want [testapp]", names)
	}

	// Verify InitializeAll calls the function.
	cr := document.NewControllerRegistry()
	hr := hooks.NewHookRegistry()
	if err := InitializeAll(cr, hr); err != nil {
		t.Fatalf("InitializeAll: %v", err)
	}
	if !called {
		t.Fatal("InitializeAll did not call registered function")
	}
}

func TestRegisterInitDuplicate(t *testing.T) {
	ResetForTesting()
	defer ResetForTesting()

	fn := func(cr *document.ControllerRegistry, hr *hooks.HookRegistry) {}

	if err := RegisterInit("dup", fn); err != nil {
		t.Fatalf("first RegisterInit: %v", err)
	}
	if err := RegisterInit("dup", fn); err == nil {
		t.Fatal("second RegisterInit: expected error for duplicate, got nil")
	}
}

func TestMustRegisterInitPanicsOnDuplicate(t *testing.T) {
	ResetForTesting()
	defer ResetForTesting()

	fn := func(cr *document.ControllerRegistry, hr *hooks.HookRegistry) {}
	MustRegisterInit("panicky", fn)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("MustRegisterInit: expected panic on duplicate, got none")
		}
	}()
	MustRegisterInit("panicky", fn)
}

func TestInitializeAllOrder(t *testing.T) {
	ResetForTesting()
	defer ResetForTesting()

	var order []string
	for _, name := range []string{"alpha", "beta", "gamma"} {
		n := name
		MustRegisterInit(n, func(cr *document.ControllerRegistry, hr *hooks.HookRegistry) {
			order = append(order, n)
		})
	}

	cr := document.NewControllerRegistry()
	hr := hooks.NewHookRegistry()
	if err := InitializeAll(cr, hr); err != nil {
		t.Fatalf("InitializeAll: %v", err)
	}

	if len(order) != 3 || order[0] != "alpha" || order[1] != "beta" || order[2] != "gamma" {
		t.Fatalf("InitializeAll order = %v, want [alpha beta gamma]", order)
	}
}

func TestInitializeAllEmpty(t *testing.T) {
	ResetForTesting()
	defer ResetForTesting()

	cr := document.NewControllerRegistry()
	hr := hooks.NewHookRegistry()
	if err := InitializeAll(cr, hr); err != nil {
		t.Fatalf("InitializeAll with no registrations: %v", err)
	}
}

func TestResetForTesting(t *testing.T) {
	ResetForTesting()
	defer ResetForTesting()

	MustRegisterInit("willbereset", func(cr *document.ControllerRegistry, hr *hooks.HookRegistry) {})
	if len(RegisteredInitNames()) != 1 {
		t.Fatal("expected 1 registration before reset")
	}

	ResetForTesting()
	if len(RegisteredInitNames()) != 0 {
		t.Fatal("expected 0 registrations after reset")
	}
}

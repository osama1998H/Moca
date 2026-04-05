package auth

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestNewCustomRuleRegistry(t *testing.T) {
	r := NewCustomRuleRegistry()
	if r == nil {
		t.Fatal("expected non-nil registry")
	}
	if r.rules == nil {
		t.Fatal("expected initialized rules map")
	}
}

func TestCustomRuleRegistry_Register(t *testing.T) {
	r := NewCustomRuleRegistry()
	fn := func(ctx context.Context, user *User, doctype string) error {
		return nil
	}

	if err := r.Register("rule1", fn); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Duplicate registration should fail.
	if err := r.Register("rule1", fn); err == nil {
		t.Fatal("expected error for duplicate registration")
	}
}

func TestCustomRuleRegistry_Evaluate_NotRegistered(t *testing.T) {
	r := NewCustomRuleRegistry()
	err := r.Evaluate(context.Background(), "nonexistent", &User{}, "DocType")
	if err == nil {
		t.Fatal("expected error for unregistered rule")
	}
}

func TestCustomRuleRegistry_Evaluate_Allow(t *testing.T) {
	r := NewCustomRuleRegistry()
	_ = r.Register("allow_all", func(ctx context.Context, user *User, doctype string) error {
		return nil
	})

	err := r.Evaluate(context.Background(), "allow_all", &User{Email: "test@example.com"}, "SalesOrder")
	if err != nil {
		t.Fatalf("expected nil error for allow rule, got: %v", err)
	}
}

func TestCustomRuleRegistry_Evaluate_Deny(t *testing.T) {
	r := NewCustomRuleRegistry()
	_ = r.Register("deny_all", func(ctx context.Context, user *User, doctype string) error {
		return errors.New("access denied")
	})

	err := r.Evaluate(context.Background(), "deny_all", &User{Email: "test@example.com"}, "SalesOrder")
	if err == nil {
		t.Fatal("expected error for deny rule")
	}
	if err.Error() != "access denied" {
		t.Errorf("error = %q, want %q", err.Error(), "access denied")
	}
}

func TestCustomRuleRegistry_Evaluate_ReceivesArguments(t *testing.T) {
	r := NewCustomRuleRegistry()
	var gotUser *User
	var gotDoctype string
	_ = r.Register("check_args", func(ctx context.Context, user *User, doctype string) error {
		gotUser = user
		gotDoctype = doctype
		return nil
	})

	user := &User{Email: "admin@example.com", Roles: []string{"Admin"}}
	_ = r.Evaluate(context.Background(), "check_args", user, "PurchaseOrder")

	if gotUser.Email != "admin@example.com" {
		t.Errorf("user.Email = %q, want %q", gotUser.Email, "admin@example.com")
	}
	if gotDoctype != "PurchaseOrder" {
		t.Errorf("doctype = %q, want %q", gotDoctype, "PurchaseOrder")
	}
}

func TestCustomRuleRegistry_EvaluateAll_AllPass(t *testing.T) {
	r := NewCustomRuleRegistry()
	_ = r.Register("r1", func(ctx context.Context, user *User, doctype string) error { return nil })
	_ = r.Register("r2", func(ctx context.Context, user *User, doctype string) error { return nil })

	err := r.EvaluateAll(context.Background(), []string{"r1", "r2"}, &User{}, "DocType")
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}
}

func TestCustomRuleRegistry_EvaluateAll_FailFast(t *testing.T) {
	r := NewCustomRuleRegistry()
	callOrder := []string{}
	_ = r.Register("pass", func(ctx context.Context, user *User, doctype string) error {
		callOrder = append(callOrder, "pass")
		return nil
	})
	_ = r.Register("fail", func(ctx context.Context, user *User, doctype string) error {
		callOrder = append(callOrder, "fail")
		return errors.New("denied")
	})
	_ = r.Register("never", func(ctx context.Context, user *User, doctype string) error {
		callOrder = append(callOrder, "never")
		return nil
	})

	err := r.EvaluateAll(context.Background(), []string{"pass", "fail", "never"}, &User{}, "DocType")
	if err == nil {
		t.Fatal("expected error from fail rule")
	}
	// "never" should not be called due to fail-fast.
	if len(callOrder) != 2 || callOrder[0] != "pass" || callOrder[1] != "fail" {
		t.Errorf("callOrder = %v, want [pass fail]", callOrder)
	}
}

func TestCustomRuleRegistry_EvaluateAll_EmptyList(t *testing.T) {
	r := NewCustomRuleRegistry()
	err := r.EvaluateAll(context.Background(), nil, &User{}, "DocType")
	if err != nil {
		t.Fatalf("expected nil for empty rule list, got: %v", err)
	}
}

func TestCustomRuleRegistry_EvaluateAll_UnregisteredRule(t *testing.T) {
	r := NewCustomRuleRegistry()
	_ = r.Register("r1", func(ctx context.Context, user *User, doctype string) error { return nil })

	err := r.EvaluateAll(context.Background(), []string{"r1", "nonexistent"}, &User{}, "DocType")
	if err == nil {
		t.Fatal("expected error for unregistered rule in EvaluateAll")
	}
}

func TestCustomRuleRegistry_ConcurrentAccess(t *testing.T) {
	r := NewCustomRuleRegistry()

	// Register concurrently with unique names.
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := "rule_" + string(rune('A'+i%26)) + string(rune('0'+i/26))
			_ = r.Register(name, func(ctx context.Context, user *User, doctype string) error {
				return nil
			})
		}(i)
	}
	wg.Wait()

	// Evaluate concurrently.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.Evaluate(context.Background(), "rule_A0", &User{}, "DocType")
		}()
	}
	wg.Wait()
}

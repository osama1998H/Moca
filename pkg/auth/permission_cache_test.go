package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/moca-framework/moca/pkg/auth"
	"github.com/moca-framework/moca/pkg/meta"
)

func nullLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(nullWriter{}, nil))
}

type nullWriter struct{}

func (nullWriter) Write(p []byte) (int, error) { return len(p), nil }

func seedRegistry(t *testing.T, site, doctype string, perms []meta.PermRule) *meta.Registry {
	t.Helper()
	r := meta.NewRegistry(nil, nil, nullLogger())
	mt := &meta.MetaType{
		Name:        doctype,
		Module:      "core",
		Permissions: perms,
	}
	r.SeedL1ForTest(site, doctype, mt)
	return r
}

func startMiniredis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return mr, rc
}

// ── Resolve ──────────────────────────────────────────────────────────────────

func TestCachedPermissionResolver_NilRedis(t *testing.T) {
	reg := seedRegistry(t, "site1", "SalesOrder", []meta.PermRule{
		{Role: "Sales User", DocTypePerm: int(auth.PermRead | auth.PermCreate)},
	})
	resolver := auth.NewCachedPermissionResolver(reg, nil, nil, nullLogger())

	user := &auth.User{Email: "alice@example.com", Roles: []string{"Sales User"}}
	ep, err := resolver.Resolve(context.Background(), "site1", user, "SalesOrder")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !ep.HasPerm("read") || !ep.HasPerm("create") {
		t.Error("expected read and create permissions")
	}
	if ep.HasPerm("delete") {
		t.Error("expected delete=false")
	}
}

func TestCachedPermissionResolver_CacheMissThenHit(t *testing.T) {
	_, rc := startMiniredis(t)
	reg := seedRegistry(t, "site1", "SalesOrder", []meta.PermRule{
		{Role: "Sales User", DocTypePerm: int(auth.PermRead)},
	})
	resolver := auth.NewCachedPermissionResolver(reg, rc, nil, nullLogger())
	user := &auth.User{Email: "alice@example.com", Roles: []string{"Sales User"}}
	ctx := context.Background()

	// First call: cache miss, resolves from registry.
	ep1, err := resolver.Resolve(ctx, "site1", user, "SalesOrder")
	if err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	if !ep1.HasPerm("read") {
		t.Error("expected read=true on first call")
	}

	// Verify the cache key was populated.
	key := fmt.Sprintf("perm:%s:%s:%s", "site1", "alice@example.com", "SalesOrder")
	data, err := rc.Get(ctx, key).Bytes()
	if err != nil {
		t.Fatalf("expected cache to be populated: %v", err)
	}
	var cached auth.EffectivePerms
	if err := json.Unmarshal(data, &cached); err != nil {
		t.Fatalf("unmarshal cached: %v", err)
	}
	if !cached.HasPerm("read") {
		t.Error("cached perms should include read")
	}
}

func TestCachedPermissionResolver_CacheHit(t *testing.T) {
	mr, rc := startMiniredis(t)
	_ = mr

	// Create a resolver with NO L1 data — if it touches registry, it fails.
	emptyReg := meta.NewRegistry(nil, nil, nullLogger())
	resolver := auth.NewCachedPermissionResolver(emptyReg, rc, nil, nullLogger())

	// Pre-populate Redis cache.
	ep := &auth.EffectivePerms{DocTypePerm: auth.PermRead | auth.PermWrite}
	data, _ := json.Marshal(ep)
	key := fmt.Sprintf("perm:%s:%s:%s", "site1", "bob@example.com", "Task")
	rc.Set(context.Background(), key, data, 0)

	user := &auth.User{Email: "bob@example.com", Roles: []string{"Worker"}}
	got, err := resolver.Resolve(context.Background(), "site1", user, "Task")
	if err != nil {
		t.Fatalf("Resolve (cache hit): %v", err)
	}
	if !got.HasPerm("read") || !got.HasPerm("write") {
		t.Error("expected read+write from cached perms")
	}
	if got.HasPerm("delete") {
		t.Error("expected delete=false from cached perms")
	}
}

func TestCachedPermissionResolver_InvalidatePermCache(t *testing.T) {
	_, rc := startMiniredis(t)
	reg := seedRegistry(t, "site1", "SalesOrder", []meta.PermRule{
		{Role: "A", DocTypePerm: int(auth.PermRead)},
	})
	resolver := auth.NewCachedPermissionResolver(reg, rc, nil, nullLogger())
	user := &auth.User{Email: "alice@example.com", Roles: []string{"A"}}
	ctx := context.Background()

	// Populate cache.
	if _, err := resolver.Resolve(ctx, "site1", user, "SalesOrder"); err != nil {
		t.Fatal(err)
	}

	// Invalidate.
	if err := resolver.InvalidatePermCache(ctx, "site1", "alice@example.com", "SalesOrder"); err != nil {
		t.Fatalf("InvalidatePermCache: %v", err)
	}

	// Verify key is gone.
	key := fmt.Sprintf("perm:%s:%s:%s", "site1", "alice@example.com", "SalesOrder")
	exists, _ := rc.Exists(ctx, key).Result()
	if exists != 0 {
		t.Error("expected cache key to be deleted")
	}
}

func TestCachedPermissionResolver_InvalidateUserPermCache(t *testing.T) {
	_, rc := startMiniredis(t)
	reg := seedRegistry(t, "site1", "SalesOrder", []meta.PermRule{
		{Role: "A", DocTypePerm: int(auth.PermRead)},
	})
	// Seed a second doctype.
	mt2 := &meta.MetaType{
		Name:        "Task",
		Module:      "core",
		Permissions: []meta.PermRule{{Role: "A", DocTypePerm: int(auth.PermRead)}},
	}
	reg.SeedL1ForTest("site1", "Task", mt2)

	resolver := auth.NewCachedPermissionResolver(reg, rc, nil, nullLogger())
	user := &auth.User{Email: "alice@example.com", Roles: []string{"A"}}
	ctx := context.Background()

	// Populate cache for both doctypes.
	if _, err := resolver.Resolve(ctx, "site1", user, "SalesOrder"); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(ctx, "site1", user, "Task"); err != nil {
		t.Fatal(err)
	}

	// Invalidate all for user.
	if err := resolver.InvalidateUserPermCache(ctx, "site1", "alice@example.com"); err != nil {
		t.Fatalf("InvalidateUserPermCache: %v", err)
	}

	// Both keys should be gone.
	for _, dt := range []string{"SalesOrder", "Task"} {
		key := fmt.Sprintf("perm:%s:%s:%s", "site1", "alice@example.com", dt)
		exists, _ := rc.Exists(ctx, key).Result()
		if exists != 0 {
			t.Errorf("expected cache key for %s to be deleted", dt)
		}
	}
}

func TestCachedPermissionResolver_CustomRuleDenial(t *testing.T) {
	_, rc := startMiniredis(t)
	customRules := auth.NewCustomRuleRegistry()
	_ = customRules.Register("deny_all", func(_ context.Context, _ *auth.User, _ string) error {
		return errors.New("access denied by custom rule")
	})

	reg := seedRegistry(t, "site1", "SalesOrder", []meta.PermRule{
		{Role: "A", DocTypePerm: int(auth.PermRead), CustomRule: "deny_all"},
	})
	resolver := auth.NewCachedPermissionResolver(reg, rc, customRules, nullLogger())
	user := &auth.User{Email: "alice@example.com", Roles: []string{"A"}}

	_, err := resolver.Resolve(context.Background(), "site1", user, "SalesOrder")
	if err == nil {
		t.Fatal("expected error from custom rule denial")
	}

	// The static perms should still be cached despite custom rule denial.
	key := fmt.Sprintf("perm:%s:%s:%s", "site1", "alice@example.com", "SalesOrder")
	exists, _ := rc.Exists(context.Background(), key).Result()
	if exists == 0 {
		t.Error("expected static perms to be cached even when custom rule denies")
	}
}

func TestCachedPermissionResolver_RegistryError(t *testing.T) {
	_, rc := startMiniredis(t)
	// Empty registry — no L1 data, nil DB. Get() will return ErrMetaTypeNotFound.
	emptyReg := meta.NewRegistry(nil, nil, nullLogger())
	resolver := auth.NewCachedPermissionResolver(emptyReg, rc, nil, nullLogger())
	user := &auth.User{Email: "alice@example.com", Roles: []string{"A"}}

	_, err := resolver.Resolve(context.Background(), "site1", user, "Unknown")
	if err == nil {
		t.Fatal("expected error from registry")
	}
}

func TestCachedPermissionResolver_NilRegistry(t *testing.T) {
	resolver := auth.NewCachedPermissionResolver(nil, nil, nil, nullLogger())
	user := &auth.User{Email: "test@example.com", Roles: []string{"A"}}

	_, err := resolver.Resolve(context.Background(), "site1", user, "DocType")
	if err == nil {
		t.Fatal("expected error for nil registry")
	}
}

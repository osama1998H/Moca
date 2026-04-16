# Security Remediation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix all 10 security findings (F1–F10) plus hardening items (H1–H3) from three security reviews, delivered as a single PR.

**Architecture:** Dev handler gets a role-checking middleware, input validators, and path containment. SAML gets audience enforcement. SSO handler gets encryption enforcement. `moca init` defaults change. All changes are in existing files except one new integration test file.

**Tech Stack:** Go 1.26+, `net/http`, `slices`, `filepath`, `regexp`, `pkg/auth`, `pkg/api`, `pkg/meta`

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `pkg/api/dev_handler.go` | Modify | Auth middleware, path containment, body limit, error sanitization, ReadDir error handling |
| `pkg/api/dev_validation.go` | Modify | `ValidateAppName`, `ValidateModuleName`, field type allowlist |
| `internal/serve/server.go` | Modify | Pass dev middleware, startup SSO + dev mode warnings |
| `pkg/auth/saml.go` | Modify | Audience fix (line 152) |
| `pkg/api/sso_handler.go` | Modify | Encryption enforcement in `loadAndDecryptConfig` |
| `cmd/moca/init.go` | Modify | Default `DeveloperMode: false`, `--dev` flag |
| `pkg/api/dev_handler_test.go` | Modify | Unit tests for middleware, path containment, body limit, error sanitization |
| `pkg/api/dev_validation_test.go` | Modify | Tests for new validators + field type allowlist |
| `pkg/auth/saml_test.go` | Modify | Audience validation test |
| `pkg/api/sso_handler_test.go` | Modify | Encryption enforcement tests |
| `pkg/api/dev_api_integration_test.go` | Create | End-to-end tests for auth + path traversal through full middleware chain |

---

### Task 1: Dev Auth Middleware + Route Wrapping (F1)

**Files:**
- Modify: `pkg/api/dev_handler.go:1-40`
- Modify: `internal/serve/server.go:353-358`
- Test: `pkg/api/dev_handler_test.go`

- [ ] **Step 1: Write failing tests for devAuthMiddleware**

Add to `pkg/api/dev_handler_test.go`:

```go
func TestDevAuthMiddleware_RejectsNilUser(t *testing.T) {
	mw := api.DevAuthMiddleware()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})
	handler := mw(inner)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestDevAuthMiddleware_RejectsGuestUser(t *testing.T) {
	mw := api.DevAuthMiddleware()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})
	handler := mw(inner)

	req := httptest.NewRequest("GET", "/", nil)
	ctx := api.WithUser(req.Context(), &auth.User{Email: "Guest", Roles: []string{"Guest"}})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestDevAuthMiddleware_RejectsNonAdmin(t *testing.T) {
	mw := api.DevAuthMiddleware()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})
	handler := mw(inner)

	req := httptest.NewRequest("GET", "/", nil)
	ctx := api.WithUser(req.Context(), &auth.User{Email: "user@test.com", Roles: []string{"Editor"}})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestDevAuthMiddleware_AllowsAdmin(t *testing.T) {
	mw := api.DevAuthMiddleware()
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := mw(inner)

	req := httptest.NewRequest("GET", "/", nil)
	ctx := api.WithUser(req.Context(), &auth.User{Email: "admin@test.com", Roles: []string{"Administrator"}})
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !called {
		t.Fatal("expected inner handler to be called")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -race -run TestDevAuthMiddleware ./pkg/api/...`
Expected: compilation error — `api.DevAuthMiddleware` undefined

- [ ] **Step 3: Implement devAuthMiddleware and refactor RegisterDevRoutes**

In `pkg/api/dev_handler.go`, add import `"slices"` and add before `RegisterDevRoutes`:

```go
// DevAuthMiddleware returns middleware that requires the Administrator role
// for all dev API endpoints. It reads the user from request context (set by
// the global authMiddleware) and returns 403 if the user is nil or lacks
// the Administrator role.
func DevAuthMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := UserFromContext(r.Context())
			if user == nil || !slices.Contains(user.Roles, "Administrator") {
				writeJSON(w, http.StatusForbidden, map[string]string{
					"error": "developer API requires Administrator role",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
```

Replace the existing `RegisterDevRoutes` method (lines 32-40):

```go
// RegisterDevRoutes registers dev-mode routes on the given mux.
// Optional middleware is applied to each handler (outermost first).
func (h *DevHandler) RegisterDevRoutes(mux *http.ServeMux, version string, mw ...func(http.Handler) http.Handler) {
	wrap := func(hf http.HandlerFunc) http.Handler {
		var handler http.Handler = hf
		for i := len(mw) - 1; i >= 0; i-- {
			handler = mw[i](handler)
		}
		return handler
	}
	p := "/api/" + version + "/dev"
	mux.Handle("GET "+p+"/apps", wrap(h.HandleListApps))
	mux.Handle("POST "+p+"/doctype", wrap(h.HandleCreateDocType))
	mux.Handle("PUT "+p+"/doctype/{name}", wrap(h.HandleUpdateDocType))
	mux.Handle("GET "+p+"/doctype/{name}", wrap(h.HandleGetDocType))
	mux.Handle("DELETE "+p+"/doctype/{name}", wrap(h.HandleDeleteDocType))
}
```

In `internal/serve/server.go`, replace line 356:

```go
devHandler.RegisterDevRoutes(gw.Mux(), "v1", api.DevAuthMiddleware())
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race -run TestDevAuthMiddleware ./pkg/api/...`
Expected: all 4 tests PASS

- [ ] **Step 5: Verify existing dev handler tests still pass**

Run: `go test -race -run TestDevHandler ./pkg/api/...`
Expected: all existing tests PASS (they call handlers directly, bypassing middleware)

- [ ] **Step 6: Commit**

```bash
git add pkg/api/dev_handler.go pkg/api/dev_handler_test.go internal/serve/server.go
git commit -m "fix(security): add Administrator role check to dev API routes (F1)

Dev routes were callable by any user including unauthenticated Guest.
Add devAuthMiddleware that requires the Administrator role, applied
to all five /api/v1/dev/* routes via RegisterDevRoutes."
```

---

### Task 2: Path Traversal Prevention — Input Validators (F2, Layer 1)

**Files:**
- Modify: `pkg/api/dev_validation.go`
- Modify: `pkg/api/dev_validation_test.go`

- [ ] **Step 1: Write failing tests for ValidateAppName and ValidateModuleName**

Add to `pkg/api/dev_validation_test.go`:

```go
// ── ValidateAppName ────────────────────────────────────────────────────────

func TestValidateAppName_Valid(t *testing.T) {
	cases := []string{"core", "my-app", "app_v2", "a", "test123"}
	for _, name := range cases {
		if err := ValidateAppName(name); err != nil {
			t.Errorf("ValidateAppName(%q) returned unexpected error: %v", name, err)
		}
	}
}

func TestValidateAppName_Invalid(t *testing.T) {
	cases := []struct {
		name string
		desc string
	}{
		{"", "empty"},
		{"../../etc", "path traversal"},
		{"foo/bar", "contains slash"},
		{".hidden", "starts with dot"},
		{"MyApp", "contains uppercase"},
		{"123app", "starts with digit"},
		{"app name", "contains space"},
	}
	for _, tc := range cases {
		if err := ValidateAppName(tc.name); err == nil {
			t.Errorf("ValidateAppName(%q) expected error for %s, got nil", tc.name, tc.desc)
		}
	}
}

// ── ValidateModuleName ────────────────────────────────────────────────────

func TestValidateModuleName_Valid(t *testing.T) {
	cases := []string{"core", "selling", "hr_module", "mod-1"}
	for _, name := range cases {
		if err := ValidateModuleName(name); err != nil {
			t.Errorf("ValidateModuleName(%q) returned unexpected error: %v", name, err)
		}
	}
}

func TestValidateModuleName_Invalid(t *testing.T) {
	cases := []struct {
		name string
		desc string
	}{
		{"", "empty"},
		{"../etc", "path traversal"},
		{"foo/bar", "contains slash"},
		{".hidden", "starts with dot"},
		{"Selling", "contains uppercase"},
	}
	for _, tc := range cases {
		if err := ValidateModuleName(tc.name); err == nil {
			t.Errorf("ValidateModuleName(%q) expected error for %s, got nil", tc.name, tc.desc)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -race -run "TestValidateAppName|TestValidateModuleName" ./pkg/api/...`
Expected: compilation error — `ValidateAppName` undefined

- [ ] **Step 3: Implement validators**

Add to `pkg/api/dev_validation.go` after the `reFieldName` declaration (line 13):

```go
// reAppModuleName matches valid app and module names: lowercase letter start,
// followed by lowercase letters, digits, underscores, or hyphens.
// This prevents path traversal by disallowing dots, slashes, and other special chars.
var reAppModuleName = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// ValidateAppName checks that name is a valid app directory name.
func ValidateAppName(name string) error {
	if !reAppModuleName.MatchString(name) {
		return errors.New("app name must match ^[a-z][a-z0-9_-]*$ (lowercase, digits, hyphens, underscores)")
	}
	return nil
}

// ValidateModuleName checks that name is a valid module directory name.
func ValidateModuleName(name string) error {
	if !reAppModuleName.MatchString(name) {
		return errors.New("module name must match ^[a-z][a-z0-9_-]*$ (lowercase, digits, hyphens, underscores)")
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race -run "TestValidateAppName|TestValidateModuleName" ./pkg/api/...`
Expected: all tests PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/api/dev_validation.go pkg/api/dev_validation_test.go
git commit -m "fix(security): add ValidateAppName and ValidateModuleName (F2, layer 1)

Strict regex prevents path traversal characters (dots, slashes) in
app and module names used in filepath.Join."
```

---

### Task 3: Path Traversal Prevention — Containment Check + Handler Updates (F2, Layer 2)

**Files:**
- Modify: `pkg/api/dev_handler.go:117-247`
- Test: `pkg/api/dev_handler_test.go`

- [ ] **Step 1: Write failing tests for path traversal rejection and containment**

Add to `pkg/api/dev_handler_test.go`:

```go
func TestDevHandler_CreateDocType_PathTraversal_App(t *testing.T) {
	dir := t.TempDir()
	h := api.NewDevHandler(dir, nil, nil)

	body := map[string]any{
		"name":        "Exploit",
		"app":         "../../etc",
		"module":      "core",
		"layout":      map[string]any{"tabs": []any{}},
		"fields":      map[string]any{"title": map[string]any{"field_type": "Data", "name": "title"}},
		"settings":    map[string]any{},
		"permissions": []any{},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/v1/dev/doctype", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.HandleCreateDocType(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for path traversal in app, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDevHandler_CreateDocType_PathTraversal_Module(t *testing.T) {
	dir := t.TempDir()
	h := api.NewDevHandler(dir, nil, nil)

	body := map[string]any{
		"name":        "Exploit",
		"app":         "testapp",
		"module":      "../../../etc",
		"layout":      map[string]any{"tabs": []any{}},
		"fields":      map[string]any{"title": map[string]any{"field_type": "Data", "name": "title"}},
		"settings":    map[string]any{},
		"permissions": []any{},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/api/v1/dev/doctype", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.HandleCreateDocType(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for path traversal in module, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDevHandler_UpdateDocType_ValidatesNameFromURL(t *testing.T) {
	dir := t.TempDir()
	h := api.NewDevHandler(dir, nil, nil)

	body := map[string]any{
		"app":         "testapp",
		"module":      "core",
		"layout":      map[string]any{"tabs": []any{}},
		"fields":      map[string]any{},
		"settings":    map[string]any{},
		"permissions": []any{},
	}
	bodyBytes, _ := json.Marshal(body)

	mux := http.NewServeMux()
	h.RegisterDevRoutes(mux, "v1")

	// ".." is not a valid DocType name
	req := httptest.NewRequest("PUT", "/api/v1/dev/doctype/..", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid name from URL, got %d: %s", w.Code, w.Body.String())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -race -run "TestDevHandler_CreateDocType_PathTraversal|TestDevHandler_UpdateDocType_ValidatesName" ./pkg/api/...`
Expected: FAIL — path traversal app gives 201 (writes file), URL name gives 404

- [ ] **Step 3: Implement ensureInsideAppsDir and update handlers**

Add to `pkg/api/dev_handler.go`, add `"errors"` and `"strings"` to imports, then add after `NewDevHandler`:

```go
// ensureInsideAppsDir verifies that target resolves to a path under h.appsDir.
// Returns an error if the resolved path escapes the apps directory.
func (h *DevHandler) ensureInsideAppsDir(target string) error {
	abs, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	base, err := filepath.Abs(h.appsDir)
	if err != nil {
		return fmt.Errorf("resolve appsDir: %w", err)
	}
	rel, err := filepath.Rel(base, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return errors.New("path escapes apps directory")
	}
	return nil
}
```

In `HandleCreateDocType`, replace lines 130-137 (the emptiness checks for App/Module) with:

```go
	if err := ValidateAppName(req.App); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := ValidateModuleName(req.Module); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
```

After `dtDir` is built (after line 159), add:

```go
	if err := h.ensureInsideAppsDir(dtDir); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}
```

In `HandleUpdateDocType`, add after `req.Name = name` (line 205):

```go
	if err := ValidateDocTypeName(name); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := ValidateAppName(req.App); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := ValidateModuleName(req.Module); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
```

After `jsonPath` is built (after line 223), add:

```go
	if err := h.ensureInsideAppsDir(jsonPath); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race -run "TestDevHandler_CreateDocType_PathTraversal|TestDevHandler_UpdateDocType_ValidatesName" ./pkg/api/...`
Expected: all 3 tests PASS

- [ ] **Step 5: Verify all existing tests still pass**

Run: `go test -race ./pkg/api/...`
Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add pkg/api/dev_handler.go pkg/api/dev_handler_test.go
git commit -m "fix(security): add path containment check to dev handlers (F2, layer 2)

ensureInsideAppsDir verifies resolved paths stay under appsDir.
ValidateAppName/ValidateModuleName replace bare emptiness checks.
HandleUpdateDocType now validates the URL-sourced name parameter."
```

---

### Task 4: SAML Audience Fix (F3)

**Files:**
- Modify: `pkg/auth/saml.go:152`
- Test: `pkg/auth/saml_test.go`

- [ ] **Step 1: Write failing test**

Add to `pkg/auth/saml_test.go`:

```go
func TestSAMLProvider_ParseResponse_UsesEntityIDAudience(t *testing.T) {
	// Verify the SP is configured with the correct audience restriction.
	// We can't easily test a full SAML response parse without a real IdP,
	// but we can verify the EntityID is set correctly and not empty.
	certPEM, keyPEM := generateTestCertAndKey(t)

	cfg := &SSOProviderConfig{
		IdPEntityID:    "https://idp.example.com",
		IdPSSOURL:      "https://idp.example.com/sso",
		IdPCertificate: certPEM,
		SPCertificate:  certPEM,
		SPPrivateKey:   keyPEM,
	}

	metadataURL := "https://app.example.com/api/v1/auth/saml/metadata?provider=test"
	sp, err := NewSAMLProvider(cfg, metadataURL,
		"https://app.example.com/api/v1/auth/saml/acs?provider=test")
	if err != nil {
		t.Fatalf("NewSAMLProvider: %v", err)
	}

	// The entity ID must match the metadata URL — this is what gets passed
	// as the allowed audience to ParseResponse.
	if sp.sp.EntityID != metadataURL {
		t.Errorf("EntityID = %q, want %q", sp.sp.EntityID, metadataURL)
	}
}
```

- [ ] **Step 2: Run test to verify it passes (this tests the precondition)**

Run: `go test -race -run TestSAMLProvider_ParseResponse_UsesEntityIDAudience ./pkg/auth/...`
Expected: PASS (EntityID is already set correctly — the bug is that ParseResponse doesn't use it)

- [ ] **Step 3: Fix the audience bypass**

In `pkg/auth/saml.go`, replace line 152:

```go
	assertion, err := p.sp.ParseResponse(r, []string{p.sp.EntityID})
```

- [ ] **Step 4: Verify all SAML tests pass**

Run: `go test -race ./pkg/auth/...`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/auth/saml.go pkg/auth/saml_test.go
git commit -m "fix(security): enforce SAML audience restriction (F3)

Pass p.sp.EntityID as allowed audience to ParseResponse instead of
empty string. Prevents cross-SP SAML assertion replay attacks."
```

---

### Task 5: SSO Encryption Enforcement (F4 + F5)

**Files:**
- Modify: `pkg/api/sso_handler.go:424-454`
- Modify: `internal/serve/server.go` (startup warning)
- Test: `pkg/api/sso_handler_test.go`

- [ ] **Step 1: Write failing tests for encryption enforcement**

Add to `pkg/api/sso_handler_test.go`:

```go
func TestSSOHandler_LoadAndDecryptConfig_RejectsPlaintextWithoutEncryptor(t *testing.T) {
	configs := map[string]*auth.SSOProviderConfig{
		"google": {
			ProviderName: "google",
			ProviderType: "OAuth2",
			ClientID:     "id",
			ClientSecret: "plaintext-secret",
			AuthorizeURL: "https://accounts.google.com/o/oauth2/auth",
			TokenURL:     "https://oauth2.googleapis.com/token",
		},
	}
	env := newSSOTestEnv(t, mockConfigLoader(configs))

	// Try to authorize — the handler calls loadAndDecryptConfig internally.
	req := env.makeRequest("GET", "/api/v1/auth/sso/authorize?provider=google")
	w := httptest.NewRecorder()
	env.mux.ServeHTTP(w, req)

	// Should fail because encryptor is nil but secrets exist.
	// The handler redirects to login with error on SSO failure.
	if w.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "sso_failed") {
		t.Fatalf("expected error redirect, got Location: %s", loc)
	}
}

func TestSSOHandler_LoadAndDecryptConfig_RejectsUnencryptedWithEncryptor(t *testing.T) {
	configs := map[string]*auth.SSOProviderConfig{
		"google": {
			ProviderName: "google",
			ProviderType: "OAuth2",
			ClientID:     "id",
			ClientSecret: "plaintext-not-encrypted",
			AuthorizeURL: "https://accounts.google.com/o/oauth2/auth",
			TokenURL:     "https://oauth2.googleapis.com/token",
		},
	}

	// Create test env with encryptor
	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mini.Close)

	redisClient := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	sessionMgr := auth.NewSessionManager(redisClient, 24*time.Hour)
	provisioner := auth.NewUserProvisioner(slog.Default())

	// Create a real encryptor
	encryptor, err := auth.NewFieldEncryptor(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatalf("NewFieldEncryptor: %v", err)
	}

	handler := NewSSOHandler(sessionMgr, provisioner,
		mockConfigLoader(configs), encryptor, redisClient, slog.Default())

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, "v1")

	site := &tenancy.SiteContext{Name: "test-site"}
	req := httptest.NewRequest("GET", "/api/v1/auth/sso/authorize?provider=google", nil)
	ctx := WithSite(req.Context(), site)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Should fail because secret is not encrypted (no enc:v1: prefix).
	if w.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "sso_failed") {
		t.Fatalf("expected error redirect, got Location: %s", loc)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -race -run "TestSSOHandler_LoadAndDecryptConfig_Rejects" ./pkg/api/...`
Expected: FAIL — first test passes (encryptor=nil just skips decryption, redirect happens for different reason) or test fails asserting sso_failed in the redirect

- [ ] **Step 3: Implement encryption enforcement**

In `pkg/api/sso_handler.go`, replace the `loadAndDecryptConfig` method (lines 424-454):

```go
// loadAndDecryptConfig loads an SSO provider config and decrypts Password fields.
func (h *SSOHandler) loadAndDecryptConfig(
	ctx context.Context,
	site *tenancy.SiteContext,
	providerName string,
) (*auth.SSOProviderConfig, error) {
	cfg, err := h.loadConfig(ctx, site, providerName)
	if err != nil {
		return nil, err
	}

	// Reject plaintext secrets when no encryption key is configured.
	if h.encryptor == nil {
		if cfg.ClientSecret != "" || cfg.SPPrivateKey != "" {
			return nil, fmt.Errorf("SSO provider %q has secrets but MOCA_ENCRYPTION_KEY is not configured; "+
				"set the encryption key and re-save the provider", cfg.ProviderName)
		}
		return cfg, nil
	}

	// Reject secrets that are not encrypted (plaintext migration gap).
	if cfg.ClientSecret != "" && !auth.IsEncrypted(cfg.ClientSecret) {
		return nil, fmt.Errorf("SSO provider %q: client_secret is not encrypted; "+
			"re-save the provider to encrypt it", cfg.ProviderName)
	}
	if cfg.SPPrivateKey != "" && !auth.IsEncrypted(cfg.SPPrivateKey) {
		return nil, fmt.Errorf("SSO provider %q: sp_private_key is not encrypted; "+
			"re-save the provider to encrypt it", cfg.ProviderName)
	}

	// Decrypt Password-type fields.
	if cfg.ClientSecret != "" {
		cfg.ClientSecret, err = h.encryptor.Decrypt(cfg.ClientSecret)
		if err != nil {
			return nil, fmt.Errorf("decrypt client_secret: %w", err)
		}
	}
	if cfg.SPPrivateKey != "" {
		cfg.SPPrivateKey, err = h.encryptor.Decrypt(cfg.SPPrivateKey)
		if err != nil {
			return nil, fmt.Errorf("decrypt sp_private_key: %w", err)
		}
	}

	return cfg, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race -run "TestSSOHandler_LoadAndDecryptConfig_Rejects" ./pkg/api/...`
Expected: both tests PASS

- [ ] **Step 5: Verify all SSO tests still pass**

Run: `go test -race ./pkg/api/...`
Expected: all PASS

- [ ] **Step 6: Add startup warning for unencrypted secrets in server.go**

In `internal/serve/server.go`, after the `fieldEncryptor` setup block (after line 210), add:

```go
	// ── Startup check: detect unencrypted SSO secrets ───────────────────
	if fieldEncryptor != nil && dbManager.SystemPool() != nil {
		var unencProviders []string
		rows, qErr := dbManager.SystemPool().Query(ctx,
			`SELECT "name" FROM "tab_sso_provider"
			 WHERE ("client_secret" != '' AND "client_secret" NOT LIKE 'enc:v1:%')
			    OR ("sp_private_key" != '' AND "sp_private_key" NOT LIKE 'enc:v1:%')`)
		if qErr == nil {
			defer rows.Close()
			for rows.Next() {
				var name string
				if rows.Scan(&name) == nil {
					unencProviders = append(unencProviders, name)
				}
			}
		}
		// Ignore query errors (table may not exist yet).
		if len(unencProviders) > 0 {
			logger.Warn("unencrypted SSO secrets detected — re-save these providers to encrypt them",
				slog.String("providers", strings.Join(unencProviders, ", ")))
		}
	}
```

- [ ] **Step 7: Commit**

```bash
git add pkg/api/sso_handler.go pkg/api/sso_handler_test.go internal/serve/server.go
git commit -m "fix(security): enforce encryption for SSO secrets (F4+F5)

Reject SSO provider configs with plaintext secrets — both when no
encryption key is set (F4) and when the key exists but secrets are
not yet encrypted (F5). Add startup warning for migration detection."
```

---

### Task 6: DeveloperMode Default + Startup Warning (F6 + H2 + H3)

**Files:**
- Modify: `cmd/moca/init.go:272-277`
- Modify: `internal/serve/server.go:353-358`

- [ ] **Step 1: Change default to DeveloperMode: false**

In `cmd/moca/init.go`, change line 276 from `DeveloperMode: true` to:

```go
			DeveloperMode: false,
```

- [ ] **Step 2: Add --dev flag to init command**

In `cmd/moca/init.go`, in the `NewInitCommand` function after the existing flags (around line 50), add:

```go
	f.Bool("dev", false, "Enable developer mode in generated config")
```

In the `defaultProjectConfig` function (or wherever the config is assembled from flags), read the flag and apply it. Find where `DeveloperMode` is set in the generated config and replace the hardcoded value with the flag value. If the config is built in `defaultProjectConfig()` which doesn't take flags, the flag needs to be applied in `runInit` after calling `defaultProjectConfig()`:

```go
	// In runInit, after cfg := defaultProjectConfig(name):
	if devMode, _ := cmd.Flags().GetBool("dev"); devMode {
		cfg.Development.DeveloperMode = true
	}
```

- [ ] **Step 3: Add dev mode startup warning in server.go**

In `internal/serve/server.go`, after the dev routes block (after line 358), add:

```go
		// Warn if dev mode is exposed on a non-loopback address.
		bindAddr := cfg.Host
		if bindAddr == "" || bindAddr == "0.0.0.0" || bindAddr == "::" {
			logger.Warn("developer mode is enabled on a non-loopback address; "+
				"dev API routes are exposed to the network — this is unsafe for production",
				slog.String("bind", bindAddr),
				slog.Int("port", cfg.Port),
			)
		}
```

This should be inside the existing `if cfg.Config.Development.DeveloperMode` block.

- [ ] **Step 4: Verify compilation**

Run: `go build ./cmd/moca/... && go build ./internal/serve/...`
Expected: builds successfully

- [ ] **Step 5: Commit**

```bash
git add cmd/moca/init.go internal/serve/server.go
git commit -m "fix(security): default DeveloperMode to false, add startup warning (F6)

moca init now generates DeveloperMode: false. Use --dev flag to opt in.
Server warns at startup if dev mode is enabled on a non-loopback address."
```

---

### Task 7: Error Sanitization + Body Limit + ReadDir Error Handling (F7 + F9 + F10)

**Files:**
- Modify: `pkg/api/dev_handler.go`
- Test: `pkg/api/dev_handler_test.go`

- [ ] **Step 1: Write failing test for path leak in error response**

Add to `pkg/api/dev_handler_test.go`:

```go
func TestDevHandler_UpdateDocType_ErrorDoesNotLeakPath(t *testing.T) {
	dir := t.TempDir()
	h := api.NewDevHandler(dir, nil, nil)

	body := map[string]any{
		"app":         "testapp",
		"module":      "selling",
		"layout":      map[string]any{"tabs": []any{}},
		"fields":      map[string]any{},
		"settings":    map[string]any{},
		"permissions": []any{},
	}
	bodyBytes, _ := json.Marshal(body)

	mux := http.NewServeMux()
	h.RegisterDevRoutes(mux, "v1")

	req := httptest.NewRequest("PUT", "/api/v1/dev/doctype/NonExistent", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}

	// The error response must NOT contain filesystem paths.
	body404 := w.Body.String()
	if strings.Contains(body404, dir) {
		t.Fatalf("error response leaks filesystem path: %s", body404)
	}
	if strings.Contains(body404, "modules") && strings.Contains(body404, "doctypes") {
		t.Fatalf("error response leaks internal path structure: %s", body404)
	}
}

func TestDevHandler_CreateDocType_BodySizeLimit(t *testing.T) {
	dir := t.TempDir()
	h := api.NewDevHandler(dir, nil, nil)

	// Create a body larger than 1 MiB.
	bigField := strings.Repeat("x", 2<<20) // 2 MiB
	body := `{"name":"Test","app":"testapp","module":"core","fields":{"f":{"field_type":"` + bigField + `"}}}`

	req := httptest.NewRequest("POST", "/api/v1/dev/doctype", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.HandleCreateDocType(w, req)

	// Should be rejected — either 400 (bad request) or 413 (too large).
	if w.Code == http.StatusCreated {
		t.Fatalf("expected request to be rejected for oversized body, got 201")
	}
}
```

Add `"strings"` to the import block if not already present.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -race -run "TestDevHandler_UpdateDocType_ErrorDoesNotLeakPath|TestDevHandler_CreateDocType_BodySizeLimit" ./pkg/api/...`
Expected: path leak test FAILS (response contains full path), body limit test might FAIL (201 returned)

- [ ] **Step 3: Implement all three fixes in dev_handler.go**

**F9 — Body size limit.** At the top of `HandleCreateDocType` (after the function signature, before json decode), add:

```go
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB
```

Do the same at the top of `HandleUpdateDocType`.

**F7 — Error sanitization.** Make these replacements in `dev_handler.go`:

Line 51 — `HandleListApps` error:
```go
		h.logger.Debug("read apps directory failed", slog.String("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
```

Line 161 — `HandleCreateDocType` mkdir error:
```go
		h.logger.Debug("create doctype directory failed", slog.String("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create directory"})
```

Line 173 — `HandleCreateDocType` write error:
```go
		h.logger.Debug("write doctype file failed", slog.String("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to write doctype"})
```

Line 226 — `HandleUpdateDocType` not found:
```go
		h.logger.Debug("doctype not found", slog.String("path", jsonPath))
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "doctype not found"})
```

Line 301 — `HandleDeleteDocType` RemoveAll error:
```go
					h.logger.Debug("delete doctype failed", slog.String("error", err.Error()))
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
```

**F10 — ReadDir error handling.** In `HandleGetDocType`, replace `entries, _ := os.ReadDir(h.appsDir)` (line 254) with:

```go
	entries, err := os.ReadDir(h.appsDir)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "doctype not found"})
			return
		}
		h.logger.Error("read apps directory", slog.String("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
```

Replace `modules, _ := os.ReadDir(modulesDir)` (line 260) with:

```go
		modules, err := os.ReadDir(modulesDir)
		if err != nil {
			h.logger.Debug("read modules directory failed", slog.String("path", modulesDir), slog.String("error", err.Error()))
			continue
		}
```

Apply the same two patterns in `HandleDeleteDocType` (lines 287 and 293).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race -run "TestDevHandler_UpdateDocType_ErrorDoesNotLeakPath|TestDevHandler_CreateDocType_BodySizeLimit" ./pkg/api/...`
Expected: both PASS

- [ ] **Step 5: Run all dev handler tests**

Run: `go test -race -run TestDevHandler ./pkg/api/...`
Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add pkg/api/dev_handler.go pkg/api/dev_handler_test.go
git commit -m "fix(security): sanitize errors, add body limit, handle ReadDir errors (F7+F9+F10)

Strip filesystem paths from API error responses — log at DEBUG.
Add 1 MiB MaxBytesReader to create/update handlers.
Check and log os.ReadDir errors instead of silently discarding them."
```

---

### Task 8: ValidateFieldDefs Allowlist Check (F8)

**Files:**
- Modify: `pkg/api/dev_validation.go:82-90`
- Test: `pkg/api/dev_validation_test.go`

- [ ] **Step 1: Write failing test**

Add to `pkg/api/dev_validation_test.go`:

```go
func TestValidateFieldDefs_UnrecognizedFieldType(t *testing.T) {
	fields := map[string]meta.FieldDef{
		"title": {FieldType: "Data", Name: "title"},
		"bad":   {FieldType: "NotAType", Name: "bad"},
	}
	err := ValidateFieldDefs(fields)
	if err == nil {
		t.Error("ValidateFieldDefs expected error for unrecognized field_type, got nil")
	}
}

func TestValidateFieldDefs_AllValidTypes(t *testing.T) {
	// A sample of known valid types should pass.
	fields := map[string]meta.FieldDef{
		"f1": {FieldType: "Data", Name: "f1"},
		"f2": {FieldType: "Int", Name: "f2"},
		"f3": {FieldType: "Currency", Name: "f3"},
		"f4": {FieldType: "Date", Name: "f4"},
		"f5": {FieldType: "Link", Name: "f5"},
	}
	if err := ValidateFieldDefs(fields); err != nil {
		t.Errorf("ValidateFieldDefs returned unexpected error: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -race -run TestValidateFieldDefs_UnrecognizedFieldType ./pkg/api/...`
Expected: FAIL — error is nil

- [ ] **Step 3: Add IsValid() check**

In `pkg/api/dev_validation.go`, add `"fmt"` to imports and replace `ValidateFieldDefs` (lines 82-90):

```go
// ValidateFieldDefs checks that every field has a non-empty, recognized field_type.
func ValidateFieldDefs(fields map[string]meta.FieldDef) error {
	for name, fd := range fields {
		if fd.FieldType == "" {
			return errors.New("field '" + name + "' has no field_type")
		}
		if !meta.FieldType(fd.FieldType).IsValid() {
			return fmt.Errorf("field '%s' has unrecognized field_type %q", name, fd.FieldType)
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race -run TestValidateFieldDefs ./pkg/api/...`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/api/dev_validation.go pkg/api/dev_validation_test.go
git commit -m "fix(security): validate field types against allowlist (F8)

ValidateFieldDefs now checks meta.FieldType.IsValid() to reject
unrecognized field types before writing them to disk."
```

---

### Task 9: Integration Tests

**Files:**
- Create: `pkg/api/dev_api_integration_test.go`

- [ ] **Step 1: Write integration tests**

Create `pkg/api/dev_api_integration_test.go`:

```go
package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/osama1998H/moca/pkg/api"
	"github.com/osama1998H/moca/pkg/auth"
)

// setupDevMux creates a mux with dev routes wrapped by DevAuthMiddleware,
// simulating the production server.go configuration.
func setupDevMux(t *testing.T) (*http.ServeMux, string) {
	t.Helper()
	dir := t.TempDir()
	h := api.NewDevHandler(dir, nil, nil)
	mux := http.NewServeMux()
	h.RegisterDevRoutes(mux, "v1", api.DevAuthMiddleware())
	return mux, dir
}

func devRequest(method, path string, body any) *http.Request {
	var req *http.Request
	if body != nil {
		data, _ := json.Marshal(body)
		req = httptest.NewRequest(method, path, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	return req
}

func withAdminCtx(r *http.Request) *http.Request {
	ctx := api.WithUser(r.Context(), &auth.User{
		Email: "admin@test.com",
		Roles: []string{"Administrator"},
	})
	return r.WithContext(ctx)
}

func withGuestCtx(r *http.Request) *http.Request {
	ctx := api.WithUser(r.Context(), &auth.User{
		Email: "Guest",
		Roles: []string{"Guest"},
	})
	return r.WithContext(ctx)
}

func validDocTypeBody() map[string]any {
	return map[string]any{
		"name":   "IntegTest",
		"app":    "testapp",
		"module": "core",
		"layout": map[string]any{"tabs": []any{}},
		"fields": map[string]any{
			"title": map[string]any{"field_type": "Data", "name": "title"},
		},
		"settings":    map[string]any{},
		"permissions": []any{},
	}
}

// ── Auth enforcement ──────────────────────────────────────────────────────

func TestIntegration_DevAPI_NoUser_Returns403(t *testing.T) {
	mux, _ := setupDevMux(t)

	req := devRequest("POST", "/api/v1/dev/doctype", validDocTypeBody())
	// No user context — simulates unauthenticated request.
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for no user, got %d: %s", w.Code, w.Body.String())
	}
}

func TestIntegration_DevAPI_GuestUser_Returns403(t *testing.T) {
	mux, _ := setupDevMux(t)

	req := withGuestCtx(devRequest("POST", "/api/v1/dev/doctype", validDocTypeBody()))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for Guest, got %d: %s", w.Code, w.Body.String())
	}
}

func TestIntegration_DevAPI_Admin_Creates(t *testing.T) {
	mux, dir := setupDevMux(t)

	req := withAdminCtx(devRequest("POST", "/api/v1/dev/doctype", validDocTypeBody()))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for admin, got %d: %s", w.Code, w.Body.String())
	}

	// Verify file on disk.
	jsonPath := filepath.Join(dir, "testapp", "modules", "core", "doctypes", "integ_test", "integ_test.json")
	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		t.Fatal("expected doctype file to be created on disk")
	}
}

func TestIntegration_DevAPI_PathTraversal_Returns400(t *testing.T) {
	mux, _ := setupDevMux(t)

	body := validDocTypeBody()
	body["app"] = "../../etc"

	req := withAdminCtx(devRequest("POST", "/api/v1/dev/doctype", body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for path traversal, got %d: %s", w.Code, w.Body.String())
	}
}

func TestIntegration_DevAPI_Delete_NonAdmin_Returns403(t *testing.T) {
	mux, _ := setupDevMux(t)

	req := withGuestCtx(devRequest("DELETE", "/api/v1/dev/doctype/SomeType", nil))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for Guest delete, got %d: %s", w.Code, w.Body.String())
	}
}

func TestIntegration_DevAPI_FullRoundTrip(t *testing.T) {
	mux, _ := setupDevMux(t)

	// Create
	req := withAdminCtx(devRequest("POST", "/api/v1/dev/doctype", validDocTypeBody()))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Get
	req = withAdminCtx(devRequest("GET", "/api/v1/dev/doctype/IntegTest", nil))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Update
	updateBody := validDocTypeBody()
	updateBody["fields"] = map[string]any{
		"title":       map[string]any{"field_type": "Data", "name": "title"},
		"description": map[string]any{"field_type": "Text", "name": "description"},
	}
	req = withAdminCtx(devRequest("PUT", "/api/v1/dev/doctype/IntegTest", updateBody))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Delete
	req = withAdminCtx(devRequest("DELETE", "/api/v1/dev/doctype/IntegTest", nil))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify deleted
	req = withAdminCtx(devRequest("GET", "/api/v1/dev/doctype/IntegTest", nil))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("get after delete: expected 404, got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run integration tests**

Run: `go test -race -run TestIntegration_DevAPI ./pkg/api/...`
Expected: all 6 tests PASS

- [ ] **Step 3: Commit**

```bash
git add pkg/api/dev_api_integration_test.go
git commit -m "test: add integration tests for dev API security (auth + path traversal)

Full stack tests covering: no-user 403, guest 403, admin 201,
path traversal 400, delete non-admin 403, CRUD round-trip."
```

---

### Task 10: Final Verification

- [ ] **Step 1: Run full test suite**

Run: `make test`
Expected: all tests PASS with race detector

- [ ] **Step 2: Run linter**

Run: `make lint`
Expected: no new warnings

- [ ] **Step 3: Run integration tests**

Run: `make test-integration`
Expected: all tests PASS (requires Docker)

- [ ] **Step 4: Verify compilation of all binaries**

Run: `make build`
Expected: all 5 binaries build successfully

- [ ] **Step 5: Final commit if any loose changes**

```bash
git status
# If any uncommitted fixes, commit them.
```

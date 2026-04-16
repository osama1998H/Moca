package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/osama1998H/moca/pkg/meta"
	"gopkg.in/yaml.v3"
)

// DevHandler serves dev-mode API endpoints for creating/editing DocType
// definition files on disk. Only available when developer mode is enabled.
type DevHandler struct {
	registry *meta.Registry
	logger   *slog.Logger
	appsDir  string
}

// NewDevHandler creates a DevHandler that reads/writes DocType files
// under the given apps directory.
func NewDevHandler(appsDir string, registry *meta.Registry, logger *slog.Logger) *DevHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &DevHandler{appsDir: appsDir, registry: registry, logger: logger}
}

// ensureInsideAppsDir verifies that target resolves to a path under h.appsDir.
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

// DevAuthMiddleware returns middleware that requires the Administrator role
// for all dev API endpoints.
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

// HandleListApps returns the list of installed apps with their modules.
// Each app entry includes the app name and the module names from its manifest.
func (h *DevHandler) HandleListApps(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(h.appsDir)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, map[string]any{"data": []any{}})
			return
		}
		h.logger.Debug("read apps directory failed", slog.String("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	type appInfo struct {
		Name    string   `json:"name"`
		Modules []string `json:"modules"`
	}

	apps := make([]appInfo, 0)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		mPath := filepath.Join(h.appsDir, e.Name(), "manifest.yaml")
		data, err := os.ReadFile(mPath)
		if err != nil {
			continue // skip dirs without manifest
		}

		// Parse module names from manifest.yaml
		var manifest struct {
			Modules []struct {
				Name string `yaml:"name"`
			} `yaml:"modules"`
		}
		modules := []string{}
		if yaml.Unmarshal(data, &manifest) == nil {
			for _, m := range manifest.Modules {
				if m.Name != "" {
					modules = append(modules, m.Name)
				}
			}
		}

		apps = append(apps, appInfo{Name: e.Name(), Modules: modules})
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": apps})
}

// DevDocTypeRequest is the request body for creating/updating a DocType.
type DevDocTypeRequest struct {
	Fields      map[string]meta.FieldDef `json:"fields"`
	Name        string                   `json:"name"`
	App         string                   `json:"app"`
	Module      string                   `json:"module"`
	Layout      meta.LayoutTree          `json:"layout"`
	Permissions []meta.PermRule          `json:"permissions"`
	Settings    DevDocTypeSettings       `json:"settings"`
}

// DevDocTypeSettings holds DocType configuration flags and metadata.
type DevDocTypeSettings struct {
	NamingRule    meta.NamingStrategy `json:"naming_rule"`
	TitleField    string              `json:"title_field"`
	SortField     string              `json:"sort_field"`
	SortOrder     string              `json:"sort_order"`
	ImageField    string              `json:"image_field"`
	SearchFields  []string            `json:"search_fields"`
	IsSubmittable bool                `json:"is_submittable"`
	IsSingle      bool                `json:"is_single"`
	IsChildTable  bool                `json:"is_child_table"`
	IsVirtual     bool                `json:"is_virtual"`
	TrackChanges  bool                `json:"track_changes"`
}

// HandleCreateDocType creates a new DocType definition on disk.
func (h *DevHandler) HandleCreateDocType(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB
	var req DevDocTypeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if err := ValidateDocTypeName(req.Name); err != nil {
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

	// Validate field names
	for name := range req.Fields {
		if err := ValidateFieldName(name); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}

	// Validate field types
	if err := ValidateFieldDefs(req.Fields); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// Build the tree-native JSON document for disk
	docDef := buildDocTypeJSON(req)

	// Create directory structure: {appsDir}/{app}/modules/{module_snake}/doctypes/{dt_snake}/
	moduleSnake := toSnakeCaseDev(req.Module)
	dtSnake := toSnakeCaseDev(req.Name)
	dtDir := filepath.Join(h.appsDir, req.App, "modules", moduleSnake, "doctypes", dtSnake)
	if err := h.ensureInsideAppsDir(dtDir); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}
	if err := os.MkdirAll(dtDir, 0o755); err != nil {
		h.logger.Debug("create doctype directory failed", slog.String("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create directory"})
		return
	}

	// Write {dt_snake}.json with tree-native format
	jsonPath := filepath.Join(dtDir, dtSnake+".json")
	data, err := json.MarshalIndent(docDef, "", "  ")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "marshal: " + err.Error()})
		return
	}
	if err := os.WriteFile(jsonPath, data, 0o644); err != nil {
		h.logger.Debug("write doctype file failed", slog.String("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to write doctype"})
		return
	}

	// Write controller stub only if it doesn't already exist
	goPath := filepath.Join(dtDir, dtSnake+".go")
	if _, err := os.Stat(goPath); os.IsNotExist(err) {
		stub := fmt.Sprintf("package %s\n\n// %s controller.\n// Add lifecycle hooks here.\ntype %s struct{}\n", dtSnake, req.Name, req.Name)
		if err := os.WriteFile(goPath, []byte(stub), 0o644); err != nil {
			h.logger.Warn("failed to write controller stub", "error", err)
		}
	}

	// Register in registry if available
	if h.registry != nil {
		if _, err := h.registry.Register(r.Context(), siteFromContext(r), data); err != nil {
			h.logger.Warn("registry registration failed (non-fatal)", "error", err)
		}
	}

	writeJSON(w, http.StatusCreated, map[string]any{"data": docDef})
}

// HandleUpdateDocType updates an existing DocType JSON file on disk.
// It does NOT overwrite the .go controller file (preserves developer code).
func (h *DevHandler) HandleUpdateDocType(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB
	name := r.PathValue("name")
	var req DevDocTypeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	req.Name = name

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

	for fieldName := range req.Fields {
		if err := ValidateFieldName(fieldName); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}

	// Validate field types
	if err := ValidateFieldDefs(req.Fields); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	docDef := buildDocTypeJSON(req)
	moduleSnake := toSnakeCaseDev(req.Module)
	dtSnake := toSnakeCaseDev(req.Name)
	jsonPath := filepath.Join(h.appsDir, req.App, "modules", moduleSnake, "doctypes", dtSnake, dtSnake+".json")

	if err := h.ensureInsideAppsDir(jsonPath); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}

	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		h.logger.Debug("doctype not found", slog.String("path", jsonPath))
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "doctype not found"})
		return
	}

	data, err := json.MarshalIndent(docDef, "", "  ")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "marshal: " + err.Error()})
		return
	}
	if err := os.WriteFile(jsonPath, data, 0o644); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "write: " + err.Error()})
		return
	}

	if h.registry != nil {
		if _, err := h.registry.Register(r.Context(), siteFromContext(r), data); err != nil {
			h.logger.Warn("registry re-registration failed", "error", err)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": docDef})
}

// HandleGetDocType reads a DocType definition from disk by searching
// all apps for a matching doctype name. Returns the JSON content with
// _app and _module_dir metadata.
func (h *DevHandler) HandleGetDocType(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
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
	for _, app := range entries {
		if !app.IsDir() {
			continue
		}
		modulesDir := filepath.Join(h.appsDir, app.Name(), "modules")
		modules, err := os.ReadDir(modulesDir)
		if err != nil {
			h.logger.Debug("read modules directory failed", slog.String("path", modulesDir), slog.String("error", err.Error()))
			continue
		}
		for _, mod := range modules {
			if !mod.IsDir() {
				continue
			}
			dtSnake := toSnakeCaseDev(name)
			jsonPath := filepath.Join(modulesDir, mod.Name(), "doctypes", dtSnake, dtSnake+".json")
			data, err := os.ReadFile(jsonPath)
			if err != nil {
				continue
			}
			var docDef map[string]any
			if err := json.Unmarshal(data, &docDef); err != nil {
				continue
			}
			docDef["_app"] = app.Name()
			docDef["_module_dir"] = mod.Name()
			writeJSON(w, http.StatusOK, map[string]any{"data": docDef})
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "doctype not found: " + name})
}

// HandleDeleteDocType removes a DocType directory from disk.
func (h *DevHandler) HandleDeleteDocType(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
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
	for _, app := range entries {
		if !app.IsDir() {
			continue
		}
		modulesDir := filepath.Join(h.appsDir, app.Name(), "modules")
		modules, err := os.ReadDir(modulesDir)
		if err != nil {
			h.logger.Debug("read modules directory failed", slog.String("path", modulesDir), slog.String("error", err.Error()))
			continue
		}
		for _, mod := range modules {
			if !mod.IsDir() {
				continue
			}
			dtSnake := toSnakeCaseDev(name)
			dtDir := filepath.Join(modulesDir, mod.Name(), "doctypes", dtSnake)
			if _, err := os.Stat(dtDir); err == nil {
				if err := os.RemoveAll(dtDir); err != nil {
					h.logger.Debug("delete doctype failed", slog.String("error", err.Error()))
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
					return
				}
				writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
				return
			}
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "doctype not found: " + name})
}

// buildDocTypeJSON converts a DevDocTypeRequest into the tree-native
// JSON document that gets persisted to disk.
func buildDocTypeJSON(req DevDocTypeRequest) map[string]any {
	fieldsObj := make(map[string]any, len(req.Fields))
	for name, fd := range req.Fields {
		fieldsObj[name] = fd
	}
	return map[string]any{
		"name":           req.Name,
		"module":         req.Module,
		"layout":         req.Layout,
		"fields":         fieldsObj,
		"naming_rule":    req.Settings.NamingRule,
		"title_field":    req.Settings.TitleField,
		"sort_field":     req.Settings.SortField,
		"sort_order":     req.Settings.SortOrder,
		"search_fields":  req.Settings.SearchFields,
		"image_field":    req.Settings.ImageField,
		"is_submittable": req.Settings.IsSubmittable,
		"is_single":      req.Settings.IsSingle,
		"is_child_table": req.Settings.IsChildTable,
		"is_virtual":     req.Settings.IsVirtual,
		"track_changes":  req.Settings.TrackChanges,
		"permissions":    req.Permissions,
	}
}

// writeJSON writes a JSON response with the given status code and value.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

// siteFromContext extracts the site identifier from the request context,
// falling back to "default" if not set by tenant middleware.
func siteFromContext(r *http.Request) string {
	if site, ok := r.Context().Value("site").(string); ok {
		return site
	}
	return "default"
}

// toSnakeCaseDev converts a PascalCase name to snake_case by reusing
// meta.TableName and stripping the "tab_" prefix.
func toSnakeCaseDev(s string) string {
	if s == "" {
		return ""
	}
	tn := meta.TableName(s) // "tab_sales_order"
	return tn[4:]           // "sales_order"
}

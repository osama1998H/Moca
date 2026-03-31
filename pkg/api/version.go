package api

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/moca-framework/moca/pkg/meta"
)

// VersionRouter manages per-version route registration and injects
// API version context and deprecation/sunset headers.
type VersionRouter struct {
	versions map[string]*versionEntry
	handler  *ResourceHandler
	logger   *slog.Logger
}

// versionEntry holds the configuration for a single API version.
type versionEntry struct {
	version *meta.APIVersion // nil for the implicit default v1
}

// NewVersionRouter creates a VersionRouter with a default active "v1".
// Call ConfigureVersions to add version-specific configurations.
func NewVersionRouter(handler *ResourceHandler, logger *slog.Logger) *VersionRouter {
	return &VersionRouter{
		versions: map[string]*versionEntry{
			"v1": {version: nil}, // default active v1
		},
		handler: handler,
		logger:  logger,
	}
}

// ConfigureVersions replaces the default version set with explicit versions.
// If versions is empty, the default "v1" is retained.
func (vr *VersionRouter) ConfigureVersions(versions []meta.APIVersion) {
	if len(versions) == 0 {
		return
	}
	vr.versions = make(map[string]*versionEntry, len(versions))
	for i := range versions {
		vr.versions[versions[i].Version] = &versionEntry{version: &versions[i]}
	}
}

// RegisterRoutes registers REST resource routes for every configured version
// on the given mux. Each version gets its own /api/{version}/... routes.
func (vr *VersionRouter) RegisterRoutes(mux *http.ServeMux) {
	for version := range vr.versions {
		vr.handler.RegisterRoutes(mux, version)
	}
}

// Middleware returns an HTTP middleware that:
//  1. Extracts the API version from the URL path (/api/v1/... → "v1").
//  2. Rejects sunset versions with 410 Gone.
//  3. Adds Deprecation and Sunset headers for deprecated versions.
//  4. Stores the version string in the request context.
func (vr *VersionRouter) Middleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			version := extractVersionFromPath(r.URL.Path)
			if version == "" {
				next.ServeHTTP(w, r)
				return
			}

			entry, ok := vr.versions[version]
			if !ok {
				// Unknown version — let the mux 404 handle it.
				next.ServeHTTP(w, r)
				return
			}

			if entry.version != nil {
				switch entry.version.Status {
				case "sunset":
					writeError(w, http.StatusGone, "API_VERSION_SUNSET",
						"API version "+version+" has been sunset")
					return
				case "deprecated":
					w.Header().Set("Deprecation", "true")
					if entry.version.SunsetDate != nil {
						w.Header().Set("Sunset", entry.version.SunsetDate.Format(http.TimeFormat))
					}
				}
			}

			ctx := WithAPIVersion(r.Context(), version)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// extractVersionFromPath parses the API version from a URL path.
// Expects paths like /api/v1/resource/... and returns "v1".
// Returns "" for paths that don't match the /api/{version}/ pattern.
func extractVersionFromPath(path string) string {
	// Trim leading slash and split: ["api", "v1", "resource", ...]
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) < 2 || parts[0] != "api" {
		return ""
	}
	v := parts[1]
	if !strings.HasPrefix(v, "v") {
		return ""
	}
	return v
}

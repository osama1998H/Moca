package serve

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// registerStaticFiles serves files from dir on the /desk/ URL path with SPA
// fallback. If a requested path does not match a file on disk, index.html is
// served instead so that React Router can handle client-side routing.
// If dir is empty or does not exist, no handler is registered.
func registerStaticFiles(mux *http.ServeMux, dir string, logger *slog.Logger) {
	if dir == "" {
		return
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return
	}
	logger.Info("serving static files", slog.String("path", "/desk/"), slog.String("dir", dir))
	mux.Handle("GET /desk/", &spaHandler{dir: dir})
}

// spaHandler serves static files from dir. When a requested file does not
// exist (and the path has no file extension), it serves index.html for
// client-side routing.
type spaHandler struct {
	dir string
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Strip the /desk/ prefix to get the relative file path.
	relPath := strings.TrimPrefix(r.URL.Path, "/desk/")
	if relPath == "" {
		relPath = "index.html"
	}

	absPath := filepath.Join(h.dir, filepath.FromSlash(relPath))

	// If the file exists, serve it directly.
	if _, err := os.Stat(absPath); err == nil {
		http.StripPrefix("/desk/", http.FileServer(http.Dir(h.dir))).ServeHTTP(w, r)
		return
	}

	// SPA fallback: if the path has no file extension, serve index.html
	// so React Router can handle the route.
	if filepath.Ext(relPath) == "" {
		indexPath := filepath.Join(h.dir, "index.html")
		http.ServeFile(w, r, indexPath)
		return
	}

	// File with extension not found — return 404.
	http.NotFound(w, r)
}

package serve

import (
	"log/slog"
	"net/http"
	"os"
)

// registerStaticFiles serves files from dir on the /desk/ URL path.
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
	mux.Handle("GET /desk/", http.StripPrefix("/desk/", http.FileServer(http.Dir(dir))))
}

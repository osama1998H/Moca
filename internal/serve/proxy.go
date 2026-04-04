package serve

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
)

// registerDeskDevProxy registers a reverse proxy on /desk/ that forwards all
// requests (including WebSocket upgrades for Vite HMR) to the Vite dev server
// running on the given port. This replaces registerStaticFiles during development.
func registerDeskDevProxy(mux *http.ServeMux, port int, logger *slog.Logger) {
	target, _ := url.Parse(fmt.Sprintf("http://localhost:%d", port))

	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.Out.Host = target.Host
			// Preserve WebSocket upgrade headers for Vite HMR.
			if r.In.Header.Get("Upgrade") != "" {
				r.Out.Header.Set("Upgrade", r.In.Header.Get("Upgrade"))
				r.Out.Header.Set("Connection", r.In.Header.Get("Connection"))
			}
		},
	}

	logger.Info("proxying desk to Vite dev server",
		slog.String("path", "/desk/"),
		slog.String("target", target.String()),
	)
	mux.Handle("/desk/", proxy)
}

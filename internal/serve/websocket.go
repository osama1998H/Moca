package serve

import (
	"encoding/json"
	"net/http"
)

// registerWebSocketStub registers a handler on /ws that returns HTTP 501,
// indicating that WebSocket support is not yet implemented.
func registerWebSocketStub(mux *http.ServeMux) {
	mux.HandleFunc("GET /ws", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotImplemented)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "WebSocket not implemented",
		})
	})
}

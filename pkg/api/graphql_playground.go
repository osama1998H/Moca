package api

import (
	_ "embed"
	"net/http"
)

//go:embed graphiql.html
var graphiqlHTML []byte

// servePlayground serves the embedded GraphiQL interactive IDE.
func servePlayground(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(graphiqlHTML)
}

// Package httpx wires the API's stdlib router and cross-cutting HTTP
// middleware (request id, request logging). Handlers here are deliberately
// minimal: T3 regenerates them against the oapi-codegen ServerInterface.
package httpx

import (
	"log/slog"
	"net/http"
)

// NewRouter builds the API's http.Handler: a stdlib ServeMux with Go 1.22
// method+path patterns, wrapped in request-id and request-logging
// middleware.
func NewRouter(logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/healthz", handleHealthz)

	var handler http.Handler = mux
	handler = requestLogger(logger)(handler)
	handler = requestID(handler)
	return handler
}

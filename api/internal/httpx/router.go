// Package httpx wires the API's stdlib router and cross-cutting HTTP
// middleware (request id, request logging) around the oapi-codegen strict
// server interface generated from contracts/openapi.yaml (ADR-002).
package httpx

import (
	"log/slog"
	"net/http"

	"github.com/Sulton-Ali/savdo/api/gen"
)

// NewRouter builds the API's http.Handler: routes registered by the
// generated strict handler onto a stdlib ServeMux (Go 1.22 method+path
// patterns), under the "/v1" base the spec's `servers` entry declares, all
// wrapped in request-id and request-logging middleware.
func NewRouter(logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	strictHandler := gen.NewStrictHandler(server{}, nil)
	gen.HandlerFromMuxWithBaseURL(strictHandler, mux, "/v1")

	var handler http.Handler = mux
	handler = requestLogger(logger)(handler)
	handler = requestID(handler)
	return handler
}

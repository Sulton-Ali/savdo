// Package httpx wires the API's stdlib router and cross-cutting HTTP
// middleware (panic recovery, request id, request logging) around the
// oapi-codegen strict server interface generated from
// contracts/openapi.yaml (ADR-002), and maps every error the strict server
// surfaces — a malformed request or a handler's own error — onto the
// shared ADR-013 error envelope via internal/apierr.
package httpx

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/media"
	"github.com/Sulton-Ali/savdo/api/internal/shop"
)

// NewRouter builds the API's http.Handler: routes registered by the
// generated strict handler onto a stdlib ServeMux (Go 1.22 method+path
// patterns), under the "/v1" base the spec's `servers` entry declares, all
// wrapped in panic-recovery, request-id and request-logging middleware
// (outermost to innermost, in that order). pool backs GET /readyz's DB
// check; it may be nil in tests that never exercise that route. authSvc
// backs both auth.Service.Middleware — run for every operation, allow-
// listing only GetHealthz/GetReadyz/Login (internal/auth/middleware.go) —
// and the five `/auth/*` operations via auth.NewHandler. shopSvc backs
// the nine `/shop`, `/locations` and `/staff` operations via
// shop.NewHandler, forwarded from server's own methods (shop.go). mediaSvc
// backs POST /media via media.NewHandler, forwarded from media.go.
// devMedia, when non-nil, is additionally mounted at GET /media/ — cmd/api
// passes media.DevHandler(mediaStorage) here only when Config.Env !=
// "prod" (docs/07-DEVOPS.md § Local development; § Production: Caddy
// serves the same volume there instead, so this stays nil and unmounted).
// A caller-built http.Handler rather than a directory string: it lets
// cmd/api hand over the exact same *media.LocalStorage instance mediaSvc
// itself writes through, so DevHandler's path-confinement check
// (LocalStorage.resolve) is guaranteed to agree with where files actually
// are, not a second, independently-constructed root.
func NewRouter(logger *slog.Logger, pool *pgxpool.Pool, authSvc *auth.Service, shopSvc *shop.Service, mediaSvc *media.Service, devMedia http.Handler) http.Handler {
	mux := http.NewServeMux()

	strictHandler := gen.NewStrictHandlerWithOptions(
		server{pool: pool, Handler: auth.NewHandler(authSvc), shop: shop.NewHandler(shopSvc), media: media.NewHandler(mediaSvc)},
		[]gen.StrictMiddlewareFunc{authSvc.Middleware},
		gen.StrictHTTPServerOptions{
			RequestErrorHandlerFunc:  writeRequestError,
			ResponseErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) { apierr.Write(w, err) },
		},
	)

	gen.HandlerWithOptions(strictHandler, gen.StdHTTPServerOptions{
		BaseURL:          "/v1",
		BaseRouter:       mux,
		ErrorHandlerFunc: writeRequestError,
	})

	if devMedia != nil {
		mux.Handle("/media/", http.StripPrefix("/media/", devMedia))
	}

	var handler http.Handler = mux
	handler = requestLogger(logger)(handler)
	handler = requestID(handler)
	handler = maxBytesBody(handler)
	handler = recoverer(handler)
	return handler
}

// writeRequestError maps a request that failed before reaching a handler —
// a malformed JSON body (the strict server's own decode step, including
// one that overran maxRequestBodyBytes — see maxBytesBody in
// bodylimit.go), or an invalid/missing query or path parameter (the
// generated ServerInterfaceWrapper) — onto a 400 VALIDATION_FAILED
// response (docs/05-API.md § Conventions), including the failing
// parameter's name, or the body-too-large reason, when the generated
// error carries one.
func writeRequestError(w http.ResponseWriter, _ *http.Request, err error) {
	details := map[string]any{"reason": "bad_request"}

	var invalidParam *gen.InvalidParamFormatError
	var requiredParam *gen.RequiredParamError
	var maxBytesErr *http.MaxBytesError
	switch {
	case errors.As(err, &invalidParam):
		details["parameter"] = invalidParam.ParamName
	case errors.As(err, &requiredParam):
		details["parameter"] = requiredParam.ParamName
	case errors.As(err, &maxBytesErr):
		details["reason"] = "body_too_large"
	}

	apierr.Write(w, &apierr.Error{
		Status:  http.StatusBadRequest,
		Code:    gen.VALIDATIONFAILED,
		Details: details,
	})
}

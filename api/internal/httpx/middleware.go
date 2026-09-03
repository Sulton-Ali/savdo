package httpx

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/internal/apierr"
)

type contextKey int

const requestIDKey contextKey = iota

// recoverer turns a panic anywhere downstream (including inside a handler's
// own service call) into a mapped 500 INTERNAL response instead of
// crashing the process or leaking a Go stack trace to the client. It is the
// outermost middleware, so it also protects requestID and requestLogger.
func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				apierr.Write(w, fmt.Errorf("panic: %v", rec))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// requestID assigns a time-ordered (UUID v7) request id to every request,
// stores it on the context and echoes it back as a response header so it
// can be correlated with the log line the request logger writes.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.NewV7()
		idStr := id.String()
		if err != nil {
			// Extremely unlikely (crypto/rand failure); fall back to a
			// random v4 id rather than leaving requests uncorrelated.
			idStr = uuid.NewString()
		}

		w.Header().Set(apierr.RequestIDHeader, idStr)
		ctx := context.WithValue(r.Context(), requestIDKey, idStr)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requestIDFromContext returns the id set by requestID, or "" if absent.
func requestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// statusRecorder captures the status code written by the wrapped handler so
// it can be logged after the fact.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// requestLogger logs one structured line per request: method, path, status,
// duration and request id.
func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(rec, r)

			logger.Info("http_request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", requestIDFromContext(r.Context()),
			)
		})
	}
}

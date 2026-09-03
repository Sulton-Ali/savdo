package httpx

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
)

type contextKey int

const requestIDKey contextKey = iota

// requestID assigns a time-ordered (UUID v7) request id to every request,
// stores it on the context and echoes it back as a response header so it
// can be correlated with the log line the request logger writes.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idStr := uuid.NewString()
		if id, err := uuid.NewV7(); err == nil {
			idStr = id.String()
		}

		w.Header().Set("X-Request-Id", idStr)
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

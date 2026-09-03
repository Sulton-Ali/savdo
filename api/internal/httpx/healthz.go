package httpx

import (
	"encoding/json"
	"net/http"
)

// handleHealthz reports the process is up. It does not touch the database —
// that is GET /readyz, added once cmd/api opens a pool (Phase 1).
func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

package auth

import (
	"testing"

	"github.com/Sulton-Ali/savdo/api/internal/apierr"
)

// errStatus extracts the HTTP status an *apierr.Error carries, failing the
// test if err isn't one — every error this package returns to a caller is
// supposed to be typed (AGENTS.md: "Errors are typed and mapped to
// ErrorCode"), so a test asserting on status is also asserting that shape.
func errStatus(t *testing.T, err error) int {
	t.Helper()
	apiErr, ok := err.(*apierr.Error)
	if !ok {
		t.Fatalf("error type = %T, want *apierr.Error", err)
	}
	return apiErr.Status
}

package pagination_test

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/pagination"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	createdAt := time.Date(2026, 3, 4, 12, 30, 0, 123456789, time.UTC)
	id := uuid.New()

	cursor := pagination.Encode(createdAt, id)
	if cursor == "" {
		t.Fatal("Encode returned an empty cursor")
	}

	gotCreatedAt, gotID, err := pagination.Decode(cursor)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if !gotCreatedAt.Equal(createdAt) {
		t.Fatalf("createdAt = %v, want %v", gotCreatedAt, createdAt)
	}
	if gotID != id {
		t.Fatalf("id = %v, want %v", gotID, id)
	}
}

func TestEncodeIsURLSafe(t *testing.T) {
	cursor := pagination.Encode(time.Now(), uuid.New())
	for _, r := range cursor {
		if r == '+' || r == '/' || r == '=' {
			t.Fatalf("cursor %q contains a non-URL-safe character %q", cursor, r)
		}
	}
}

func TestDecodeRejectsInvalidCursors(t *testing.T) {
	tests := []struct {
		name   string
		cursor string
	}{
		{"not base64url", "not valid base64!!"},
		{"empty string", ""},
		{"decodes but has no separator", base64.RawURLEncoding.EncodeToString([]byte("no-separator-here"))},
		{"bad timestamp", base64.RawURLEncoding.EncodeToString([]byte("not-a-time|" + uuid.New().String()))},
		{"bad uuid", base64.RawURLEncoding.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano) + "|not-a-uuid"))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := pagination.Decode(tt.cursor)
			if err == nil {
				t.Fatal("Decode() error = nil, want a validation error")
			}
			apiErr, ok := err.(*apierr.Error)
			if !ok {
				t.Fatalf("Decode() error type = %T, want *apierr.Error", err)
			}
			if apiErr.Status != 400 {
				t.Fatalf("Decode() error status = %d, want 400", apiErr.Status)
			}
		})
	}
}

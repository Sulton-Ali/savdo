package auth

import (
	"strings"
	"testing"

	"github.com/Sulton-Ali/savdo/api/internal/apierr"
)

func TestHashVerifyRoundTrip(t *testing.T) {
	hash, err := Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}

	ok, err := Verify(hash, "correct horse battery staple")
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !ok {
		t.Fatal("Verify() = false, want true for the correct password")
	}
}

func TestVerifyRejectsWrongPassword(t *testing.T) {
	hash, err := Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}

	ok, err := Verify(hash, "wrong password entirely")
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if ok {
		t.Fatal("Verify() = true, want false for a wrong password")
	}
}

func TestHashProducesThePHCFormat(t *testing.T) {
	hash, err := Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}

	parts := strings.Split(hash, "$")
	if len(parts) != 6 {
		t.Fatalf("hash has %d $-separated parts, want 6: %q", len(parts), hash)
	}
	if parts[0] != "" || parts[1] != "argon2id" {
		t.Fatalf("hash prefix = %q/%q, want empty/argon2id", parts[0], parts[1])
	}
	if parts[2] != "v=19" {
		t.Fatalf("version segment = %q, want v=19", parts[2])
	}
	if parts[3] != "m=65536,t=3,p=4" {
		t.Fatalf("params segment = %q, want m=65536,t=3,p=4", parts[3])
	}
	if parts[4] == "" || parts[5] == "" {
		t.Fatalf("salt or hash segment empty: %q", hash)
	}
}

func TestHashSaltsEveryCall(t *testing.T) {
	h1, err := Hash("same password")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	h2, err := Hash("same password")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}
	if h1 == h2 {
		t.Fatal("two Hash() calls for the same password produced identical output — salt is not random")
	}
}

func TestHashRejectsShortPasswords(t *testing.T) {
	_, err := Hash("short1")
	if err == nil {
		t.Fatal("Hash() error = nil, want a validation error for a 6-character password")
	}
	apiErr, ok := err.(*apierr.Error)
	if !ok {
		t.Fatalf("Hash() error type = %T, want *apierr.Error", err)
	}
	if apiErr.Status != 400 {
		t.Fatalf("Hash() error status = %d, want 400", apiErr.Status)
	}
	if _, hasField := apiErr.Details["fields"].(map[string]string)["password"]; !hasField {
		t.Fatalf("Hash() error details = %+v, want a \"password\" field", apiErr.Details)
	}
}

func TestVerifyRejectsMalformedHash(t *testing.T) {
	_, err := Verify("not-a-hash-at-all", "anything")
	if err == nil {
		t.Fatal("Verify() error = nil, want an error for a malformed hash")
	}
}

func TestDummyHashIsAValidHashNoRealPasswordMatches(t *testing.T) {
	hash := dummyHash()

	ok, err := Verify(hash, "some guess an attacker might try")
	if err != nil {
		t.Fatalf("Verify(dummyHash, ...) error = %v, want no error (dummyHash must be well-formed)", err)
	}
	if ok {
		t.Fatal("Verify(dummyHash, ...) = true, want false: nothing should match the dummy hash")
	}

	// Calling dummyHash twice must return the same value (computed once).
	if dummyHash() != hash {
		t.Fatal("dummyHash() is not stable across calls")
	}
}

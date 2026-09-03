package auth

import (
	"strings"
	"testing"

	"github.com/Sulton-Ali/savdo/api/gen"
)

func TestValidateLoginRequest(t *testing.T) {
	tests := []struct {
		name       string
		body       gen.LoginRequest
		wantFields []string
	}{
		{
			name: "valid",
			body: gen.LoginRequest{Username: "owner1", Password: "correct-horse", Client: gen.Web},
		},
		{
			name:       "empty username",
			body:       gen.LoginRequest{Username: "", Password: "correct-horse", Client: gen.Web},
			wantFields: []string{"username"},
		},
		{
			name:       "empty password",
			body:       gen.LoginRequest{Username: "owner1", Password: "", Client: gen.Web},
			wantFields: []string{"password"},
		},
		{
			name:       "username too long",
			body:       gen.LoginRequest{Username: strings.Repeat("a", maxLoginUsernameLength+1), Password: "correct-horse", Client: gen.Web},
			wantFields: []string{"username"},
		},
		{
			name:       "username at the limit is fine",
			body:       gen.LoginRequest{Username: strings.Repeat("a", maxLoginUsernameLength), Password: "correct-horse", Client: gen.Web},
			wantFields: nil,
		},
		{
			name:       "password too long",
			body:       gen.LoginRequest{Username: "owner1", Password: strings.Repeat("a", maxLoginPasswordLength+1), Client: gen.Web},
			wantFields: []string{"password"},
		},
		{
			name:       "password at the limit is fine",
			body:       gen.LoginRequest{Username: "owner1", Password: strings.Repeat("a", maxLoginPasswordLength), Client: gen.Web},
			wantFields: nil,
		},
		{
			name:       "invalid client",
			body:       gen.LoginRequest{Username: "owner1", Password: "correct-horse", Client: gen.SessionClient("desktop")},
			wantFields: []string{"client"},
		},
		{
			// Cyrillic letters are 2 bytes each in UTF-8, so 40 of them
			// is 80 bytes — well over maxLoginUsernameLength (64) if
			// counted in bytes, but exactly at the character limit.
			// maxLength (the contract's and this one) counts characters,
			// not bytes.
			name:       "40-char Cyrillic username (80 bytes) is within the character limit",
			body:       gen.LoginRequest{Username: strings.Repeat("а", 40), Password: "correct-horse", Client: gen.Web},
			wantFields: nil,
		},
		{
			name:       "65-char Cyrillic username exceeds the character limit",
			body:       gen.LoginRequest{Username: strings.Repeat("а", 65), Password: "correct-horse", Client: gen.Web},
			wantFields: []string{"username"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateLoginRequest(&tt.body)
			if len(got) != len(tt.wantFields) {
				t.Fatalf("validateLoginRequest() = %v, want fields %v", got, tt.wantFields)
			}
			for _, f := range tt.wantFields {
				if _, ok := got[f]; !ok {
					t.Fatalf("validateLoginRequest() = %v, want it to flag field %q", got, f)
				}
			}
		})
	}
}

// TestValidateLoginRequestCountsCharactersNotBytes pins down the exact
// scenario a byte-counting regression would break: a 40-character
// Cyrillic username is 80 bytes, comfortably over
// maxLoginUsernameLength(64) in bytes but exactly at the character limit.
func TestValidateLoginRequestCountsCharactersNotBytes(t *testing.T) {
	username := strings.Repeat("а", 40)
	if got := len(username); got != 80 {
		t.Fatalf("test setup: len(username) in bytes = %d, want 80 (40 Cyrillic chars x 2 bytes)", got)
	}

	body := &gen.LoginRequest{Username: username, Password: "correct-horse", Client: gen.Web}
	if fields := validateLoginRequest(body); len(fields) != 0 {
		t.Fatalf("validateLoginRequest() = %v, want no fields flagged for a 40-character (80-byte) username", fields)
	}

	tooLong := strings.Repeat("а", 65)
	body = &gen.LoginRequest{Username: tooLong, Password: "correct-horse", Client: gen.Web}
	fields := validateLoginRequest(body)
	if _, ok := fields["username"]; !ok {
		t.Fatalf("validateLoginRequest() = %v, want username flagged for a 65-character username", fields)
	}
}

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

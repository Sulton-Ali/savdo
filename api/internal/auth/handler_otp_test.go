package auth

import (
	"strings"
	"testing"

	"github.com/Sulton-Ali/savdo/api/gen"
)

func TestValidateOtpRequest(t *testing.T) {
	tests := []struct {
		name       string
		body       gen.OtpRequest
		wantFields []string
	}{
		{
			name: "valid",
			body: gen.OtpRequest{Username: "owner1", Purpose: gen.PasswordReset},
		},
		{
			name:       "empty username",
			body:       gen.OtpRequest{Username: "", Purpose: gen.PasswordReset},
			wantFields: []string{"username"},
		},
		{
			name:       "username too long",
			body:       gen.OtpRequest{Username: strings.Repeat("a", maxOtpUsernameLength+1), Purpose: gen.PasswordReset},
			wantFields: []string{"username"},
		},
		{
			name:       "unsupported purpose (link_telegram)",
			body:       gen.OtpRequest{Username: "owner1", Purpose: gen.LinkTelegram},
			wantFields: []string{"purpose"},
		},
		{
			name:       "unsupported purpose (confirm_action)",
			body:       gen.OtpRequest{Username: "owner1", Purpose: gen.ConfirmAction},
			wantFields: []string{"purpose"},
		},
		{
			name:       "garbage purpose",
			body:       gen.OtpRequest{Username: "owner1", Purpose: gen.OtpPurpose("nonsense")},
			wantFields: []string{"purpose"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateOtpRequest(&tt.body)
			if len(got) != len(tt.wantFields) {
				t.Fatalf("validateOtpRequest() = %v, want fields %v", got, tt.wantFields)
			}
			for _, f := range tt.wantFields {
				if _, ok := got[f]; !ok {
					t.Fatalf("validateOtpRequest() = %v, want it to flag field %q", got, f)
				}
			}
		})
	}
}

func TestValidateOtpVerifyRequest(t *testing.T) {
	tests := []struct {
		name       string
		body       gen.OtpVerifyRequest
		wantFields []string
	}{
		{
			name: "valid",
			body: gen.OtpVerifyRequest{Username: "owner1", Purpose: gen.PasswordReset, Code: "123456"},
		},
		{
			name:       "code too short",
			body:       gen.OtpVerifyRequest{Username: "owner1", Purpose: gen.PasswordReset, Code: "123"},
			wantFields: []string{"code"},
		},
		{
			name:       "code too long",
			body:       gen.OtpVerifyRequest{Username: "owner1", Purpose: gen.PasswordReset, Code: "1234567"},
			wantFields: []string{"code"},
		},
		{
			name:       "empty username and code",
			body:       gen.OtpVerifyRequest{Username: "", Purpose: gen.PasswordReset, Code: ""},
			wantFields: []string{"username", "code"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateOtpVerifyRequest(&tt.body)
			if len(got) != len(tt.wantFields) {
				t.Fatalf("validateOtpVerifyRequest() = %v, want fields %v", got, tt.wantFields)
			}
			for _, f := range tt.wantFields {
				if _, ok := got[f]; !ok {
					t.Fatalf("validateOtpVerifyRequest() = %v, want it to flag field %q", got, f)
				}
			}
		})
	}
}

func TestValidatePasswordResetRequest(t *testing.T) {
	tests := []struct {
		name       string
		body       gen.PasswordResetRequest
		wantFields []string
	}{
		{
			name: "valid",
			body: gen.PasswordResetRequest{ActionToken: "token123", NewPassword: "a-fine-password"},
		},
		{
			name:       "empty action token",
			body:       gen.PasswordResetRequest{ActionToken: "", NewPassword: "a-fine-password"},
			wantFields: []string{"actionToken"},
		},
		{
			name:       "password too short",
			body:       gen.PasswordResetRequest{ActionToken: "token123", NewPassword: "short"},
			wantFields: []string{"newPassword"},
		},
		{
			name:       "password too long",
			body:       gen.PasswordResetRequest{ActionToken: "token123", NewPassword: strings.Repeat("a", MaxPasswordLength+1)},
			wantFields: []string{"newPassword"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validatePasswordResetRequest(&tt.body)
			if len(got) != len(tt.wantFields) {
				t.Fatalf("validatePasswordResetRequest() = %v, want fields %v", got, tt.wantFields)
			}
			for _, f := range tt.wantFields {
				if _, ok := got[f]; !ok {
					t.Fatalf("validatePasswordResetRequest() = %v, want it to flag field %q", got, f)
				}
			}
		})
	}
}

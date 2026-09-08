package auth

import (
	"strings"
	"testing"

	"github.com/Sulton-Ali/savdo/api/gen"
)

func TestValidateTelegramAuthRequest(t *testing.T) {
	tests := []struct {
		name       string
		body       gen.TelegramAuthRequest
		wantFields []string
	}{
		{
			name: "valid",
			body: gen.TelegramAuthRequest{Id: "123", AuthDate: 1700000000, Hash: "abcd"},
		},
		{
			name:       "missing id",
			body:       gen.TelegramAuthRequest{AuthDate: 1700000000, Hash: "abcd"},
			wantFields: []string{"id"},
		},
		{
			name:       "missing hash",
			body:       gen.TelegramAuthRequest{Id: "123", AuthDate: 1700000000},
			wantFields: []string{"hash"},
		},
		{
			name:       "missing authDate",
			body:       gen.TelegramAuthRequest{Id: "123", Hash: "abcd"},
			wantFields: []string{"authDate"},
		},
		{
			name:       "id too long",
			body:       gen.TelegramAuthRequest{Id: strings.Repeat("1", maxTelegramFieldLength+1), AuthDate: 1700000000, Hash: "abcd"},
			wantFields: []string{"id"},
		},
		{
			name:       "username too long",
			body:       gen.TelegramAuthRequest{Id: "123", AuthDate: 1700000000, Hash: "abcd", Username: strPtr(strings.Repeat("a", maxTelegramFieldLength+1))},
			wantFields: []string{"username"},
		},
		{
			name:       "everything missing",
			body:       gen.TelegramAuthRequest{},
			wantFields: []string{"id", "hash", "authDate"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateTelegramAuthRequest(&tt.body)
			if len(got) != len(tt.wantFields) {
				t.Fatalf("validateTelegramAuthRequest() = %v, want fields %v", got, tt.wantFields)
			}
			for _, f := range tt.wantFields {
				if _, ok := got[f]; !ok {
					t.Fatalf("validateTelegramAuthRequest() = %v, want it to flag field %q", got, f)
				}
			}
		})
	}
}

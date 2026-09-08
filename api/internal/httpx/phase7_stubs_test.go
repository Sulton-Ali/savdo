package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestPhase7RoutesExist proves the ten operations T3 adds to
// contracts/openapi.yaml are actually wired into the router (a route that
// does not exist at all would 404 from the generated mux itself, before
// ever reaching auth.Service.Middleware or a handler) and that Middleware
// treats each one the way its contract entry says it should:
//
//   - the five no-session operations (docs/05-API.md § Auth/Bot,
//     auth.allowlistedOperations) reach phase7_stubs.go's stub and get its
//     placeholder 501, proving they run without a session at all — a
//     route that needed one would 401 first, never reaching the stub;
//   - the three `/auth/telegram/link` (any role) and two
//     `/bot/conversations*` (manager+) operations are NOT allow-listed,
//     so an unauthenticated request 401s before reaching the stub, same
//     as every other authenticated route in this router (auth_test.go's
//     TestGetMeWithNoCredentialIsUnauthenticated).
//
// This is router-level wiring coverage, not business-logic coverage —
// the stubs have none (phase7_stubs.go's own doc comment); T4/T5 own the
// real behavior and its tests.
func TestPhase7RoutesExist(t *testing.T) {
	router := NewRouter(testLogger(), nil, testAuthService(), testShopService(), testMediaService(), nil, testCatalogService(), testStockService(), testCrmService(), testReportsService(), testSalesService(), testContentService(), testPublicService())

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{"authenticateTelegram (no session)", http.MethodPost, "/v1/auth/telegram", "{}", http.StatusNotImplemented},
		{"requestOtp (no session)", http.MethodPost, "/v1/auth/otp/request", "{}", http.StatusNotImplemented},
		{"verifyOtp (no session)", http.MethodPost, "/v1/auth/otp/verify", "{}", http.StatusNotImplemented},
		{"resetPassword (no session)", http.MethodPost, "/v1/auth/password/reset", "{}", http.StatusNotImplemented},
		{"handleBotWebhook (no session)", http.MethodPost, "/v1/bot/webhook/some-secret", "{}", http.StatusNotImplemented},
		{"createTelegramLink (any role) without session is 401", http.MethodPost, "/v1/auth/telegram/link", "", http.StatusUnauthorized},
		{"deleteTelegramLink (any role) without session is 401", http.MethodDelete, "/v1/auth/telegram/link", "", http.StatusUnauthorized},
		{"getTelegramLink (any role) without session is 401", http.MethodGet, "/v1/auth/telegram/link", "", http.StatusUnauthorized},
		{"listBotConversations (manager+) without session is 401", http.MethodGet, "/v1/bot/conversations", "", http.StatusUnauthorized},
		{"listBotConversationMessages (manager+) without session is 401", http.MethodGet, "/v1/bot/conversations/" + uuid.New().String() + "/messages", "", http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req *http.Request
			if tt.body != "" {
				req = httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
				req.Header.Set("Content-Type", "application/json")
			} else {
				req = httptest.NewRequest(tt.method, tt.path, nil)
			}
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

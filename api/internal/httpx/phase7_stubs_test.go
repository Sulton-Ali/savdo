package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestPhase7RoutesExist proves the ten operations T3 added to
// contracts/openapi.yaml are wired into the router (a route that does
// not exist at all would 404 from the generated mux itself, before ever
// reaching auth.Service.Middleware or a handler) and that Middleware
// treats each one the way its contract entry says it should. Both T4
// (internal/bot) and T5 (internal/auth) have since given every one of
// them a real handler — the phase7_stubs.go placeholder file this test
// once exercised is gone — so this is router-level wiring/gating
// coverage only, not business-logic coverage (that lives in internal/
// auth's and internal/bot's own tests):
//
//   - the four `/auth/telegram`, `/auth/otp/*` and `/auth/password/reset`
//     operations are allow-listed (auth.allowlistedOperations) and run
//     without a session; an empty "{}" body reaches the real handler and
//     answers 400 VALIDATION_FAILED, proving Middleware let it through
//     rather than 401ing first;
//   - `/bot/webhook/*` is also allow-listed and real (bot.Handler.
//     HandleBotWebhook); a request with the wrong secret 404s (constant-
//     time compare, bot/handler.go's own doc comment) rather than 501ing
//     or 401ing — its own fuller coverage (secret match/mismatch) lives
//     in bot_test.go, not here;
//   - the three `/auth/telegram/link` (any role) and two
//     `/bot/conversations*` (manager+) operations are NOT allow-listed,
//     so an unauthenticated request 401s before ever reaching a handler,
//     same as every other authenticated route in this router
//     (auth_test.go's TestGetMeWithNoCredentialIsUnauthenticated).
func TestPhase7RoutesExist(t *testing.T) {
	router := NewRouter(testLogger(), nil, testAuthService(), testShopService(), testMediaService(), nil, testCatalogService(), testStockService(), testCrmService(), testReportsService(), testSalesService(), testContentService(), testPublicService(), testBotService(), testWebhookSecret)

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{"authenticateTelegram (no session, empty body)", http.MethodPost, "/v1/auth/telegram", "{}", http.StatusBadRequest},
		{"requestOtp (no session, empty body)", http.MethodPost, "/v1/auth/otp/request", "{}", http.StatusBadRequest},
		{"verifyOtp (no session, empty body)", http.MethodPost, "/v1/auth/otp/verify", "{}", http.StatusBadRequest},
		{"resetPassword (no session, empty body)", http.MethodPost, "/v1/auth/password/reset", "{}", http.StatusBadRequest},
		{"handleBotWebhook (no session, wrong secret) is 404, not 401/501", http.MethodPost, "/v1/bot/webhook/some-secret", "{}", http.StatusNotFound},
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

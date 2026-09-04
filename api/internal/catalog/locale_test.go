package catalog

import (
	"context"
	"net/http"
	"testing"
)

func resolveWithHeader(svc *Service, header string) string {
	ctx := context.WithValue(context.Background(), acceptLanguageCtxKey{}, header)
	return svc.resolveLocale(ctx)
}

func TestResolveLocale(t *testing.T) {
	svc := &Service{defaultLocale: "uz"}
	tests := []struct {
		header string
		want   string
	}{
		{"ru", "ru"},
		{"ru-RU,en;q=0.9", "ru"},
		{"fr", "uz"}, // unsupported -> shop default
		{"", "uz"},   // absent -> shop default
		{"en", "en"},
		{" uz ", "uz"},
	}
	for _, tt := range tests {
		if got := resolveWithHeader(svc, tt.header); got != tt.want {
			t.Errorf("resolveLocale(%q) = %q, want %q", tt.header, got, tt.want)
		}
	}
}

func TestAcceptLanguageMiddleware_stashesHeaderOnContext(t *testing.T) {
	var captured string
	next := func(ctx context.Context, _ http.ResponseWriter, _ *http.Request, _ any) (any, error) {
		captured = AcceptLanguageFromContext(ctx)
		return nil, nil
	}
	mw := AcceptLanguageMiddleware(next, "ListProducts")

	req, err := http.NewRequest(http.MethodGet, "/v1/products", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Accept-Language", "ru")

	if _, err := mw(context.Background(), nil, req, nil); err != nil {
		t.Fatalf("middleware: %v", err)
	}
	if captured != "ru" {
		t.Fatalf("captured header = %q, want %q", captured, "ru")
	}
}

func TestTranslationFallback(t *testing.T) {
	tests := []struct {
		localeUsed, requested string
		want                  bool
	}{
		{"uz", "uz", false},
		{"uz", "ru", true},
		{"", "uz", true},
	}
	for _, tt := range tests {
		if got := translationFallback(tt.localeUsed, tt.requested); got != tt.want {
			t.Errorf("translationFallback(%q, %q) = %v, want %v", tt.localeUsed, tt.requested, got, tt.want)
		}
	}
}

func TestEffectiveLocale(t *testing.T) {
	if got := effectiveLocale("", "ru"); string(got) != "ru" {
		t.Errorf("effectiveLocale(\"\", ru) = %q, want ru", got)
	}
	if got := effectiveLocale("uz", "ru"); string(got) != "uz" {
		t.Errorf("effectiveLocale(uz, ru) = %q, want uz", got)
	}
}

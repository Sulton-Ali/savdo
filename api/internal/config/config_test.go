package config

import (
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	const dbURL = "postgres://savdo:savdo@localhost:5432/savdo?sslmode=disable"

	tests := []struct {
		name    string
		env     map[string]string
		want    Config
		wantErr bool
	}{
		{
			name: "defaults when only the required DATABASE_URL/BOT_USERNAME/TELEGRAM_BOT_TOKEN are set",
			env: map[string]string{
				"DATABASE_URL":       dbURL,
				"BOT_USERNAME":       "savdo_bot",
				"TELEGRAM_BOT_TOKEN": "123456:fake-token-for-tests",
			},
			want: Config{
				Addr:                 ":8080",
				LogLevel:             "info",
				DatabaseURL:          dbURL,
				Env:                  "dev",
				SessionWebTTL:        168 * time.Hour,
				SessionMobileTTL:     720 * time.Hour,
				LoginRateIPPerMin:    10,
				LoginRateUserPerMin:  5,
				CookieSecure:         false,
				ShopSlug:             "savdo-demo",
				PublicShopSlug:       "savdo-demo",
				MediaDir:             "../infra/data/media",
				MediaBaseURL:         "/media",
				MediaMaxBytes:        10485760,
				MediaConcurrency:     2,
				MediaQueue:           8,
				AIProvider:           "anthropic",
				AIModel:              "claude-sonnet-5",
				AIPriceInputPerMTok:  "2.00",
				AIPriceOutputPerMTok: "10.00",
				BotUsername:          "savdo_bot",
				TelegramBotToken:     "123456:fake-token-for-tests",
			},
		},
		{
			name:    "missing DATABASE_URL fails fast",
			env:     map[string]string{},
			wantErr: true,
		},
		{
			name: "env overrides defaults",
			env: map[string]string{
				"API_ADDR":                ":9090",
				"LOG_LEVEL":               "debug",
				"DATABASE_URL":            dbURL,
				"ENV":                     "prod",
				"SESSION_WEB_TTL":         "24h",
				"SESSION_MOBILE_TTL":      "48h",
				"LOGIN_RATE_IP_PER_MIN":   "20",
				"LOGIN_RATE_USER_PER_MIN": "3",
				"COOKIE_SECURE":           "false",
				"SHOP_SLUG":               "acme-shop",
				"MEDIA_DIR":               "/data/media",
				"MEDIA_CONCURRENCY":       "4",
				"MEDIA_QUEUE":             "16",
				"BOT_USERNAME":            "savdo_bot",
				"TELEGRAM_BOT_TOKEN":      "123456:fake-token-for-tests",
			},
			want: Config{
				Addr:                ":9090",
				LogLevel:            "debug",
				DatabaseURL:         dbURL,
				Env:                 "prod",
				SessionWebTTL:       24 * time.Hour,
				SessionMobileTTL:    48 * time.Hour,
				LoginRateIPPerMin:   20,
				LoginRateUserPerMin: 3,
				// COOKIE_SECURE explicitly "false" must win over the prod
				// default of true.
				CookieSecure:         false,
				ShopSlug:             "acme-shop",
				PublicShopSlug:       "savdo-demo",
				MediaDir:             "/data/media",
				MediaBaseURL:         "/media",
				MediaMaxBytes:        10485760,
				MediaConcurrency:     4,
				MediaQueue:           16,
				AIProvider:           "anthropic",
				AIModel:              "claude-sonnet-5",
				AIPriceInputPerMTok:  "2.00",
				AIPriceOutputPerMTok: "10.00",
				BotUsername:          "savdo_bot",
				TelegramBotToken:     "123456:fake-token-for-tests",
			},
		},
		{
			name: "prod defaults CookieSecure to true when unset",
			env: map[string]string{
				"DATABASE_URL": dbURL,
				"ENV":          "prod",
				// prod requires an absolute MEDIA_DIR (see the negative
				// cases below) — set one here so this case can isolate
				// the CookieSecure-defaulting behavior it's actually
				// testing.
				"MEDIA_DIR":          "/data/media",
				"BOT_USERNAME":       "savdo_bot",
				"TELEGRAM_BOT_TOKEN": "123456:fake-token-for-tests",
			},
			want: Config{
				Addr:                 ":8080",
				LogLevel:             "info",
				DatabaseURL:          dbURL,
				Env:                  "prod",
				SessionWebTTL:        168 * time.Hour,
				SessionMobileTTL:     720 * time.Hour,
				LoginRateIPPerMin:    10,
				LoginRateUserPerMin:  5,
				CookieSecure:         true,
				ShopSlug:             "savdo-demo",
				PublicShopSlug:       "savdo-demo",
				MediaDir:             "/data/media",
				MediaBaseURL:         "/media",
				MediaMaxBytes:        10485760,
				MediaConcurrency:     2,
				MediaQueue:           8,
				AIProvider:           "anthropic",
				AIModel:              "claude-sonnet-5",
				AIPriceInputPerMTok:  "2.00",
				AIPriceOutputPerMTok: "10.00",
				BotUsername:          "savdo_bot",
				TelegramBotToken:     "123456:fake-token-for-tests",
			},
		},
		{
			// Review A MAJOR 2: a relative MEDIA_DIR in prod would resolve
			// against whatever directory the process happened to start in,
			// not necessarily the mounted volume — fail fast, the same way
			// a missing DATABASE_URL does, rather than silently writing
			// media to the wrong place. The dev default ("../infra/data/
			// media") is itself relative, so simply not overriding
			// MEDIA_DIR is enough to trigger this in prod.
			name: "prod requires an absolute MEDIA_DIR (default left unset)",
			env: map[string]string{
				"DATABASE_URL": dbURL,
				"ENV":          "prod",
			},
			wantErr: true,
		},
		{
			name: "prod rejects an explicit relative MEDIA_DIR",
			env: map[string]string{
				"DATABASE_URL": dbURL,
				"ENV":          "prod",
				"MEDIA_DIR":    "./data/media",
			},
			wantErr: true,
		},
		{
			name: "dev tolerates a relative MEDIA_DIR",
			env: map[string]string{
				"DATABASE_URL":       dbURL,
				"ENV":                "dev",
				"MEDIA_DIR":          "./data/media",
				"BOT_USERNAME":       "savdo_bot",
				"TELEGRAM_BOT_TOKEN": "123456:fake-token-for-tests",
			},
			want: Config{
				Addr:                 ":8080",
				LogLevel:             "info",
				DatabaseURL:          dbURL,
				Env:                  "dev",
				SessionWebTTL:        168 * time.Hour,
				SessionMobileTTL:     720 * time.Hour,
				LoginRateIPPerMin:    10,
				LoginRateUserPerMin:  5,
				CookieSecure:         false,
				ShopSlug:             "savdo-demo",
				PublicShopSlug:       "savdo-demo",
				MediaDir:             "./data/media",
				MediaBaseURL:         "/media",
				MediaMaxBytes:        10485760,
				MediaConcurrency:     2,
				MediaQueue:           8,
				AIProvider:           "anthropic",
				AIModel:              "claude-sonnet-5",
				AIPriceInputPerMTok:  "2.00",
				AIPriceOutputPerMTok: "10.00",
				BotUsername:          "savdo_bot",
				TelegramBotToken:     "123456:fake-token-for-tests",
			},
		},
		{
			name: "missing BOT_USERNAME fails fast",
			env: map[string]string{
				"DATABASE_URL":       dbURL,
				"TELEGRAM_BOT_TOKEN": "123456:fake-token-for-tests",
			},
			wantErr: true,
		},
		{
			name: "empty BOT_USERNAME (set but blank) fails fast",
			env: map[string]string{
				"DATABASE_URL":       dbURL,
				"BOT_USERNAME":       "",
				"TELEGRAM_BOT_TOKEN": "123456:fake-token-for-tests",
			},
			wantErr: true,
		},
		{
			// Review [empty bot token] CRITICAL 1: an empty TELEGRAM_BOT_TOKEN
			// makes VerifyLoginWidget's secret_key SHA-256("") — a public
			// constant an attacker can compute too — so Load must never let the
			// API start without a real one, the same way it never starts
			// without DATABASE_URL.
			name: "missing TELEGRAM_BOT_TOKEN fails fast",
			env: map[string]string{
				"DATABASE_URL": dbURL,
				"BOT_USERNAME": "savdo_bot",
			},
			wantErr: true,
		},
		{
			name: "empty TELEGRAM_BOT_TOKEN (set but blank) fails fast",
			env: map[string]string{
				"DATABASE_URL":       dbURL,
				"BOT_USERNAME":       "savdo_bot",
				"TELEGRAM_BOT_TOKEN": "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			got, err := Load()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Load() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Fatalf("Load() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

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
			name: "defaults when only the required DATABASE_URL is set",
			env:  map[string]string{"DATABASE_URL": dbURL},
			want: Config{
				Addr:                ":8080",
				LogLevel:            "info",
				DatabaseURL:         dbURL,
				Env:                 "dev",
				SessionWebTTL:       168 * time.Hour,
				SessionMobileTTL:    720 * time.Hour,
				LoginRateIPPerMin:   10,
				LoginRateUserPerMin: 5,
				CookieSecure:        false,
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
				CookieSecure: false,
			},
		},
		{
			name: "prod defaults CookieSecure to true when unset",
			env: map[string]string{
				"DATABASE_URL": dbURL,
				"ENV":          "prod",
			},
			want: Config{
				Addr:                ":8080",
				LogLevel:            "info",
				DatabaseURL:         dbURL,
				Env:                 "prod",
				SessionWebTTL:       168 * time.Hour,
				SessionMobileTTL:    720 * time.Hour,
				LoginRateIPPerMin:   10,
				LoginRateUserPerMin: 5,
				CookieSecure:        true,
			},
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

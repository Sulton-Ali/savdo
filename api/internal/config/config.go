// Package config loads the API's configuration from environment variables
// into a single struct at startup. Missing required values fail fast.
package config

import (
	"fmt"
	"os"
	"time"

	"github.com/caarlos0/env/v11"
)

// Config holds every environment-derived setting the api and savdo binaries
// need.
type Config struct {
	Addr        string `env:"API_ADDR" envDefault:":8080"`
	LogLevel    string `env:"LOG_LEVEL" envDefault:"info"`
	DatabaseURL string `env:"DATABASE_URL,required"`

	// Env is "dev" or "prod". It gates defaults such as CookieSecure.
	Env string `env:"ENV" envDefault:"dev"`

	// SessionWebTTL and SessionMobileTTL are the sliding session lifetimes
	// for the web (cookie) and mobile (bearer token) clients (D-29).
	SessionWebTTL    time.Duration `env:"SESSION_WEB_TTL" envDefault:"168h"`
	SessionMobileTTL time.Duration `env:"SESSION_MOBILE_TTL" envDefault:"720h"`

	// LoginRateIPPerMin and LoginRateUserPerMin bound login attempts per IP
	// and per username per minute (docs/03-ARCHITECTURE.md § Cross-cutting).
	LoginRateIPPerMin   int `env:"LOGIN_RATE_IP_PER_MIN" envDefault:"10"`
	LoginRateUserPerMin int `env:"LOGIN_RATE_USER_PER_MIN" envDefault:"5"`

	// CookieSecure sets the Secure flag on the session cookie. It has no
	// envDefault tag: Load applies the dev/prod-dependent default itself,
	// below, only when the variable was not set at all.
	CookieSecure bool `env:"COOKIE_SECURE"`
}

// Load parses the environment into a Config, applying defaults. It fails
// fast (returns an error) when a required value such as DATABASE_URL is
// missing.
func Load() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}

	// CookieSecure defaults to false in dev and true in prod; an explicit
	// COOKIE_SECURE (either value) always wins over that default.
	if _, explicit := os.LookupEnv("COOKIE_SECURE"); !explicit {
		cfg.CookieSecure = cfg.Env == "prod"
	}

	return cfg, nil
}

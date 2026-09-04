// Package config loads the API's configuration from environment variables
// into a single struct at startup. Missing required values fail fast.
package config

import (
	"fmt"
	"os"
	"path/filepath"
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

	// ShopSlug names the one shop this single-shop MVP serves
	// (docs/03-ARCHITECTURE.md § Auth spec: "Shop resolution"). cmd/api
	// resolves it to a shop id once at startup via GetShopBySlug and
	// fails fast if no such shop exists. A future multi-tenant version
	// replaces this with per-request host/slug resolution (ADR-004).
	ShopSlug string `env:"SHOP_SLUG" envDefault:"savdo-demo"`

	// MediaDir is the local-disk root media.LocalStorage writes under
	// (ADR-008). In prod (docs/07-DEVOPS.md § Production) this is the
	// Docker volume mounted at /data/media. The dev default is relative
	// to cmd/api's working directory, which the documented dev
	// entrypoint (`make api`, Makefile's `api:` target) sets to `api/`
	// — so "../infra/data/media" lands at the repo-root
	// infra/data/media/ docs/07-DEVOPS.md § Local development names.
	MediaDir string `env:"MEDIA_DIR" envDefault:"../infra/data/media"`

	// MediaBaseURL prefixes every media.Storage key to build the URLs
	// MediaFile.urls returns. In dev the API itself serves this prefix
	// (router.go); in prod Caddy does (docs/07-DEVOPS.md § Production).
	MediaBaseURL string `env:"MEDIA_BASE_URL" envDefault:"/media"`

	// MediaMaxBytes caps a single POST /media upload's file part, checked
	// before any derivative is built (the uploaded bytes themselves are
	// never stored — O-16). bodylimit.go's route-aware body limit applies
	// only to /v1/media (a higher, separate cap covering the whole
	// multipart envelope, not just the file part); everything else stays
	// under maxRequestBodyBytes.
	MediaMaxBytes int64 `env:"MEDIA_MAX_BYTES" envDefault:"10485760"`

	// MediaConcurrency bounds how many uploads may be decoding/deriving
	// WebP derivatives at once, process-wide (Review B MAJOR 4 — the
	// CPU- and memory-heavy part of an upload, gated in
	// internal/media/service.go by a semaphore this many slots deep).
	MediaConcurrency int `env:"MEDIA_CONCURRENCY" envDefault:"2"`
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

	// MediaDir's dev default is relative to cmd/api's working directory
	// (see its doc comment above) — fine in dev, where that directory is
	// fixed by convention, but a relative path in prod would resolve
	// against whatever directory the process happened to start in
	// (a systemd unit's WorkingDirectory, a container's WORKDIR, ...),
	// silently writing media somewhere other than the mounted volume.
	// Fail fast here, the same way a missing DATABASE_URL does, rather
	// than let that surface later as files that vanish on redeploy.
	if cfg.Env == "prod" && !filepath.IsAbs(cfg.MediaDir) {
		return Config{}, fmt.Errorf("config: MEDIA_DIR must be an absolute path when ENV=prod (got %q)", cfg.MediaDir)
	}

	return cfg, nil
}

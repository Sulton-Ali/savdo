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
	// LoginRateIPPerMin also sizes the shared per-IP limiter Login,
	// AuthenticateTelegram, ResetPassword and (keyed by Telegram user id
	// instead of IP) CompleteLink all draw from (D-118, auth/service.go's
	// ipLimiter) — it is no longer login-only despite the name.
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

	// PublicShopSlug names the shop `GET /public/*` (Phase 6, D-105) serves
	// — a separate slug from ShopSlug so a future deployment can point the
	// authenticated API and the public landing at different shops; in the
	// single-shop MVP both env vars are the same value. Unlike ShopSlug,
	// this is resolved per request (internal/public.Service), not once at
	// startup — cmd/api only does a best-effort warm-up lookup and logs a
	// warning, never fails startup, since a bad value here degrades one
	// unauthenticated surface, not the whole API. Phase 8 replaces this
	// with a hostname-to-shop lookup (ADR-011 amendment).
	PublicShopSlug string `env:"PUBLIC_SHOP_SLUG" envDefault:"savdo-demo"`

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

	// MediaQueue bounds how many uploads may be admitted (spooling,
	// waiting for a decode slot, or decoding) at once, process-wide —
	// the outer admission gate a Review B follow-up added in front of
	// MediaConcurrency's inner one: without it, an unbounded number of
	// requests could each spool up to MediaMaxBytes to disk and then
	// queue up for a decode slot, filling MEDIA_DIR/.tmp under
	// sustained load. Default 8 (MediaConcurrency's own default × 4);
	// the two aren't derived from each other at parse time — sizing
	// MediaQueue relative to a non-default MediaConcurrency is an
	// operator concern, not this struct's (cmd/api logs a warning at
	// startup if MediaQueue < MediaConcurrency, since that combination
	// can never let every decode slot fill).
	//
	// This gate is process-wide, not per-shop: fine for the single-shop
	// MVP this module ships for (docs/06-ROADMAP.md Phase 2), but a
	// future multi-tenant deployment (ADR-004) sharing one process across
	// shops would let one busy shop's uploads starve every other shop's
	// out of the same queue. Splitting it per-shop is follow-up work for
	// whenever ADR-004's tenant-ready design actually goes multi-process
	// or multi-shop-per-process — not needed now.
	MediaQueue int `env:"MEDIA_QUEUE" envDefault:"8"`

	// AIProvider names the internal/ai.Client implementation cmd/bot builds
	// (ADR-009): "anthropic" (built), "openai_compat" (built, self-hosted
	// endpoints) or "gemini" (registered but not built — O-29; internal/ai
	// returns a typed "provider not built" error for it).
	AIProvider string `env:"AI_PROVIDER" envDefault:"anthropic"`

	// AIModel is the model id passed to the provider as-is (internal/ai's
	// SDK usage note: model ids are plain strings, never a typed
	// constant). Model choice is Q-01; the default is the current
	// generation's cheaper/mid tier.
	AIModel string `env:"AI_MODEL" envDefault:"claude-sonnet-5"`

	// AIPriceInputPerMTok and AIPriceOutputPerMTok are USD per million
	// tokens, decimal strings (ADR-007: no float money), that
	// internal/ai.Usage.CostEstimate is computed from (O-28). Defaults are
	// claude-sonnet-5's published per-token pricing.
	AIPriceInputPerMTok  string `env:"AI_PRICE_INPUT_PER_MTOK" envDefault:"2.00"`
	AIPriceOutputPerMTok string `env:"AI_PRICE_OUTPUT_PER_MTOK" envDefault:"10.00"`

	// BotUsername names the bot the Telegram Login Widget signs requests
	// against and CreateTelegramLink's deep link points users at (ADR-005:
	// `https://t.me/<BOT_USERNAME>?start=link_<code>`) — non-secret, unlike
	// TelegramBotToken below. Required only when ENV=prod, the same
	// precedent as MediaDir's prod-only absolute-path check below: an
	// empty value would make CreateTelegramLink hand back a deep link of
	// `https://t.me/?start=...` — no bot named, so it goes nowhere — so
	// Load fails fast in prod the same way a missing DATABASE_URL does,
	// rather than let that surface later as a dead link in the admin UI.
	// In dev it may be empty, so a fresh clone's `cp infra/.env.example
	// infra/.env && make api` / `make seed` (which loads the full Config
	// for MEDIA_DIR) doesn't need Telegram credentials just to run;
	// CreateTelegramLink itself also refuses to hand back a deep link with
	// no bot named (telegram.go) as a last line of defense.
	BotUsername string `env:"BOT_USERNAME"`

	// AIDailyTokenBudget is O-25/D-120's env-level daily token cap for the
	// bot's own per-shop budget gate (bot.Service.shopOverBudget, via
	// bot.Config.DailyTokenBudget): applied whenever a shop's own
	// ai_daily_token_budget column is NULL. Default 200000 — a real cap,
	// not "unlimited": D-120 reverses O-25's original "NULL means
	// unlimited" reading, so a shop with no budget row set still has a
	// real (generous) daily ceiling by default, not none at all. An
	// explicit 0 here, combined with a NULL shop column, fails the bot
	// closed instead — a static reply, no LLM call — until one of the two
	// is configured.
	AIDailyTokenBudget int `env:"AI_DAILY_TOKEN_BUDGET" envDefault:"200000"`

	// TelegramBotToken is shared by two independent readers of the same
	// secret (D-112, docs/07-DEVOPS.md § Environment variables):
	// AuthenticateTelegram uses it to verify the Telegram Login Widget's
	// HMAC (ADR-005), and cmd/bot/main.go (a distinct read of the same
	// env var, its own Config value) uses it to authenticate every
	// Telegram Bot API call the bot itself makes. Secret; owner-provided
	// in infra/.env only, never in the repo or logs (hard rule 9). Not
	// `,required` here — cmd/api shares this Config and must still start
	// without a bot configured at all (it only degrades the webhook route
	// and Telegram login/link/OTP, see BotWebhookSecret below) — but
	// required when ENV=prod (same precedent as MediaDir below): an empty
	// botToken would make VerifyLoginWidget's secret_key SHA-256("") — a
	// public constant, not a secret — so anyone could forge a valid
	// signature for any linked Telegram id and get a session; Load fails
	// fast on that in prod, the same way a missing DATABASE_URL does. In
	// dev it may be empty — VerifyLoginWidget itself also refuses to run
	// with an empty botToken (telegram.go), and cmd/bot/main.go checks it
	// is non-empty itself and fails fast on its own — as a last line of
	// defense either way, so a fresh clone's `make api`/`make seed` never
	// needs Telegram credentials just to run.
	TelegramBotToken string `env:"TELEGRAM_BOT_TOKEN"`

	// BotMode selects cmd/bot's transport: "polling" (the only one
	// cmd/bot/main.go implements in Phase 7 — "polling stays in
	// cmd/bot") or "webhook" (Phase 8, served by cmd/api instead;
	// cmd/bot/main.go fails fast rather than silently doing nothing if
	// asked for it).
	BotMode string `env:"BOT_MODE" envDefault:"polling"`

	// BotWebhookSecret is the path secret `POST /bot/webhook/{secret}`
	// (docs/05-API.md § Bot) compares in constant time
	// (bot.Handler.HandleBotWebhook). Empty (the default) makes that
	// route 404 for every request — the safe state for a deployment that
	// has not configured a real Telegram webhook yet (Phase 7 ships
	// polling only).
	BotWebhookSecret string `env:"BOT_WEBHOOK_SECRET"`

	// SiteURL is the landing's own base URL, used to build D-115's site
	// links (from a slug, never by the model) and D-116's absolute media
	// URL for a product photo. D-100's placeholder until Phase 8 has a
	// real domain.
	SiteURL string `env:"SITE_URL" envDefault:"http://localhost:3000"`
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

	// BOT_USERNAME and TELEGRAM_BOT_TOKEN are required only in prod (same
	// pattern as MEDIA_DIR above): dev/CI must be able to run `make api`/
	// `make seed` on a fresh clone without Telegram credentials, but a
	// production deployment must never start with Telegram login/link/OTP
	// silently disabled by a missing value.
	if cfg.Env == "prod" && cfg.BotUsername == "" {
		return Config{}, fmt.Errorf("config: BOT_USERNAME is required when ENV=prod")
	}
	if cfg.Env == "prod" && cfg.TelegramBotToken == "" {
		return Config{}, fmt.Errorf("config: TELEGRAM_BOT_TOKEN is required when ENV=prod")
	}

	return cfg, nil
}

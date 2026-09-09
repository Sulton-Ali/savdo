// Command api serves the Savdo HTTP API.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	telegram "github.com/go-telegram/bot"
	"github.com/jackc/pgx/v5"

	"github.com/Sulton-Ali/savdo/api/internal/ai"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/bot"
	"github.com/Sulton-Ali/savdo/api/internal/catalog"
	"github.com/Sulton-Ali/savdo/api/internal/config"
	"github.com/Sulton-Ali/savdo/api/internal/content"
	"github.com/Sulton-Ali/savdo/api/internal/crm"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/httpx"
	"github.com/Sulton-Ali/savdo/api/internal/media"
	"github.com/Sulton-Ali/savdo/api/internal/public"
	"github.com/Sulton-Ali/savdo/api/internal/reports"
	"github.com/Sulton-Ali/savdo/api/internal/sales"
	"github.com/Sulton-Ali/savdo/api/internal/shop"
	"github.com/Sulton-Ali/savdo/api/internal/stock"
)

func main() {
	if err := run(); err != nil {
		slog.Error("api exited with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Open the pool and verify connectivity now, so an unreachable
	// database fails startup immediately instead of surfacing as a wall of
	// per-request errors once traffic arrives (docs/03-ARCHITECTURE.md §
	// Cross-cutting: Health). Never log cfg.DatabaseURL: it carries the DB
	// password.
	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	queries := db.New(pool)

	// Resolve the one shop this MVP serves (docs/03-ARCHITECTURE.md §
	// Auth spec: "Shop resolution"). A future multi-tenant version
	// replaces this with per-request host/slug resolution (ADR-004); for
	// now, failing fast here — rather than lazily on the first request —
	// means a misconfigured or unseeded deployment never serves traffic
	// at all.
	shopRow, err := queries.GetShopBySlug(ctx, cfg.ShopSlug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("no shop with slug %q — run `savdo seed`", cfg.ShopSlug)
		}
		return fmt.Errorf("load shop %q: %w", cfg.ShopSlug, err)
	}

	authSvc := auth.NewService(pool, queries, cfg, shopRow.ID)
	// BOT_USERNAME/TELEGRAM_BOT_TOKEN are required only when ENV=prod
	// (config.Load); in dev/CI they may be empty so a fresh clone's
	// `make api`/`make seed` doesn't need Telegram credentials just to
	// run. Loud, one-line, values-free warning so that's visible at
	// startup rather than only discovered later as a 401/500 on
	// /auth/telegram or /auth/telegram/link — same posture as the
	// PUBLIC_SHOP_SLUG warm-up warning below.
	if cfg.BotUsername == "" || cfg.TelegramBotToken == "" {
		logger.Warn("BOT_USERNAME/TELEGRAM_BOT_TOKEN not set; Telegram login, link and OTP are disabled until both are configured")
	}
	shopSvc := shop.NewService(pool, queries)
	catalogSvc := catalog.NewService(pool, queries, shopRow.DefaultLocale, cfg.MediaBaseURL)
	stockSvc := stock.NewService(pool, queries)
	crmSvc := crm.NewService(queries)
	reportsSvc := reports.NewService(queries)
	salesSvc := sales.NewService(queries)
	contentSvc := content.NewService(queries)

	// publicSvc serves GET /public/* (Phase 6, D-105) for whatever shop
	// Config.PublicShopSlug names — resolved per request (cached), not
	// once here like shopRow above, since Phase 8 replaces this with a
	// hostname lookup and the resolution point should not move again
	// (public.Service's own doc comment). Wired as the invalidator for
	// content and catalog writes (content.Invalidator/catalog.Invalidator
	// — see those types' doc comments for why the interface is declared
	// there, not in internal/public) so a content PUT or a catalogue
	// product/category/variant/image write clears the affected shop's
	// cached public responses.
	publicSvc := public.NewService(queries, contentSvc, cfg.PublicShopSlug, cfg.MediaBaseURL)
	contentSvc.SetInvalidator(publicSvc)
	catalogSvc.SetInvalidator(publicSvc)
	// Best-effort startup check: unlike ShopSlug above, a bad
	// PUBLIC_SHOP_SLUG never fails startup — it only degrades the public
	// landing (every GET /public/* 404s until it is fixed), not the
	// authenticated API.
	if err := publicSvc.WarmShop(ctx); err != nil {
		logger.Warn("PUBLIC_SHOP_SLUG did not resolve to a shop; GET /public/* will 404 until fixed",
			"slug", cfg.PublicShopSlug, "error", err)
	}

	// LocalStorage writes under Config.MediaDir (ADR-008); mediaSvc caps
	// an upload's file part at Config.MediaMaxBytes, bounds concurrent
	// decode/derive work at Config.MediaConcurrency, and builds derivative
	// URLs under Config.MediaBaseURL. In dev the API also serves the same
	// directory itself (devMedia below); in prod Caddy does
	// (docs/07-DEVOPS.md § Production), so devMedia stays nil there.
	mediaStorage, err := media.NewLocalStorage(cfg.MediaDir, cfg.MediaBaseURL)
	if err != nil {
		return fmt.Errorf("init media storage: %w", err)
	}
	// Best-effort startup housekeeping: remove any spool or atomic-write
	// temp file an earlier crash left behind (Review B MINOR 10). Not a
	// correctness requirement — SweepTemp logs and continues past any
	// single file it can't remove — so it never blocks startup.
	mediaStorage.SweepTemp(time.Hour)
	// A queue smaller than the concurrency it's supposed to feed can
	// never let every decode slot fill — not a config error worth
	// failing startup over (the server still works, just under-uses its
	// own concurrency budget), but worth a loud warning.
	if cfg.MediaQueue < cfg.MediaConcurrency {
		logger.Warn("MEDIA_QUEUE is smaller than MEDIA_CONCURRENCY; some decode slots will never be reachable",
			"media_queue", cfg.MediaQueue, "media_concurrency", cfg.MediaConcurrency)
	}
	mediaSvc := media.NewService(queries, mediaStorage, cfg.MediaBaseURL, cfg.MediaMaxBytes, cfg.MediaConcurrency, cfg.MediaQueue)
	var devMedia http.Handler
	if cfg.Env != "prod" {
		devMedia = media.DevHandler(mediaStorage)
	}

	// botSvc backs the two admin `/bot/conversations*` operations
	// unconditionally (they only ever touch the database, never
	// Telegram/the LLM) and `POST /bot/webhook/{secret}` only once the
	// owner sets BOT_WEBHOOK_SECRET (Phase 7 ships polling only via
	// cmd/bot, docs/00-DECISIONS.md D-114's own note; the default empty
	// secret makes that route 404 for every request regardless of
	// whether aiClient/botSender below are fully wired). aiClient is
	// built the same way cmd/bot/main.go builds its own — a config
	// error (a bad AI_PROVIDER/AI_PRICE_* value) fails startup the same
	// way an unreachable database does, since it is a real
	// misconfiguration, not "the bot isn't set up yet".
	aiClient, err := ai.New(ai.Config{
		Provider: cfg.AIProvider, Model: cfg.AIModel,
		PriceInputPerMTok: cfg.AIPriceInputPerMTok, PriceOutputPerMTok: cfg.AIPriceOutputPerMTok,
	})
	if err != nil {
		return fmt.Errorf("init ai client: %w", err)
	}
	// botShopRow resolves PUBLIC_SHOP_SLUG (bot.Config.ShopID's own doc
	// comment: the bot answers for the same shop the public landing
	// does) — best-effort like publicSvc.WarmShop above: a bad
	// PUBLIC_SHOP_SLUG only degrades /bot/webhook/*, which is unreachable
	// anyway until BOT_WEBHOOK_SECRET is set, never the whole API.
	botShopRow, err := queries.GetShopBySlug(ctx, cfg.PublicShopSlug)
	if err != nil {
		logger.Warn("PUBLIC_SHOP_SLUG did not resolve to a shop; POST /bot/webhook/* will error until fixed",
			"slug", cfg.PublicShopSlug, "error", err)
	}

	// Item 10: BOT_WEBHOOK_SECRET set with no TELEGRAM_BOT_TOKEN means
	// every webhook update would need to reply through a botSender that
	// can never exist — fail fast at startup rather than let the first
	// real webhook call discover it (bot.nilSender turns that into a
	// logged error instead of a panic if this check is ever bypassed, but
	// this is the real fix: a webhook nobody can ever get a reply from is
	// a misconfiguration, not a degrade-gracefully case).
	if err := validateBotWebhookConfig(cfg); err != nil {
		return err
	}

	// botSender only does outgoing Telegram Bot API calls (send message/
	// photo) — cmd/api never polls or registers a webhook itself
	// (cmd/bot's own doc comment: "polling stays in cmd/bot"). Built
	// with WithSkipGetMe so an unreachable/invalid TELEGRAM_BOT_TOKEN
	// degrades only the not-yet-enabled webhook route, never cmd/api's
	// own startup, unlike cmd/bot's own readiness check (main.go there),
	// which must fail fast because sending replies is its only job.
	var botSender bot.Sender
	if cfg.TelegramBotToken != "" {
		tgBot, err := telegram.New(cfg.TelegramBotToken, telegram.WithSkipGetMe())
		if err != nil {
			logger.Warn("failed to init the Telegram Bot API client; POST /bot/webhook/* will fail to send replies until fixed", "error", err)
		} else {
			botSender = bot.TelegramSender{Bot: tgBot}
		}
	}
	botSvc := bot.NewService(pool, queries, aiClient, public.NewHandler(publicSvc), contentSvc, botSender, nil,
		bot.Config{
			ShopID: botShopRow.ID, SiteURL: cfg.SiteURL,
			PriceInputPerMTok: cfg.AIPriceInputPerMTok, PriceOutputPerMTok: cfg.AIPriceOutputPerMTok,
			DailyTokenBudget: cfg.AIDailyTokenBudget,
		}, nil, logger)

	srv := &http.Server{
		Addr: cfg.Addr,
		Handler: httpx.NewRouter(logger, pool, authSvc, shopSvc, mediaSvc, devMedia, catalogSvc, stockSvc, crmSvc, reportsSvc, salesSvc, contentSvc, publicSvc,
			botSvc, cfg.BotWebhookSecret),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("api listening", "addr", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		stop()
		logger.Info("shutting down")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return <-serveErr
	}
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	return slog.New(handler)
}

// validateBotWebhookConfig is item 10's own startup guard: a configured
// BOT_WEBHOOK_SECRET with an empty TELEGRAM_BOT_TOKEN would leave
// botSender a nil bot.Sender (below) — bot.NewService's own nilSender
// guard (internal/bot/sender.go) turns that into a logged error instead
// of a panic if this check is ever bypassed, but the real fix is never
// starting up in that combination at all: a webhook that can never send
// a single reply is a misconfiguration, not something to degrade
// gracefully into (unlike a bad PUBLIC_SHOP_SLUG or an unreachable
// Telegram Bot API, which only degrade the not-yet-enabled webhook
// route).
func validateBotWebhookConfig(cfg config.Config) error {
	if cfg.BotWebhookSecret != "" && cfg.TelegramBotToken == "" {
		return errors.New("BOT_WEBHOOK_SECRET is set but TELEGRAM_BOT_TOKEN is empty; the webhook could never send a reply")
	}
	return nil
}

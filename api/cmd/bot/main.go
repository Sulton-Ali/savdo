// Command bot runs Savdo's Telegram bot via long polling
// (docs/00-DECISIONS.md D-111/D-113..D-116, O-24..O-29): the only
// transport Phase 7 implements (BOT_MODE=polling, the default). Webhook
// mode is Phase 8's job, served by cmd/api instead (internal/httpx/bot.go's
// HandleBotWebhook) — this binary fails fast rather than silently doing
// nothing if BOT_MODE=webhook is set.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/jackc/pgx/v5"

	"github.com/Sulton-Ali/savdo/api/internal/ai"
	"github.com/Sulton-Ali/savdo/api/internal/bot"
	"github.com/Sulton-Ali/savdo/api/internal/config"
	"github.com/Sulton-Ali/savdo/api/internal/content"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/public"
)

// botShutdownTimeout bounds how long run() waits (svc.Close, MAJOR 2)
// for an already-dispatched turn to finish before closing the database
// pool anyway — generous relative to chat.go's own worst case (5 rounds
// * 45s chatCallTimeout), but not unbounded: a wedged turn must not hold
// process shutdown open forever.
const botShutdownTimeout = 30 * time.Second

func main() {
	if err := run(); err != nil {
		slog.Error("bot exited with error", "error", err)
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

	if cfg.BotMode != "polling" {
		return fmt.Errorf("cmd/bot only implements BOT_MODE=polling in Phase 7 (got %q); webhook mode is served by cmd/api instead (Phase 8)", cfg.BotMode)
	}

	// Startup readiness: fail fast on a missing secret rather than start
	// a bot that can never do anything useful — neither value is ever
	// included in the returned error (hard rule 9).
	if cfg.TelegramBotToken == "" {
		return errors.New("TELEGRAM_BOT_TOKEN is required to run cmd/bot")
	}
	if cfg.AIProvider == "anthropic" && os.Getenv("ANTHROPIC_API_KEY") == "" {
		return errors.New("ANTHROPIC_API_KEY is required to run cmd/bot when AI_PROVIDER=anthropic")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	queries := db.New(pool)

	// The bot answers for PUBLIC_SHOP_SLUG's shop (bot.Config.ShopID's
	// own doc comment: "the same shop internal/public's own Service
	// reads through") — resolved once, failing fast: unlike cmd/api,
	// this binary has nothing else to degrade gracefully into if it
	// cannot resolve one.
	shopRow, err := queries.GetShopBySlug(ctx, cfg.PublicShopSlug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("no shop with slug %q — run `savdo seed`", cfg.PublicShopSlug)
		}
		return fmt.Errorf("load shop %q: %w", cfg.PublicShopSlug, err)
	}

	aiClient, err := ai.New(ai.Config{
		Provider: cfg.AIProvider, Model: cfg.AIModel,
		PriceInputPerMTok: cfg.AIPriceInputPerMTok, PriceOutputPerMTok: cfg.AIPriceOutputPerMTok,
	})
	if err != nil {
		return fmt.Errorf("init ai client: %w", err)
	}

	contentSvc := content.NewService(queries)
	publicSvc := public.NewService(queries, contentSvc, cfg.PublicShopSlug, cfg.MediaBaseURL)
	pubHandler := public.NewHandler(publicSvc)

	// tgBot both polls Telegram for updates (Start, below) and sends
	// every reply (bot.TelegramSender wraps this same instance) — one
	// long-lived client for the process's whole life. New's own GetMe
	// check (not skipped, unlike cmd/api's best-effort webhook-sender
	// client) is this binary's live confirmation that TELEGRAM_BOT_TOKEN
	// is not just present but actually valid; its errors never include
	// the token itself (the SDK redacts it in every wrapped URL error).
	// WithNotAsyncHandlers disables go-telegram/bot's own default
	// dispatch (github.com/go-telegram/bot@v1.25.0/process_update.go: a
	// bare `go r(ctx, b, upd)` per update, no recover, no concurrency
	// cap) — the registered handler below calls bot.Service.Dispatch
	// instead, which provides both, shared with httpx.HandleBotWebhook
	// (item 8/9 of the Phase 7 T4 fix wave).
	tgBot, err := telegram.New(cfg.TelegramBotToken,
		telegram.WithErrorsHandler(func(err error) {
			// MINOR 5: the SDK hands this handler its own HTTP/decode error,
			// which for some failures (a malformed request the Bot API
			// rejected) can echo request content back in its own message —
			// log the type only, like logProviderError/logWriteError
			// (internal/bot).
			logger.Error("telegram bot error", "error_type", fmt.Sprintf("%T", err))
		}),
		telegram.WithNotAsyncHandlers(),
	)
	if err != nil {
		return fmt.Errorf("init telegram bot client: %w", err)
	}

	svc := bot.NewService(pool, queries, aiClient, pubHandler, contentSvc, bot.TelegramSender{Bot: tgBot}, nil,
		bot.Config{
			ShopID: shopRow.ID, SiteURL: cfg.SiteURL,
			PriceInputPerMTok: cfg.AIPriceInputPerMTok, PriceOutputPerMTok: cfg.AIPriceOutputPerMTok,
			DailyTokenBudget: cfg.AIDailyTokenBudget,
		}, nil, logger)
	// MAJOR 2: Dispatch's own goroutines run under svc's baseCtx, not this
	// function's own ctx (internal/bot/service.go's own doc comment), so
	// they keep running past tgBot.Start(ctx) returning on SIGTERM —
	// svc.Close waits (bounded by botShutdownTimeout) for every already-
	// accepted turn to finish persisting before pool.Close() (deferred
	// above) runs out from under it; this defer is registered *after*
	// pool.Close()'s own, so it runs first on unwind (LIFO).
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), botShutdownTimeout)
		defer cancel()
		if err := svc.Close(shutdownCtx); err != nil {
			logger.Warn("bot: graceful shutdown timed out; some in-flight turns may not have finished", "error", err)
		}
	}()

	// Customer mode only (D-111): every update reaches Dispatch, which
	// itself ignores anything HandleUpdate would (a non-text message) —
	// no go-telegram/bot pattern-matched handler is registered beyond
	// this one, since Dispatch/HandleUpdate is its own single dispatcher
	// (update.go's own doc comment). Dispatch (not HandleUpdate directly)
	// so this shares Service's own panic recovery, concurrency cap and
	// per-chat serialization with the webhook path (dispatch.go).
	tgBot.RegisterHandlerMatchFunc(
		func(*models.Update) bool { return true },
		func(_ context.Context, _ *telegram.Bot, update *models.Update) {
			svc.Dispatch(update)
		},
	)

	logger.Info("bot polling started", "shop_id", shopRow.ID, "shop_slug", cfg.PublicShopSlug)
	tgBot.Start(ctx)
	logger.Info("bot stopped")
	return nil
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	return slog.New(handler)
}

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

	"github.com/jackc/pgx/v5"

	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/config"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/httpx"
	"github.com/Sulton-Ali/savdo/api/internal/media"
	"github.com/Sulton-Ali/savdo/api/internal/shop"
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

	authSvc := auth.NewService(queries, cfg, shopRow.ID)
	shopSvc := shop.NewService(pool, queries)

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
	mediaSvc := media.NewService(queries, mediaStorage, cfg.MediaBaseURL, cfg.MediaMaxBytes, cfg.MediaConcurrency, cfg.MediaQueue)
	var devMedia http.Handler
	if cfg.Env != "prod" {
		devMedia = media.DevHandler(mediaStorage)
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           httpx.NewRouter(logger, pool, authSvc, shopSvc, mediaSvc, devMedia),
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

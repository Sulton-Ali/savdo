// This file is reply formatting. It deliberately never sends Telegram's
// MarkdownV2 parse mode (D-119: "Bot replies stay plain text in Phase 7
// (no MarkdownV2); MarkdownV2 is a Phase 8 polish candidate"):
// MarkdownV2's escaping rules (a fixed set of ASCII punctuation must be
// backslash-escaped *everywhere*, including inside what would otherwise
// look like an already-escaped sequence) are exactly the kind of trap a
// single missed character turns into a Telegram 400 that drops the
// whole reply. Plain text sidesteps it entirely: Telegram still
// auto-links bare http(s) URLs in a plain-text message, so D-115's site
// links stay clickable without either risk.

package bot

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// productURL and categoryURL build D-115's landing links from a slug —
// never by the model, always here, from the exact routes web/src/routes
// defines: `/$locale/p/$slug` (web/src/components/ProductCard.tsx) and
// `/$locale/c/$slug`.
func productURL(siteURL, locale, slug string) string {
	return fmt.Sprintf("%s/%s/p/%s", strings.TrimRight(siteURL, "/"), locale, slug)
}

func categoryURL(siteURL, locale, slug string) string {
	return fmt.Sprintf("%s/%s/c/%s", strings.TrimRight(siteURL, "/"), locale, slug)
}

// absoluteMediaURL resolves a MediaUrls path (e.g. "/media/xyz_card.webp",
// internal/media.URLs' own join of Config.MediaBaseURL) against siteURL
// for D-116's `sendPhoto`, which needs an absolute URL — Telegram cannot
// resolve a relative path the way a browser resolves it against the page
// it loaded. Already-absolute values (a prod deployment could point
// MEDIA_BASE_URL at a CDN host) pass through unchanged.
func absoluteMediaURL(siteURL, path string) string {
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return strings.TrimRight(siteURL, "/") + path
}

// telegramCaptionMaxLen is Telegram's own limit on a sendPhoto caption's
// length (Bot API docs, `caption`: 0-1024 characters) — characters, not
// bytes: len(string) counts UTF-8 bytes, which would cap a Cyrillic or
// other multi-byte-per-rune answer at roughly a third of what Telegram
// itself actually allows (MINOR 3), wrongly forcing the two-message
// fallback for an answer that would have fit as a caption.
const telegramCaptionMaxLen = 1024

// sendAnswer sends outcome to chatID: D-116's product photo when
// outcome.Photo names one with a cover image, captioned with the
// model's own answer text (never a synthesized "name/price/availability"
// string — MAJOR 7's own fix: the *answer* accompanies the photo, so the
// admin transcript, which persists outcome.Text as the row's content,
// always matches what the customer actually read). An answer longer
// than Telegram's own 1024-character caption limit is sent as a bare
// photo followed by the answer as its own text message instead of being
// truncated. Falls back to a plain text message when there is no cover
// image, or when sending the photo fails.
func (s *Service) sendAnswer(ctx context.Context, shop db.Shop, chatID int64, outcome llmOutcome) {
	if outcome.Photo != nil && outcome.Photo.CoverURL != "" {
		url := absoluteMediaURL(s.cfg.SiteURL, outcome.Photo.CoverURL)
		caption, sendTextAfter := outcome.Text, false
		if utf8.RuneCountInString(caption) > telegramCaptionMaxLen {
			caption, sendTextAfter = "", true
		}
		if err := s.sender.SendPhoto(ctx, chatID, url, caption); err == nil {
			if sendTextAfter {
				if err := s.sender.SendMessage(ctx, chatID, outcome.Text); err != nil {
					s.logger.Error("bot: send message failed", "shop_id", shop.ID, "error", err)
				}
			}
			return
		}
		s.logger.Warn("bot: send photo failed, falling back to text", "shop_id", shop.ID)
	}
	if err := s.sender.SendMessage(ctx, chatID, outcome.Text); err != nil {
		s.logger.Error("bot: send message failed", "shop_id", shop.ID, "error", err)
	}
}

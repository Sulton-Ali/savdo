// This file is reply formatting. It deliberately never sends Telegram's
// MarkdownV2 parse mode: docs/00-DECISIONS.md's task spec allows "plain
// text or Telegram MarkdownV2 escaped properly", and MarkdownV2's escaping
// rules (a fixed set of ASCII punctuation must be backslash-escaped
// *everywhere*, including inside what would otherwise look like an
// already-escaped sequence) are exactly the "known trap" this task calls
// out — a single missed character turns into a Telegram 400 that drops
// the whole reply. Plain text sidesteps it entirely: Telegram still
// auto-links bare http(s) URLs in a plain-text message, so D-115's site
// links stay clickable without either risk.

package bot

import (
	"context"
	"fmt"
	"strings"

	"github.com/Sulton-Ali/savdo/api/gen"
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

// availabilityWord renders a gen.Availability for a plain-text caption/
// tool text a customer reads directly — never the raw enum value, which
// would read as an internal code rather than a sentence a customer
// understands, in the message's own language (D-113).
func availabilityWord(a, locale string) string {
	t := localeTexts(locale)
	switch gen.Availability(a) {
	case gen.InStock:
		return t.availabilityInStock
	case gen.Low:
		return t.availabilityLow
	default:
		return t.availabilityOutOfStock
	}
}

// sendAnswer sends outcome to chatID: D-116's product photo when
// outcome.Photo names one (falling back to plain text if the photo has
// no cover image, or if sending it fails), otherwise the plain-text
// answer itself.
func (s *Service) sendAnswer(ctx context.Context, shop db.Shop, chatID int64, locale string, outcome llmOutcome) {
	if outcome.Photo != nil && outcome.Photo.CoverURL != "" {
		url := absoluteMediaURL(s.cfg.SiteURL, outcome.Photo.CoverURL)
		caption := fmt.Sprintf("%s\n%s %s — %s", outcome.Photo.Name, outcome.Photo.Price, shop.Currency, availabilityWord(outcome.Photo.Availability, locale))
		if err := s.sender.SendPhoto(ctx, chatID, url, caption); err == nil {
			return
		}
		s.logger.Warn("bot: send photo failed, falling back to text", "shop_id", shop.ID)
	}
	if err := s.sender.SendMessage(ctx, chatID, outcome.Text); err != nil {
		s.logger.Error("bot: send message failed", "shop_id", shop.ID, "error", err)
	}
}

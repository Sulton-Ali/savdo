// Package content implements the Phase 6 landing content blocks
// (docs/03-ARCHITECTURE.md § Module map: "content" owns content_blocks;
// docs/04-DATA-MODEL.md § 6, D-99, D-104, D-107, O-19). Every operation
// reads its tenant boundary from auth.FromContext (ADR-004) — never from a
// path, query or body parameter — and is gated by auth.PermContentManage
// (owner/manager, docs/04-DATA-MODEL.md § 7 "Landing content"). The public
// composition GET /public/shop (a later task) resolves each key's fallback
// through Resolve; this task's contract stops at GET/PUT /content/{key}.
package content

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// Invalidator is the public-cache-clearing side effect a content write
// triggers (O-20: "cleared for the shop on any content PUT"). Declared
// here, the consumer, rather than in internal/public, the producer, so
// wiring *public.Service in (main.go, via SetInvalidator) never makes
// this package import internal/public — internal/public already imports
// internal/content for Resolve, and a content -> public edge too would
// be a cycle (docs/08-AI-WORKFLOW.md § Known failure modes).
type Invalidator interface {
	Invalidate(shopID uuid.UUID)
}

// Service holds content's dependencies. Every operation is a single
// statement (no multi-table write needs a transaction), so unlike
// stock.Service/shop.Service there is no pool field here — q is enough
// (mirrors crm.Service). invalidator is nil until SetInvalidator runs
// (cmd/api/main.go, after both content.Service and public.Service exist)
// — every call site checks it via invalidatePublic, so a test or a
// caller that never wires one behaves exactly as before this field
// existed.
type Service struct {
	q           *db.Queries
	invalidator Invalidator
}

// NewService builds the content Service.
func NewService(q *db.Queries) *Service {
	return &Service{q: q}
}

// SetInvalidator wires inv as the public-cache invalidator Upsert calls
// after a successful write. Optional: cmd/api's own wiring is the only
// caller today; tests that never call it get the pre-Phase-6 behaviour
// (no invalidation attempted).
func (s *Service) SetInvalidator(inv Invalidator) {
	s.invalidator = inv
}

// invalidatePublic clears shopID's public-response cache, when an
// Invalidator is wired at all — a no-op otherwise, so this is safe to
// call unconditionally from every write path.
func (s *Service) invalidatePublic(shopID uuid.UUID) {
	if s.invalidator != nil {
		s.invalidator.Invalidate(shopID)
	}
}

// Handler implements content's slice of gen.StrictServerInterface
// (GetContent, PutContent — handler.go).
type Handler struct {
	svc *Service
}

// NewHandler wraps svc for the strict server interface.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Get returns every locale saved for key (docs/05-API.md "Content and
// public": GET /content/{key} — "no fallback here"). An unrecognized key
// is a 400 VALIDATION_FAILED — key.Valid() is the generated ContentKey
// enum check (api/gen/api.gen.go), the same guard PutContent runs.
func (s *Service) Get(ctx context.Context, shopID uuid.UUID, key gen.ContentKey) (gen.ContentResource, error) {
	if !key.Valid() {
		return gen.ContentResource{}, apierr.Validation(map[string]string{"key": "invalid"})
	}

	rows, err := s.q.ListContentBlocksByKey(ctx, db.ListContentBlocksByKeyParams{
		ShopID: shopID, Key: db.ContentBlockKey(key),
	})
	if err != nil {
		return gen.ContentResource{}, fmt.Errorf("content: list blocks: %w", err)
	}

	resource := gen.ContentResource{Key: key}
	for _, row := range rows {
		block, err := toLocaleBlock(row)
		if err != nil {
			return gen.ContentResource{}, err
		}
		switch gen.Locale(row.Locale) {
		case gen.LocaleUz:
			resource.Locales.Uz = &block
		case gen.LocaleRu:
			resource.Locales.Ru = &block
		case gen.LocaleEn:
			resource.Locales.En = &block
		}
	}
	return resource, nil
}

// Upsert validates data against key's O-19 shape and saves it as
// (shopID, key, locale)'s content, replacing whatever that locale held
// before — it never touches another locale (D-104: a block may have only
// some locales filled). userID becomes the stored row's updated_by.
//
// A malformed key or locale (either enum's Valid() false — the generated
// binding does not itself reject an out-of-enum string, ContentKey.Valid's
// own doc comment) is 400 VALIDATION_FAILED; a data shape error (missing
// required field, unknown field, or one of the key-specific rules
// validate.go documents) is 422 VALIDATION_FAILED, since the request
// itself is well-formed JSON, just not a valid instance of the key's
// schema (docs/05-API.md § Conventions' 422 bullet).
func (s *Service) Upsert(ctx context.Context, shopID uuid.UUID, key gen.ContentKey, locale gen.Locale, data map[string]interface{}, userID uuid.UUID) (gen.ContentBlock, error) {
	if !key.Valid() {
		return gen.ContentBlock{}, apierr.Validation(map[string]string{"key": "invalid"})
	}
	if !locale.Valid() {
		return gen.ContentBlock{}, apierr.Validation(map[string]string{"locale": "invalid"})
	}

	normalized, fields := normalize(key, data)
	if len(fields) > 0 {
		return gen.ContentBlock{}, apierr.Unprocessable(fields)
	}

	row, err := s.q.UpsertContentBlock(ctx, db.UpsertContentBlockParams{
		ShopID: shopID, Key: db.ContentBlockKey(key), Locale: string(locale),
		Data: normalized, UpdatedBy: &userID,
	})
	if err != nil {
		return gen.ContentBlock{}, fmt.Errorf("content: upsert block: %w", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(row.Data, &out); err != nil {
		return gen.ContentBlock{}, fmt.Errorf("content: decode stored block: %w", err)
	}
	block := gen.ContentBlock{Key: key, Locale: locale, Data: out, UpdatedAt: row.UpdatedAt}
	if row.UpdatedBy != nil {
		u := openapi_types.UUID(*row.UpdatedBy)
		block.UpdatedBy = &u
	}
	s.invalidatePublic(shopID)
	return block, nil
}

// ResolvedBlock is one key's content resolved for a requested locale with
// the D-104 fallback (requested → uz). Not part of this task's contract —
// Get/Upsert's gen types are; Resolve is Go-only plumbing for the
// forthcoming GET /public/shop composition to call directly.
type ResolvedBlock struct {
	// Data is nil when the key has no saved row in any locale.
	Data map[string]interface{}
	// Locale is the locale the data actually came from (may differ from
	// requested); the zero value when Data is nil.
	Locale gen.Locale
	// TranslationFallback is true when Data came from uz because the
	// requested locale had no saved row (D-104); always false for uz
	// itself and when Data is nil.
	TranslationFallback bool
}

// Resolve returns all six keys for shopID, each resolved for locale with
// the D-104 fallback (requested → uz → absent — never a third locale,
// unlike ADR-012's general "requested → uz → any" product-translation
// rule). One query for every key/locale (ListContentBlocksForShop) rather
// than six, since the public landing composition this feeds needs the
// whole set on every call.
func (s *Service) Resolve(ctx context.Context, shopID uuid.UUID, locale gen.Locale) (map[gen.ContentKey]ResolvedBlock, error) {
	rows, err := s.q.ListContentBlocksForShop(ctx, shopID)
	if err != nil {
		return nil, fmt.Errorf("content: list shop blocks: %w", err)
	}

	byKey := map[gen.ContentKey]map[gen.Locale]db.ContentBlock{}
	for _, row := range rows {
		k := gen.ContentKey(row.Key)
		if byKey[k] == nil {
			byKey[k] = map[gen.Locale]db.ContentBlock{}
		}
		byKey[k][gen.Locale(row.Locale)] = row
	}

	out := make(map[gen.ContentKey]ResolvedBlock, len(allKeys))
	for _, key := range allKeys {
		locales := byKey[key]
		if row, ok := locales[locale]; ok {
			data, err := decodeData(row)
			if err != nil {
				return nil, err
			}
			out[key] = ResolvedBlock{Data: data, Locale: locale}
			continue
		}
		if row, ok := locales[gen.LocaleUz]; ok && locale != gen.LocaleUz {
			data, err := decodeData(row)
			if err != nil {
				return nil, err
			}
			out[key] = ResolvedBlock{Data: data, Locale: gen.LocaleUz, TranslationFallback: true}
			continue
		}
		out[key] = ResolvedBlock{}
	}
	return out, nil
}

// allKeys is the O-19 vocabulary in a fixed order, so Resolve's output
// (and any caller ranging over it) is deterministic — Go map iteration
// order is not.
var allKeys = []gen.ContentKey{gen.Hero, gen.About, gen.Hours, gen.Contacts, gen.Social, gen.Seo}

// toLocaleBlock converts one content_blocks row to the wire shape GET
// /content/{key} returns per locale.
func toLocaleBlock(row db.ContentBlock) (gen.ContentLocaleBlock, error) {
	data, err := decodeData(row)
	if err != nil {
		return gen.ContentLocaleBlock{}, err
	}
	block := gen.ContentLocaleBlock{Data: data, UpdatedAt: row.UpdatedAt}
	if row.UpdatedBy != nil {
		u := openapi_types.UUID(*row.UpdatedBy)
		block.UpdatedBy = &u
	}
	return block, nil
}

// decodeData unmarshals a stored row's jsonb data column back into a
// generic map — the data was validated and normalized on write (Upsert),
// so a decode failure here means the row was written some other way; it
// is reported as a 500 rather than silently swallowed.
func decodeData(row db.ContentBlock) (map[string]interface{}, error) {
	var data map[string]interface{}
	if err := json.Unmarshal(row.Data, &data); err != nil {
		return nil, fmt.Errorf("content: decode stored block %s/%s: %w", row.Key, row.Locale, err)
	}
	return data, nil
}

package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/ai"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// The three tool names ADR-009/O-27 fix for the customer-mode bot. The
// registry (toolDefinitions) has exactly these three entries — no other
// name is ever offered to the model, and executeTool's switch below
// rejects anything else without executing it (O-27's own assertion: "the
// tool registry is asserted to be exactly {search_products,
// variant_availability, shop_info}").
const (
	toolSearchProducts      = "search_products"
	toolVariantAvailability = "variant_availability"
	toolShopInfo            = "shop_info"
)

// maxSearchLimit/defaultSearchLimit bound search_products' own `limit`
// input — the task spec's own "limit<=8".
const (
	defaultSearchLimit = 8
	maxSearchLimit     = 8
)

var (
	searchProductsSchema = json.RawMessage(`{
		"type": "object",
		"properties": {
			"q": {"type": "string", "description": "Free-text search over the product name"},
			"category": {"type": "string", "description": "A category slug (from shop_info or a previous search result) to filter by"},
			"limit": {"type": "integer", "minimum": 1, "maximum": 8, "description": "Maximum number of results, default 8"}
		},
		"additionalProperties": false
	}`)

	variantAvailabilitySchema = json.RawMessage(`{
		"type": "object",
		"properties": {
			"slug": {"type": "string", "description": "The product's slug, from a search_products result"}
		},
		"required": ["slug"],
		"additionalProperties": false
	}`)

	shopInfoSchema = json.RawMessage(`{
		"type": "object",
		"properties": {},
		"additionalProperties": false
	}`)
)

// toolDefinitions is the fixed registry every ai.Request in this package
// carries — never more, never fewer (O-27).
func toolDefinitions() []ai.Tool {
	return []ai.Tool{
		{
			Name:        toolSearchProducts,
			Description: "Search the shop's active products by free text and/or category slug. Returns up to `limit` matches, each with id, slug, name, price, availability (a word: in_stock/low/out_of_stock — never a quantity) and categorySlug. Never invent a product that is not in the result.",
			InputSchema: searchProductsSchema,
		},
		{
			Name:        toolVariantAvailability,
			Description: "Get one product's variants by its slug: each variant's attributes (e.g. size, color), price and availability (a word — never a quantity). Use the slug from a search_products result.",
			InputSchema: variantAvailabilitySchema,
		},
		{
			Name:        toolShopInfo,
			Description: "Get the shop's name, opening hours, address and contacts (phone, Telegram, Instagram).",
			InputSchema: shopInfoSchema,
		},
	}
}

// photoCandidate is what D-116 needs to send a product's cover photo:
// filled in by toolSearchProducts (when it returned exactly one hit) or
// toolVariantAvailabilityCall (whenever the product has a cover image),
// carried back up through the tool loop (chat.go) as "the most recent
// tool call's single-product result" — overwritten every round, so only
// the *last* round's value survives to sendAnswer, matching D-116's own
// "the model's final tool call was variant_availability, or a single
// search hit". CoverURL is the only field format.go needs: MAJOR 7's fix
// sends the model's own answer text as the photo's caption, never a
// synthesized "name/price/availability" string, so this no longer
// carries those separately (they were already in the tool result the
// model's own answer is grounded in).
type photoCandidate struct {
	CoverURL string // relative (MediaUrls.Card); format.go resolves it against SiteURL. Empty when the product has no cover image.
}

// strictUnmarshal decodes raw into out, rejecting unknown fields and any
// trailing data — the closest this package gets to "validated against
// the schema" without a full JSON Schema validator (docs/00-DECISIONS.md
// O-27's "reject unknown tool names and bad JSON with a tool error
// result, never execute"). An empty/absent raw is treated as `{}` (valid
// for shop_info, which takes no input).
func strictUnmarshal(raw json.RawMessage, out any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage(`{}`)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if dec.More() {
		return fmt.Errorf("bot: trailing data after tool input")
	}
	return nil
}

func toolOK(callID string, v any) ai.ToolResult {
	b, err := json.Marshal(v)
	if err != nil {
		return toolError(callID, "internal_error")
	}
	return ai.ToolResult{CallID: callID, Content: string(b)}
}

func toolError(callID, reason string) ai.ToolResult {
	return ai.ToolResult{CallID: callID, Content: fmt.Sprintf(`{"error":%q}`, reason)}
}

func toolErrorResult(callID, reason string) ai.ToolResult {
	r := toolError(callID, reason)
	r.IsError = true
	return r
}

// executeTool runs one model-requested tool call against the public-read
// boundary (hard rule 10) and reports a photoCandidate when the result
// names exactly one product (D-116) — nil otherwise. Every branch reads
// through s.pub (internal/public.Handler, the same functions the public
// landing calls) or s.content (internal/content.Service.Resolve); nothing
// here ever touches internal/catalog, internal/stock, internal/crm or
// internal/sales.
func (s *Service) executeTool(ctx context.Context, shop db.Shop, locale string, call ai.ToolCall) (ai.ToolResult, *photoCandidate) {
	switch call.Name {
	case toolSearchProducts:
		return s.toolCallSearchProducts(ctx, locale, call)
	case toolVariantAvailability:
		return s.toolCallVariantAvailability(ctx, locale, call)
	case toolShopInfo:
		return s.toolCallShopInfo(ctx, shop, locale, call), nil
	default:
		return toolErrorResult(call.ID, "unknown_tool"), nil
	}
}

type toolProductItem struct {
	ID           string `json:"id"`
	Slug         string `json:"slug"`
	Name         string `json:"name"`
	Price        string `json:"price"`
	Availability string `json:"availability"`
	CategorySlug string `json:"categorySlug,omitempty"`
	URL          string `json:"url"`
}

type searchProductsInput struct {
	Q        *string `json:"q"`
	Category *string `json:"category"`
	Limit    *int    `json:"limit"`
}

func (s *Service) toolCallSearchProducts(ctx context.Context, locale string, call ai.ToolCall) (ai.ToolResult, *photoCandidate) {
	var in searchProductsInput
	if err := strictUnmarshal(call.Input, &in); err != nil {
		return toolErrorResult(call.ID, "invalid_input"), nil
	}
	limit := defaultSearchLimit
	if in.Limit != nil {
		limit = *in.Limit
	}
	if limit < 1 {
		limit = 1
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}

	resp, err := withLocale(ctx, locale, func(ctx context.Context) (gen.ListPublicProductsResponseObject, error) {
		return s.pub.ListPublicProducts(ctx, gen.ListPublicProductsRequestObject{
			Params: gen.ListPublicProductsParams{Q: in.Q, Category: in.Category, Limit: &limit},
		})
	})
	if err != nil {
		return toolErrorResult(call.ID, "lookup_failed"), nil
	}
	page, ok := resp.(gen.ListPublicProducts200JSONResponse)
	if !ok {
		return toolErrorResult(call.ID, "lookup_failed"), nil
	}

	items := make([]toolProductItem, 0, len(page.Items))
	for _, it := range page.Items {
		item := toolProductItem{
			ID: it.Id.String(), Slug: it.Slug, Name: it.Name,
			Price: it.Price.Current, Availability: string(it.Availability),
			URL: productURL(s.cfg.SiteURL, locale, it.Slug),
		}
		if cat, err := it.CategorySlug.Get(); err == nil {
			item.CategorySlug = cat
		}
		items = append(items, item)
	}

	var photo *photoCandidate
	if len(items) == 1 {
		var coverURL string
		if page.Items[0].CoverImage != nil {
			coverURL = page.Items[0].CoverImage.Urls.Card
		}
		photo = &photoCandidate{CoverURL: coverURL}
	}

	return toolOK(call.ID, map[string]any{"items": items}), photo
}

type variantAvailabilityInput struct {
	Slug string `json:"slug"`
}

type toolVariant struct {
	Attributes   gen.AttributeValues `json:"attributes"`
	Price        string              `json:"price"`
	Availability string              `json:"availability"`
}

func (s *Service) toolCallVariantAvailability(ctx context.Context, locale string, call ai.ToolCall) (ai.ToolResult, *photoCandidate) {
	var in variantAvailabilityInput
	if err := strictUnmarshal(call.Input, &in); err != nil || strings.TrimSpace(in.Slug) == "" {
		return toolErrorResult(call.ID, "invalid_input"), nil
	}

	resp, err := withLocale(ctx, locale, func(ctx context.Context) (gen.GetPublicProductBySlugResponseObject, error) {
		return s.pub.GetPublicProductBySlug(ctx, gen.GetPublicProductBySlugRequestObject{Slug: in.Slug})
	})
	if err != nil {
		var apiErr *apierr.Error
		if errors.As(err, &apiErr) && apiErr.Code == gen.NOTFOUND {
			return toolErrorResult(call.ID, "not_found"), nil
		}
		return toolErrorResult(call.ID, "lookup_failed"), nil
	}
	product, ok := resp.(gen.GetPublicProductBySlug200JSONResponse)
	if !ok {
		return toolErrorResult(call.ID, "lookup_failed"), nil
	}

	var variants []toolVariant
	if product.Variants != nil {
		for _, v := range *product.Variants {
			variants = append(variants, toolVariant{
				Attributes: v.Attributes, Price: v.Price.Current, Availability: string(v.Availability),
			})
		}
	}

	out := struct {
		Slug     string        `json:"slug"`
		Name     string        `json:"name"`
		URL      string        `json:"url"`
		Variants []toolVariant `json:"variants"`
	}{Slug: product.Slug, Name: product.Name, URL: productURL(s.cfg.SiteURL, locale, product.Slug), Variants: variants}

	var coverURL string
	if product.Images != nil {
		coverURL = coverImageURL(*product.Images)
	}
	photo := &photoCandidate{CoverURL: coverURL}

	return toolOK(call.ID, out), photo
}

// coverImageURL picks D-83's cover image (isCover, else first by
// position — images already arrive sorted by position, mirroring
// internal/public's own coverImages) and returns its card-size URL, or
// "" when the product has none.
func coverImageURL(images []gen.ProductImage) string {
	if len(images) == 0 {
		return ""
	}
	for _, img := range images {
		if img.IsCover {
			return img.Urls.Card
		}
	}
	return images[0].Urls.Card
}

type toolHoursDay struct {
	Day    string `json:"day"`
	Closed bool   `json:"closed"`
	Open   string `json:"open,omitempty"`
	Close  string `json:"close,omitempty"`
}

func (s *Service) toolCallShopInfo(ctx context.Context, shop db.Shop, locale string, call ai.ToolCall) ai.ToolResult {
	if err := strictUnmarshal(call.Input, &struct{}{}); err != nil {
		return toolErrorResult(call.ID, "invalid_input")
	}

	resolved, err := s.content.Resolve(ctx, shop.ID, gen.Locale(locale))
	if err != nil {
		return toolErrorResult(call.ID, "lookup_failed")
	}

	out := struct {
		Name      string         `json:"name"`
		Hours     []toolHoursDay `json:"hours,omitempty"`
		HoursNote string         `json:"hoursNote,omitempty"`
		Address   string         `json:"address,omitempty"`
		Phone     string         `json:"phone,omitempty"`
		MapURL    string         `json:"mapUrl,omitempty"`
		Telegram  string         `json:"telegram,omitempty"`
		Instagram string         `json:"instagram,omitempty"`
	}{Name: shop.Name}

	if block, ok := resolved[gen.Hours]; ok && block.Data != nil {
		var h gen.ContentHours
		if err := decodeInto(block.Data, &h); err == nil {
			for _, d := range h.Days {
				day := toolHoursDay{Day: string(d.Day), Closed: d.Closed}
				if d.Open != nil {
					day.Open = *d.Open
				}
				if d.Close != nil {
					day.Close = *d.Close
				}
				out.Hours = append(out.Hours, day)
			}
			if h.Note != nil {
				out.HoursNote = *h.Note
			}
		}
	}
	if block, ok := resolved[gen.Contacts]; ok && block.Data != nil {
		var c gen.ContentContacts
		if err := decodeInto(block.Data, &c); err == nil {
			out.Address, out.Phone = c.Address, c.Phone
			if c.MapUrl != nil {
				out.MapURL = *c.MapUrl
			}
		}
	}
	if block, ok := resolved[gen.Social]; ok && block.Data != nil {
		var soc gen.ContentSocial
		if err := decodeInto(block.Data, &soc); err == nil {
			if soc.Telegram != nil {
				out.Telegram = *soc.Telegram
			}
			if soc.Instagram != nil {
				out.Instagram = *soc.Instagram
			}
		}
	}

	return toolOK(call.ID, out)
}

// decodeInto round-trips a resolved content block's generic
// map[string]interface{} into its O-19 typed shape — mirrors
// internal/public's own unexported decodeBlock (public/convert.go),
// duplicated here for the same "different package, no shared export"
// reason internal/public/pricing.go's promoActive doc comment gives for
// its own duplication of internal/sales' copy.
func decodeInto(data map[string]interface{}, out any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

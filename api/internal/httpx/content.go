package httpx

import (
	"context"

	"github.com/Sulton-Ali/savdo/api/gen"
)

// The two methods below satisfy gen.StrictServerInterface's `/content/{key}`
// operations by forwarding to server.content (*content.Handler) — named,
// not embedded, for the same reason server.shop/server.crm are
// (healthz.go's doc comment on the server struct).

// GetContent gets a content block's saved locales.
func (s server) GetContent(ctx context.Context, req gen.GetContentRequestObject) (gen.GetContentResponseObject, error) {
	return s.content.GetContent(ctx, req)
}

// PutContent saves one locale of a content block.
func (s server) PutContent(ctx context.Context, req gen.PutContentRequestObject) (gen.PutContentResponseObject, error) {
	return s.content.PutContent(ctx, req)
}

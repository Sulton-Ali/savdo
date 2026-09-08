package content

import (
	"context"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
)

// GetContent returns every locale saved for key. Requires content.manage
// (manager+, docs/04-DATA-MODEL.md § 7 "Landing content").
func (h *Handler) GetContent(ctx context.Context, req gen.GetContentRequestObject) (gen.GetContentResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermContentManage); err != nil {
		return nil, err
	}

	resource, err := h.svc.Get(ctx, authCtx.ShopID, req.Key)
	if err != nil {
		return nil, err
	}
	return gen.GetContent200JSONResponse(resource), nil
}

// PutContent upserts one locale of key's content. Requires content.manage
// (manager+). data is validated against key's O-19 schema in
// Service.Upsert; a shape error is 422 VALIDATION_FAILED, an unrecognized
// key or locale is 400 VALIDATION_FAILED (Service.Upsert's own doc
// comment).
func (h *Handler) PutContent(ctx context.Context, req gen.PutContentRequestObject) (gen.PutContentResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermContentManage); err != nil {
		return nil, err
	}
	body := req.Body

	block, err := h.svc.Upsert(ctx, authCtx.ShopID, req.Key, body.Locale, body.Data, authCtx.UserID)
	if err != nil {
		return nil, err
	}
	return gen.PutContent200JSONResponse(block), nil
}

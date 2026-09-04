package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"

	"github.com/Sulton-Ali/savdo/api/gen"
	"github.com/Sulton-Ali/savdo/api/internal/apierr"
	"github.com/Sulton-Ali/savdo/api/internal/auth"
	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// Handler implements gen.StrictServerInterface's UploadMedia operation.
// internal/httpx forwards to it (media.go), the same pattern
// internal/httpx/shop.go uses for *shop.Handler.
type Handler struct {
	svc *Service
}

// NewHandler wraps svc for the strict server interface.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// UploadMedia uploads an image (POST /media). Requires catalog.write
// (manager+, docs/05-API.md § Catalogue and media).
func (h *Handler) UploadMedia(ctx context.Context, req gen.UploadMediaRequestObject) (gen.UploadMediaResponseObject, error) {
	authCtx, ok := auth.FromContext(ctx)
	if !ok {
		return nil, apierr.Unauthenticated()
	}
	if err := auth.Require(ctx, auth.PermCatalogWrite); err != nil {
		return nil, err
	}

	part, err := findFilePart(req.Body)
	if err != nil {
		return nil, err
	}
	defer func() { _ = part.Close() }()

	row, err := h.svc.Upload(ctx, authCtx.ShopID, authCtx.UserID, part)
	if err != nil {
		return nil, fmt.Errorf("media: upload: %w", err)
	}

	return gen.UploadMedia201JSONResponse(toGenMediaFile(h.svc.BaseURL(), row)), nil
}

// findFilePart scans a multipart request for the part named "file"
// (docs/06-ROADMAP.md Phase 2 T3 spec: "read the multipart part named
// `file`; ignore other parts"), closing every part it skips over.
func findFilePart(r *multipart.Reader) (*multipart.Part, error) {
	for {
		part, err := r.NextPart()
		if errors.Is(err, io.EOF) {
			return nil, apierr.Validation(map[string]string{"file": "required"})
		}
		if err != nil {
			return nil, fmt.Errorf("media: read multipart body: %w", err)
		}
		if part.FormName() == "file" {
			return part, nil
		}
		_ = part.Close()
	}
}

// toGenMediaFile builds the MediaFile response for row, whose derivative
// URLs are computed from its storage_key by URLs (keys.go).
func toGenMediaFile(baseURL string, row db.MediaFile) gen.MediaFile {
	var width, height int
	if row.Width != nil {
		width = int(*row.Width)
	}
	if row.Height != nil {
		height = int(*row.Height)
	}
	return gen.MediaFile{
		Id:        row.ID,
		Mime:      row.Mime,
		Width:     width,
		Height:    height,
		SizeBytes: int(row.SizeBytes),
		Urls:      URLs(baseURL, row.StorageKey),
	}
}

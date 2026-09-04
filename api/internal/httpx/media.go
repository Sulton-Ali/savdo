package httpx

import (
	"context"

	"github.com/Sulton-Ali/savdo/api/gen"
)

// UploadMedia satisfies gen.StrictServerInterface's `/media` operation by
// forwarding to server.media (*media.Handler) — same forwarding pattern as
// shop.go, kept in its own file because *media.Handler and *shop.Handler
// are both named "Handler" (embedding both anonymously in server would
// collide, per healthz.go's doc comment on the server struct).
func (s server) UploadMedia(ctx context.Context, req gen.UploadMediaRequestObject) (gen.UploadMediaResponseObject, error) {
	return s.media.UploadMedia(ctx, req)
}

// Package audit is a writer-only module (docs/03-ARCHITECTURE.md § Module
// map: "audit_log (writer only, no handler)"): one function, Write, that
// inserts a single audit_log row (docs/04-DATA-MODEL.md § 6) inside the
// caller's own transaction. Phase 3 writers are stock adjustments,
// purchase receives and purchase cancels (D-47); this package does not
// know or care which — a caller passes an Entry, Write inserts it.
package audit

import (
	"context"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/internal/db"
)

// Entry is one audit_log row's writable fields. ShopID and ActorID are
// never taken from anything but the caller's already-resolved
// auth.Context (hard rule 1) — this package has no auth import and no
// context-reading of its own, precisely so it cannot be tempted to. Before
// and After are pre-marshalled JSON (json.RawMessage / []byte); either may
// be nil (a create has no Before, per audit.sql.go's own doc comment) —
// nil marshals to SQL NULL, never the four-byte string "null".
type Entry struct {
	ShopID     uuid.UUID
	ActorID    uuid.UUID
	Action     string
	EntityType string
	EntityID   uuid.UUID
	Before     []byte
	After      []byte
}

// Write inserts one audit_log row for e, through q — the caller's own
// *db.Queries, already bound to its transaction (db.Queries.WithTx) so the
// audit row commits or rolls back atomically with whatever it is
// documenting (e.g. stock.Handler.CreateAdjustment writes the movement and
// the audit row in the same transaction, D-47). Write never opens or
// commits a transaction itself.
func Write(ctx context.Context, q *db.Queries, e Entry) error {
	_, err := q.InsertAuditLog(ctx, db.InsertAuditLogParams{
		ID:         newID(),
		ShopID:     e.ShopID,
		ActorID:    e.ActorID,
		Action:     e.Action,
		EntityType: e.EntityType,
		EntityID:   e.EntityID,
		Before:     e.Before,
		After:      e.After,
	})
	return err
}

// newID mints a UUID v7 (time-ordered, docs/03-ARCHITECTURE.md §
// Cross-cutting: "IDs"), falling back to a v4 in the practically
// unreachable case the v7 generator's clock read fails — mirrors
// catalog.newID/shop.newID's own id generation.
func newID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.New()
	}
	return id
}

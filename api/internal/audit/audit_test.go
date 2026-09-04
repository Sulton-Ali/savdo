package audit_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/Sulton-Ali/savdo/api/internal/audit"
	"github.com/Sulton-Ali/savdo/api/internal/db"
	"github.com/Sulton-Ali/savdo/api/internal/db/testdb"
)

func seedShop(ctx context.Context, t *testing.T, q *db.Queries, slug string) db.Shop {
	t.Helper()
	shopRow, err := q.CreateShop(ctx, db.CreateShopParams{ID: uuid.New(), Slug: slug, Name: "Audit Shop " + slug})
	if err != nil {
		t.Fatalf("seedShop: %v", err)
	}
	return shopRow
}

func seedUser(ctx context.Context, t *testing.T, q *db.Queries, shopID uuid.UUID, username string) db.User {
	t.Helper()
	user, err := q.CreateUser(ctx, db.CreateUserParams{
		ID: uuid.New(), ShopID: shopID, Username: username, PasswordHash: "x",
		FullName: "Audit User", Role: db.UserRoleOwner, Locale: "uz",
	})
	if err != nil {
		t.Fatalf("seedUser: %v", err)
	}
	return user
}

// TestWrite_insertsOneRowWithinCallerTx proves Write does exactly one
// thing — insert a row through the *db.Queries it is given — and that a
// caller running it inside a transaction that is then rolled back sees no
// row survive, the same atomicity stock.Handler.CreateAdjustment relies on
// (D-47: the audit row and the movement commit or roll back together).
func TestWrite_insertsOneRowWithinCallerTx(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := seedShop(ctx, t, q, "audit-write")
	user := seedUser(ctx, t, q, shop.ID, "audit-write-user")
	entityID := uuid.New()

	after, err := json.Marshal(map[string]any{"qty": "5.000"})
	if err != nil {
		t.Fatalf("marshal after: %v", err)
	}

	entry := audit.Entry{
		ShopID: shop.ID, ActorID: user.ID, Action: "stock.adjust",
		EntityType: "stock_movement", EntityID: entityID, After: after,
	}

	// Committed write: the row must be visible afterwards.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := audit.Write(ctx, q.WithTx(tx), entry); err != nil {
		t.Fatalf("Write (commit path): %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE shop_id = $1 AND entity_id = $2`, shop.ID, entityID).Scan(&count); err != nil {
		t.Fatalf("count after commit: %v", err)
	}
	if count != 1 {
		t.Fatalf("count after commit = %d, want 1", count)
	}

	var gotAction, gotEntityType string
	var gotAfter []byte
	if err := pool.QueryRow(ctx, `SELECT action, entity_type, after FROM audit_log WHERE shop_id = $1 AND entity_id = $2`, shop.ID, entityID).
		Scan(&gotAction, &gotEntityType, &gotAfter); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if gotAction != "stock.adjust" || gotEntityType != "stock_movement" {
		t.Fatalf("action/entity_type = %q/%q, want stock.adjust/stock_movement", gotAction, gotEntityType)
	}
	// jsonb re-serializes on the way out (e.g. a space after ':'), so
	// compare decoded values rather than raw bytes.
	var gotAfterVal, wantAfterVal map[string]any
	if err := json.Unmarshal(gotAfter, &gotAfterVal); err != nil {
		t.Fatalf("unmarshal gotAfter: %v", err)
	}
	if err := json.Unmarshal(after, &wantAfterVal); err != nil {
		t.Fatalf("unmarshal want after: %v", err)
	}
	if gotAfterVal["qty"] != wantAfterVal["qty"] {
		t.Fatalf("after = %v, want %v", gotAfterVal, wantAfterVal)
	}

	// Rolled-back write: a second entry, same shape, must leave no row.
	entryB := entry
	entityIDB := uuid.New()
	entryB.EntityID = entityIDB

	tx2, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin 2: %v", err)
	}
	if err := audit.Write(ctx, q.WithTx(tx2), entryB); err != nil {
		t.Fatalf("Write (rollback path): %v", err)
	}
	if err := tx2.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE shop_id = $1 AND entity_id = $2`, shop.ID, entityIDB).Scan(&count); err != nil {
		t.Fatalf("count after rollback: %v", err)
	}
	if count != 0 {
		t.Fatalf("count after rollback = %d, want 0", count)
	}
}

// TestWrite_beforeNilStaysNull confirms a nil Before (the common "create"
// case, per InsertAuditLog's own doc comment) round-trips as SQL NULL, not
// as the four-byte JSON string "null" — audit.sql.go's InsertAuditLogParams
// takes []byte, and a nil []byte must bind as NULL through pgx.
func TestWrite_beforeNilStaysNull(t *testing.T) {
	pool := testdb.New(t)
	testdb.Truncate(t, pool)
	ctx := context.Background()
	q := db.New(pool)

	shop := seedShop(ctx, t, q, "audit-nil-before")
	user := seedUser(ctx, t, q, shop.ID, "audit-nil-before-user")
	entityID := uuid.New()

	if err := audit.Write(ctx, q, audit.Entry{
		ShopID: shop.ID, ActorID: user.ID, Action: "stock.adjust",
		EntityType: "stock_movement", EntityID: entityID, After: []byte(`{"qty":"1.000"}`),
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	var before *string
	if err := pool.QueryRow(ctx, `SELECT before FROM audit_log WHERE shop_id = $1 AND entity_id = $2`, shop.ID, entityID).Scan(&before); err != nil {
		t.Fatalf("read before: %v", err)
	}
	if before != nil {
		t.Fatalf("before = %v, want nil (SQL NULL)", *before)
	}
}

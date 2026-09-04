-- name: InsertAuditLog :one
-- Writer-only (§ 03-ARCHITECTURE.md module map: audit has no handler).
-- before/after are optional (e.g. a create has no before).
INSERT INTO audit_log (id, shop_id, actor_id, action, entity_type, entity_id, before, after)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

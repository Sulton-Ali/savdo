-- +goose Up
-- § 04-DATA-MODEL.md § 6. Written by services for: price changes, stock
-- adjustments, voids, role changes, settings changes. Phase 3 writers:
-- stock adjustments, purchase receives, purchase cancels (D-47).
CREATE TABLE audit_log (
    id           uuid PRIMARY KEY,
    shop_id      uuid NOT NULL REFERENCES shops (id),
    actor_id     uuid NOT NULL REFERENCES users (id),
    action       text NOT NULL,
    entity_type  text NOT NULL,
    entity_id    uuid NOT NULL,
    before       jsonb,
    after        jsonb,
    created_at   timestamptz NOT NULL DEFAULT now()
);

-- Serves "history for this entity" reads, shop_id leading (hard rule 1 and 10).
CREATE INDEX audit_log_shop_id_entity_type_entity_id_created_at_idx
    ON audit_log (shop_id, entity_type, entity_id, created_at);
CREATE INDEX audit_log_actor_id_idx ON audit_log (actor_id);

-- +goose Down
DROP TABLE audit_log;

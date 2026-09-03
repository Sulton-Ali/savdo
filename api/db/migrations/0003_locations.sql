-- +goose Up
CREATE TYPE location_kind AS ENUM ('store', 'warehouse');

CREATE TABLE locations (
    id          uuid PRIMARY KEY,
    shop_id     uuid NOT NULL REFERENCES shops (id),
    name        text NOT NULL,
    kind        location_kind NOT NULL,
    is_default  boolean NOT NULL DEFAULT false,
    is_active   boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (shop_id, name)
);

-- At most one default location per shop. A plain UNIQUE(shop_id, is_default)
-- would also forbid more than one non-default location, so it has to be
-- partial: only rows where is_default is true participate.
CREATE UNIQUE INDEX locations_shop_id_default_key ON locations (shop_id) WHERE is_default;

-- +goose Down
DROP TABLE locations;
DROP TYPE location_kind;

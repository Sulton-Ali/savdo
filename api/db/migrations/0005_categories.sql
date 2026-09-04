-- +goose Up
CREATE TABLE categories (
    id          uuid PRIMARY KEY,
    shop_id     uuid NOT NULL REFERENCES shops (id),
    parent_id   uuid REFERENCES categories (id),
    slug        text NOT NULL,
    sort_order  integer NOT NULL DEFAULT 0,
    is_active   boolean NOT NULL DEFAULT true,
    -- References media_files, created later in 0007_media.sql. The column
    -- (and CountProductsInCategory-style reads) can exist before that table
    -- does; the FK constraint itself is added by 0007 once media_files
    -- exists, so the column is left unconstrained here.
    image_id    uuid,
    deleted_at  timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (shop_id, slug)
);

CREATE INDEX categories_parent_id_idx ON categories (parent_id);
CREATE INDEX categories_image_id_idx ON categories (image_id);

CREATE TABLE category_translations (
    category_id  uuid NOT NULL REFERENCES categories (id),
    locale       text NOT NULL,
    name         text NOT NULL,
    description  text,
    PRIMARY KEY (category_id, locale)
);

-- +goose Down
DROP TABLE category_translations;
DROP TABLE categories;

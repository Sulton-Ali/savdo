-- +goose Up
CREATE TABLE media_files (
    id           uuid PRIMARY KEY,
    shop_id      uuid NOT NULL REFERENCES shops (id),
    storage_key  text NOT NULL UNIQUE,
    mime         text NOT NULL,
    size_bytes   bigint NOT NULL,
    width        integer,
    height       integer,
    sha256       bytea NOT NULL,
    uploaded_by  uuid REFERENCES users (id),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (shop_id, sha256)
);

CREATE INDEX media_files_uploaded_by_idx ON media_files (uploaded_by);

CREATE TABLE product_images (
    id          uuid PRIMARY KEY,
    shop_id     uuid NOT NULL REFERENCES shops (id),
    product_id  uuid NOT NULL REFERENCES products (id),
    variant_id  uuid REFERENCES product_variants (id),
    media_id    uuid NOT NULL REFERENCES media_files (id),
    sort_order  integer NOT NULL DEFAULT 0,
    is_cover    boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (product_id, media_id)
);

-- At most one cover image per product, same reasoning as
-- locations_shop_id_default_key: a plain UNIQUE(product_id, is_cover) would
-- also forbid more than one non-cover row, so it has to be partial.
CREATE UNIQUE INDEX product_images_one_cover_key ON product_images (product_id) WHERE is_cover;
CREATE INDEX product_images_shop_id_idx ON product_images (shop_id);
CREATE INDEX product_images_variant_id_idx ON product_images (variant_id);
CREATE INDEX product_images_media_id_idx ON product_images (media_id);

-- categories.image_id was added in 0005_categories.sql, before media_files
-- existed; the FK constraint can only be added now.
ALTER TABLE categories
    ADD CONSTRAINT categories_image_id_fkey FOREIGN KEY (image_id) REFERENCES media_files (id);

-- +goose Down
ALTER TABLE categories DROP CONSTRAINT categories_image_id_fkey;
DROP TABLE product_images;
DROP TABLE media_files;

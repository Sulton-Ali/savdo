-- +goose Up
-- content_blocks (§ 04-DATA-MODEL.md § 6, D-99, D-104): per-shop, per-locale
-- landing-page content — hero, about, hours, contacts, social, seo. One row
-- per (shop, key, locale); shapes are validated against JSON Schemas in the
-- OpenAPI contract, not by the database, so `data` is a plain jsonb blob
-- here. key is a real enum (top-of-file convention: "real enum types"),
-- unlike locale, which stays plain text with no CHECK — the same choice
-- product_translations/category_translations/unit_translations already
-- made for locale, so this table matches their house style rather than
-- introducing a new one for the same column shape.
CREATE TYPE content_block_key AS ENUM ('hero', 'about', 'hours', 'contacts', 'social', 'seo');

CREATE TABLE content_blocks (
    shop_id     uuid NOT NULL REFERENCES shops (id),
    key         content_block_key NOT NULL,
    locale      text NOT NULL,
    data        jsonb NOT NULL,
    -- Who last saved this block (manager+ only, § 04-DATA-MODEL.md § 7).
    -- Nullable like every other created_by/updated_by FK in this schema
    -- (purchases.created_by, sale_drafts.created_by) — the service always
    -- sets it, but the column itself does not require it.
    updated_by  uuid REFERENCES users (id),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (shop_id, key, locale)
);

-- updated_by is a bare FK not covered as a leading column of any other
-- index (unlike shop_id/key/locale, already the primary key's own leading
-- columns), so it needs its own index (top-of-file convention: "index
-- every FK").
CREATE INDEX content_blocks_updated_by_idx ON content_blocks (updated_by);

-- +goose Down
DROP TABLE content_blocks;
DROP TYPE content_block_key;

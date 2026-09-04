-- +goose Up
CREATE TABLE units (
    id          uuid PRIMARY KEY,
    shop_id     uuid NOT NULL REFERENCES shops (id),
    code        text NOT NULL,
    precision   smallint NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (shop_id, code)
);

CREATE TABLE unit_translations (
    unit_id  uuid NOT NULL REFERENCES units (id),
    locale   text NOT NULL,
    name     text NOT NULL,
    PRIMARY KEY (unit_id, locale)
);

CREATE TABLE attribute_definitions (
    id          uuid PRIMARY KEY,
    shop_id     uuid NOT NULL REFERENCES shops (id),
    code        text NOT NULL,
    sort_order  integer NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (shop_id, code)
);

CREATE TABLE attribute_definition_translations (
    attribute_definition_id  uuid NOT NULL REFERENCES attribute_definitions (id),
    locale                   text NOT NULL,
    name                     text NOT NULL,
    PRIMARY KEY (attribute_definition_id, locale)
);

-- +goose Down
DROP TABLE attribute_definition_translations;
DROP TABLE attribute_definitions;
DROP TABLE unit_translations;
DROP TABLE units;

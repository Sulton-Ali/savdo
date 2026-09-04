-- name: ListAttributeDefinitions :many
-- Resolved name by locale fallback (requested -> 'uz' -> any), plus every
-- translation as a JSON object {locale: name} so the admin edit form can
-- show all locales without a second round trip.
SELECT
    ad.*,
    t.locale AS locale_used,
    t.name AS name,
    COALESCE(agg.translations, '{}'::jsonb) AS translations
FROM attribute_definitions ad
LEFT JOIN LATERAL (
    SELECT adt.locale, adt.name
    FROM attribute_definition_translations adt
    WHERE adt.attribute_definition_id = ad.id
    ORDER BY
        CASE
            WHEN adt.locale = sqlc.arg('locale') THEN 0
            WHEN adt.locale = 'uz' THEN 1
            ELSE 2
        END
    LIMIT 1
) t ON true
LEFT JOIN LATERAL (
    SELECT jsonb_object_agg(adt2.locale, adt2.name) AS translations
    FROM attribute_definition_translations adt2
    WHERE adt2.attribute_definition_id = ad.id
) agg ON true
WHERE ad.shop_id = sqlc.arg('shop_id')
ORDER BY ad.sort_order, ad.code;

-- name: GetAttributeDefinition :one
SELECT * FROM attribute_definitions
WHERE shop_id = $1 AND id = $2;

-- name: CreateAttributeDefinition :one
INSERT INTO attribute_definitions (id, shop_id, code, sort_order)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: UpdateAttributeDefinition :one
-- Patch: code and sort_order are optional.
UPDATE attribute_definitions
SET
    code = COALESCE(sqlc.narg('code'), code),
    sort_order = COALESCE(sqlc.narg('sort_order'), sort_order),
    updated_at = now()
WHERE shop_id = sqlc.arg('shop_id') AND id = sqlc.arg('id')
RETURNING *;

-- name: UpsertAttributeDefinitionTranslation :exec
INSERT INTO attribute_definition_translations (attribute_definition_id, locale, name)
VALUES ($1, $2, $3)
ON CONFLICT (attribute_definition_id, locale) DO UPDATE
SET name = EXCLUDED.name;

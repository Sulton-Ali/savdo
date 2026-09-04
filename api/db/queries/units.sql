-- name: ListUnits :many
-- Locale fallback: requested -> 'uz' -> any (whatever the LATERAL picks when
-- neither of the first two exists), returning locale_used so the caller can
-- tell whether the requested locale actually had a translation.
SELECT
    u.*,
    -- COALESCE to '': a unit with zero translations must still list, not
    -- fail to scan (LEFT JOIN LATERAL leaves these NULL and sqlc does not
    -- infer that as nullable).
    COALESCE(t.locale, '') AS locale_used,
    COALESCE(t.name, '') AS name
FROM units u
LEFT JOIN LATERAL (
    SELECT ut.locale, ut.name
    FROM unit_translations ut
    WHERE ut.unit_id = u.id
    ORDER BY
        CASE
            WHEN ut.locale = sqlc.arg('locale') THEN 0
            WHEN ut.locale = 'uz' THEN 1
            ELSE 2
        END
    LIMIT 1
) t ON true
WHERE u.shop_id = sqlc.arg('shop_id')
ORDER BY u.code;

-- name: UpsertUnit :one
-- Units are seeded per shop (§ 04-DATA-MODEL.md); upsert on (shop_id, code)
-- so re-running the seed is idempotent.
INSERT INTO units (id, shop_id, code, precision)
VALUES ($1, $2, $3, $4)
ON CONFLICT (shop_id, code) DO UPDATE
SET precision = EXCLUDED.precision, updated_at = now()
RETURNING *;

-- name: UpsertUnitTranslation :exec
INSERT INTO unit_translations (unit_id, locale, name)
VALUES ($1, $2, $3)
ON CONFLICT (unit_id, locale) DO UPDATE
SET name = EXCLUDED.name;

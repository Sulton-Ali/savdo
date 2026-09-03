-- name: CreateSession :one
INSERT INTO sessions (id, shop_id, user_id, token_hash, client, user_agent, ip, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetSessionByTokenHash :one
-- The only lookup that runs before a shop_id is known: the hashed token is
-- itself the credential (globally unique), and this query is how the auth
-- context (including shop_id) gets established in the first place. Every
-- query after this one takes shop_id from that context.
SELECT
    s.id, s.shop_id, s.user_id, s.token_hash, s.client, s.user_agent, s.ip,
    s.expires_at, s.last_seen_at, s.revoked_at, s.created_at,
    u.username, u.full_name, u.role, u.locale, u.is_active
FROM sessions s
JOIN users u ON u.id = s.user_id AND u.shop_id = s.shop_id
WHERE s.token_hash = $1
    AND s.revoked_at IS NULL
    AND s.expires_at > now();

-- name: TouchSession :exec
-- Sliding session (D-29): the caller recomputes expires_at from the
-- session's client (web 7d, mobile 30d) and extends it on every
-- authenticated request.
UPDATE sessions
SET expires_at = $3, last_seen_at = now()
WHERE shop_id = $1 AND id = $2 AND revoked_at IS NULL;

-- name: RevokeSession :exec
UPDATE sessions
SET revoked_at = now()
WHERE shop_id = $1 AND id = $2 AND user_id = $3 AND revoked_at IS NULL;

-- name: RevokeAllUserSessions :exec
UPDATE sessions
SET revoked_at = now()
WHERE shop_id = $1 AND user_id = $2 AND revoked_at IS NULL;

-- name: ListUserSessions :many
SELECT * FROM sessions
WHERE shop_id = $1 AND user_id = $2
ORDER BY created_at DESC;

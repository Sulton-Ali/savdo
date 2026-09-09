-- name: CreateOTPCode :one
INSERT INTO otp_codes (id, shop_id, user_id, purpose, code_hash, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetActiveOTPCode :one
-- Newest unused, unexpired code for a (user, purpose) pair — the one the
-- service checks the submitted code against. "Newest" matters because
-- ExpireOTPCodes (below) marks every earlier active code used when a new
-- one is issued ("new code invalidates old"), but this query does not
-- depend on that having run: it would still pick the most recently
-- created active row even if two somehow existed at once.
SELECT * FROM otp_codes
WHERE shop_id = $1 AND user_id = $2 AND purpose = $3
    AND used_at IS NULL AND expires_at > now()
ORDER BY created_at DESC
LIMIT 1;

-- name: IncrementOTPAttempts :one
-- Called on every wrong-code submission; the service compares the
-- returned attempts against its own max-attempts constant and marks the
-- code used (MarkOTPUsed) once exceeded, same shape as a login lockout.
UPDATE otp_codes
SET attempts = attempts + 1
WHERE shop_id = $1 AND id = $2
RETURNING *;

-- name: MarkOTPUsed :one
-- used_at IS NULL guard: a code cannot be marked used twice, so a replay
-- of an already-consumed code's id updates zero rows (:one still errors
-- pgx.ErrNoRows in that case, which the service maps to "code invalid").
UPDATE otp_codes
SET used_at = now()
WHERE shop_id = $1 AND id = $2 AND used_at IS NULL
RETURNING *;

-- name: ExpireOTPCodes :execrows
-- Issuing a fresh code for the same (user, purpose) invalidates every
-- still-active one first: only one code is ever checkable at a time
-- ("new code invalidates old" — a service-level rule, not itself an owner
-- decision; D-06 is only "Telegram delivery, no SMS"). Already-expired-
-- but-unused rows are also marked used here — they are dead either way,
-- and this keeps GetActiveOTPCode's "at most one live row" invariant
-- simple to reason about.
UPDATE otp_codes
SET used_at = now()
WHERE shop_id = $1 AND user_id = $2 AND purpose = $3 AND used_at IS NULL;

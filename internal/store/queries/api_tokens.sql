-- name: CreateAPIToken :exec
INSERT INTO api_tokens (id, user_id, name, token_hash, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?);

-- name: ListAPITokens :many
SELECT id, user_id, name, last_used_at, created_at FROM api_tokens WHERE user_id = ? ORDER BY id DESC;

-- name: DeleteAPIToken :one
DELETE FROM api_tokens WHERE id = ? AND user_id = ? RETURNING id, user_id, name, last_used_at, created_at;

-- name: APITokenUser :one
SELECT t.id, t.user_id, t.name, t.last_used_at, t.created_at,
       u.id AS owner_id, u.email, u.password_hash, u.role, u.created_at AS owner_created_at, u.updated_at AS owner_updated_at
FROM api_tokens t JOIN users u ON u.id = t.user_id
WHERE t.token_hash = ?;

-- name: TouchAPIToken :exec
UPDATE api_tokens SET last_used_at = sqlc.arg(now), updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id) AND (last_used_at IS NULL OR last_used_at < sqlc.arg(before));

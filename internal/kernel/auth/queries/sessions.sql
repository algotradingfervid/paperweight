-- name: GetSession :one
SELECT data, created_at, last_seen_at FROM sessions WHERE token_hash = ?;

-- name: InsertSession :exec
INSERT INTO sessions (token_hash, user_id, data, created_at, last_seen_at, ip, user_agent)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: UpdateSession :exec
UPDATE sessions SET data = ?, user_id = ?, last_seen_at = ?, ip = ?, user_agent = ?
WHERE token_hash = ?;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash = ?;

-- name: ListLiveSessions :many
SELECT token_hash, data FROM sessions WHERE last_seen_at >= ? AND created_at >= ?;

-- name: ListLiveSessionsForUser :many
SELECT id, token_hash, created_at, last_seen_at, ip, user_agent FROM sessions
WHERE user_id = ? AND last_seen_at >= ? AND created_at >= ?
ORDER BY last_seen_at DESC, id DESC;

-- name: DeleteSessionForUser :execrows
DELETE FROM sessions WHERE id = ? AND user_id = ?;

-- name: DeleteSessionsForUser :exec
DELETE FROM sessions WHERE user_id = ? AND token_hash <> ?;

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions WHERE last_seen_at < ? OR created_at < ?;

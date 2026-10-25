-- name: AuthLogGetSignInUser :one
SELECT id, CAST(coalesce(password_hash, '') AS TEXT) AS password_hash, must_change_password, enabled FROM users WHERE email = ?;

-- name: AuthLogSetLastLogin :exec
UPDATE users SET last_login_at = sqlc.arg(at) WHERE id = sqlc.arg(id);

-- name: AuthLogSetPasswordHash :exec
UPDATE users SET password_hash = sqlc.arg(password_hash) WHERE id = sqlc.arg(id) AND password_hash = sqlc.arg(old_hash);

-- name: AuthLogInsert :exec
INSERT INTO auth_log (at, event, user_id, email_hash, ip, user_agent)
VALUES (?, ?, ?, ?, ?, ?);

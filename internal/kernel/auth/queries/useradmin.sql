-- name: ListUsers :many
SELECT id, email, name, enabled FROM users ORDER BY id;

-- name: TouchUser :exec
UPDATE users SET version = version + 1, updated_at = sqlc.arg(at), updated_by = sqlc.narg(by)
WHERE id = sqlc.arg(id);

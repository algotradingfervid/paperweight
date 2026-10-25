-- name: GetPrincipalUser :one
SELECT id, email, name, enabled, must_change_password FROM users WHERE id = ?;

-- name: ListPrincipalRoles :many
SELECT r.name FROM user_roles ur
JOIN roles r ON r.id = ur.role_id
WHERE ur.user_id = ?
ORDER BY r.name;

-- name: CountUserRole :one
SELECT count(*) FROM user_roles ur
JOIN roles r ON r.id = ur.role_id
WHERE ur.user_id = sqlc.arg(user_id) AND r.name_key = lower(sqlc.arg(role));

-- name: ListPrincipalRestrictions :many
SELECT dimension, value_id FROM user_restrictions
WHERE user_id = ?
ORDER BY dimension, value_id;

-- name: ListEnabledUserIDs :many
SELECT id FROM users WHERE enabled = 1 ORDER BY id;

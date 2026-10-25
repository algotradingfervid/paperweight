-- name: GetUser :one
SELECT id, email, name, mobile, CAST(ifnull(password_hash, '') AS TEXT) AS password_hash, enabled,
    CAST(last_login_at IS NOT NULL AS INTEGER) AS signed_in, version
FROM users WHERE id = ?;

-- name: UserIDByEmail :one
SELECT id FROM users WHERE email = ?;

-- name: InsertUser :one
INSERT INTO users (email, name, mobile, password_hash, created_at, created_by, updated_at, updated_by)
VALUES (sqlc.arg(email), sqlc.arg(name), sqlc.arg(mobile), sqlc.arg(password_hash),
        sqlc.arg(at), sqlc.narg(by), sqlc.arg(at), sqlc.narg(by))
RETURNING id;

-- name: UpdateUserFields :exec
UPDATE users SET email = sqlc.arg(email), name = sqlc.arg(name), mobile = sqlc.arg(mobile),
    enabled = sqlc.arg(enabled), version = version + 1, updated_at = sqlc.arg(at), updated_by = sqlc.narg(by)
WHERE id = sqlc.arg(id);

-- name: SetUserEnabled :exec
UPDATE users SET enabled = sqlc.arg(enabled), version = version + 1, updated_at = sqlc.arg(at), updated_by = sqlc.narg(by)
WHERE id = sqlc.arg(id);

-- name: SetUserPassword :exec
UPDATE users SET password_hash = sqlc.arg(password_hash), must_change_password = sqlc.arg(must_change),
    version = version + 1, updated_at = sqlc.arg(at), updated_by = sqlc.narg(by)
WHERE id = sqlc.arg(id);

-- name: ChangeUserPassword :execrows
UPDATE users SET password_hash = sqlc.arg(password_hash), must_change_password = 0,
    version = version + 1, updated_at = sqlc.arg(at), updated_by = sqlc.narg(by)
WHERE id = sqlc.arg(id) AND password_hash = sqlc.arg(old_hash);

-- name: DeleteUserRow :exec
DELETE FROM users WHERE id = ?;

-- name: ListUserRoles :many
SELECT r.name FROM user_roles ur JOIN roles r ON r.id = ur.role_id WHERE ur.user_id = ? ORDER BY r.name;

-- name: RoleByName :one
SELECT id, name FROM roles WHERE name_key = lower(sqlc.arg(name));

-- name: DeleteUserRoles :exec
DELETE FROM user_roles WHERE user_id = ?;

-- name: InsertUserRole :exec
INSERT OR IGNORE INTO user_roles (user_id, role_id) VALUES (?, ?);

-- name: ListUserRestrictions :many
SELECT value_id FROM user_restrictions WHERE user_id = ? AND dimension = ? ORDER BY value_id;

-- name: DeleteUserRestrictions :exec
DELETE FROM user_restrictions WHERE user_id = ? AND dimension = ?;

-- name: InsertUserRestriction :exec
INSERT OR IGNORE INTO user_restrictions (user_id, dimension, value_id) VALUES (?, ?, ?);

-- name: CountEnabledAdmins :one
SELECT count(DISTINCT u.id) FROM users u
JOIN user_roles ur ON ur.user_id = u.id
JOIN roles r ON r.id = ur.role_id
WHERE u.enabled = 1 AND r.is_admin = 1 AND u.id != sqlc.arg(except_id);

-- name: CountUserAuditRefs :one
SELECT count(*) FROM users WHERE created_by = CAST(sqlc.arg(id) AS INTEGER) OR updated_by = CAST(sqlc.arg(id) AS INTEGER);

-- name: CountRoleAuditRefs :one
SELECT count(*) FROM roles WHERE created_by = CAST(sqlc.arg(id) AS INTEGER) OR updated_by = CAST(sqlc.arg(id) AS INTEGER);

-- name: DeleteUserSessions :exec
DELETE FROM sessions WHERE user_id = CAST(sqlc.arg(user_id) AS INTEGER);

-- name: DeleteUserSessionsExcept :exec
DELETE FROM sessions WHERE user_id = CAST(sqlc.arg(user_id) AS INTEGER) AND token_hash != sqlc.arg(keep_token_hash);

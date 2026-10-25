-- name: ListRoles :many
SELECT r.id, r.name, r.is_admin, r.version,
       (SELECT count(*) FROM user_roles ur WHERE ur.role_id = r.id) AS users
FROM roles r ORDER BY r.id;

-- name: CreateRole :one
INSERT INTO roles (name, name_key, created_at, created_by, updated_at, updated_by)
VALUES (sqlc.arg(name), lower(sqlc.arg(name)), sqlc.arg(at), sqlc.arg(by), sqlc.arg(at), sqlc.arg(by))
RETURNING id;

-- name: RenameRole :exec
UPDATE roles SET name = sqlc.arg(name), name_key = lower(sqlc.arg(name)), version = version + 1,
       updated_at = sqlc.arg(at), updated_by = sqlc.arg(by)
WHERE id = sqlc.arg(id);

-- name: BumpRoleVersion :exec
-- A rights change: updated_by stays as is (the audit log has who).
UPDATE roles SET version = version + 1, updated_at = sqlc.arg(at) WHERE id = sqlc.arg(id);

-- name: CountRoleUsers :one
SELECT count(*) FROM user_roles WHERE role_id = ?;

-- name: DeleteRole :exec
DELETE FROM roles WHERE id = ?;

-- name: DeleteDocTypeRows :exec
DELETE FROM role_doctypes WHERE doctype = ?;

-- name: ListDocTypeRights :many
SELECT role_id, right_name, scope FROM role_rights WHERE doctype = ?;

-- name: ListAllRightsByRoleID :many
SELECT role_id, doctype, right_name, scope FROM role_rights;

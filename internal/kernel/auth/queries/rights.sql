-- name: GetRoleByName :one
SELECT * FROM roles WHERE name_key = lower(sqlc.arg(name));

-- name: GetRoleByID :one
SELECT * FROM roles WHERE id = ?;

-- name: InsertRole :one
INSERT INTO roles (name, name_key, created_at, updated_at)
VALUES (sqlc.arg(name), lower(sqlc.arg(name)), sqlc.arg(at), sqlc.arg(at))
RETURNING id;

-- name: CountRoleDocType :one
SELECT count(*) FROM role_doctypes WHERE role_id = ? AND doctype = ?;

-- name: InsertRoleDocType :exec
INSERT INTO role_doctypes (role_id, doctype, customised) VALUES (?, ?, 0);

-- name: MarkRoleDocTypeCustomised :exec
INSERT INTO role_doctypes (role_id, doctype, customised) VALUES (?, ?, 1)
ON CONFLICT (role_id, doctype) DO UPDATE SET customised = 1;

-- name: DeleteRoleRights :exec
DELETE FROM role_rights WHERE role_id = ? AND doctype = ?;

-- name: InsertRoleRight :exec
INSERT INTO role_rights (role_id, doctype, right_name, scope) VALUES (?, ?, ?, ?);

-- name: ListRoleRightsByName :many
SELECT rr.right_name, rr.scope FROM role_rights rr
JOIN roles r ON r.id = rr.role_id
WHERE r.name_key = lower(sqlc.arg(role)) AND rr.doctype = sqlc.arg(doctype);

-- name: ListAllRoleRights :many
SELECT r.name AS role, rr.doctype, rr.right_name, rr.scope FROM role_rights rr
JOIN roles r ON r.id = rr.role_id;

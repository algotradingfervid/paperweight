-- name: GetPermVersion :one
SELECT perm_version FROM auth_meta WHERE id = 1;

-- name: BumpPermVersion :exec
UPDATE auth_meta SET perm_version = perm_version + 1 WHERE id = 1;

-- name: GetSeeded :one
SELECT seeded FROM auth_meta WHERE id = 1;

-- name: SetSeeded :exec
UPDATE auth_meta SET seeded = 1 WHERE id = 1;

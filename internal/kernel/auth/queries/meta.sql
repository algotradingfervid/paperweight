-- name: GetPermVersion :one
SELECT perm_version FROM auth_meta WHERE id = 1;

-- name: BumpPermVersion :exec
UPDATE auth_meta SET perm_version = perm_version + 1 WHERE id = 1;

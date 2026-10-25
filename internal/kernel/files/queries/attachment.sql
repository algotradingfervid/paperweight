-- name: InsertAttachment :one
INSERT INTO attachment (parent_doctype, parent_id, file_name, size, content_type, sha256, uploaded_by, uploaded_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: GetAttachment :one
SELECT * FROM attachment WHERE id = ?;

-- name: CountForParent :one
SELECT count(*) FROM attachment WHERE parent_doctype = ? AND parent_id = ?;

-- name: ListForParent :many
SELECT * FROM attachment
WHERE parent_doctype = ? AND parent_id = ?
ORDER BY uploaded_at DESC, id DESC;

-- name: ExistsForParentSHA :one
SELECT EXISTS (SELECT 1 FROM attachment WHERE parent_doctype = ? AND parent_id = ? AND sha256 = ?);

-- name: CountForSHA :one
SELECT count(*) FROM attachment WHERE sha256 = ?;

-- name: DeleteAttachment :exec
DELETE FROM attachment WHERE id = ?;

-- name: DeleteForParent :exec
DELETE FROM attachment WHERE parent_doctype = ? AND parent_id = ?;

-- name: CopyRow :one
INSERT INTO attachment (parent_doctype, parent_id, file_name, size, content_type, sha256, uploaded_by, uploaded_at)
SELECT sqlc.arg(parent_doctype), sqlc.arg(parent_id), file_name, size, content_type, sha256, sqlc.arg(uploaded_by), sqlc.arg(uploaded_at)
FROM attachment WHERE attachment.id = sqlc.arg(id)
RETURNING *;

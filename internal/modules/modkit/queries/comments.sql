-- Comments on module documents (PO spec section 22). The read check on each
-- record is the service's, inside the same transaction.

-- name: InsertComment :one
INSERT INTO module_comment (doctype, record_id, body, created_by, created_at)
VALUES (?, ?, ?, ?, ?)
RETURNING id;

-- name: ListComments :many
SELECT id, doctype, record_id, body, created_by, created_at FROM module_comment
WHERE doctype = ? AND record_id = ?
ORDER BY id;

-- name: CommentsSince :many
SELECT id, doctype, record_id, body, created_by, created_at FROM module_comment
WHERE created_at >= ?
ORDER BY created_at DESC, id DESC;

-- name: InsertTerms :one
INSERT INTO payment_terms (code, name, created_at, created_by, updated_at, updated_by)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: UpdateTerms :execrows
UPDATE payment_terms
SET code = ?, name = ?, version = version + 1, updated_at = ?, updated_by = ?
WHERE id = ? AND version = ?;

-- name: GetTerms :one
SELECT id, code, name, disabled, version FROM payment_terms WHERE id = ?;

-- name: GetTermsByCode :one
SELECT id, code, name, disabled, version FROM payment_terms WHERE code = ?;

-- name: ListRows :many
SELECT portion, days FROM payment_terms_row WHERE parent_id = ? ORDER BY line_no;

-- name: InsertRow :exec
INSERT INTO payment_terms_row (parent_id, line_no, portion, days) VALUES (?, ?, ?, ?);

-- name: DeleteRows :exec
DELETE FROM payment_terms_row WHERE parent_id = ?;

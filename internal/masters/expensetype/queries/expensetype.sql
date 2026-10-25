-- name: GetExpenseType :one
SELECT id, code, name, default_hsn_sac, default_tax_code_id, disabled, version FROM expense_type WHERE id = ?;

-- name: GetExpenseTypeByCode :one
SELECT id, code, name, default_hsn_sac, default_tax_code_id, disabled, version FROM expense_type WHERE code = ?;

-- name: InsertExpenseType :one
INSERT INTO expense_type (code, name, default_hsn_sac, default_tax_code_id, created_at, created_by, updated_at, updated_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: UpdateExpenseType :execrows
UPDATE expense_type SET code = ?, name = ?, default_hsn_sac = ?, default_tax_code_id = ?,
    version = version + 1, updated_at = ?, updated_by = ?
WHERE id = ? AND version = ?;

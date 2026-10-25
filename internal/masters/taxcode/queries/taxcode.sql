-- name: GetTaxCode :one
SELECT id, code, name, treatment, rate, disabled, version FROM tax_code WHERE id = ?;

-- name: GetTaxCodeByCode :one
SELECT id, code, name, treatment, rate, disabled, version FROM tax_code WHERE code = ?;

-- name: InsertTaxCode :one
INSERT INTO tax_code (code, name, treatment, rate, created_at, created_by, updated_at, updated_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: UpdateTaxCode :execrows
UPDATE tax_code SET code = ?, name = ?, treatment = ?, rate = ?,
    version = version + 1, updated_at = ?, updated_by = ?
WHERE id = ? AND version = ?;

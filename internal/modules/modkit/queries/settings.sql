-- name: GetSettings :one
SELECT locked_up_to, default_po_terms, version FROM business_settings WHERE id = 1;

-- name: UpdateSettings :execrows
-- Saves at the version the page posted: 0 rows = stale (CD-94).
UPDATE business_settings
SET locked_up_to = ?, default_po_terms = ?, version = version + 1, updated_at = ?, updated_by = ?
WHERE id = 1 AND version = ?;

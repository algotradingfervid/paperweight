-- name: GetUoM :one
SELECT id, code, name, uqc, whole_number, disabled, version FROM uom WHERE id = ?;

-- name: GetUoMByCode :one
SELECT id, code, name, uqc, whole_number, disabled, version FROM uom WHERE code = ?;

-- name: InsertUoM :one
INSERT INTO uom (code, name, uqc, whole_number, created_at, created_by, updated_at, updated_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: UpdateUoM :exec
UPDATE uom SET code = ?, name = ?, uqc = ?, whole_number = ?,
    version = version + 1, updated_at = ?, updated_by = ?
WHERE id = ? AND version = ?;

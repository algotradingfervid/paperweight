-- name: GetCompany :one
SELECT id, name, pan, version FROM company WHERE id = 1;

-- name: InsertCompany :exec
INSERT INTO company (id, name, pan, created_at, created_by, updated_at, updated_by)
VALUES (1, ?, ?, ?, ?, ?, ?);

-- name: UpdateCompany :exec
UPDATE company SET name = ?, pan = ?, version = version + 1, updated_at = ?, updated_by = ?
WHERE id = 1 AND version = ?;

-- name: GetBranch :one
SELECT * FROM branch WHERE id = ?;

-- name: BranchIDByCode :one
SELECT id FROM branch WHERE code = ?;

-- name: InsertBranch :one
INSERT INTO branch (code, name, state_code, gstin, is_head_office, created_at, created_by, updated_at, updated_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: UpdateBranch :exec
UPDATE branch SET code = ?, name = ?, state_code = ?, gstin = ?, is_head_office = ?,
    version = version + 1, updated_at = ?, updated_by = ?
WHERE id = ? AND version = ?;

-- name: OtherHeadOffice :many
SELECT id FROM branch WHERE is_head_office = 1 AND id <> ?;

-- name: ClearHeadOffice :exec
UPDATE branch SET is_head_office = 0, version = version + 1, updated_at = ?, updated_by = ?
WHERE id = ?;

-- name: CountOtherEnabledBranches :one
SELECT count(*) FROM branch WHERE disabled = 0 AND id <> ?;

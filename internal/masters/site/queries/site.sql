-- name: InsertSite :one
INSERT INTO site (code, name, branch_id, project_id, type,
                  created_at, created_by, updated_at, updated_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: UpdateSite :execrows
UPDATE site
SET code = ?, name = ?, branch_id = ?, project_id = ?, type = ?,
    version = version + 1, updated_at = ?, updated_by = ?
WHERE id = ? AND version = ?;

-- name: GetSite :one
SELECT id, code, name, branch_id, project_id, type, disabled, version
FROM site WHERE id = ?;

-- name: GetSiteByCode :one
SELECT id, code, name, branch_id, project_id, type, disabled, version
FROM site WHERE code = ?;

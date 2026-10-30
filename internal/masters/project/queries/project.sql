-- name: InsertProject :one
INSERT INTO project (code, name, branch_id, customer_id, start_date, end_date, status,
                     created_at, created_by, updated_at, updated_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: UpdateProject :execrows
UPDATE project
SET code = ?, name = ?, branch_id = ?, customer_id = ?, start_date = ?, end_date = ?, status = ?,
    version = version + 1, updated_at = ?, updated_by = ?
WHERE id = ? AND version = ?;

-- name: GetProject :one
SELECT id, code, name, branch_id, customer_id, start_date, end_date, status, disabled, version
FROM project WHERE id = ?;

-- name: GetProjectByCode :one
SELECT id, code, name, branch_id, customer_id, start_date, end_date, status, disabled, version
FROM project WHERE code = ?;

-- name: SitesOutsideBranch :many
SELECT code FROM site WHERE project_id = ? AND branch_id <> ? ORDER BY code;

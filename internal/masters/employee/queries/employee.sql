-- name: InsertEmployee :one
INSERT INTO employee (code, name, branch_id, user_id, manager_id, joining_date, birth_date, relieving_date,
                      status, disabled, created_at, created_by, updated_at, updated_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: UpdateEmployee :exec
UPDATE employee
SET code = ?, name = ?, branch_id = ?, user_id = ?, manager_id = ?, joining_date = ?, birth_date = ?,
    relieving_date = ?, status = ?, disabled = ?, user_version_on_leave = ?,
    version = version + 1, updated_at = ?, updated_by = ?
WHERE id = ? AND version = ?;

-- name: GetEmployee :one
SELECT id, code, name, branch_id, user_id, manager_id, joining_date, birth_date, relieving_date,
       status, disabled, user_version_on_leave, version
FROM employee WHERE id = ?;

-- name: GetEmployeeByCode :one
SELECT id, code, name, branch_id, user_id, manager_id, joining_date, birth_date, relieving_date,
       status, disabled, user_version_on_leave, version
FROM employee WHERE code = ?;

-- name: ActiveEmployeeForUser :one
SELECT id, code FROM employee
WHERE user_id = ? AND status = 'Active' AND id <> ?
ORDER BY id LIMIT 1;

-- name: CountEmployees :one
SELECT count(*) FROM employee;

-- name: ActiveReports :many
SELECT id, code, name FROM employee
WHERE manager_id = ? AND status = 'Active'
ORDER BY code;

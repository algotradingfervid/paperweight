-- Advance settlement (MOD-EXP E4, spec section 4 Settlement). Used is always
-- the sum over the approved and closed claims naming the advance (D-DOC-13).

-- name: E4Used :one
SELECT CAST(COALESCE(SUM(less_advance), 0) AS INTEGER)
FROM expense_claim
WHERE advance_id = ? AND state IN ('approved', 'closed');

-- name: E4SetLess :exec
UPDATE expense_claim SET less_advance = ?, net_payable = ? WHERE id = ?;

-- name: E4SetUsed :exec
UPDATE employee_advance
SET used_amount = ?, version = version + 1, updated_at = ?, updated_by = ?
WHERE id = ?;

-- name: E4AdvanceRef :one
SELECT created_by, employee_id, snap_employee_name, amount, used_amount
FROM employee_advance
WHERE id = ?;

-- An employee's open advances: approved (so not closed), marked as paid
-- (CD-271 G1) and with something left; oldest first.
-- name: E4OpenAdvances :many
SELECT id, number, created_by, employee_id, advance_date, purpose, amount, used_amount
FROM employee_advance
WHERE employee_id = ?
  AND state = 'approved'
  AND paid_on IS NOT NULL
  AND COALESCE(amount, 0) - used_amount > 0
ORDER BY advance_date, id;

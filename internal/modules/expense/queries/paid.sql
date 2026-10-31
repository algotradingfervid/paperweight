-- Mark as paid and Undo paid (MOD-EXP E9, spec section 4, CD-271 G1).

-- name: E9Advance :one
SELECT id, number, state, version, created_by, employee_id, advance_date, paid_on
FROM employee_advance
WHERE id = ?;

-- name: E9SetPaidOn :exec
UPDATE employee_advance
SET paid_on = ?, version = version + 1, updated_at = ?, updated_by = ?
WHERE id = ?;

-- Only pending, approved and closed claims use an advance (CD-281 a).
-- name: E9ClaimsUsing :one
SELECT COUNT(*) FROM expense_claim
WHERE advance_id = ? AND state IN ('pending_approval', 'approved', 'closed');

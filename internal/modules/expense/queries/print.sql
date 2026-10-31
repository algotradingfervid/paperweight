-- Prints (MOD-EXP E7, spec section 10): filled from the snapshots. The
-- letterhead is the claimant's branch.

-- name: E7ClaimPrint :one
SELECT c.snap_employee_name, c.snap_employee_code, c.purpose, c.period_from, c.period_to,
       c.total, c.less_advance, c.net_payable, c.advance_id, a.number AS advance_number, e.branch_id
FROM expense_claim c
LEFT JOIN employee_advance a ON a.id = c.advance_id
LEFT JOIN employee e ON e.id = c.employee_id
WHERE c.id = ?;

-- name: E7ClaimPrintLines :many
SELECT expense_date, snap_type_name, description, amount, snap_project_code, receipt_sha256
FROM expense_claim_line
WHERE parent_id = ?
ORDER BY line_no, id;

-- name: E7AdvancePrint :one
SELECT a.snap_employee_name, a.snap_employee_code, a.advance_date, a.purpose, a.amount, e.branch_id
FROM employee_advance a
LEFT JOIN employee e ON e.id = a.employee_id
WHERE a.id = ?;

-- name: E7AdvancePrintLines :many
SELECT snap_type_name, description, amount, snap_project_code
FROM employee_advance_line
WHERE parent_id = ?
ORDER BY line_no, id;

-- The latest approval by an approver. An approval with no rule
-- (approved_no_rule) names no approver, so the claimant never appears to
-- approve their own payout.
-- name: E7ApprovedBy :one
SELECT user_name, at
FROM approval_history
WHERE doctype = ? AND doc_id = ? AND action = 'approved'
ORDER BY id DESC
LIMIT 1;

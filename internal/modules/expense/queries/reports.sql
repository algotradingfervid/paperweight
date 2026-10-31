-- Reports (MOD-EXP E8, spec section 13). Each returns every row of its
-- states; reports.go keeps only the rows in the viewer's read scope. Labels
-- of other records come from the snapshots.

-- name: E8OpenClaims :many
-- Approved, not closed claims, with the latest approval (approved on and by;
-- NULL when the claim has no approval history row) and the count of lines
-- without a receipt; employee_id 0 means every employee.
SELECT c.id,
       CAST(COALESCE(c.number, '') AS TEXT) AS number,
       c.snap_employee_name,
       c.purpose,
       CAST(COALESCE(c.total, 0) AS INTEGER) AS total,
       CAST(COALESCE(c.less_advance, 0) AS INTEGER) AS less_advance,
       CAST(COALESCE(c.net_payable, 0) AS INTEGER) AS net_payable,
       h.at AS approved_at,
       h.user_name AS approved_by,
       CAST((SELECT COUNT(*) FROM expense_claim_line l
             WHERE l.parent_id = c.id AND l.receipt_sha256 = '') AS INTEGER) AS no_receipt
FROM expense_claim c
LEFT JOIN approval_history h ON h.id = (
    SELECT MAX(x.id) FROM approval_history x
    WHERE x.doctype = 'expense.Claim' AND x.doc_id = c.id AND x.action IN ('approved', 'approved_no_rule'))
WHERE c.state = 'approved'
  AND (CAST(sqlc.arg(employee_id) AS INTEGER) = 0 OR c.employee_id = sqlc.arg(employee_id))
ORDER BY h.at, c.id;

-- name: E8OpenAdvances :many
-- Approved, not closed advances; employee_id 0 means every employee.
SELECT a.id,
       CAST(COALESCE(a.number, '') AS TEXT) AS number,
       a.snap_employee_name,
       CAST(COALESCE(a.advance_date, '') AS TEXT) AS advance_date,
       a.purpose,
       CAST(COALESCE(a.amount, 0) AS INTEGER) AS amount,
       a.paid_on,
       a.used_amount
FROM employee_advance a
WHERE a.state = 'approved'
  AND (CAST(sqlc.arg(employee_id) AS INTEGER) = 0 OR a.employee_id = sqlc.arg(employee_id))
ORDER BY a.advance_date, a.id;

-- name: E8ClaimLines :many
-- Lines of approved and closed claims. month is YYYY-MM of the line date and
-- empty means every month; a 0 id means every type, project or employee.
SELECT c.id AS claim_id,
       CAST(COALESCE(c.number, '') AS TEXT) AS number,
       c.snap_employee_name,
       c.state,
       l.line_no,
       CAST(COALESCE(l.expense_date, '') AS TEXT) AS expense_date,
       l.snap_type_name,
       l.snap_project_code,
       l.snap_site_code,
       l.description,
       CAST(COALESCE(l.amount, 0) AS INTEGER) AS amount,
       CAST(COALESCE(l.gst_amount, 0) AS INTEGER) AS gst_amount,
       l.supplier_gstin,
       l.invoice_no,
       l.snap_billed_gstin,
       l.receipt_sha256
FROM expense_claim_line l
JOIN expense_claim c ON c.id = l.parent_id
WHERE c.state IN ('approved', 'closed')
  AND (CAST(sqlc.arg(month) AS TEXT) = '' OR substr(l.expense_date, 1, 7) = sqlc.arg(month))
  AND (CAST(sqlc.arg(type_id) AS INTEGER) = 0 OR l.expense_type_id = sqlc.arg(type_id))
  AND (CAST(sqlc.arg(project_id) AS INTEGER) = 0 OR l.project_id = sqlc.arg(project_id))
  AND (CAST(sqlc.arg(employee_id) AS INTEGER) = 0 OR c.employee_id = sqlc.arg(employee_id))
ORDER BY l.expense_date DESC, c.id, l.line_no;

-- name: S4ExpenseTypes :many
-- Every expense type by name, for the lines report's Type filter.
SELECT id, name
FROM expense_type
ORDER BY name, id;

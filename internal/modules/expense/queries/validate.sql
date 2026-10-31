-- Claim and advance Validate (MOD-EXP E2): employee default, line defaults,
-- snapshots, totals and Billed to.

-- name: E2SetClaimEmployee :exec
UPDATE expense_claim SET employee_id = ? WHERE id = ?;

-- name: E2SetAdvanceEmployee :exec
UPDATE employee_advance SET employee_id = ? WHERE id = ?;

-- name: E2ClaimLines :many
SELECT id, line_no, expense_date, expense_type_id, amount, company_bill, supplier_gstin, invoice_no,
       billed_to_id, tax_code_id, hsn_sac, gst_amount
FROM expense_claim_line
WHERE parent_id = ?
ORDER BY line_no;

-- name: E2AdvanceLines :many
SELECT amount
FROM employee_advance_line
WHERE parent_id = ?
ORDER BY line_no;

-- name: E2SetClaimLineDefaults :exec
UPDATE expense_claim_line SET hsn_sac = ?, tax_code_id = ? WHERE id = ?;

-- name: E2SetClaimLineBilledTo :exec
UPDATE expense_claim_line SET billed_to_id = ? WHERE id = ?;

-- name: E2SetClaimTotals :exec
UPDATE expense_claim SET total = ?, net_payable = ?, period_from = ?, period_to = ? WHERE id = ?;

-- name: E2SetAdvanceAmount :exec
UPDATE employee_advance SET amount = ? WHERE id = ?;

-- name: E2Employee :one
SELECT code, name FROM employee WHERE id = ?;

-- name: E2SnapClaim :exec
UPDATE expense_claim SET snap_employee_code = ?, snap_employee_name = ? WHERE id = ?;

-- name: E2SnapAdvance :exec
UPDATE employee_advance SET snap_employee_code = ?, snap_employee_name = ? WHERE id = ?;

-- name: E2SnapClaimLines :exec
UPDATE expense_claim_line SET
    snap_type_code    = COALESCE((SELECT t.code FROM expense_type t WHERE t.id = expense_claim_line.expense_type_id), ''),
    snap_type_name    = COALESCE((SELECT t.name FROM expense_type t WHERE t.id = expense_claim_line.expense_type_id), ''),
    snap_project_code = COALESCE((SELECT p.code FROM project p WHERE p.id = expense_claim_line.project_id), ''),
    snap_site_code    = COALESCE((SELECT s.code FROM site s WHERE s.id = expense_claim_line.site_id), ''),
    snap_billed_gstin = COALESCE((SELECT b.gstin FROM branch b WHERE b.id = expense_claim_line.billed_to_id), '')
WHERE parent_id = ?;

-- name: E2SnapAdvanceLines :exec
UPDATE employee_advance_line SET
    snap_type_code    = COALESCE((SELECT t.code FROM expense_type t WHERE t.id = employee_advance_line.expense_type_id), ''),
    snap_type_name    = COALESCE((SELECT t.name FROM expense_type t WHERE t.id = employee_advance_line.expense_type_id), ''),
    snap_project_code = COALESCE((SELECT p.code FROM project p WHERE p.id = employee_advance_line.project_id), ''),
    snap_site_code    = COALESCE((SELECT s.code FROM site s WHERE s.id = employee_advance_line.site_id), '')
WHERE parent_id = ?;

-- Billed to (CD-271 G4, CD-281 (c)): only enabled branches with a GSTIN count.

-- name: E2BranchGSTINs :one
SELECT COUNT(DISTINCT gstin) FROM branch WHERE disabled = 0 AND gstin <> '';

-- name: E2DefaultBranch :one
SELECT id FROM branch WHERE disabled = 0 AND gstin <> '' ORDER BY is_head_office DESC, code LIMIT 1;

-- name: E2BranchGSTIN :one
SELECT gstin FROM branch WHERE id = ?;

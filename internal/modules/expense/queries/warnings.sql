-- Claim warnings (spec section 5; MOD-EXP E6). A live claim is pending,
-- approved or closed, as the engine's linked-document check counts it
-- (D-DOC-05): cancelled claims never count.

-- name: E6ClaimRef :one
SELECT created_by, employee_id
FROM expense_claim
WHERE id = ?;

-- name: E6ClaimLines :many
SELECT line_no, expense_date, expense_type_id, amount, receipt_sha256, company_bill, supplier_gstin, invoice_no
FROM expense_claim_line
WHERE parent_id = ?
ORDER BY line_no;

-- name: E6SameReceipt :many
SELECT DISTINCT c.id
FROM expense_claim c
JOIN expense_claim_line l ON l.parent_id = c.id
WHERE l.receipt_sha256 = sqlc.arg(receipt_sha256)
  AND l.receipt_sha256 <> ''
  AND c.id <> sqlc.arg(claim_id)
  AND c.state IN ('pending_approval', 'approved', 'closed')
ORDER BY c.id;

-- name: E6SameLine :many
SELECT DISTINCT c.id
FROM expense_claim c
JOIN expense_claim_line l ON l.parent_id = c.id
WHERE c.employee_id = sqlc.arg(employee_id)
  AND l.expense_date = sqlc.arg(expense_date)
  AND l.expense_type_id = sqlc.arg(expense_type_id)
  AND l.amount = sqlc.arg(amount)
  AND c.id <> sqlc.arg(claim_id)
  AND c.state IN ('pending_approval', 'approved', 'closed')
ORDER BY c.id;

-- name: E6SameInvoice :many
SELECT DISTINCT c.id
FROM expense_claim c
JOIN expense_claim_line l ON l.parent_id = c.id
WHERE l.supplier_gstin = sqlc.arg(supplier_gstin)
  AND l.invoice_no = sqlc.arg(invoice_no)
  AND c.id <> sqlc.arg(claim_id)
  AND c.state IN ('pending_approval', 'approved', 'closed')
ORDER BY c.id;

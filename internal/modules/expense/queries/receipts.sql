-- Receipts (MOD-EXP E3, spec 3.3). Lines in line_no order, so a row's
-- position is the line number the user sees.

-- name: E3ReceiptLines :many
SELECT receipt_sha256, no_receipt_reason
FROM expense_claim_line
WHERE parent_id = ?
ORDER BY line_no, id;

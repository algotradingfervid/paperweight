-- Shared reads (MOD-EXP E1). Each later task adds queries/<task>.sql with
-- query names carrying its prefix (E3ReceiptLines, E8OpenClaims).

-- name: ClaimHeader :one
SELECT id, number, state, version, employee_id, claim_date, advance_id, less_advance, total, updated_by
FROM expense_claim
WHERE id = ?;

-- name: AdvanceHeader :one
SELECT id, number, state, version, employee_id, advance_date, amount, used_amount, paid_on, updated_by
FROM employee_advance
WHERE id = ?;

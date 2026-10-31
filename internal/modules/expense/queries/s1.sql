-- Claim details (MOD-EXP S1, UX section 5.3).

-- An employee's approved advances not marked as paid yet with something
-- left (CD-271 G1): Claim details then shows the paid-only hint. It runs
-- only when AdvanceChoices is empty, so a readable paid advance with
-- something left never reaches it, and paid_on IS NULL guards nothing a
-- test can reach. The left > 0 term is unreachable today too: only a
-- pending, approved or closed claim uses an advance, it can't be submitted
-- against an unpaid one, and Undo paid is refused once one uses it, so an
-- unpaid advance's used is 0 and its amount is positive. Both terms keep
-- the query true to its name if those rules change.
-- name: S1UnpaidAdvances :many
SELECT id
FROM employee_advance
WHERE employee_id = ?
  AND state = 'approved'
  AND paid_on IS NULL
  AND COALESCE(amount, 0) - used_amount > 0
ORDER BY id;

-- A claim line's Billed-to GSTIN as snapshotted (CD-271 G4), for its card.
-- name: S1BilledGSTIN :one
SELECT snap_billed_gstin FROM expense_claim_line WHERE id = ?;

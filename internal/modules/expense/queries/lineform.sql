-- name: S2LineTypes :many
-- The expense line page's choices (MOD-EXP S2, UX 5.5). Every expense
-- type by name: the enabled ones are offered, and any names a
-- stored line's type in the same-receipt hints.
SELECT id, name, disabled
FROM expense_type
ORDER BY name, id;

-- name: S2BilledTo :many
-- The branches Billed to offers: enabled, with a GSTIN, by code (CD-271 G4).
SELECT id, code, name, gstin
FROM branch
WHERE disabled = 0 AND gstin <> ''
ORDER BY code;

-- name: S2ClaimRef :one
-- What the receipt upload checks on the claim: AUTH's record (created_by,
-- employee_id; the claim declares no project or site fields), the version
-- and the number for DOC's stale text.
SELECT number, version, created_by, employee_id
FROM expense_claim
WHERE id = ?;

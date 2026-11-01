-- Claim and advance line rules from the form audit (MOD-EXP E13, CD-337 (5)).

-- Lines whose site is on a project and whose project is blank (to default)
-- or another one (refused), with the codes the refusal names.

-- name: E13ClaimLineSites :many
SELECT l.id, l.line_no, l.project_id, s.code AS site_code, s.project_id AS site_project_id, p.code AS project_code
FROM expense_claim_line l
JOIN site s ON s.id = l.site_id
JOIN project p ON p.id = s.project_id
WHERE l.parent_id = ? AND (l.project_id IS NULL OR l.project_id <> s.project_id);

-- name: E13AdvanceLineSites :many
SELECT l.id, l.line_no, l.project_id, s.code AS site_code, s.project_id AS site_project_id, p.code AS project_code
FROM employee_advance_line l
JOIN site s ON s.id = l.site_id
JOIN project p ON p.id = s.project_id
WHERE l.parent_id = ? AND (l.project_id IS NULL OR l.project_id <> s.project_id);

-- name: E13SetClaimLineProject :exec
UPDATE expense_claim_line SET project_id = ? WHERE id = ?;

-- name: E13SetAdvanceLineProject :exec
UPDATE employee_advance_line SET project_id = ? WHERE id = ?;

-- Our branch GSTINs: enabled branches with one, as E2BranchGSTINs counts.

-- name: E13BranchGSTINs :many
SELECT gstin FROM branch WHERE disabled = 0 AND gstin <> '';

-- name: InsertRule :one
INSERT INTO approval_rules (
    doctype, from_amount, approver_type, approver_role, fallback_role, allow_self_approval, enabled,
    created_at, created_by, updated_at, updated_by
) VALUES (
    sqlc.arg(doctype), sqlc.arg(from_amount), sqlc.arg(approver_type), sqlc.arg(approver_role),
    sqlc.arg(fallback_role), sqlc.arg(allow_self_approval), 1,
    sqlc.arg(created_at), sqlc.arg(created_by), sqlc.arg(created_at), sqlc.arg(created_by)
)
RETURNING id;

-- name: UpdateRule :execrows
UPDATE approval_rules
SET doctype = sqlc.arg(doctype),
    from_amount = sqlc.arg(from_amount),
    approver_type = sqlc.arg(approver_type),
    approver_role = sqlc.arg(approver_role),
    fallback_role = sqlc.arg(fallback_role),
    allow_self_approval = sqlc.arg(allow_self_approval),
    version = version + 1,
    updated_at = sqlc.arg(updated_at),
    updated_by = sqlc.arg(updated_by)
WHERE id = sqlc.arg(id) AND version = sqlc.arg(version);

-- name: SetRuleEnabled :execrows
UPDATE approval_rules
SET enabled = sqlc.arg(enabled),
    version = version + 1,
    updated_at = sqlc.arg(updated_at),
    updated_by = sqlc.arg(updated_by)
WHERE id = sqlc.arg(id);

-- name: DeleteRule :exec
DELETE FROM approval_rules WHERE id = ?;

-- name: GetRule :one
SELECT * FROM approval_rules WHERE id = ?;

-- name: ListRules :many
SELECT * FROM approval_rules ORDER BY doctype, from_amount, id;

-- name: ListEnabledRulesForDocType :many
SELECT * FROM approval_rules WHERE doctype = ? AND enabled = 1 ORDER BY from_amount;

-- name: FindEnabledDuplicate :one
SELECT * FROM approval_rules
WHERE doctype = sqlc.arg(doctype) AND from_amount = sqlc.arg(from_amount) AND enabled = 1 AND id <> sqlc.arg(id)
LIMIT 1;

-- name: CountRuleUsage :one
SELECT COUNT(DISTINCT doctype || ':' || doc_id) FROM approval_requests WHERE rule_id = ?;

-- name: InsertRequest :one
INSERT INTO approval_requests (
    doctype, doc_id, rule_id, amount, approver_roles, manager_user, manager_name, allow_self,
    requester, requester_name, creator, claimant_user, submitted_at,
    created_at, created_by, updated_at, updated_by
) VALUES (
    sqlc.arg(doctype), sqlc.arg(doc_id), sqlc.arg(rule_id), sqlc.arg(amount), sqlc.arg(approver_roles),
    sqlc.arg(manager_user), sqlc.arg(manager_name), sqlc.arg(allow_self),
    sqlc.arg(requester), sqlc.arg(requester_name), sqlc.arg(creator), sqlc.arg(claimant_user), sqlc.arg(submitted_at),
    sqlc.arg(submitted_at), sqlc.arg(created_by), sqlc.arg(submitted_at), sqlc.arg(created_by)
)
RETURNING id;

-- name: GetOpenRequest :one
SELECT * FROM approval_requests WHERE doctype = ? AND doc_id = ? AND outcome IS NULL;

-- name: GetLatestRequest :one
SELECT * FROM approval_requests WHERE doctype = ? AND doc_id = ? ORDER BY id DESC LIMIT 1;

-- name: CloseRequest :execrows
UPDATE approval_requests
SET outcome = sqlc.arg(outcome),
    decided_by = sqlc.arg(decided_by),
    decided_by_name = sqlc.arg(decided_by_name),
    decided_at = sqlc.arg(decided_at),
    reason = sqlc.arg(reason),
    version = version + 1,
    updated_at = sqlc.arg(decided_at),
    updated_by = sqlc.arg(decided_by)
WHERE id = sqlc.arg(id) AND outcome IS NULL;

-- name: ListOpenRequests :many
SELECT * FROM approval_requests WHERE outcome IS NULL ORDER BY submitted_at, id;

-- name: ListLatestRequestsByRequester :many
SELECT r.* FROM approval_requests r
WHERE r.requester = sqlc.arg(requester)
  AND r.id = (
    SELECT MAX(l.id) FROM approval_requests l
    WHERE l.doctype = r.doctype AND l.doc_id = r.doc_id AND l.requester = r.requester
  )
ORDER BY r.submitted_at DESC, r.id DESC;

-- name: InsertHistory :one
INSERT INTO approval_history (
    doctype, doc_id, request_id, action, user_id, user_name, at, amount, rule_id, reason, self_approved
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: ListHistory :many
SELECT * FROM approval_history WHERE doctype = ? AND doc_id = ? ORDER BY id;

-- name: LastHistory :one
SELECT * FROM approval_history WHERE doctype = ? AND doc_id = ? ORDER BY id DESC LIMIT 1;

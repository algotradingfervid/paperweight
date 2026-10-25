-- name: InsertEntry :exec
INSERT INTO audit_log (
    doc_type, doc_id, action, from_state, to_state, reason, changes, user_id, at,
    cause_doc_type, cause_doc_id, cause_number, cause_action
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListByDoc :many
SELECT doc_type, doc_id, action, from_state, to_state, reason, changes, user_id, at,
       cause_doc_type, cause_doc_id, cause_number, cause_action
FROM audit_log
WHERE doc_type = ? AND doc_id = ?
ORDER BY id;

-- name: CountAll :one
SELECT count(*) FROM audit_log;

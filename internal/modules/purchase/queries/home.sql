-- name: HomeDrafts :many
-- My drafts (P14, spec 25): the owner's POs in draft or rejected, rejected
-- first, then newest. The caller keeps the rows Auth.Scope allows.
SELECT o.id, o.state, o.rounded_total, o.snap_vendor_name,
       CAST(COALESCE(o.order_date, '') AS TEXT) AS order_date,
       CAST(COALESCE(p.code, '') AS TEXT) AS project_code
FROM purchase_order o
LEFT JOIN project p ON p.id = o.project_id
WHERE o.created_by = ? AND o.state IN ('draft', 'rejected')
ORDER BY o.state = 'rejected' DESC, o.created_at DESC, o.id DESC;

-- name: HomeWaiting :many
-- Waiting for approval (P14, spec 25): the owner's pending POs. The caller
-- orders them by their approval request's submit time.
SELECT o.id, o.state, o.rounded_total, o.snap_vendor_name
FROM purchase_order o
WHERE o.created_by = ? AND o.state = 'pending_approval'
ORDER BY o.id;

-- name: HomeToSend :many
-- To send (P14, spec 23 and 25): approved POs with no Sent record, the
-- owner's or (owner 0) everyone's, oldest approval first. approved_at is the
-- latest approval in approval_history, as the print reads it (P8).
SELECT o.id, o.state, o.rounded_total, o.snap_vendor_name, o.created_by,
       CAST(COALESCE((SELECT h.at FROM approval_history h
                      WHERE h.doctype = 'purchase.Order' AND h.doc_id = o.id
                        AND h.action IN ('approved', 'approved_no_rule')
                      ORDER BY h.id DESC LIMIT 1), '') AS TEXT) AS approved_at
FROM purchase_order o
WHERE o.state = 'approved'
  AND (CAST(sqlc.arg(owner) AS INTEGER) = 0 OR o.created_by = sqlc.arg(owner))
  AND NOT EXISTS (SELECT 1 FROM purchase_order_sent s WHERE s.po_id = o.id)
ORDER BY approved_at, o.id;

-- name: HomeToClose :many
-- To close (P14, spec 27.2): approved POs with at least one rest row, oldest
-- row first. since is the oldest row's created_at; no GRN id is read.
SELECT o.id, o.state, o.rounded_total, o.snap_vendor_name,
       CAST(MIN(r.created_at) AS TEXT) AS since
FROM purchase_order o
JOIN purchase_order_rest r ON r.po_id = o.id
WHERE o.state = 'approved'
GROUP BY o.id
ORDER BY since, o.id;

-- name: HomeAwaiting :many
-- Awaiting delivery (P14, spec 25 and 27.1): approved POs not fully
-- received, earliest due date first (expected_on when its row exists, else
-- required_by), POs with no due date last.
SELECT o.id, o.state, o.rounded_total, o.snap_vendor_name, o.receipt_status,
       CAST(COALESCE(o.required_by, '') AS TEXT) AS required_by,
       CAST(COALESCE(e.expected_on, '') AS TEXT) AS expected_on,
       CAST(COALESCE(e.expected_on, o.required_by, '') AS TEXT) AS due
FROM purchase_order o
LEFT JOIN purchase_order_expected e ON e.po_id = o.id
WHERE o.state = 'approved' AND o.receipt_status <> 'fully_received'
ORDER BY due = '', due, o.id;

-- name: HomeRequestsToOrder :many
-- Requests to order (P14, spec 25): submitted (approved) requests, oldest
-- needed-by first, those with none last. The caller keeps those with a row
-- pending to order.
SELECT r.id, r.state, r.created_by,
       CAST(COALESCE(r.required_by, '') AS TEXT) AS required_by,
       CAST(COALESCE(s.code, '') AS TEXT) AS site_code
FROM purchase_request r
LEFT JOIN site s ON s.id = r.site_id
WHERE r.state = 'approved'
ORDER BY r.required_by IS NULL, r.required_by, r.id;

-- name: HomeRequestLines :many
-- The rows of the submitted requests, for PendingToOrder (P14).
SELECT l.id, l.parent_id
FROM purchase_request_line l
JOIN purchase_request r ON r.id = l.parent_id
WHERE r.state = 'approved'
ORDER BY l.parent_id, l.line_no;

-- name: HomeMyRequests :many
-- My requests (P14, spec 25): the owner's open requests (draft or
-- submitted), newest first.
SELECT r.id, r.state, r.order_status,
       CAST(COALESCE(r.required_by, '') AS TEXT) AS required_by,
       CAST(COALESCE(s.code, '') AS TEXT) AS site_code
FROM purchase_request r
LEFT JOIN site s ON s.id = r.site_id
WHERE r.created_by = ? AND r.state IN ('draft', 'approved')
ORDER BY r.created_at DESC, r.id DESC;

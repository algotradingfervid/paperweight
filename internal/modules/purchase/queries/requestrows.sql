-- name: OrderRequestRows :many
-- checkRequestRows and requestWarnings (P6, spec 20.5): each line of the PO
-- that links a request row, with that row and its request, in line order. A
-- row that doesn't exist drops out (DOC refuses it). Blanks read 0.
SELECT l.line_no,
       CAST(COALESCE(l.item_id, 0) AS INTEGER) AS item_id,
       CAST(COALESCE(l.uom_id, 0) AS INTEGER) AS uom_id,
       CAST(COALESCE(l.qty, 0) AS INTEGER) AS qty,
       CAST(COALESCE(o.site_id, 0) AS INTEGER) AS site_id,
       CAST(COALESCE(o.project_id, 0) AS INTEGER) AS project_id,
       r.id AS row_id,
       r.line_no AS row_no,
       CAST(COALESCE(r.item_id, 0) AS INTEGER) AS row_item_id,
       CAST(COALESCE(r.uom_id, 0) AS INTEGER) AS row_uom_id,
       CAST(COALESCE(r.qty, 0) AS INTEGER) AS row_qty,
       q.id AS request_id,
       COALESCE(q.number, '') AS request_number,
       q.state AS request_state,
       CAST(COALESCE(q.site_id, 0) AS INTEGER) AS request_site_id,
       CAST(COALESCE(q.project_id, 0) AS INTEGER) AS request_project_id
FROM purchase_order_line l
JOIN purchase_order o ON o.id = l.parent_id
JOIN purchase_request_line r ON r.id = l.request_line_id
JOIN purchase_request q ON q.id = r.parent_id
WHERE l.parent_id = ?
ORDER BY l.line_no;

-- name: RequestLinkedLines :many
-- Every PO line on a row of the request, in any state, oldest PO first and
-- in line order (P6, spec 20.2). Blank quantities read 0.
SELECT l.id,
       CAST(l.request_line_id AS INTEGER) AS row_id,
       o.id AS po_id,
       COALESCE(o.number, '') AS po_number,
       o.state,
       o.created_by,
       CAST(COALESCE(l.qty, 0) AS INTEGER) AS qty,
       CAST(COALESCE(l.already_received, 0) AS INTEGER) AS already_received,
       l.received_qty
FROM purchase_order_line l
JOIN purchase_order o ON o.id = l.parent_id
JOIN purchase_request_line r ON r.id = l.request_line_id
WHERE r.parent_id = ?
ORDER BY o.id, l.line_no;

-- name: OrderRequestIDs :many
-- The requests a PO's lines link (P6): the ones its hooks refresh.
SELECT DISTINCT r.parent_id
FROM purchase_order_line l
JOIN purchase_request_line r ON r.id = l.request_line_id
WHERE l.parent_id = ?
ORDER BY r.parent_id;

-- name: RequestRowOf :one
-- PendingToOrder: a request row's request, its state and the row's qty (P6).
SELECT r.parent_id,
       q.state,
       CAST(COALESCE(r.qty, 0) AS INTEGER) AS qty
FROM purchase_request_line r
JOIN purchase_request q ON q.id = r.parent_id
WHERE r.id = ?;

-- name: SetRequestLineOrdered :exec
-- RefreshOrderStatus: a plain UPDATE, no version, no updated_*, no audit
-- (CD-280 D3).
UPDATE purchase_request_line SET ordered_qty = ? WHERE id = ?;

-- name: SetRequestOrderStatus :exec
-- RefreshOrderStatus: a plain UPDATE, no version, no updated_*, no audit
-- (CD-280 D3).
UPDATE purchase_request SET order_status = ? WHERE id = ?;

-- name: RequestLinkedOrders :many
-- LinkedOrders: every PO that isn't cancelled with a line on a row of the
-- request, oldest first (P6).
SELECT DISTINCT o.id,
       COALESCE(o.number, '') AS number,
       o.snap_vendor_name,
       o.state,
       o.receipt_status
FROM purchase_order o
JOIN purchase_order_line l ON l.parent_id = o.id
JOIN purchase_request_line r ON r.id = l.request_line_id
WHERE r.parent_id = ? AND o.state <> 'cancelled'
ORDER BY o.id;

-- name: OpenRequests :many
-- OpenRequestLines: submitted requests for a site and project, oldest
-- needed-by first, a blank needed-by last, then oldest request (P6).
SELECT id,
       COALESCE(number, '') AS number,
       created_by,
       COALESCE(required_by, '') AS required_by
FROM purchase_request
WHERE state = 'approved' AND site_id = ? AND project_id = ?
ORDER BY required_by IS NULL, required_by, id;

-- name: RequestRowAt :one
-- Add from requests (P15c): a ticked row's request and line number.
SELECT parent_id, line_no FROM purchase_request_line WHERE id = ?;

-- S3: what the check page, the document page and the cancel page show of a
-- GRN beyond its fields (UX doc 5.5, 5.6, 5.8).

-- name: S3Receipt :one
-- The GRN's PO and its own snapshot of the site and project (spec 3.1).
SELECT COALESCE(purchase_order_id, 0) AS purchase_order_id,
       COALESCE(snap_site_name, '') AS site_name, COALESCE(snap_project_code, '') AS project_code
FROM goods_receipt
WHERE id = ?;

-- name: S3Order :one
-- The PO's number, order date, state and receipt status, for a viewer who
-- can read it.
SELECT COALESCE(number, '') AS number, COALESCE(order_date, '') AS order_date, state, receipt_status
FROM purchase_order
WHERE id = ?;

-- name: S3OrderLines :many
-- Each PO line's ordered, already received and received (the roll-up).
SELECT COALESCE(qty, 0) AS qty, COALESCE(already_received, 0) AS already_received, received_qty
FROM purchase_order_line
WHERE parent_id = ?;

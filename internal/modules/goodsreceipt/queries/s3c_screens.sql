-- S3c: what a GRN line card shows (UX doc 5.5 P1).

-- name: S3cLines :many
-- Each line of GRN id with its PO line number and its own snapshots of the
-- item, the unit and the PO line's net rate (spec 3.2).
SELECT l.id, pl.line_no AS po_line_no, COALESCE(l.snap_item_code, '') AS item_code,
       COALESCE(l.snap_item_name, '') AS item_name, COALESCE(l.snap_uom_code, '') AS uom_code,
       COALESCE(l.snap_rate, 0) AS rate
FROM goods_receipt_line l
JOIN purchase_order_line pl ON pl.id = l.po_line_id
WHERE l.parent_id = ?;

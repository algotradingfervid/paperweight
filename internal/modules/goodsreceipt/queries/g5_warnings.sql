-- G5: warnings, notes and the period lock (GRN spec 4, 5; CD-234, CD-238,
-- CD-272 Gaps 4 and 13). Pending is purchase.Pending over the accepted
-- quantity of OTHER approved or closed GRNs: a GRN's own accepted never
-- counts against itself.

-- name: G5Header :one
-- The header facts the warnings, notes and lock read.
SELECT number, purchase_order_id, receipt_date,
       COALESCE(snap_po_number, '') AS snap_po_number, COALESCE(snap_po_date, '') AS snap_po_date,
       COALESCE(snap_vendor_gstin, '') AS snap_vendor_gstin, accepted_value, eway_bill_no
FROM goods_receipt
WHERE id = ?;

-- name: G5Lines :many
-- Each line of the GRN with its own PO line: the PO line number, ordered,
-- already received and accepted on other approved or closed GRNs.
SELECT l.line_no, l.po_line_id, pl.line_no AS po_line_no,
       COALESCE(l.accepted_qty, 0) AS accepted_qty, COALESCE(l.snap_uom_code, '') AS uom_code,
       COALESCE(pl.qty, 0) AS qty, COALESCE(pl.already_received, 0) AS already_received,
       CAST(COALESCE((SELECT SUM(o.accepted_qty) FROM goods_receipt_line o
                      JOIN goods_receipt g ON g.id = o.parent_id
                      WHERE o.po_line_id = pl.id AND g.id <> l.parent_id
                        AND g.state IN ('approved', 'closed')), 0) AS INTEGER) AS other_accepted
FROM goods_receipt_line l
JOIN purchase_order_line pl ON pl.id = l.po_line_id
WHERE l.parent_id = ?
ORDER BY l.line_no;

-- name: G5POLines :many
-- Every line of the PO with ordered, already received and accepted on
-- approved or closed GRNs other than grn_id.
SELECT pl.id, pl.line_no, o.number AS po_number, pl.snap_uom_code,
       COALESCE(pl.qty, 0) AS qty, COALESCE(pl.already_received, 0) AS already_received,
       CAST(COALESCE((SELECT SUM(x.accepted_qty) FROM goods_receipt_line x
                      JOIN goods_receipt g ON g.id = x.parent_id
                      WHERE x.po_line_id = pl.id AND g.id <> sqlc.arg(grn_id)
                        AND g.state IN ('approved', 'closed')), 0) AS INTEGER) AS other_accepted
FROM purchase_order_line pl
JOIN purchase_order o ON o.id = pl.parent_id
WHERE pl.parent_id = sqlc.arg(po_id)
ORDER BY pl.line_no;

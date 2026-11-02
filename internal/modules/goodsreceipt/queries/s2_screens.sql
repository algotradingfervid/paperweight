-- S2: what the Quantities step shows (UX doc 5.4).

-- name: S2POLines :many
-- Every line of GRN grn_id's PO, in PO line order, with its PO, its item
-- and unit snapshots and the units uom.CheckLineQty needs (as G2Lines reads
-- them). Pending comes from G5POLines.
SELECT pl.id, pl.parent_id AS po_id, pl.line_no, pl.snap_item_code, pl.snap_item_name, pl.snap_uom_code,
       pl.snap_factor AS factor, COALESCE(lu.whole_number, 0) AS line_whole,
       COALESCE(su.code, '') AS stock_uom_code, COALESCE(su.whole_number, 0) AS stock_whole
FROM goods_receipt g
JOIN purchase_order_line pl ON pl.parent_id = g.purchase_order_id
LEFT JOIN uom lu ON lu.id = pl.uom_id
LEFT JOIN item i ON i.id = pl.item_id
LEFT JOIN uom su ON su.id = i.stock_uom_id
WHERE g.id = ?
ORDER BY pl.line_no;

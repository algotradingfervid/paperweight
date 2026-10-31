-- G2: Validate's reads and writes and the submit-only rules (GRN spec 3.1,
-- 3.2, 4). Every derived column is written from the current inputs or
-- cleared, never left stale (CD-302): a blank or unknown purchase order
-- clears the header copies, a po_line_id that doesn't resolve clears its
-- line's copies.

-- name: G2SnapReceipt :exec
-- The header copies from the stored purchase_order_id, and a blank receipt
-- date defaults to today.
UPDATE goods_receipt SET
    receipt_date = COALESCE(NULLIF(receipt_date, ''), sqlc.arg(today)),
    vendor_id = (SELECT o.vendor_id FROM purchase_order o WHERE o.id = goods_receipt.purchase_order_id),
    site_id = (SELECT o.site_id FROM purchase_order o WHERE o.id = goods_receipt.purchase_order_id),
    project_id = (SELECT o.project_id FROM purchase_order o WHERE o.id = goods_receipt.purchase_order_id),
    snap_vendor_name = (SELECT o.snap_vendor_name FROM purchase_order o WHERE o.id = goods_receipt.purchase_order_id),
    snap_vendor_gstin = (SELECT o.snap_vendor_gstin FROM purchase_order o WHERE o.id = goods_receipt.purchase_order_id),
    snap_vendor_address = (SELECT o.snap_vendor_address FROM purchase_order o WHERE o.id = goods_receipt.purchase_order_id),
    snap_po_number = (SELECT o.number FROM purchase_order o WHERE o.id = goods_receipt.purchase_order_id),
    snap_po_date = (SELECT o.order_date FROM purchase_order o WHERE o.id = goods_receipt.purchase_order_id),
    snap_site_name = (SELECT o.snap_site_name FROM purchase_order o WHERE o.id = goods_receipt.purchase_order_id),
    snap_project_code = (SELECT pr.code FROM purchase_order o JOIN project pr ON pr.id = o.project_id
                         WHERE o.id = goods_receipt.purchase_order_id),
    snap_branch_id = (SELECT o.branch_id FROM purchase_order o WHERE o.id = goods_receipt.purchase_order_id)
WHERE goods_receipt.id = sqlc.arg(id);

-- name: G2SnapLines :exec
-- Each line's unit and snapshots from its own po_line_id (snap_rate is the
-- PO line's net rate, snap_is_service the item master's type), and accepted
-- as received less rejected, 0 when rejected is more.
UPDATE goods_receipt_line SET
    uom_id = (SELECT pl.uom_id FROM purchase_order_line pl WHERE pl.id = goods_receipt_line.po_line_id),
    snap_item_code = (SELECT pl.snap_item_code FROM purchase_order_line pl WHERE pl.id = goods_receipt_line.po_line_id),
    snap_item_name = (SELECT pl.snap_item_name FROM purchase_order_line pl WHERE pl.id = goods_receipt_line.po_line_id),
    snap_hsn_sac = (SELECT pl.hsn_sac FROM purchase_order_line pl WHERE pl.id = goods_receipt_line.po_line_id),
    snap_uom_code = (SELECT pl.snap_uom_code FROM purchase_order_line pl WHERE pl.id = goods_receipt_line.po_line_id),
    snap_rate = (SELECT pl.rate FROM purchase_order_line pl WHERE pl.id = goods_receipt_line.po_line_id),
    snap_is_service = (SELECT CASE i.item_type WHEN 'Service' THEN 1 WHEN 'Goods' THEN 0 END
                       FROM purchase_order_line pl JOIN item i ON i.id = pl.item_id
                       WHERE pl.id = goods_receipt_line.po_line_id),
    accepted_qty = CASE WHEN COALESCE(rejected_qty, 0) > COALESCE(received_qty, 0) THEN 0
                        ELSE COALESCE(received_qty, 0) - COALESCE(rejected_qty, 0) END
WHERE goods_receipt_line.parent_id = ?;

-- name: G2Receipt :one
-- The header as Validate checks it, after G2SnapReceipt.
SELECT g.purchase_order_id, o.number AS po_number, g.receipt_date, g.challan_date
FROM goods_receipt g
LEFT JOIN purchase_order o ON o.id = g.purchase_order_id
WHERE g.id = ?;

-- name: G2Lines :many
-- The lines as Validate checks them, after G2SnapLines, with each line's PO
-- line (po_id is NULL when po_line_id doesn't resolve) and the units
-- uom.CheckLineQty needs: the PO line's and the item's stock unit. Texts
-- number a line by its PO line (po_line_no), the posted position when the
-- PO line isn't on the GRN's PO or doesn't resolve (AD-12, UX 5.4 P2).
SELECT l.line_no,
       CAST(CASE WHEN pl.parent_id = g.purchase_order_id THEN pl.line_no ELSE l.line_no END AS INTEGER) AS po_line_no,
       l.po_line_id, COALESCE(l.received_qty, 0) AS received_qty,
       COALESCE(l.rejected_qty, 0) AS rejected_qty, COALESCE(l.accepted_qty, 0) AS accepted_qty,
       COALESCE(l.snap_rate, 0) AS snap_rate, COALESCE(l.snap_uom_code, '') AS uom_code,
       pl.parent_id AS po_id, COALESCE(pl.qty, 0) AS ordered_qty, COALESCE(pl.snap_factor, 0) AS factor,
       COALESCE(lu.whole_number, 0) AS line_whole,
       COALESCE(su.code, '') AS stock_uom_code, COALESCE(su.whole_number, 0) AS stock_whole
FROM goods_receipt_line l
JOIN goods_receipt g ON g.id = l.parent_id
LEFT JOIN purchase_order_line pl ON pl.id = l.po_line_id
LEFT JOIN uom lu ON lu.id = pl.uom_id
LEFT JOIN item i ON i.id = pl.item_id
LEFT JOIN uom su ON su.id = i.stock_uom_id
WHERE l.parent_id = ?
ORDER BY l.line_no;

-- name: G2SetAcceptedValue :exec
-- The sum over the stored lines, or NULL over the money maximum (CD-308).
UPDATE goods_receipt SET accepted_value = ? WHERE id = ?;

-- name: G2SubmitHeader :one
-- The challan fields and whether any line is goods (GRN spec 4, Submit).
SELECT g.challan_no, g.challan_kind,
       EXISTS (SELECT 1 FROM goods_receipt_line l WHERE l.parent_id = g.id AND l.snap_is_service = 0) AS has_goods
FROM goods_receipt g
WHERE g.id = ?;

-- name: G2RejectedNoReason :many
-- Lines that reject some quantity with a blank reason, with the PO line
-- number their text uses (AD-12, UX 5.4 P2).
SELECT l.line_no, COALESCE(pl.line_no, l.line_no) AS po_line_no
FROM goods_receipt_line l
LEFT JOIN purchase_order_line pl ON pl.id = l.po_line_id
WHERE l.parent_id = ? AND l.rejected_qty > 0 AND TRIM(l.reject_reason) = ''
ORDER BY l.line_no;

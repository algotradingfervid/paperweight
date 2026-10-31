-- G9: the reports (GRN spec 12), from the GRN's own columns and snapshots,
-- on approved and closed GRNs only. A 0 id is every vendor or item; a blank
-- month ("YYYY-MM" of the receipt date) is every month. The caller keeps
-- only the GRNs in the viewer's read scope, and blanks po_number when the
-- viewer can't read po_id (CD-340).

-- name: G9Bills :many
-- Bills to enter: one row per GRN, oldest receipt date first. no_bill keeps
-- GRNs with no grn_bill row; photos counts the GRN's attachments.
SELECT g.id, CAST(COALESCE(g.number, '') AS TEXT) AS number,
       CAST(COALESCE(g.receipt_date, '') AS TEXT) AS receipt_date,
       CAST(COALESCE(g.snap_vendor_name, '') AS TEXT) AS vendor_name,
       CAST(COALESCE(g.snap_vendor_gstin, '') AS TEXT) AS vendor_gstin,
       CAST(COALESCE(g.snap_po_number, '') AS TEXT) AS po_number,
       CAST(COALESCE(g.purchase_order_id, 0) AS INTEGER) AS po_id,
       g.challan_no,
       CAST(COALESCE(b.invoice_no, '') AS TEXT) AS invoice_no,
       CAST(COALESCE(b.invoice_date, '') AS TEXT) AS invoice_date,
       b.invoice_amount,
       CAST(COALESCE(g.accepted_value, 0) AS INTEGER) AS accepted_value,
       CAST((SELECT COUNT(*) FROM attachment a
             WHERE a.parent_doctype = sqlc.arg(doctype) AND a.parent_id = g.id) AS INTEGER) AS photos
FROM goods_receipt g
LEFT JOIN grn_bill b ON b.grn_id = g.id
WHERE g.state IN ('approved', 'closed')
  AND (CAST(sqlc.arg(no_bill) AS INTEGER) = 0 OR b.id IS NULL)
  AND (CAST(sqlc.arg(vendor_id) AS INTEGER) = 0 OR g.vendor_id = sqlc.arg(vendor_id))
  AND (CAST(sqlc.arg(month) AS TEXT) = '' OR substr(g.receipt_date, 1, 7) = sqlc.arg(month))
ORDER BY g.receipt_date, g.id;

-- name: G9Items :many
-- Received items: one row per GRN line, newest receipt date first, then
-- GRN id descending, then each GRN's lines by line_no. rejected_only keeps
-- lines with rejected above 0.
SELECT g.id AS grn_id, l.id AS line_id, CAST(COALESCE(g.number, '') AS TEXT) AS number,
       CAST(COALESCE(g.receipt_date, '') AS TEXT) AS receipt_date,
       CAST(COALESCE(g.snap_vendor_name, '') AS TEXT) AS vendor_name,
       CAST(COALESCE(g.snap_po_number, '') AS TEXT) AS po_number,
       CAST(COALESCE(g.purchase_order_id, 0) AS INTEGER) AS po_id,
       CAST(COALESCE(l.snap_item_code, '') AS TEXT) AS item_code,
       CAST(COALESCE(l.snap_item_name, '') AS TEXT) AS item_name,
       CAST(COALESCE(l.snap_hsn_sac, '') AS TEXT) AS hsn_sac,
       CAST(COALESCE(l.received_qty, 0) AS INTEGER) AS received_qty,
       CAST(COALESCE(l.accepted_qty, 0) AS INTEGER) AS accepted_qty,
       CAST(COALESCE(l.rejected_qty, 0) AS INTEGER) AS rejected_qty,
       l.reject_reason,
       CAST(COALESCE(l.snap_uom_code, '') AS TEXT) AS uom_code,
       CAST(COALESCE(l.snap_rate, 0) AS INTEGER) AS rate
FROM goods_receipt_line l
JOIN goods_receipt g ON g.id = l.parent_id
LEFT JOIN purchase_order_line pl ON pl.id = l.po_line_id
WHERE g.state IN ('approved', 'closed')
  AND (CAST(sqlc.arg(rejected_only) AS INTEGER) = 0 OR COALESCE(l.rejected_qty, 0) > 0)
  AND (CAST(sqlc.arg(vendor_id) AS INTEGER) = 0 OR g.vendor_id = sqlc.arg(vendor_id))
  AND (CAST(sqlc.arg(item_id) AS INTEGER) = 0 OR pl.item_id = sqlc.arg(item_id))
  AND (CAST(sqlc.arg(month) AS TEXT) = '' OR substr(g.receipt_date, 1, 7) = sqlc.arg(month))
ORDER BY g.receipt_date DESC, g.id DESC, l.line_no;

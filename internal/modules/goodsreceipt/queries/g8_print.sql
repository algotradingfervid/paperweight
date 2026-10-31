-- G8: the print reads (GRN spec 9), from the GRN's own columns and
-- snapshots only. Bill details and attachments are not printed.

-- name: G8ReceiptPrint :one
SELECT receipt_date, challan_kind, challan_no, challan_date, vehicle_no, transporter,
       eway_bill_no, remarks, COALESCE(snap_vendor_name, '') AS vendor_name,
       COALESCE(snap_vendor_gstin, '') AS vendor_gstin, COALESCE(snap_vendor_address, '') AS vendor_address,
       COALESCE(snap_po_number, '') AS po_number, snap_po_date, COALESCE(snap_branch_id, 0) AS branch_id
FROM goods_receipt
WHERE id = ?;

-- name: G8ReceiptPrintLines :many
SELECT COALESCE(snap_item_code, '') AS item_code, COALESCE(snap_item_name, '') AS item_name,
       COALESCE(snap_hsn_sac, '') AS hsn_sac, COALESCE(snap_uom_code, '') AS uom_code,
       COALESCE(received_qty, 0) AS received_qty, COALESCE(accepted_qty, 0) AS accepted_qty,
       COALESCE(rejected_qty, 0) AS rejected_qty, reject_reason
FROM goods_receipt_line
WHERE parent_id = ?
ORDER BY line_no;

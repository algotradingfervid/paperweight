-- The PO print (P8, PO spec section 9): filled from the snapshots only.
-- Remarks, comments, the sent record, Expected on and the rest rows are
-- never read here.

-- name: OrderPrint :one
-- amends_number is the number of the PO this one amends (amended_from_id).
-- site_id reads the site's current delivery address (CD-337 (1)); the PO
-- keeps no snapshot of it.
SELECT o.order_date, o.required_by, o.branch_id, o.vendor_quote_ref, o.terms, o.reverse_charge, o.earlier_po_no,
       o.net_total, o.cgst_total, o.sgst_total, o.igst_total, o.grand_total, o.rounded_total,
       o.snap_vendor_name, o.snap_vendor_gstin, o.snap_vendor_category, o.snap_vendor_state, o.snap_vendor_address,
       o.snap_place_state, o.site_id, o.snap_site_name, o.snap_terms_name, a.number AS amends_number
FROM purchase_order o
LEFT JOIN purchase_order a ON a.id = o.amended_from_id
WHERE o.id = ?;

-- name: OrderPrintLines :many
SELECT snap_item_code, snap_item_name, description, hsn_sac, qty, snap_uom_code, rate, amount,
       cgst, sgst, igst, snap_gst_rate
FROM purchase_order_line
WHERE parent_id = ?
ORDER BY line_no, id;

-- name: OrderApprovedBy :one
-- The latest approval. A PO approved with no rule (approved_no_rule) names
-- its submitter (PO-046), unlike an expense claim.
SELECT user_name, at
FROM approval_history
WHERE doctype = ? AND doc_id = ? AND action IN ('approved', 'approved_no_rule')
ORDER BY id DESC
LIMIT 1;

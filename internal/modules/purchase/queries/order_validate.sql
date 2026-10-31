-- name: OrderForValidate :one
-- Validate, BeforeSubmit and OrderWarnings: the header fields their rules
-- read (P2). Blank links read 0.
SELECT state,
       CAST(COALESCE(vendor_id, 0) AS INTEGER) AS vendor_id,
       CAST(COALESCE(vendor_address_id, 0) AS INTEGER) AS vendor_address_id,
       CAST(COALESCE(site_id, 0) AS INTEGER) AS site_id,
       CAST(COALESCE(project_id, 0) AS INTEGER) AS project_id,
       CAST(COALESCE(payment_terms_id, 0) AS INTEGER) AS payment_terms_id,
       CAST(COALESCE(order_date, '') AS TEXT) AS order_date,
       CAST(COALESCE(required_by, '') AS TEXT) AS required_by,
       terms, reverse_charge, earlier_po_no,
       CAST(COALESCE(earlier_po_date, '') AS TEXT) AS earlier_po_date,
       CAST(COALESCE(snap_vendor_id, 0) AS INTEGER) AS snap_vendor_id
FROM purchase_order
WHERE id = ?;

-- name: OrderLinesForValidate :many
-- Validate, BeforeSubmit and OrderWarnings: each line as stored (P2). Blank
-- links read 0; a blank price or discount stays NULL, so a default can fill it.
SELECT id, line_no,
       CAST(COALESCE(item_id, 0) AS INTEGER) AS item_id,
       CAST(COALESCE(uom_id, 0) AS INTEGER) AS uom_id,
       CAST(COALESCE(tax_code_id, 0) AS INTEGER) AS tax_code_id,
       CAST(COALESCE(qty, 0) AS INTEGER) AS qty,
       price, discount_pct, description, hsn_sac,
       CAST(COALESCE(already_received, 0) AS INTEGER) AS already_received,
       received_qty
FROM purchase_order_line
WHERE parent_id = ?
ORDER BY line_no;

-- name: SetOrderVendor :exec
-- Validate's draft write of the vendor part: the address and payment terms
-- defaults and the vendor snapshots (P2). Written on every draft Validate
-- (CD-302): computed from the current vendor, or cleared when there is no
-- vendor or it is refused. A plain UPDATE inside DOC's write.
UPDATE purchase_order
SET vendor_address_id = sqlc.narg(vendor_address_id),
    payment_terms_id = sqlc.narg(payment_terms_id),
    snap_vendor_id = sqlc.narg(snap_vendor_id),
    snap_vendor_code = sqlc.arg(snap_vendor_code),
    snap_vendor_name = sqlc.arg(snap_vendor_name),
    snap_vendor_gstin = sqlc.arg(snap_vendor_gstin),
    snap_vendor_category = sqlc.arg(snap_vendor_category),
    snap_vendor_state = sqlc.arg(snap_vendor_state),
    snap_vendor_address = sqlc.arg(snap_vendor_address),
    snap_terms_name = sqlc.arg(snap_terms_name)
WHERE id = sqlc.arg(id);

-- name: SetOrderSite :exec
-- Validate's draft write of the site part: the project default, the branch
-- copied from the site and the site and branch snapshots (P2). Written on
-- every draft Validate (CD-302): computed from the current site, or cleared
-- when there is none; a refused site and project check skips only the
-- project default.
UPDATE purchase_order
SET project_id = sqlc.narg(project_id),
    branch_id = sqlc.narg(branch_id),
    snap_site_name = sqlc.arg(snap_site_name),
    snap_branch_name = sqlc.arg(snap_branch_name),
    snap_branch_gstin = sqlc.arg(snap_branch_gstin),
    snap_place_state = sqlc.arg(snap_place_state)
WHERE id = sqlc.arg(id);

-- name: SetOrderTotals :exec
-- Validate's draft write of the terms default, the totals and receipt_status
-- (P2).
UPDATE purchase_order
SET terms = sqlc.arg(terms),
    net_total = sqlc.arg(net_total),
    cgst_total = sqlc.arg(cgst_total),
    sgst_total = sqlc.arg(sgst_total),
    igst_total = sqlc.arg(igst_total),
    grand_total = sqlc.arg(grand_total),
    rounded_total = sqlc.arg(rounded_total),
    receipt_status = sqlc.arg(receipt_status)
WHERE id = sqlc.arg(id);

-- name: SetOrderLineValidated :exec
-- Validate's draft write of one line: defaults, net rate, amount, GST and
-- snapshots (P2).
UPDATE purchase_order_line
SET description = sqlc.arg(description),
    hsn_sac = sqlc.arg(hsn_sac),
    uom_id = sqlc.narg(uom_id),
    tax_code_id = sqlc.narg(tax_code_id),
    price = sqlc.arg(price),
    discount_pct = sqlc.arg(discount_pct),
    already_received = sqlc.arg(already_received),
    rate = sqlc.arg(rate),
    amount = sqlc.arg(amount),
    cgst = sqlc.arg(cgst),
    sgst = sqlc.arg(sgst),
    igst = sqlc.arg(igst),
    snap_item_code = sqlc.arg(snap_item_code),
    snap_item_name = sqlc.arg(snap_item_name),
    snap_uom_code = sqlc.arg(snap_uom_code),
    snap_factor = sqlc.arg(snap_factor),
    snap_stock_qty = sqlc.arg(snap_stock_qty),
    snap_gst_rate = sqlc.arg(snap_gst_rate),
    snap_tax_code = sqlc.arg(snap_tax_code)
WHERE id = sqlc.arg(id);

-- name: OrderAmount :one
-- The Amount hook: the rounded total, plus the tax under reverse charge (P2).
SELECT rounded_total, cgst_total + sgst_total + igst_total AS tax, reverse_charge
FROM purchase_order
WHERE id = ?;

-- name: DefaultPOTerms :one
-- Validate's default terms when the PO has none (spec 14.1), read in its tx.
SELECT default_po_terms FROM business_settings WHERE id = 1;

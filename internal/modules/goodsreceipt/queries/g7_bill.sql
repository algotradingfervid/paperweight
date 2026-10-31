-- G7: bill details (GRN spec 3.3; CD-234, CD-272 Gaps 1 and 5). One grn_bill
-- row per GRN with its own version; no row is version 0.

-- name: G7Receipt :one
-- The GRN facts SaveBill, Bill and the move to an amendment read: the rights
-- fields, the state, the vendor, accepted value and the gate's challan.
SELECT COALESCE(number, '') AS number, state, created_by, updated_by,
       COALESCE(project_id, 0) AS project_id, COALESCE(site_id, 0) AS site_id,
       COALESCE(vendor_id, 0) AS vendor_id, COALESCE(accepted_value, 0) AS accepted_value,
       challan_kind, challan_no, COALESCE(challan_date, '') AS challan_date, amended_from_id
FROM goods_receipt
WHERE id = ?;

-- name: G7Bill :one
SELECT invoice_no, invoice_date, invoice_amount, version, updated_by, updated_at
FROM grn_bill
WHERE grn_id = ?;

-- name: G7BillInsert :exec
INSERT INTO grn_bill (grn_id, invoice_no, invoice_date, invoice_amount, version,
                      created_at, created_by, updated_at, updated_by)
VALUES (sqlc.arg(grn_id), sqlc.arg(invoice_no), sqlc.arg(invoice_date), sqlc.arg(invoice_amount), 1,
        sqlc.arg(at), sqlc.arg(by), sqlc.arg(at), sqlc.arg(by));

-- name: G7BillUpdate :exec
UPDATE grn_bill
SET invoice_no = sqlc.arg(invoice_no), invoice_date = sqlc.arg(invoice_date),
    invoice_amount = sqlc.arg(invoice_amount), version = version + 1,
    updated_at = sqlc.arg(at), updated_by = sqlc.arg(by)
WHERE grn_id = sqlc.arg(grn_id);

-- name: G7BillCopy :execrows
-- The amended-from GRN's row copied to the amendment at version 1; 0 rows
-- when the amended-from GRN has none.
INSERT INTO grn_bill (grn_id, invoice_no, invoice_date, invoice_amount, version,
                      created_at, created_by, updated_at, updated_by)
SELECT sqlc.arg(to_id), src.invoice_no, src.invoice_date, src.invoice_amount, 1,
       sqlc.arg(at), sqlc.arg(by), sqlc.arg(at), sqlc.arg(by)
FROM grn_bill AS src
WHERE src.grn_id = sqlc.arg(from_id);

-- name: G7Duplicates :many
-- Other GRNs, not cancelled, of the same vendor whose bill has the same
-- invoice number, with their rights fields for the read check.
SELECT g.id, COALESCE(g.number, '') AS number, g.created_by,
       COALESCE(g.project_id, 0) AS project_id, COALESCE(g.site_id, 0) AS site_id
FROM grn_bill b
JOIN goods_receipt g ON g.id = b.grn_id
WHERE b.invoice_no = sqlc.arg(invoice_no) AND g.vendor_id = sqlc.arg(vendor_id)
  AND g.id <> sqlc.arg(grn_id) AND g.state <> 'cancelled'
ORDER BY g.id;

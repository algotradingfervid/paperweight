-- name: GetSent :one
-- The Sent record of a PO (P11, spec 23); no row means version 0.
SELECT po_id, sent_on, sent_via, note, sent_by, sent_at, version
FROM purchase_order_sent WHERE po_id = ?;

-- name: InsertSent :exec
-- A mark with posted version 0: the first row, at version 1. The caller has
-- read inside the same write that there is no row.
INSERT INTO purchase_order_sent (po_id, sent_on, sent_via, note, sent_by, sent_at, version)
VALUES (?, ?, ?, ?, ?, ?, 1);

-- name: UpdateSent :exec
-- A mark with the posted current version, which the caller has checked
-- inside the same write.
UPDATE purchase_order_sent
SET sent_on = ?, sent_via = ?, note = ?, sent_by = ?, sent_at = ?, version = version + 1
WHERE po_id = ?;

-- name: DeleteSent :exec
-- Mark as not sent with the posted current version, which the caller has
-- checked inside the same write.
DELETE FROM purchase_order_sent WHERE po_id = ?;

-- name: OrderPageSummary :one
-- The Send and Expected on pages' summary from the PO's snapshot (P15e):
-- the vendor name and required by (blank reads '').
SELECT snap_vendor_name, CAST(COALESCE(required_by, '') AS TEXT) AS required_by
FROM purchase_order WHERE id = ?;

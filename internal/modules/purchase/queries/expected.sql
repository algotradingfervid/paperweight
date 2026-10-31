-- name: GetExpected :one
-- The Expected on record of a PO (P18, spec 27.1); no row means version 0.
SELECT po_id, expected_on, note, set_by, set_at, version
FROM purchase_order_expected WHERE po_id = ?;

-- name: InsertExpected :exec
-- A set with posted version 0: the first row, at version 1. The caller has
-- read inside the same write that there is no row.
INSERT INTO purchase_order_expected (po_id, expected_on, note, set_by, set_at, version)
VALUES (?, ?, ?, ?, ?, 1);

-- name: UpdateExpected :exec
-- A set with the posted current version, which the caller has checked
-- inside the same write.
UPDATE purchase_order_expected
SET expected_on = ?, note = ?, set_by = ?, set_at = ?, version = version + 1
WHERE po_id = ?;

-- name: DeleteExpected :exec
-- A clear with the posted current version, which the caller has checked
-- inside the same write.
DELETE FROM purchase_order_expected WHERE po_id = ?;

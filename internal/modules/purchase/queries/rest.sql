-- name: InsertRest :execrows
-- SetRestNotComing (P19, PO spec 27.2): DO NOTHING keeps the first row for a
-- (po, grn) pair, so calling again inserts nothing and the caller, seeing
-- 0 rows, posts no second comment.
INSERT INTO purchase_order_rest (po_id, grn_id, created_by, created_at)
VALUES (?, ?, ?, ?)
ON CONFLICT (po_id, grn_id) DO NOTHING;

-- name: DeleteRest :exec
-- SetRestNotComing(on=false): a missing row is a no-op (P19).
DELETE FROM purchase_order_rest WHERE po_id = ? AND grn_id = ?;

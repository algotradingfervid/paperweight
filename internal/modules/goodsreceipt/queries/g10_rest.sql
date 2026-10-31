-- G10: vendor says the rest won't come (GRN spec 3.1, 5; CD-276).

-- name: G10Rest :one
-- The tick, the GRN's PO and its updated_by, who the PO comment is posted as.
SELECT rest_not_coming, COALESCE(purchase_order_id, 0) AS purchase_order_id, updated_by
FROM goods_receipt
WHERE id = ?;

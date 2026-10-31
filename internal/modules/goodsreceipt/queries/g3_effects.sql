-- G3: effects and receipt status (GRN spec 5). GRN never writes the PO's
-- received_qty: the kernel roll-up does.

-- name: G3ReceiptPO :one
-- The purchase order a GRN receives against.
SELECT purchase_order_id FROM goods_receipt WHERE id = ?;

-- name: G3AcceptedByLine :many
-- Every line of the PO with the accepted quantity summed over GRN lines
-- whose GRN is approved or closed (0 when there are none). The kernel has
-- already set this GRN's state when the hooks run.
SELECT pl.id,
       CAST(COALESCE(SUM(CASE WHEN g.state IN ('approved', 'closed') THEN l.accepted_qty END), 0) AS INTEGER) AS accepted
FROM purchase_order_line pl
LEFT JOIN goods_receipt_line l ON l.po_line_id = pl.id
LEFT JOIN goods_receipt g ON g.id = l.parent_id
WHERE pl.parent_id = ?
GROUP BY pl.id
ORDER BY pl.id;

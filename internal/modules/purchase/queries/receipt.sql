-- name: ReceiptLines :many
-- RefreshReceiptStatus: each PO line's quantities (P4). A blank qty or
-- already_received (a draft line) counts 0.
SELECT id,
       CAST(COALESCE(qty, 0) AS INTEGER) AS qty,
       CAST(COALESCE(already_received, 0) AS INTEGER) AS already_received
FROM purchase_order_line
WHERE parent_id = ?
ORDER BY line_no;

-- name: OrderState :one
-- RefreshReceiptStatus calls refreshClosedRequests only for a closed PO (P4).
SELECT state FROM purchase_order WHERE id = ?;

-- name: SetReceiptStatus :exec
-- A plain UPDATE: no version, no updated_*, no audit (CD-280 D3).
UPDATE purchase_order SET receipt_status = ? WHERE id = ?;

-- Shared reads (G1). Each later task adds its own queries/<task>.sql, with
-- query names carrying its prefix (G2PO, G3Accepted, G7Bill and so on).

-- name: ReceiptDate :one
-- The Date hook: receipt_date sets the FY of the GRN's number (spec 6).
SELECT receipt_date FROM goods_receipt WHERE id = ?;

-- name: ReceiptAcceptedValue :one
-- The Amount hook: the accepted value before tax (spec 3.1).
SELECT accepted_value FROM goods_receipt WHERE id = ?;

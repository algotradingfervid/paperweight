-- name: OrderDate :one
-- The Date hook: order_date sets the FY of the PO's number (P1).
SELECT order_date FROM purchase_order WHERE id = ?;

-- name: RequestDate :one
-- The Date hook: request_date sets the FY of the request's number (P1).
SELECT request_date FROM purchase_request WHERE id = ?;

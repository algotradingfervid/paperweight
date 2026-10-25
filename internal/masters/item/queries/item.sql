-- name: GetItem :one
SELECT id, code, name, item_type, hsn_sac, stock_uom_id, purchase_uom_id, default_tax_code_id,
    is_stock, standard_rate, disabled, version
FROM item WHERE id = ?;

-- name: GetItemByCode :one
SELECT id, code, name, item_type, hsn_sac, stock_uom_id, purchase_uom_id, default_tax_code_id,
    is_stock, standard_rate, disabled, version
FROM item WHERE code = ?;

-- name: InsertItem :one
INSERT INTO item (code, name, item_type, hsn_sac, stock_uom_id, purchase_uom_id, default_tax_code_id,
    is_stock, standard_rate, created_at, created_by, updated_at, updated_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: UpdateItem :execrows
UPDATE item SET code = ?, name = ?, item_type = ?, hsn_sac = ?, stock_uom_id = ?, purchase_uom_id = ?,
    default_tax_code_id = ?, is_stock = ?, standard_rate = ?,
    version = version + 1, updated_at = ?, updated_by = ?
WHERE id = ? AND version = ?;

-- name: ListItemUoMs :many
SELECT uom_id, factor FROM item_uom WHERE parent_id = ? ORDER BY line_no;

-- name: GetItemUoMFactor :one
SELECT factor FROM item_uom WHERE parent_id = ? AND uom_id = ?;

-- name: DeleteItemUoMs :exec
DELETE FROM item_uom WHERE parent_id = ?;

-- name: InsertItemUoM :exec
INSERT INTO item_uom (parent_id, line_no, uom_id, factor) VALUES (?, ?, ?, ?);

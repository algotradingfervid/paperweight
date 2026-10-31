-- name: LastPriceFor :one
-- The default price of a line saved with a blank price (P7, spec 24.2): the
-- price and discount of the newest line (latest order_date, then highest id)
-- on an approved or closed PO with the same vendor, item, UoM and project.
-- A line saved with no price (no last price and no standard rate) is none;
-- a priced line with no discount counts as discount 0.
SELECT CAST(l.price AS INTEGER) AS price, CAST(COALESCE(l.discount_pct, 0) AS INTEGER) AS discount_pct
FROM purchase_order_line l
JOIN purchase_order o ON o.id = l.parent_id
WHERE o.state IN ('approved', 'closed')
  AND o.vendor_id = ? AND l.item_id = ? AND l.uom_id = ? AND o.project_id = ?
  AND l.price IS NOT NULL
ORDER BY o.order_date DESC, o.id DESC, l.id DESC
LIMIT 1;

-- name: LastPricesForItem :many
-- Every line of item and UoM on an approved or closed PO, newest first (P7,
-- spec 24.2 hint); the caller keeps the first one the viewer may read.
SELECT o.id AS po_id, COALESCE(o.number, '') AS number, o.snap_vendor_name,
       CAST(l.price AS INTEGER) AS price, COALESCE(o.order_date, '') AS order_date
FROM purchase_order_line l
JOIN purchase_order o ON o.id = l.parent_id
WHERE o.state IN ('approved', 'closed')
  AND l.item_id = ? AND l.uom_id = ?
  AND l.price IS NOT NULL
ORDER BY o.order_date DESC, o.id DESC, l.id DESC;

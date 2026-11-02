-- S1: what the Order and Delivery steps show
-- (UX doc 5.2, 5.3).

-- name: S1Receipt :one
-- The GRN's own snapshot of its PO (CD-217, CD-228) and whether any stored
-- line is goods (G2's has_goods).
SELECT COALESCE(g.snap_po_number, '') AS po_number, COALESCE(g.snap_vendor_name, '') AS vendor_name,
       COALESCE(g.snap_site_name, '') AS site_name,
       EXISTS (SELECT 1 FROM goods_receipt_line l WHERE l.parent_id = g.id AND l.snap_is_service = 0) AS has_goods
FROM goods_receipt g
WHERE g.id = ?;

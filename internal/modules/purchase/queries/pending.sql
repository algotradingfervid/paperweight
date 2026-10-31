-- name: PendingLines :many
-- The pending report's rows (P9, spec 13): lines with pending above 0 on
-- approved POs. A 0 id means every vendor, project, site or item. due is
-- the line's due date (spec 27.1): the PO's expected_on when its row exists,
-- else required_by; empty when neither. Earliest due first, lines with no
-- due date last, then PO number and line number.
SELECT l.id AS line_id,
       o.id AS po_id,
       CAST(COALESCE(o.number, '') AS TEXT) AS number,
       CAST(COALESCE(o.order_date, '') AS TEXT) AS order_date,
       CAST(COALESCE(o.required_by, '') AS TEXT) AS required_by,
       CAST(COALESCE(e.expected_on, '') AS TEXT) AS expected_on,
       CAST(COALESCE(e.expected_on, o.required_by, '') AS TEXT) AS due,
       o.snap_vendor_name,
       CAST(COALESCE(p.code, '') AS TEXT) AS project_code,
       CAST(COALESCE(s.code, '') AS TEXT) AS site_code,
       l.snap_item_code,
       l.snap_item_name,
       l.snap_uom_code,
       CAST(COALESCE(l.qty, 0) AS INTEGER) AS qty,
       CAST(COALESCE(l.already_received, 0) AS INTEGER) AS already_received,
       l.received_qty,
       CAST(COALESCE(l.rate, 0) AS INTEGER) AS rate
FROM purchase_order_line l
JOIN purchase_order o ON o.id = l.parent_id
LEFT JOIN purchase_order_expected e ON e.po_id = o.id
LEFT JOIN project p ON p.id = o.project_id
LEFT JOIN site s ON s.id = o.site_id
WHERE o.state = 'approved'
  AND COALESCE(l.qty, 0) - COALESCE(l.already_received, 0) - l.received_qty > 0
  AND (CAST(sqlc.arg(vendor_id) AS INTEGER) = 0 OR o.vendor_id = sqlc.arg(vendor_id))
  AND (CAST(sqlc.arg(project_id) AS INTEGER) = 0 OR o.project_id = sqlc.arg(project_id))
  AND (CAST(sqlc.arg(site_id) AS INTEGER) = 0 OR o.site_id = sqlc.arg(site_id))
  AND (CAST(sqlc.arg(item_id) AS INTEGER) = 0 OR l.item_id = sqlc.arg(item_id))
ORDER BY due = '', due, number, l.line_no;

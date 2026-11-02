-- G6: step 1's PO picker and the draft from a PO (GRN spec 8, 11; UX doc
-- 5.2). The viewer's read scope is applied in Go: Auth.Scope's predicate
-- is dynamic SQL.

-- name: G6PickerLines :many
-- Every line of each approved PO (not closed, not cancelled) matching q, a
-- case-insensitive substring of the PO number, vendor code, vendor name or
-- GSTIN (blank q matches all), ordered by vendor name, then PO number.
SELECT o.id, COALESCE(o.number, '') AS number, o.snap_vendor_name, COALESCE(s.name, '') AS site_name,
       COALESCE(o.order_date, '') AS order_date,
       COALESCE(l.qty, 0) AS qty, COALESCE(l.already_received, 0) AS already, l.received_qty
FROM purchase_order o
JOIN purchase_order_line l ON l.parent_id = o.id
LEFT JOIN site s ON s.id = o.site_id
WHERE o.state = 'approved'
  AND (CAST(sqlc.arg(q) AS TEXT) = ''
       OR instr(lower(COALESCE(o.number, '')), lower(CAST(sqlc.arg(q) AS TEXT))) > 0
       OR instr(lower(o.snap_vendor_code), lower(CAST(sqlc.arg(q) AS TEXT))) > 0
       OR instr(lower(o.snap_vendor_name), lower(CAST(sqlc.arg(q) AS TEXT))) > 0
       OR instr(lower(o.snap_vendor_gstin), lower(CAST(sqlc.arg(q) AS TEXT))) > 0)
ORDER BY o.snap_vendor_name, o.number, l.line_no;

-- name: G6PO :one
-- The PO's site and project, posted by step 1.
SELECT site_id, project_id FROM purchase_order WHERE id = ?;

-- name: G6POLines :many
-- The PO's lines with the quantities purchase.Pending reads.
SELECT id, COALESCE(qty, 0) AS qty, COALESCE(already_received, 0) AS already, received_qty
FROM purchase_order_line
WHERE parent_id = ?
ORDER BY line_no;

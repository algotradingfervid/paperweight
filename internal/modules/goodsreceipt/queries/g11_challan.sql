-- G11: the duplicate challan warning (CD-337 (6)). Other GRNs, not
-- cancelled, of the same vendor whose challan no. matches this GRN's,
-- trimmed and in any case, with their rights fields for the read check. A
-- blank challan matches nothing. Each row carries this GRN's own challan
-- no., trimmed, for the text.

-- name: G11Duplicates :many
SELECT o.id, COALESCE(o.number, '') AS number, o.created_by,
       COALESCE(o.project_id, 0) AS project_id, COALESCE(o.site_id, 0) AS site_id,
       CAST(TRIM(s.challan_no) AS TEXT) AS challan_no
FROM goods_receipt s
JOIN goods_receipt o ON o.vendor_id = s.vendor_id
  AND LOWER(TRIM(o.challan_no)) = LOWER(TRIM(s.challan_no))
WHERE s.id = sqlc.arg(grn_id) AND TRIM(s.challan_no) <> ''
  AND o.id <> s.id AND o.state <> 'cancelled'
ORDER BY o.id;

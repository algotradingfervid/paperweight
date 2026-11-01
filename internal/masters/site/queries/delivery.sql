-- A site's delivery address and contact (CD-337 (1), M0-58): at most one of
-- each, owned through site_id. A change deletes and re-inserts them.

-- name: SiteAddress :one
SELECT line1, line2, city, state_code, pincode FROM address WHERE site_id = ?;

-- name: SiteContact :one
-- A site contact has at most one phone, its mobile.
SELECT c.name, CAST(coalesce(f.number, '') AS TEXT) AS mobile
FROM contact c LEFT JOIN contact_phone f ON f.contact_id = c.id
WHERE c.site_id = ?;

-- name: DeleteSiteAddress :exec
DELETE FROM address WHERE site_id = ?;

-- name: DeleteSiteContact :exec
-- Its phone goes with it (ON DELETE CASCADE).
DELETE FROM contact WHERE site_id = ?;

-- name: InsertSiteAddress :exec
INSERT INTO address (site_id, name, line1, line2, city, state_code, pincode, is_shipping,
                     created_at, created_by, updated_at, updated_by)
VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?);

-- name: InsertSiteContact :one
INSERT INTO contact (site_id, name, first_name, created_at, created_by, updated_at, updated_by)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: InsertSiteContactMobile :exec
INSERT INTO contact_phone (contact_id, number, kind, is_primary) VALUES (?, ?, 'mobile', 1);

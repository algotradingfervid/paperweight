-- The canonical Fallback Approver role (Expense spec §6, CD-238, CD-264 (5)).
-- SeedDefaults creates roles only on first install (CD-158), so an existing
-- database would never get it from there. A migration runs once, so a role an
-- administrator later deletes is never recreated. Its rights come from the
-- expense DocTypes' DefaultRights through SeedDefaults.
-- +goose Up
INSERT INTO roles (name, name_key, is_admin, created_at, updated_at)
SELECT 'Fallback Approver', 'fallback approver', 0,
       strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE NOT EXISTS (SELECT 1 FROM roles WHERE name_key = 'fallback approver');

-- +goose Down
DELETE FROM roles WHERE name_key = 'fallback approver';

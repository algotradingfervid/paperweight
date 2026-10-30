-- name: InsertContact :one
INSERT INTO contact (party_id, branch_id, name, first_name, middle_name, last_name, is_primary,
                     created_at, created_by, updated_at, updated_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: UpdateContact :exec
UPDATE contact
SET name = ?, first_name = ?, middle_name = ?, last_name = ?, is_primary = ?,
    version = version + 1, updated_at = ?, updated_by = ?
WHERE id = ? AND version = ?;

-- name: GetContact :one
SELECT * FROM contact WHERE id = ?;

-- name: ContactEmails :many
SELECT * FROM contact_email WHERE contact_id = ? ORDER BY id;

-- name: ContactPhones :many
SELECT * FROM contact_phone WHERE contact_id = ? ORDER BY id;

-- name: InsertContactEmail :exec
INSERT INTO contact_email (contact_id, email, is_primary) VALUES (?, ?, ?);

-- name: InsertContactPhone :exec
INSERT INTO contact_phone (contact_id, number, kind, is_primary) VALUES (?, ?, ?, ?);

-- name: DeleteContactEmails :exec
DELETE FROM contact_email WHERE contact_id = ?;

-- name: DeleteContactPhones :exec
DELETE FROM contact_phone WHERE contact_id = ?;

-- name: OtherPrimaryContacts :many
SELECT id FROM contact
WHERE (party_id = sqlc.narg(party_id) OR branch_id = sqlc.narg(branch_id)) AND id <> sqlc.arg(id) AND is_primary = 1;

-- name: ClearPrimaryContact :exec
UPDATE contact SET is_primary = 0, version = version + 1, updated_at = ?, updated_by = ? WHERE id = ?;

-- name: SetPrimaryContact :exec
UPDATE contact SET is_primary = 1, version = version + 1, updated_at = ?, updated_by = ? WHERE id = ?;

-- name: OwnerContactFlags :many
-- The owner's contacts with what the post-state check needs (CD-190).
SELECT c.id, c.party_id, c.branch_id, c.is_primary, c.disabled,
       CAST((SELECT count(*) FROM contact_email e WHERE e.contact_id = c.id AND e.is_primary = 1) AS INTEGER) AS primary_emails,
       CAST((SELECT count(*) FROM contact_phone f WHERE f.contact_id = c.id AND f.is_primary = 1) AS INTEGER) AS primary_phones
FROM contact c
WHERE c.party_id = sqlc.narg(party_id) OR c.branch_id = sqlc.narg(branch_id);

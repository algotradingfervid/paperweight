-- name: InsertAddress :one
INSERT INTO address (party_id, branch_id, name, title, line1, line2, city, state_code, country, pincode, gstin,
                     is_billing, is_shipping, is_primary_billing, is_primary_shipping,
                     created_at, created_by, updated_at, updated_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: UpdateAddress :execrows
UPDATE address
SET name = ?, title = ?, line1 = ?, line2 = ?, city = ?, state_code = ?, country = ?, pincode = ?, gstin = ?,
    is_billing = ?, is_shipping = ?, is_primary_billing = ?, is_primary_shipping = ?,
    version = version + 1, updated_at = ?, updated_by = ?
WHERE id = ? AND version = ?;

-- name: GetAddress :one
SELECT * FROM address WHERE id = ?;

-- name: PartyAddresses :many
SELECT * FROM address WHERE party_id = ? AND disabled <= CAST(sqlc.arg(include_disabled) AS INTEGER) ORDER BY id;

-- name: PrimaryBillingOfParty :one
SELECT * FROM address WHERE party_id = ? AND is_primary_billing = 1;

-- name: PrimaryBillingOfBranch :one
SELECT * FROM address WHERE branch_id = ? AND is_primary_billing = 1;

-- name: OtherPrimaryBilling :many
SELECT id FROM address
WHERE (party_id = sqlc.narg(party_id) OR branch_id = sqlc.narg(branch_id)) AND id <> sqlc.arg(id) AND is_primary_billing = 1;

-- name: OtherPrimaryShipping :many
SELECT id FROM address
WHERE (party_id = sqlc.narg(party_id) OR branch_id = sqlc.narg(branch_id)) AND id <> sqlc.arg(id) AND is_primary_shipping = 1;

-- name: ClearPrimaryBilling :exec
UPDATE address SET is_primary_billing = 0, version = version + 1, updated_at = ?, updated_by = ? WHERE id = ?;

-- name: ClearPrimaryShipping :exec
UPDATE address SET is_primary_shipping = 0, version = version + 1, updated_at = ?, updated_by = ? WHERE id = ?;

-- name: SetPrimaryBilling :execrows
UPDATE address SET is_primary_billing = 1, version = version + 1, updated_at = ?, updated_by = ?
WHERE id = ? AND version = ?;

-- name: SetPrimaryShipping :execrows
UPDATE address SET is_primary_shipping = 1, version = version + 1, updated_at = ?, updated_by = ?
WHERE id = ? AND version = ?;

-- name: GSTINOnOtherParty :one
-- The first party other than sqlc.arg(party_id) with an address holding the GSTIN, disabled or not (MST-044).
SELECT p.code, p.disabled FROM address a JOIN party p ON p.id = a.party_id
WHERE a.gstin = sqlc.arg(gstin) AND a.party_id <> sqlc.arg(party_id)
ORDER BY a.id LIMIT 1;

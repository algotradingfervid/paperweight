-- name: InsertParty :one
INSERT INTO party (code, name, is_vendor, is_customer, pan, gst_category, default_payment_terms_id,
                   created_at, created_by, updated_at, updated_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: UpdateParty :execrows
UPDATE party
SET code = ?, name = ?, is_vendor = ?, is_customer = ?, pan = ?, gst_category = ?, default_payment_terms_id = ?,
    version = version + 1, updated_at = ?, updated_by = ?
WHERE id = ? AND version = ?;

-- name: UpdatePANAndCategory :exec
UPDATE party
SET pan = ?, gst_category = ?, version = version + 1, updated_at = ?, updated_by = ?
WHERE id = ?;

-- name: GetParty :one
SELECT id, code, name, is_vendor, is_customer, pan, gst_category, default_payment_terms_id, disabled, version
FROM party WHERE id = ?;

-- name: GetPartyByCode :one
SELECT id, code, name, is_vendor, is_customer, pan, gst_category, default_payment_terms_id, disabled, version
FROM party WHERE code = ?;

-- name: GetDefaults :one
SELECT p.default_payment_terms_id, coalesce(t.disabled, 0) AS terms_disabled
FROM party p LEFT JOIN payment_terms t ON t.id = p.default_payment_terms_id
WHERE p.id = ?;

-- name: AddressGSTINs :many
SELECT gstin FROM address WHERE party_id = ? AND gstin <> '' ORDER BY id;

-- name: PrimaryBillingAddress :one
SELECT gstin, country FROM address WHERE party_id = ? AND is_primary_billing = 1;

-- name: PartyAddressesGST :many
SELECT country, gstin, is_primary_billing, is_primary_shipping, disabled FROM address WHERE party_id = ? ORDER BY id;

-- Addresses and contacts (MST Tasks 6 and 7). Each belongs to exactly one
-- party or one branch (MST-067) and is deleted with it (MST-046). GSTINs live
-- on addresses (D-MST-06). `name` is what messages show: the address title
-- (or its city) and the contact's full name.
-- +goose Up
CREATE TABLE address (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    party_id            INTEGER REFERENCES party (id) ON DELETE CASCADE,
    branch_id           INTEGER REFERENCES branch (id) ON DELETE CASCADE,
    name                TEXT    NOT NULL,
    title               TEXT    NOT NULL DEFAULT '',
    line1               TEXT    NOT NULL,
    line2               TEXT    NOT NULL DEFAULT '',
    city                TEXT    NOT NULL,
    state_code          TEXT    REFERENCES gst_state (code) ON DELETE RESTRICT, -- NULL outside India
    country             TEXT    NOT NULL DEFAULT 'India',
    pincode             TEXT    NOT NULL DEFAULT '',
    gstin               TEXT    NOT NULL DEFAULT '',
    is_billing          INTEGER NOT NULL DEFAULT 0 CHECK (is_billing IN (0, 1)),
    is_shipping         INTEGER NOT NULL DEFAULT 0 CHECK (is_shipping IN (0, 1)),
    is_primary_billing  INTEGER NOT NULL DEFAULT 0 CHECK (is_primary_billing IN (0, 1) AND is_primary_billing <= is_billing),
    is_primary_shipping INTEGER NOT NULL DEFAULT 0 CHECK (is_primary_shipping IN (0, 1) AND is_primary_shipping <= is_shipping),
    disabled            INTEGER NOT NULL DEFAULT 0 CHECK (disabled IN (0, 1)),
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TEXT    NOT NULL,
    created_by          INTEGER,
    updated_at          TEXT    NOT NULL,
    updated_by          INTEGER,
    CHECK ((party_id IS NULL) <> (branch_id IS NULL)),
    CHECK (is_billing = 1 OR is_shipping = 1)
);
-- One primary billing and one primary shipping address per owner (MST-060,
-- Review Focus 3).
CREATE UNIQUE INDEX address_party_primary_billing ON address (party_id) WHERE is_primary_billing = 1;
CREATE UNIQUE INDEX address_party_primary_shipping ON address (party_id) WHERE is_primary_shipping = 1;
CREATE UNIQUE INDEX address_branch_primary_billing ON address (branch_id) WHERE is_primary_billing = 1;
CREATE UNIQUE INDEX address_branch_primary_shipping ON address (branch_id) WHERE is_primary_shipping = 1;
CREATE INDEX address_gstin ON address (gstin);

CREATE TABLE contact (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    party_id    INTEGER REFERENCES party (id) ON DELETE CASCADE,
    branch_id   INTEGER REFERENCES branch (id) ON DELETE CASCADE,
    name        TEXT    NOT NULL,
    first_name  TEXT    NOT NULL,
    middle_name TEXT    NOT NULL DEFAULT '',
    last_name   TEXT    NOT NULL DEFAULT '',
    is_primary  INTEGER NOT NULL DEFAULT 0 CHECK (is_primary IN (0, 1)),
    disabled    INTEGER NOT NULL DEFAULT 0 CHECK (disabled IN (0, 1)),
    version     INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT    NOT NULL,
    created_by  INTEGER,
    updated_at  TEXT    NOT NULL,
    updated_by  INTEGER,
    CHECK ((party_id IS NULL) <> (branch_id IS NULL))
);
CREATE UNIQUE INDEX contact_party_primary ON contact (party_id) WHERE is_primary = 1;

CREATE TABLE contact_email (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    contact_id INTEGER NOT NULL REFERENCES contact (id) ON DELETE CASCADE,
    email      TEXT    NOT NULL,
    is_primary INTEGER NOT NULL DEFAULT 0 CHECK (is_primary IN (0, 1))
);

CREATE TABLE contact_phone (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    contact_id INTEGER NOT NULL REFERENCES contact (id) ON DELETE CASCADE,
    number     TEXT    NOT NULL,
    kind       TEXT    NOT NULL CHECK (kind IN ('mobile', 'phone')),
    is_primary INTEGER NOT NULL DEFAULT 0 CHECK (is_primary IN (0, 1))
);

-- +goose Down
DROP TABLE contact_phone;
DROP TABLE contact_email;
DROP TABLE contact;
DROP TABLE address;

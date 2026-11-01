-- A site's optional delivery address and contact (CD-337 (1), M0-58): address
-- and contact may now belong to a party, a branch or a site, and a site has at
-- most one of each. SQLite can't alter a CHECK, so both tables are rebuilt by
-- its documented procedure (lang_altertable.html, "other kinds of table schema
-- changes"): foreign keys off, then in one transaction create the new table,
-- copy, drop the old one and rename the new one into its place. The foreign
-- keys that point at the tables (purchase_order.vendor_address_id,
-- contact_email, contact_phone) keep naming "address" and "contact", so they
-- resolve to the new tables, and with foreign keys off the drop deletes no
-- child row. Ids and the AUTOINCREMENT counters are kept. PRAGMA foreign_keys
-- is a no-op inside a transaction, so goose runs this one without its own.
-- +goose NO TRANSACTION
-- +goose Up
PRAGMA foreign_keys = OFF;
BEGIN IMMEDIATE;
CREATE TABLE address_new (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    party_id            INTEGER REFERENCES party (id) ON DELETE CASCADE,
    branch_id           INTEGER REFERENCES branch (id) ON DELETE CASCADE,
    site_id             INTEGER REFERENCES site (id) ON DELETE CASCADE,
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
    CHECK ((party_id IS NOT NULL) + (branch_id IS NOT NULL) + (site_id IS NOT NULL) = 1),
    CHECK (is_billing = 1 OR is_shipping = 1)
);
INSERT INTO address_new (id, party_id, branch_id, name, title, line1, line2, city, state_code, country, pincode, gstin,
                     is_billing, is_shipping, is_primary_billing, is_primary_shipping, disabled, version,
                     created_at, created_by, updated_at, updated_by)
SELECT id, party_id, branch_id, name, title, line1, line2, city, state_code, country, pincode, gstin,
       is_billing, is_shipping, is_primary_billing, is_primary_shipping, disabled, version,
       created_at, created_by, updated_at, updated_by
FROM address;
DELETE FROM sqlite_sequence WHERE name = 'address_new';
UPDATE sqlite_sequence SET name = 'address_new' WHERE name = 'address';
DROP TABLE address;
ALTER TABLE address_new RENAME TO address;
CREATE UNIQUE INDEX address_party_primary_billing ON address (party_id) WHERE is_primary_billing = 1;
CREATE UNIQUE INDEX address_party_primary_shipping ON address (party_id) WHERE is_primary_shipping = 1;
CREATE UNIQUE INDEX address_branch_primary_billing ON address (branch_id) WHERE is_primary_billing = 1;
CREATE UNIQUE INDEX address_branch_primary_shipping ON address (branch_id) WHERE is_primary_shipping = 1;
CREATE INDEX address_gstin ON address (gstin);
-- At most one address per site.
CREATE UNIQUE INDEX address_site ON address (site_id) WHERE site_id IS NOT NULL;

CREATE TABLE contact_new (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    party_id    INTEGER REFERENCES party (id) ON DELETE CASCADE,
    branch_id   INTEGER REFERENCES branch (id) ON DELETE CASCADE,
    site_id     INTEGER REFERENCES site (id) ON DELETE CASCADE,
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
    CHECK ((party_id IS NOT NULL) + (branch_id IS NOT NULL) + (site_id IS NOT NULL) = 1)
);
INSERT INTO contact_new (id, party_id, branch_id, name, first_name, middle_name, last_name, is_primary, disabled, version,
                     created_at, created_by, updated_at, updated_by)
SELECT id, party_id, branch_id, name, first_name, middle_name, last_name, is_primary, disabled, version,
       created_at, created_by, updated_at, updated_by
FROM contact;
DELETE FROM sqlite_sequence WHERE name = 'contact_new';
UPDATE sqlite_sequence SET name = 'contact_new' WHERE name = 'contact';
DROP TABLE contact;
ALTER TABLE contact_new RENAME TO contact;
CREATE UNIQUE INDEX contact_party_primary ON contact (party_id) WHERE is_primary = 1;
CREATE UNIQUE INDEX contact_branch_primary ON contact (branch_id) WHERE is_primary = 1;
-- At most one contact per site.
CREATE UNIQUE INDEX contact_site ON contact (site_id) WHERE site_id IS NOT NULL;

COMMIT;
PRAGMA foreign_keys = ON;

-- +goose Down
PRAGMA foreign_keys = OFF;
BEGIN IMMEDIATE;
DELETE FROM address WHERE site_id IS NOT NULL;
CREATE TABLE address_new (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    party_id            INTEGER REFERENCES party (id) ON DELETE CASCADE,
    branch_id           INTEGER REFERENCES branch (id) ON DELETE CASCADE,
    name                TEXT    NOT NULL,
    title               TEXT    NOT NULL DEFAULT '',
    line1               TEXT    NOT NULL,
    line2               TEXT    NOT NULL DEFAULT '',
    city                TEXT    NOT NULL,
    state_code          TEXT    REFERENCES gst_state (code) ON DELETE RESTRICT,
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
INSERT INTO address_new (id, party_id, branch_id, name, title, line1, line2, city, state_code, country, pincode, gstin,
                     is_billing, is_shipping, is_primary_billing, is_primary_shipping, disabled, version,
                     created_at, created_by, updated_at, updated_by)
SELECT id, party_id, branch_id, name, title, line1, line2, city, state_code, country, pincode, gstin,
       is_billing, is_shipping, is_primary_billing, is_primary_shipping, disabled, version,
       created_at, created_by, updated_at, updated_by
FROM address;
DELETE FROM sqlite_sequence WHERE name = 'address_new';
UPDATE sqlite_sequence SET name = 'address_new' WHERE name = 'address';
DROP TABLE address;
ALTER TABLE address_new RENAME TO address;
CREATE UNIQUE INDEX address_party_primary_billing ON address (party_id) WHERE is_primary_billing = 1;
CREATE UNIQUE INDEX address_party_primary_shipping ON address (party_id) WHERE is_primary_shipping = 1;
CREATE UNIQUE INDEX address_branch_primary_billing ON address (branch_id) WHERE is_primary_billing = 1;
CREATE UNIQUE INDEX address_branch_primary_shipping ON address (branch_id) WHERE is_primary_shipping = 1;
CREATE INDEX address_gstin ON address (gstin);

DELETE FROM contact_email WHERE contact_id IN (SELECT id FROM contact WHERE site_id IS NOT NULL);
DELETE FROM contact_phone WHERE contact_id IN (SELECT id FROM contact WHERE site_id IS NOT NULL);
DELETE FROM contact WHERE site_id IS NOT NULL;
CREATE TABLE contact_new (
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
INSERT INTO contact_new (id, party_id, branch_id, name, first_name, middle_name, last_name, is_primary, disabled, version,
                     created_at, created_by, updated_at, updated_by)
SELECT id, party_id, branch_id, name, first_name, middle_name, last_name, is_primary, disabled, version,
       created_at, created_by, updated_at, updated_by
FROM contact;
DELETE FROM sqlite_sequence WHERE name = 'contact_new';
UPDATE sqlite_sequence SET name = 'contact_new' WHERE name = 'contact';
DROP TABLE contact;
ALTER TABLE contact_new RENAME TO contact;
CREATE UNIQUE INDEX contact_party_primary ON contact (party_id) WHERE is_primary = 1;
CREATE UNIQUE INDEX contact_branch_primary ON contact (branch_id) WHERE is_primary = 1;

COMMIT;
PRAGMA foreign_keys = ON;

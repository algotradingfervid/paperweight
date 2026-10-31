-- Business settings (PO spec §14.1): one row, seeded here so a save is always
-- an UPDATE on the version the page posted (CD-94). locked_up_to is the
-- period lock (§14.2), blank by default. created_by NULL = seeded by migration.
-- +goose Up
CREATE TABLE business_settings (
    id               INTEGER PRIMARY KEY AUTOINCREMENT CHECK (id = 1),
    locked_up_to     TEXT,
    default_po_terms TEXT    NOT NULL DEFAULT '',
    version          INTEGER NOT NULL DEFAULT 1,
    created_at       TEXT    NOT NULL,
    created_by       INTEGER,
    updated_at       TEXT    NOT NULL,
    updated_by       INTEGER
) STRICT;

INSERT INTO business_settings (id, created_at, updated_at)
VALUES (1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));

-- +goose Down
DROP TABLE business_settings;

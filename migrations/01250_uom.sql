-- Units of measure (MST Task 8): a code master with the GST UQC it reports
-- under and whether quantities in it must be whole numbers (MST-077, MST-086).
-- +goose Up
CREATE TABLE uom (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    code         TEXT    NOT NULL UNIQUE CHECK (code = upper(code)),
    name         TEXT    NOT NULL,
    uqc          TEXT    NOT NULL DEFAULT '',
    whole_number INTEGER NOT NULL DEFAULT 0 CHECK (whole_number IN (0, 1)),
    disabled     INTEGER NOT NULL DEFAULT 0 CHECK (disabled IN (0, 1)),
    version      INTEGER NOT NULL DEFAULT 1,
    created_at   TEXT    NOT NULL,
    created_by   INTEGER,
    updated_at   TEXT    NOT NULL,
    updated_by   INTEGER
);

-- +goose Down
DROP TABLE uom;

-- Expense types (MST Task 11): a code master with an optional default HSN/SAC
-- and default tax code that expense claim lines start from (MST-121, MST-122).
-- +goose Up
CREATE TABLE expense_type (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    code                TEXT    NOT NULL UNIQUE CHECK (code = upper(code)),
    name                TEXT    NOT NULL,
    default_hsn_sac     TEXT    NOT NULL DEFAULT '',
    default_tax_code_id INTEGER REFERENCES tax_code(id) ON DELETE RESTRICT,
    disabled            INTEGER NOT NULL DEFAULT 0 CHECK (disabled IN (0, 1)),
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TEXT    NOT NULL,
    created_by          INTEGER,
    updated_at          TEXT    NOT NULL,
    updated_by          INTEGER
);

-- +goose Down
DROP TABLE expense_type;

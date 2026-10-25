-- GST tax codes (MST Task 9): a code master with a GST treatment and a rate
-- in thousandths of a percent (types.Percent: 18% = 18000). Taxable needs a
-- rate above 0; every other treatment stores 0 (MST-094, MST-095, D-MST-16).
-- +goose Up
CREATE TABLE tax_code (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    code       TEXT    NOT NULL UNIQUE CHECK (code = upper(code)),
    name       TEXT    NOT NULL,
    treatment  TEXT    NOT NULL CHECK (treatment IN ('Taxable', 'Nil-Rated', 'Exempted', 'Non-GST', 'Zero-Rated')),
    rate       INTEGER NOT NULL DEFAULT 0 CHECK ((treatment = 'Taxable') = (rate > 0) AND rate >= 0),
    disabled   INTEGER NOT NULL DEFAULT 0 CHECK (disabled IN (0, 1)),
    version    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT    NOT NULL,
    created_by INTEGER,
    updated_at TEXT    NOT NULL,
    updated_by INTEGER
);

-- +goose Down
DROP TABLE tax_code;

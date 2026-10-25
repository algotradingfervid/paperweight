-- Payment terms (MST Task 4): a code master whose rows are a portion of the
-- total due a number of days after the document date (D-MST-23).
-- +goose Up
CREATE TABLE payment_terms (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    code       TEXT    NOT NULL UNIQUE CHECK (code = upper(code)),
    name       TEXT    NOT NULL,
    disabled   INTEGER NOT NULL DEFAULT 0 CHECK (disabled IN (0, 1)),
    version    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT    NOT NULL,
    created_by INTEGER,
    updated_at TEXT    NOT NULL,
    updated_by INTEGER
);

CREATE TABLE payment_terms_row (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    parent_id INTEGER NOT NULL REFERENCES payment_terms (id) ON DELETE CASCADE,
    line_no   INTEGER NOT NULL,
    portion   INTEGER NOT NULL CHECK (portion > 0), -- thousandths of a percent
    days      INTEGER NOT NULL CHECK (days >= 0),
    UNIQUE (parent_id, days)
);

-- +goose Down
DROP TABLE payment_terms_row;
DROP TABLE payment_terms;

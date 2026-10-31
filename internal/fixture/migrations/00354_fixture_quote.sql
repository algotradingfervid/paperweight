-- Test-only fixture.Quote (Quotation) tables (M0-40c). Column types are
-- INTEGER and TEXT only (CD-34).

-- +goose Up
CREATE TABLE fixture_quote (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    number            TEXT UNIQUE,
    state             TEXT NOT NULL DEFAULT 'draft' CHECK (state IN ('draft', 'pending_approval', 'approved', 'closed', 'rejected', 'cancelled')),
    state_reason      TEXT NOT NULL DEFAULT '',
    amended_from_id   INTEGER REFERENCES fixture_quote (id),
    amendment_root_id INTEGER,
    amendment_no      INTEGER NOT NULL DEFAULT 0 CHECK (amendment_no BETWEEN 0 AND 9),
    version           INTEGER NOT NULL DEFAULT 1,
    created_at        TEXT NOT NULL,
    created_by        INTEGER NOT NULL,
    updated_at        TEXT NOT NULL,
    updated_by        INTEGER NOT NULL,
    quote_date        TEXT,
    remarks           TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX fixture_quote_one_amendment ON fixture_quote (amended_from_id) WHERE amended_from_id IS NOT NULL;

CREATE TABLE fixture_quote_line (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    parent_id INTEGER NOT NULL REFERENCES fixture_quote (id) ON DELETE CASCADE,
    line_no   INTEGER NOT NULL,
    item_id     INTEGER REFERENCES fixture_item (id) ON DELETE RESTRICT,
    qty         INTEGER,
    rate        INTEGER,
    amount      INTEGER,
    description TEXT NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE fixture_quote_line;
DROP TABLE fixture_quote;

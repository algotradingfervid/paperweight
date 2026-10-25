-- Test-only fixture.Receipt (Goods Receipt) tables and the effect log its
-- hooks write. Column types are INTEGER and TEXT only (CD-34).

-- +goose Up
CREATE TABLE fixture_receipt (
    id                INTEGER PRIMARY KEY,
    number            TEXT UNIQUE,
    state             TEXT NOT NULL DEFAULT 'draft' CHECK (state IN ('draft', 'pending_approval', 'approved', 'closed', 'rejected', 'cancelled')),
    state_reason      TEXT NOT NULL DEFAULT '',
    amended_from_id   INTEGER REFERENCES fixture_receipt (id),
    amendment_root_id INTEGER,
    amendment_no      INTEGER NOT NULL DEFAULT 0 CHECK (amendment_no BETWEEN 0 AND 9),
    version           INTEGER NOT NULL DEFAULT 1,
    created_at        TEXT NOT NULL,
    created_by        INTEGER NOT NULL,
    updated_at        TEXT NOT NULL,
    updated_by        INTEGER NOT NULL,
    receipt_date      TEXT,
    remarks           TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX fixture_receipt_one_amendment ON fixture_receipt (amended_from_id) WHERE amended_from_id IS NOT NULL;

CREATE TABLE fixture_receipt_line (
    id        INTEGER PRIMARY KEY,
    parent_id INTEGER NOT NULL REFERENCES fixture_receipt (id) ON DELETE CASCADE,
    line_no   INTEGER NOT NULL,
    order_line_id INTEGER,
    qty           INTEGER
);

CREATE TABLE fixture_effect_log (
    id            INTEGER PRIMARY KEY,
    receipt_id    INTEGER NOT NULL,
    order_line_id INTEGER,
    action        TEXT NOT NULL CHECK (action IN ('apply', 'reverse'))
);

-- +goose Down
DROP TABLE fixture_effect_log;
DROP TABLE fixture_receipt_line;
DROP TABLE fixture_receipt;

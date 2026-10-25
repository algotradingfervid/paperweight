-- Test-only fixture.Claim (Expense Claim) tables (CD-25). Column types are
-- INTEGER and TEXT only (CD-34).

-- +goose Up
CREATE TABLE fixture_claim (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    number            TEXT UNIQUE,
    state             TEXT NOT NULL DEFAULT 'draft' CHECK (state IN ('draft', 'pending_approval', 'approved', 'closed', 'rejected', 'cancelled')),
    state_reason      TEXT NOT NULL DEFAULT '',
    amended_from_id   INTEGER REFERENCES fixture_claim (id),
    amendment_root_id INTEGER,
    amendment_no      INTEGER NOT NULL DEFAULT 0 CHECK (amendment_no BETWEEN 0 AND 9),
    version           INTEGER NOT NULL DEFAULT 1,
    created_at        TEXT NOT NULL,
    created_by        INTEGER NOT NULL,
    updated_at        TEXT NOT NULL,
    updated_by        INTEGER NOT NULL,
    employee_id       INTEGER,
    claim_date        TEXT,
    total             INTEGER,
    remarks           TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX fixture_claim_one_amendment ON fixture_claim (amended_from_id) WHERE amended_from_id IS NOT NULL;

CREATE TABLE fixture_claim_line (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    parent_id INTEGER NOT NULL REFERENCES fixture_claim (id) ON DELETE CASCADE,
    line_no   INTEGER NOT NULL,
    expense_type TEXT NOT NULL DEFAULT '',
    description  TEXT NOT NULL DEFAULT '',
    amount       INTEGER
);

-- +goose Down
DROP TABLE fixture_claim_line;
DROP TABLE fixture_claim;

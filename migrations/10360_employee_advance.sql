-- Employee advances (Expense spec §4, CD-234; MOD-EXP E1). used_amount is
-- recomputed from the approved and closed claims naming the advance (D-DOC-13);
-- paid_on is set only by Mark as paid (CD-271 G1).
-- +goose Up
CREATE TABLE employee_advance (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    number             TEXT UNIQUE,
    state              TEXT    NOT NULL DEFAULT 'draft' CHECK (state IN ('draft', 'pending_approval', 'approved', 'closed', 'rejected', 'cancelled')),
    state_reason       TEXT    NOT NULL DEFAULT '',
    amended_from_id    INTEGER REFERENCES employee_advance (id),
    amendment_root_id  INTEGER,
    amendment_no       INTEGER NOT NULL DEFAULT 0 CHECK (amendment_no BETWEEN 0 AND 9),
    version            INTEGER NOT NULL DEFAULT 1,
    created_at         TEXT    NOT NULL,
    created_by         INTEGER NOT NULL,
    updated_at         TEXT    NOT NULL,
    updated_by         INTEGER NOT NULL,
    employee_id        INTEGER REFERENCES employee (id) ON DELETE RESTRICT,
    advance_date       TEXT,
    purpose            TEXT    NOT NULL DEFAULT '',
    amount             INTEGER,
    used_amount        INTEGER NOT NULL DEFAULT 0,
    paid_on            TEXT,
    snap_employee_name TEXT    NOT NULL DEFAULT '',
    snap_employee_code TEXT    NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX employee_advance_one_amendment ON employee_advance (amended_from_id) WHERE amended_from_id IS NOT NULL;
CREATE INDEX employee_advance_employee ON employee_advance (employee_id);

CREATE TABLE employee_advance_line (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    parent_id         INTEGER NOT NULL REFERENCES employee_advance (id) ON DELETE CASCADE,
    line_no           INTEGER NOT NULL,
    expense_type_id   INTEGER REFERENCES expense_type (id) ON DELETE RESTRICT,
    description       TEXT    NOT NULL DEFAULT '',
    amount            INTEGER,
    project_id        INTEGER REFERENCES project (id) ON DELETE RESTRICT,
    site_id           INTEGER REFERENCES site (id) ON DELETE RESTRICT,
    snap_type_code    TEXT    NOT NULL DEFAULT '',
    snap_type_name    TEXT    NOT NULL DEFAULT '',
    snap_project_code TEXT    NOT NULL DEFAULT '',
    snap_site_code    TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX employee_advance_line_parent ON employee_advance_line (parent_id, line_no);

-- +goose Down
DROP TABLE employee_advance_line;
DROP TABLE employee_advance;

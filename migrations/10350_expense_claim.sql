-- Expense claims (Expense spec §3; MOD-EXP E1). Every id is AUTOINCREMENT
-- (CD-156); lines have parent_id ON DELETE CASCADE and line_no (CD-161).
-- less_advance is NULL until submit, meaning as much as the advance allows
-- (CD-264 (6)). billed_to_id is the branch named on a company bill (CD-271 G4).
-- +goose Up
CREATE TABLE expense_claim (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    number             TEXT UNIQUE,
    state              TEXT    NOT NULL DEFAULT 'draft' CHECK (state IN ('draft', 'pending_approval', 'approved', 'closed', 'rejected', 'cancelled')),
    state_reason       TEXT    NOT NULL DEFAULT '',
    amended_from_id    INTEGER REFERENCES expense_claim (id),
    amendment_root_id  INTEGER,
    amendment_no       INTEGER NOT NULL DEFAULT 0 CHECK (amendment_no BETWEEN 0 AND 9),
    version            INTEGER NOT NULL DEFAULT 1,
    created_at         TEXT    NOT NULL,
    created_by         INTEGER NOT NULL,
    updated_at         TEXT    NOT NULL,
    updated_by         INTEGER NOT NULL,
    employee_id        INTEGER REFERENCES employee (id) ON DELETE RESTRICT,
    claim_date         TEXT,
    purpose            TEXT    NOT NULL DEFAULT '',
    advance_id         INTEGER REFERENCES employee_advance (id) ON DELETE RESTRICT,
    less_advance       INTEGER,
    total              INTEGER,
    net_payable        INTEGER,
    period_from        TEXT,
    period_to          TEXT,
    remarks            TEXT    NOT NULL DEFAULT '',
    snap_employee_name TEXT    NOT NULL DEFAULT '',
    snap_employee_code TEXT    NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX expense_claim_one_amendment ON expense_claim (amended_from_id) WHERE amended_from_id IS NOT NULL;
CREATE INDEX expense_claim_employee ON expense_claim (employee_id);
CREATE INDEX expense_claim_advance ON expense_claim (advance_id) WHERE advance_id IS NOT NULL;

CREATE TABLE expense_claim_line (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    parent_id         INTEGER NOT NULL REFERENCES expense_claim (id) ON DELETE CASCADE,
    line_no           INTEGER NOT NULL,
    expense_date      TEXT,
    expense_type_id   INTEGER REFERENCES expense_type (id) ON DELETE RESTRICT,
    description       TEXT    NOT NULL DEFAULT '',
    amount            INTEGER,
    project_id        INTEGER REFERENCES project (id) ON DELETE RESTRICT,
    site_id           INTEGER REFERENCES site (id) ON DELETE RESTRICT,
    receipt_sha256    TEXT    NOT NULL DEFAULT '',
    no_receipt_reason TEXT    NOT NULL DEFAULT '',
    company_bill      INTEGER NOT NULL DEFAULT 0 CHECK (company_bill IN (0, 1)),
    supplier_gstin    TEXT    NOT NULL DEFAULT '',
    invoice_no        TEXT    NOT NULL DEFAULT '',
    billed_to_id      INTEGER REFERENCES branch (id) ON DELETE RESTRICT,
    tax_code_id       INTEGER REFERENCES tax_code (id) ON DELETE RESTRICT,
    hsn_sac           TEXT    NOT NULL DEFAULT '',
    gst_amount        INTEGER,
    snap_type_code    TEXT    NOT NULL DEFAULT '',
    snap_type_name    TEXT    NOT NULL DEFAULT '',
    snap_project_code TEXT    NOT NULL DEFAULT '',
    snap_site_code    TEXT    NOT NULL DEFAULT '',
    snap_billed_gstin TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX expense_claim_line_parent ON expense_claim_line (parent_id, line_no);
CREATE INDEX expense_claim_line_receipt ON expense_claim_line (receipt_sha256) WHERE receipt_sha256 <> '';

-- +goose Down
DROP TABLE expense_claim_line;
DROP TABLE expense_claim;

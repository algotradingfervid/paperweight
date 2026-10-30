-- Test-only mst.Order tables (M8): links to every master plus the columns
-- Hooks.Validate snapshots. Column types are INTEGER and TEXT only (CD-34).

-- +goose Up
CREATE TABLE mst_order (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    number            TEXT UNIQUE,
    state             TEXT NOT NULL DEFAULT 'draft' CHECK (state IN ('draft', 'pending_approval', 'approved', 'closed', 'rejected', 'cancelled')),
    state_reason      TEXT NOT NULL DEFAULT '',
    amended_from_id   INTEGER REFERENCES mst_order (id),
    amendment_root_id INTEGER,
    amendment_no      INTEGER NOT NULL DEFAULT 0 CHECK (amendment_no BETWEEN 0 AND 9),
    version           INTEGER NOT NULL DEFAULT 1,
    created_at        TEXT NOT NULL,
    created_by        INTEGER NOT NULL,
    updated_at        TEXT NOT NULL,
    updated_by        INTEGER NOT NULL,
    vendor_id         INTEGER REFERENCES party (id) ON DELETE RESTRICT,
    address_id        INTEGER REFERENCES address (id) ON DELETE RESTRICT,
    branch_id         INTEGER REFERENCES branch (id) ON DELETE RESTRICT,
    project_id        INTEGER REFERENCES project (id) ON DELETE RESTRICT,
    site_id           INTEGER REFERENCES site (id) ON DELETE RESTRICT,
    terms_id          INTEGER REFERENCES payment_terms (id) ON DELETE RESTRICT,
    employee_id       INTEGER REFERENCES employee (id) ON DELETE RESTRICT,
    order_date        TEXT,
    snap_vendor_name  TEXT NOT NULL DEFAULT '',
    snap_vendor_gstin TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX mst_order_one_amendment ON mst_order (amended_from_id) WHERE amended_from_id IS NOT NULL;

CREATE TABLE mst_order_line (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    parent_id       INTEGER NOT NULL REFERENCES mst_order (id) ON DELETE CASCADE,
    line_no         INTEGER NOT NULL,
    item_id         INTEGER REFERENCES item (id) ON DELETE RESTRICT,
    uom_id          INTEGER REFERENCES uom (id) ON DELETE RESTRICT,
    tax_code_id     INTEGER REFERENCES tax_code (id) ON DELETE RESTRICT,
    expense_type_id INTEGER REFERENCES expense_type (id) ON DELETE RESTRICT,
    hsn_sac         TEXT,
    qty             INTEGER,
    rate            INTEGER,
    snap_item_code  TEXT NOT NULL DEFAULT '',
    snap_factor     INTEGER NOT NULL DEFAULT 0,
    snap_stock_qty  INTEGER NOT NULL DEFAULT 0
);

-- +goose Down
DROP TABLE mst_order_line;
DROP TABLE mst_order;

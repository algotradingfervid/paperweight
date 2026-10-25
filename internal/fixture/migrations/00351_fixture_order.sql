-- Test-only fixture.Order (Purchase Order) tables. Column types are INTEGER
-- and TEXT only (CD-34).

-- +goose Up
CREATE TABLE fixture_order (
    id                INTEGER PRIMARY KEY,
    number            TEXT UNIQUE,
    state             TEXT NOT NULL DEFAULT 'draft' CHECK (state IN ('draft', 'pending_approval', 'approved', 'closed', 'rejected', 'cancelled')),
    state_reason      TEXT NOT NULL DEFAULT '',
    amended_from_id   INTEGER REFERENCES fixture_order (id),
    amendment_root_id INTEGER,
    amendment_no      INTEGER NOT NULL DEFAULT 0 CHECK (amendment_no BETWEEN 0 AND 9),
    version           INTEGER NOT NULL DEFAULT 1,
    created_at        TEXT NOT NULL,
    created_by        INTEGER NOT NULL,
    updated_at        TEXT NOT NULL,
    updated_by        INTEGER NOT NULL,
    supplier_id       INTEGER REFERENCES fixture_party (id) ON DELETE RESTRICT,
    project_id        INTEGER REFERENCES fixture_project (id) ON DELETE RESTRICT,
    order_date        TEXT,
    amount            INTEGER,
    remarks           TEXT NOT NULL DEFAULT '',
    credit_days       INTEGER,
    supplier_ref      TEXT NOT NULL DEFAULT '',
    priority          TEXT NOT NULL DEFAULT '',
    discount_pct      INTEGER
);
CREATE UNIQUE INDEX fixture_order_one_amendment ON fixture_order (amended_from_id) WHERE amended_from_id IS NOT NULL;

CREATE TABLE fixture_order_line (
    id        INTEGER PRIMARY KEY,
    parent_id INTEGER NOT NULL REFERENCES fixture_order (id) ON DELETE CASCADE,
    line_no   INTEGER NOT NULL,
    item_id      INTEGER REFERENCES fixture_item (id) ON DELETE RESTRICT,
    qty          INTEGER,
    rate         INTEGER,
    uom          TEXT NOT NULL DEFAULT '',
    description  TEXT NOT NULL DEFAULT '',
    received_qty INTEGER NOT NULL DEFAULT 0
);

-- +goose Down
DROP TABLE fixture_order_line;
DROP TABLE fixture_order;

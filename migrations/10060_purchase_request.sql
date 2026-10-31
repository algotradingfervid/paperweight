-- Purchase Request (PO spec §20.2): the document convention columns and one
-- column per Field. order_status and ordered_qty are System columns kept by
-- RefreshOrderStatus (§20.3). Column types are INTEGER and TEXT only (CD-34).

-- +goose Up
CREATE TABLE purchase_request (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    number            TEXT UNIQUE,
    state             TEXT NOT NULL DEFAULT 'draft' CHECK (state IN ('draft', 'pending_approval', 'approved', 'closed', 'rejected', 'cancelled')),
    state_reason      TEXT NOT NULL DEFAULT '',
    amended_from_id   INTEGER REFERENCES purchase_request (id),
    amendment_root_id INTEGER,
    amendment_no      INTEGER NOT NULL DEFAULT 0 CHECK (amendment_no BETWEEN 0 AND 9),
    version           INTEGER NOT NULL DEFAULT 1,
    created_at        TEXT NOT NULL,
    created_by        INTEGER NOT NULL,
    updated_at        TEXT NOT NULL,
    updated_by        INTEGER NOT NULL,
    site_id           INTEGER REFERENCES site (id) ON DELETE RESTRICT,
    project_id        INTEGER REFERENCES project (id) ON DELETE RESTRICT,
    request_date      TEXT,
    required_by       TEXT,
    purpose           TEXT NOT NULL DEFAULT '',
    order_status      TEXT NOT NULL DEFAULT 'not_ordered' CHECK (order_status IN ('not_ordered', 'part_ordered', 'ordered'))
);
CREATE UNIQUE INDEX purchase_request_one_amendment ON purchase_request (amended_from_id) WHERE amended_from_id IS NOT NULL;

CREATE TABLE purchase_request_line (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    parent_id   INTEGER NOT NULL REFERENCES purchase_request (id) ON DELETE CASCADE,
    line_no     INTEGER NOT NULL,
    item_id     INTEGER REFERENCES item (id) ON DELETE RESTRICT,
    description TEXT NOT NULL DEFAULT '',
    uom_id      INTEGER REFERENCES uom (id) ON DELETE RESTRICT,
    qty         INTEGER,
    note        TEXT NOT NULL DEFAULT '',
    ordered_qty INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX purchase_request_line_parent ON purchase_request_line (parent_id, line_no);

-- +goose Down
DROP TABLE purchase_request_line;
DROP TABLE purchase_request;

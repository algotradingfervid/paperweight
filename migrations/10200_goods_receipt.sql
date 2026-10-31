-- Goods Receipt (GRN spec §3): the document convention columns, one column
-- per Field, and the snapshot columns Validate writes from the PO (spec §3.1,
-- §3.2; CD-302, CD-303). Every id is AUTOINCREMENT (CD-156); lines have
-- parent_id ON DELETE CASCADE and line_no (CD-161). purchase_order_id,
-- vendor_id, uom_id and every snap_* column are nullable: create inserts
-- before Validate runs, and System fields are left out of the insert.
-- site_id, project_id and po_line_id are plain Int fields; the foreign keys
-- are the backstop, Validate owns their rules. Column types are INTEGER and
-- TEXT only (CD-34).

-- +goose Up
CREATE TABLE goods_receipt (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    number              TEXT UNIQUE,
    state               TEXT NOT NULL DEFAULT 'draft' CHECK (state IN ('draft', 'pending_approval', 'approved', 'closed', 'rejected', 'cancelled')),
    state_reason        TEXT NOT NULL DEFAULT '',
    amended_from_id     INTEGER REFERENCES goods_receipt (id),
    amendment_root_id   INTEGER,
    amendment_no        INTEGER NOT NULL DEFAULT 0 CHECK (amendment_no BETWEEN 0 AND 9),
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TEXT NOT NULL,
    created_by          INTEGER NOT NULL,
    updated_at          TEXT NOT NULL,
    updated_by          INTEGER NOT NULL,
    purchase_order_id   INTEGER REFERENCES purchase_order (id) ON DELETE RESTRICT,
    vendor_id           INTEGER REFERENCES party (id) ON DELETE RESTRICT,
    site_id             INTEGER REFERENCES site (id) ON DELETE RESTRICT,
    project_id          INTEGER REFERENCES project (id) ON DELETE RESTRICT,
    receipt_date        TEXT,
    challan_kind        TEXT NOT NULL DEFAULT '' CHECK (challan_kind IN ('', 'Delivery challan', 'Tax invoice')),
    challan_no          TEXT NOT NULL DEFAULT '',
    challan_date        TEXT,
    vehicle_no          TEXT NOT NULL DEFAULT '',
    transporter         TEXT NOT NULL DEFAULT '',
    eway_bill_no        TEXT NOT NULL DEFAULT '',
    remarks             TEXT NOT NULL DEFAULT '',
    rest_not_coming     INTEGER NOT NULL DEFAULT 0 CHECK (rest_not_coming IN (0, 1)),
    accepted_value      INTEGER,
    snap_vendor_name    TEXT,
    snap_vendor_gstin   TEXT,
    snap_vendor_address TEXT,
    snap_po_number      TEXT,
    snap_po_date        TEXT,
    snap_site_name      TEXT,
    snap_project_code   TEXT,
    snap_branch_id      INTEGER
);
CREATE UNIQUE INDEX goods_receipt_one_amendment ON goods_receipt (amended_from_id) WHERE amended_from_id IS NOT NULL;
CREATE INDEX goods_receipt_po ON goods_receipt (purchase_order_id);

CREATE TABLE goods_receipt_line (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    parent_id       INTEGER NOT NULL REFERENCES goods_receipt (id) ON DELETE CASCADE,
    line_no         INTEGER NOT NULL,
    po_line_id      INTEGER REFERENCES purchase_order_line (id) ON DELETE RESTRICT,
    uom_id          INTEGER REFERENCES uom (id) ON DELETE RESTRICT,
    received_qty    INTEGER,
    rejected_qty    INTEGER,
    accepted_qty    INTEGER,
    reject_reason   TEXT NOT NULL DEFAULT '',
    snap_item_code  TEXT,
    snap_item_name  TEXT,
    snap_hsn_sac    TEXT,
    snap_uom_code   TEXT,
    snap_rate       INTEGER,
    snap_is_service INTEGER CHECK (snap_is_service IN (0, 1))
);
CREATE INDEX goods_receipt_line_parent ON goods_receipt_line (parent_id, line_no);
CREATE INDEX goods_receipt_line_po_line ON goods_receipt_line (po_line_id);

-- +goose Down
DROP TABLE goods_receipt_line;
DROP TABLE goods_receipt;

-- Purchase Order (PO spec §3): the document convention columns, one column
-- per Field, and the snapshot columns Validate writes while draft (D-MST-03).
-- snap_vendor_id is the vendor the address was last defaulted for (CD-280 D7
-- (d)); copy_notes is a System column written by Copy (CD-280 D7 (a)).
-- copied_from_id and request_line_id are plain integers: their rules live in
-- Validate and the LinkSpec. Column types are INTEGER and TEXT only (CD-34).

-- +goose Up
CREATE TABLE purchase_order (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    number               TEXT UNIQUE,
    state                TEXT NOT NULL DEFAULT 'draft' CHECK (state IN ('draft', 'pending_approval', 'approved', 'closed', 'rejected', 'cancelled')),
    state_reason         TEXT NOT NULL DEFAULT '',
    amended_from_id      INTEGER REFERENCES purchase_order (id),
    amendment_root_id    INTEGER,
    amendment_no         INTEGER NOT NULL DEFAULT 0 CHECK (amendment_no BETWEEN 0 AND 9),
    version              INTEGER NOT NULL DEFAULT 1,
    created_at           TEXT NOT NULL,
    created_by           INTEGER NOT NULL,
    updated_at           TEXT NOT NULL,
    updated_by           INTEGER NOT NULL,
    vendor_id            INTEGER REFERENCES party (id) ON DELETE RESTRICT,
    vendor_address_id    INTEGER REFERENCES address (id) ON DELETE RESTRICT,
    site_id              INTEGER REFERENCES site (id) ON DELETE RESTRICT,
    branch_id            INTEGER REFERENCES branch (id) ON DELETE RESTRICT,
    project_id           INTEGER REFERENCES project (id) ON DELETE RESTRICT,
    order_date           TEXT,
    required_by          TEXT,
    payment_terms_id     INTEGER REFERENCES payment_terms (id) ON DELETE RESTRICT,
    vendor_quote_ref     TEXT NOT NULL DEFAULT '',
    terms                TEXT NOT NULL DEFAULT '',
    reverse_charge       INTEGER NOT NULL DEFAULT 0 CHECK (reverse_charge IN (0, 1)),
    remarks              TEXT NOT NULL DEFAULT '',
    earlier_po_no        TEXT NOT NULL DEFAULT '',
    earlier_po_date      TEXT,
    copied_from_no       TEXT NOT NULL DEFAULT '',
    copied_from_id       INTEGER,
    copy_notes           TEXT NOT NULL DEFAULT '',
    net_total            INTEGER NOT NULL DEFAULT 0,
    cgst_total           INTEGER NOT NULL DEFAULT 0,
    sgst_total           INTEGER NOT NULL DEFAULT 0,
    igst_total           INTEGER NOT NULL DEFAULT 0,
    grand_total          INTEGER NOT NULL DEFAULT 0,
    rounded_total        INTEGER NOT NULL DEFAULT 0,
    receipt_status       TEXT NOT NULL DEFAULT 'not_received' CHECK (receipt_status IN ('not_received', 'part_received', 'fully_received')),
    snap_vendor_id       INTEGER,
    snap_vendor_code     TEXT NOT NULL DEFAULT '',
    snap_vendor_name     TEXT NOT NULL DEFAULT '',
    snap_vendor_gstin    TEXT NOT NULL DEFAULT '',
    snap_vendor_category TEXT NOT NULL DEFAULT '',
    snap_vendor_state    TEXT NOT NULL DEFAULT '',
    snap_vendor_address  TEXT NOT NULL DEFAULT '',
    snap_branch_name     TEXT NOT NULL DEFAULT '',
    snap_branch_gstin    TEXT NOT NULL DEFAULT '',
    snap_place_state     TEXT NOT NULL DEFAULT '',
    snap_site_name       TEXT NOT NULL DEFAULT '',
    snap_terms_name      TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX purchase_order_one_amendment ON purchase_order (amended_from_id) WHERE amended_from_id IS NOT NULL;

CREATE TABLE purchase_order_line (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    parent_id        INTEGER NOT NULL REFERENCES purchase_order (id) ON DELETE CASCADE,
    line_no          INTEGER NOT NULL,
    item_id          INTEGER REFERENCES item (id) ON DELETE RESTRICT,
    description      TEXT NOT NULL DEFAULT '',
    uom_id           INTEGER REFERENCES uom (id) ON DELETE RESTRICT,
    qty              INTEGER,
    price            INTEGER,
    discount_pct     INTEGER,
    rate             INTEGER,
    tax_code_id      INTEGER REFERENCES tax_code (id) ON DELETE RESTRICT,
    hsn_sac          TEXT NOT NULL DEFAULT '',
    amount           INTEGER,
    cgst             INTEGER,
    sgst             INTEGER,
    igst             INTEGER,
    already_received INTEGER,
    received_qty     INTEGER NOT NULL DEFAULT 0,
    request_line_id  INTEGER,
    snap_item_code   TEXT NOT NULL DEFAULT '',
    snap_item_name   TEXT NOT NULL DEFAULT '',
    snap_uom_code    TEXT NOT NULL DEFAULT '',
    snap_factor      INTEGER NOT NULL DEFAULT 0,
    snap_stock_qty   INTEGER NOT NULL DEFAULT 0,
    snap_gst_rate    INTEGER NOT NULL DEFAULT 0,
    snap_tax_code    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX purchase_order_line_parent ON purchase_order_line (parent_id, line_no);
CREATE INDEX purchase_order_line_request ON purchase_order_line (request_line_id) WHERE request_line_id IS NOT NULL;

-- +goose Down
DROP TABLE purchase_order_line;
DROP TABLE purchase_order;

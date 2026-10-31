-- The rest won't come (PO spec §27.2): one row per PO and GRN whose tick said
-- so, written by SetRestNotComing (CD-276). grn_id has no FK, because
-- purchase never depends on goodsreceipt (spec §14.5).

-- +goose Up
CREATE TABLE purchase_order_rest (
    po_id      INTEGER NOT NULL REFERENCES purchase_order (id) ON DELETE CASCADE,
    grn_id     INTEGER NOT NULL,
    created_by INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (po_id, grn_id)
);

-- +goose Down
DROP TABLE purchase_order_rest;

-- The Expected on record (PO spec §27.1): at most one row per PO, outside the
-- PO so R7 holds. It has its own version; 0 means no row. expected_on is a
-- types.Date (ISO text). The note length is checked in the service with a
-- message; the CHECK is the backstop.

-- +goose Up
CREATE TABLE purchase_order_expected (
    po_id       INTEGER PRIMARY KEY REFERENCES purchase_order (id) ON DELETE CASCADE,
    expected_on TEXT NOT NULL,
    note        TEXT NOT NULL DEFAULT '' CHECK (length(note) <= 200),
    set_by      INTEGER NOT NULL,
    set_at      TEXT NOT NULL,
    version     INTEGER NOT NULL CHECK (version >= 1)
);

-- +goose Down
DROP TABLE purchase_order_expected;

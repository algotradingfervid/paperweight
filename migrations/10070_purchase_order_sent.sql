-- The Sent record (PO spec §23): at most one row per PO, outside the PO so an
-- approved PO doesn't change (R7). It has its own version; 0 means no row.
-- sent_on is a types.Date (ISO text). The note length is checked in the
-- service with a message; the CHECK is the backstop.

-- +goose Up
CREATE TABLE purchase_order_sent (
    po_id    INTEGER PRIMARY KEY REFERENCES purchase_order (id) ON DELETE CASCADE,
    sent_on  TEXT NOT NULL,
    sent_via TEXT NOT NULL CHECK (sent_via IN ('email', 'whatsapp', 'courier', 'by_hand', 'other')),
    note     TEXT NOT NULL DEFAULT '' CHECK (length(note) <= 200),
    sent_by  INTEGER NOT NULL,
    sent_at  TEXT NOT NULL,
    version  INTEGER NOT NULL CHECK (version >= 1)
);

-- +goose Down
DROP TABLE purchase_order_sent;

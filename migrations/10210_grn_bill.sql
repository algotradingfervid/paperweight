-- Bill details (GRN spec §3.3, CD-234): at most one row per GRN, outside the
-- GRN so an approved GRN doesn't change. It has its own version; 0 means no
-- row (CD-247 (6)). invoice_date is a types.Date (ISO text) and
-- invoice_amount Money (paise), optional. The row lives on after cancel, so a
-- GRN with bill details is never deleted.

-- +goose Up
CREATE TABLE grn_bill (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    grn_id         INTEGER NOT NULL UNIQUE REFERENCES goods_receipt (id) ON DELETE RESTRICT,
    invoice_no     TEXT NOT NULL,
    invoice_date   TEXT NOT NULL,
    invoice_amount INTEGER,
    version        INTEGER NOT NULL CHECK (version >= 1),
    created_at     TEXT NOT NULL,
    created_by     INTEGER NOT NULL,
    updated_at     TEXT NOT NULL,
    updated_by     INTEGER NOT NULL
);

-- +goose Down
DROP TABLE grn_bill;

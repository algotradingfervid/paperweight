-- Parties (MST Task 5): one table for vendors and customers (D-MST-04). GSTINs
-- live on the party's addresses (D-MST-06); PAN and category are back-filled
-- from them (MST-049).
-- +goose Up
CREATE TABLE party (
    id                       INTEGER PRIMARY KEY AUTOINCREMENT,
    code                     TEXT    NOT NULL UNIQUE CHECK (code = upper(code)),
    name                     TEXT    NOT NULL,
    is_vendor                INTEGER NOT NULL DEFAULT 0 CHECK (is_vendor IN (0, 1)),
    is_customer              INTEGER NOT NULL DEFAULT 0 CHECK (is_customer IN (0, 1)),
    pan                      TEXT    NOT NULL DEFAULT '',
    gst_category             TEXT    NOT NULL DEFAULT 'Unregistered' CHECK (gst_category IN
        ('Registered Regular', 'Registered Composition', 'Unregistered', 'SEZ', 'Overseas')),
    default_payment_terms_id INTEGER REFERENCES payment_terms (id) ON DELETE RESTRICT,
    disabled                 INTEGER NOT NULL DEFAULT 0 CHECK (disabled IN (0, 1)),
    version                  INTEGER NOT NULL DEFAULT 1,
    created_at               TEXT    NOT NULL,
    created_by               INTEGER,
    updated_at               TEXT    NOT NULL,
    updated_by               INTEGER,
    CHECK (is_vendor = 1 OR is_customer = 1)
);

-- +goose Down
DROP TABLE party;

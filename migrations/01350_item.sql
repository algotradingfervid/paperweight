-- Items (MST Task 10): a code master, Goods or Service, with its HSN/SAC
-- code, stock and purchase UoMs, default tax code and optional standard rate
-- (types.Rate, CD-37; 0 = none). item_uom is the item's LinesTable (CD-16,
-- CD-161): one row per UoM, factor = stock units per 1 of that UoM at 1e-9
-- scale (types.Factor); the stock UoM's row has factor 1 (MST-079, MST-080).
-- +goose Up
CREATE TABLE item (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    code                TEXT    NOT NULL UNIQUE CHECK (code = upper(code)),
    name                TEXT    NOT NULL,
    item_type           TEXT    NOT NULL CHECK (item_type IN ('Goods', 'Service')),
    hsn_sac             TEXT    NOT NULL DEFAULT '',
    stock_uom_id        INTEGER NOT NULL REFERENCES uom (id) ON DELETE RESTRICT,
    purchase_uom_id     INTEGER NOT NULL REFERENCES uom (id) ON DELETE RESTRICT,
    default_tax_code_id INTEGER REFERENCES tax_code (id) ON DELETE RESTRICT,
    is_stock            INTEGER NOT NULL DEFAULT 0 CHECK (is_stock IN (0, 1)),
    standard_rate       INTEGER NOT NULL DEFAULT 0,
    disabled            INTEGER NOT NULL DEFAULT 0 CHECK (disabled IN (0, 1)),
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TEXT    NOT NULL,
    created_by          INTEGER,
    updated_at          TEXT    NOT NULL,
    updated_by          INTEGER
);

CREATE TABLE item_uom (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    parent_id INTEGER NOT NULL REFERENCES item (id) ON DELETE CASCADE,
    line_no   INTEGER NOT NULL,
    uom_id    INTEGER NOT NULL REFERENCES uom (id) ON DELETE RESTRICT,
    factor    INTEGER NOT NULL CHECK (factor > 0),
    UNIQUE (parent_id, uom_id)
);

-- +goose Down
DROP TABLE item_uom;
DROP TABLE item;

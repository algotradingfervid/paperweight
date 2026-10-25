-- Company (a singleton, MST-019) and its branches (MST-021…MST-027).
-- +goose Up
CREATE TABLE company (
    id         INTEGER PRIMARY KEY AUTOINCREMENT CHECK (id = 1),
    name       TEXT    NOT NULL,
    pan        TEXT    NOT NULL,
    disabled   INTEGER NOT NULL DEFAULT 0 CHECK (disabled IN (0, 1)),
    version    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT    NOT NULL,
    created_by INTEGER,
    updated_at TEXT    NOT NULL,
    updated_by INTEGER
) STRICT;

CREATE TABLE branch (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    code           TEXT    NOT NULL UNIQUE CHECK (code = upper(code)),
    name           TEXT    NOT NULL,
    state_code     TEXT    NOT NULL REFERENCES gst_state(code) ON DELETE RESTRICT,
    gstin          TEXT    NOT NULL DEFAULT '',
    is_head_office INTEGER NOT NULL DEFAULT 0 CHECK (is_head_office IN (0, 1)),
    disabled       INTEGER NOT NULL DEFAULT 0 CHECK (disabled IN (0, 1)),
    version        INTEGER NOT NULL DEFAULT 1,
    created_at     TEXT    NOT NULL,
    created_by     INTEGER,
    updated_at     TEXT    NOT NULL,
    updated_by     INTEGER
) STRICT;
CREATE UNIQUE INDEX branch_one_head_office ON branch(is_head_office) WHERE is_head_office = 1;

-- +goose Down
DROP TABLE branch;
DROP TABLE company;

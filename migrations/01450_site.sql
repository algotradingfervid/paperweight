-- Sites and warehouses (MST-105…MST-108).
-- +goose Up
CREATE TABLE site (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    code       TEXT    NOT NULL UNIQUE CHECK (code = upper(code)),
    name       TEXT    NOT NULL,
    branch_id  INTEGER NOT NULL REFERENCES branch(id) ON DELETE RESTRICT,
    project_id INTEGER REFERENCES project(id) ON DELETE RESTRICT,
    type       TEXT    NOT NULL CHECK (type IN ('warehouse', 'site')),
    disabled   INTEGER NOT NULL DEFAULT 0 CHECK (disabled IN (0, 1)),
    version    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT    NOT NULL,
    created_by INTEGER,
    updated_at TEXT    NOT NULL,
    updated_by INTEGER
) STRICT;

-- +goose Down
DROP TABLE site;

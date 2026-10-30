-- Projects (MST-100…MST-104).
-- +goose Up
CREATE TABLE project (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    code        TEXT    NOT NULL UNIQUE CHECK (code = upper(code)),
    name        TEXT    NOT NULL,
    branch_id   INTEGER NOT NULL REFERENCES branch(id) ON DELETE RESTRICT,
    customer_id INTEGER REFERENCES party(id) ON DELETE RESTRICT,
    start_date  TEXT,
    end_date    TEXT,
    status      TEXT    NOT NULL DEFAULT 'Open' CHECK (status IN ('Open', 'On Hold', 'Completed', 'Cancelled')),
    disabled    INTEGER NOT NULL DEFAULT 0 CHECK (disabled IN (0, 1)),
    version     INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT    NOT NULL,
    created_by  INTEGER,
    updated_at  TEXT    NOT NULL,
    updated_by  INTEGER
) STRICT;

-- +goose Down
DROP TABLE project;

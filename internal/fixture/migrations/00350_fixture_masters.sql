-- Test-only stand-in master DocTypes fixture.Party, fixture.Item and
-- fixture.Project (CD-21). Column types are INTEGER and TEXT only (CD-34).

-- +goose Up
CREATE TABLE fixture_party (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    code       TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    disabled   INTEGER NOT NULL DEFAULT 0,
    blocked    TEXT NOT NULL DEFAULT '',
    warning    TEXT NOT NULL DEFAULT '',
    version    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    created_by INTEGER NOT NULL,
    updated_at TEXT NOT NULL,
    updated_by INTEGER NOT NULL
);

CREATE TABLE fixture_item (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    code       TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    disabled   INTEGER NOT NULL DEFAULT 0,
    blocked    TEXT NOT NULL DEFAULT '',
    warning    TEXT NOT NULL DEFAULT '',
    version    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    created_by INTEGER NOT NULL,
    updated_at TEXT NOT NULL,
    updated_by INTEGER NOT NULL
);

CREATE TABLE fixture_project (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    code       TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    disabled   INTEGER NOT NULL DEFAULT 0,
    blocked    TEXT NOT NULL DEFAULT '',
    warning    TEXT NOT NULL DEFAULT '',
    version    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    created_by INTEGER NOT NULL,
    updated_at TEXT NOT NULL,
    updated_by INTEGER NOT NULL
);

-- +goose Down
DROP TABLE fixture_project;
DROP TABLE fixture_item;
DROP TABLE fixture_party;

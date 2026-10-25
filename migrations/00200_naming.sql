-- +goose Up
CREATE TABLE naming_series (
    id         INTEGER PRIMARY KEY,
    doctype    TEXT    NOT NULL UNIQUE,
    label      TEXT    NOT NULL,
    template   TEXT    NOT NULL,
    version    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT    NOT NULL,
    created_by INTEGER,
    updated_at TEXT    NOT NULL,
    updated_by INTEGER
);

CREATE TABLE naming_counters (
    id         INTEGER PRIMARY KEY,
    doctype    TEXT    NOT NULL,
    prefix     TEXT    NOT NULL,
    last       INTEGER NOT NULL CHECK (last >= 0),
    version    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT    NOT NULL,
    created_by INTEGER,
    updated_at TEXT    NOT NULL,
    updated_by INTEGER,
    UNIQUE (doctype, prefix)
);

CREATE TABLE naming_issued (
    id           INTEGER PRIMARY KEY,
    doctype      TEXT    NOT NULL,
    doc_id       INTEGER NOT NULL,
    number       TEXT    NOT NULL UNIQUE,
    prefix       TEXT    NOT NULL,
    seq          INTEGER NOT NULL,
    root         TEXT    NOT NULL,
    amendment_no INTEGER NOT NULL DEFAULT 0 CHECK (amendment_no BETWEEN 0 AND 9),
    issued_at    TEXT    NOT NULL,
    UNIQUE (doctype, doc_id),
    UNIQUE (root, amendment_no)
);

-- +goose StatementBegin
CREATE TRIGGER naming_issued_no_update BEFORE UPDATE ON naming_issued
BEGIN
    SELECT RAISE(ABORT, 'naming_issued is append-only');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER naming_issued_no_delete BEFORE DELETE ON naming_issued
BEGIN
    SELECT RAISE(ABORT, 'naming_issued is append-only');
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER naming_issued_no_delete;
DROP TRIGGER naming_issued_no_update;
DROP TABLE naming_issued;
DROP TABLE naming_counters;
DROP TABLE naming_series;

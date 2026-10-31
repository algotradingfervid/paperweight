-- Comments on module documents (PO spec §22): append-only, so the triggers
-- refuse UPDATE and DELETE. The body length is checked in the service with a
-- message; the CHECK is the backstop.
-- +goose Up
CREATE TABLE module_comment (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    doctype    TEXT    NOT NULL,
    record_id  INTEGER NOT NULL,
    body       TEXT    NOT NULL CHECK (length(body) BETWEEN 1 AND 1000),
    created_by INTEGER NOT NULL,
    created_at TEXT    NOT NULL
) STRICT;
CREATE INDEX module_comment_record ON module_comment (doctype, record_id, id);

-- +goose StatementBegin
CREATE TRIGGER module_comment_no_update BEFORE UPDATE ON module_comment
BEGIN
    SELECT RAISE(ABORT, 'comments are append-only');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER module_comment_no_delete BEFORE DELETE ON module_comment
BEGIN
    SELECT RAISE(ABORT, 'comments are append-only');
END;
-- +goose StatementEnd

-- +goose Down
DROP TABLE module_comment;

-- +goose Up
CREATE TABLE audit_log (
    id             INTEGER PRIMARY KEY,
    doc_type       TEXT NOT NULL,
    doc_id         INTEGER NOT NULL,
    action         TEXT NOT NULL,
    from_state     TEXT NOT NULL DEFAULT '',
    to_state       TEXT NOT NULL DEFAULT '',
    reason         TEXT NOT NULL DEFAULT '',
    changes        TEXT NOT NULL DEFAULT '[]',
    user_id        INTEGER NOT NULL,
    at             TEXT NOT NULL,
    cause_doc_type TEXT,
    cause_doc_id   INTEGER,
    cause_number   TEXT,
    cause_action   TEXT
);
CREATE INDEX audit_log_doc ON audit_log (doc_type, doc_id, id);

-- +goose StatementBegin
CREATE TRIGGER audit_log_no_update BEFORE UPDATE ON audit_log
BEGIN
    SELECT RAISE(ABORT, 'audit log is append-only');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER audit_log_no_delete BEFORE DELETE ON audit_log
BEGIN
    SELECT RAISE(ABORT, 'audit log is append-only');
END;
-- +goose StatementEnd

-- +goose Down
DROP TABLE audit_log;

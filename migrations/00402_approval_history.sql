-- +goose Up
CREATE TABLE approval_history (
    id            INTEGER PRIMARY KEY,
    doctype       TEXT    NOT NULL,
    doc_id        INTEGER NOT NULL,
    request_id    INTEGER REFERENCES approval_requests (id),
    action        TEXT    NOT NULL CHECK (action IN ('submitted', 'approved', 'approved_no_rule', 'rejected', 'revised', 'withdrawn')),
    user_id       INTEGER NOT NULL,
    user_name     TEXT    NOT NULL,
    at            TEXT    NOT NULL,
    amount        INTEGER,
    rule_id       INTEGER,
    reason        TEXT    NOT NULL DEFAULT '',
    self_approved INTEGER NOT NULL DEFAULT 0
);

-- +goose StatementBegin
CREATE TRIGGER approval_history_no_update BEFORE UPDATE ON approval_history
BEGIN
    SELECT RAISE(ABORT, 'approval history is append-only');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER approval_history_no_delete BEFORE DELETE ON approval_history
BEGIN
    SELECT RAISE(ABORT, 'approval history is append-only');
END;
-- +goose StatementEnd

-- +goose Down
DROP TABLE approval_history;

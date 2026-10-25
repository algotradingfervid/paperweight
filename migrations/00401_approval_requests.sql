-- +goose Up
CREATE TABLE approval_requests (
    id              INTEGER PRIMARY KEY,
    doctype         TEXT    NOT NULL,
    doc_id          INTEGER NOT NULL,
    rule_id         INTEGER NOT NULL REFERENCES approval_rules (id) ON DELETE RESTRICT,
    amount          INTEGER NOT NULL,
    approver_roles  TEXT    NOT NULL,
    manager_user    INTEGER,
    manager_name    TEXT    NOT NULL DEFAULT '',
    allow_self      INTEGER NOT NULL,
    requester       INTEGER NOT NULL,
    requester_name  TEXT    NOT NULL,
    creator         INTEGER NOT NULL,
    claimant_user   INTEGER,
    submitted_at    TEXT    NOT NULL,
    outcome         TEXT    CHECK (outcome IN ('approved', 'rejected', 'withdrawn')),
    decided_by      INTEGER,
    decided_by_name TEXT    NOT NULL DEFAULT '',
    decided_at      TEXT,
    reason          TEXT    NOT NULL DEFAULT '',
    version         INTEGER NOT NULL DEFAULT 1,
    created_at      TEXT    NOT NULL,
    created_by      INTEGER NOT NULL,
    updated_at      TEXT    NOT NULL,
    updated_by      INTEGER NOT NULL
);
CREATE UNIQUE INDEX approval_requests_open_uq ON approval_requests (doctype, doc_id) WHERE outcome IS NULL;
CREATE INDEX approval_requests_outcome_idx ON approval_requests (outcome, submitted_at, id);

-- +goose Down
DROP TABLE approval_requests;

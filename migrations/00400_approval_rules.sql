-- +goose Up
CREATE TABLE approval_rules (
    id                  INTEGER PRIMARY KEY,
    doctype             TEXT    NOT NULL,
    from_amount         INTEGER NOT NULL CHECK (from_amount >= 0),
    approver_type       TEXT    NOT NULL CHECK (approver_type IN ('role', 'reporting_manager')),
    approver_role       TEXT    NOT NULL DEFAULT '',
    fallback_role       TEXT    NOT NULL DEFAULT '',
    allow_self_approval INTEGER NOT NULL DEFAULT 0,
    enabled             INTEGER NOT NULL DEFAULT 1,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TEXT    NOT NULL,
    created_by          INTEGER NOT NULL,
    updated_at          TEXT    NOT NULL,
    updated_by          INTEGER NOT NULL
);
CREATE UNIQUE INDEX approval_rules_enabled_uq ON approval_rules (doctype, from_amount) WHERE enabled = 1;

-- +goose Down
DROP TABLE approval_rules;

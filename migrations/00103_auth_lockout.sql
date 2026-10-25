-- +goose Up
CREATE TABLE login_attempts (
    key              TEXT PRIMARY KEY, -- 'acct:' + hex(SHA-256(email)) or 'ip:' + IP
    count            INTEGER NOT NULL,
    first_failure_at TEXT NOT NULL,
    expires_at       TEXT NOT NULL
);

CREATE TABLE auth_log (
    id         INTEGER PRIMARY KEY,
    at         TEXT NOT NULL,
    event      TEXT NOT NULL CHECK (event IN ('login_success', 'login_failed', 'logout')),
    user_id    INTEGER NULL REFERENCES users (id) ON DELETE SET NULL,
    email_hash TEXT NULL,
    ip         TEXT NOT NULL,
    user_agent TEXT NOT NULL
);

-- +goose Down
DROP TABLE auth_log;
DROP TABLE login_attempts;

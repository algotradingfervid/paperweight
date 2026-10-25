-- +goose Up
CREATE TABLE users (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    email                TEXT NOT NULL UNIQUE CHECK (email = lower(trim(email))),
    name                 TEXT NOT NULL,
    mobile               TEXT NOT NULL DEFAULT '',
    password_hash        TEXT NULL,
    must_change_password INTEGER NOT NULL DEFAULT 0,
    enabled              INTEGER NOT NULL DEFAULT 1,
    last_login_at        TEXT NULL,
    version              INTEGER NOT NULL DEFAULT 1,
    created_at           TEXT NOT NULL,
    created_by           INTEGER NULL REFERENCES users (id), -- NULL = system
    updated_at           TEXT NOT NULL,
    updated_by           INTEGER NULL REFERENCES users (id)
);

CREATE TABLE roles (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL,
    name_key   TEXT NOT NULL UNIQUE, -- lower(name)
    is_admin   INTEGER NOT NULL DEFAULT 0,
    version    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    created_by INTEGER NULL REFERENCES users (id),
    updated_at TEXT NOT NULL,
    updated_by INTEGER NULL REFERENCES users (id)
);

INSERT INTO roles (name, name_key, is_admin, created_at, updated_at)
SELECT name, lower(name), is_admin,
       strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM (
    SELECT 1 AS ord, 'Administrator' AS name, 1 AS is_admin
    UNION ALL SELECT 2, 'Director', 0
    UNION ALL SELECT 3, 'Purchase Manager', 0
    UNION ALL SELECT 4, 'Purchase User', 0
    UNION ALL SELECT 5, 'Stores', 0
    UNION ALL SELECT 6, 'Masters Manager', 0
    UNION ALL SELECT 7, 'Employee', 0
)
ORDER BY ord;

CREATE TABLE user_roles (
    user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role_id INTEGER NOT NULL REFERENCES roles (id) ON DELETE RESTRICT,
    PRIMARY KEY (user_id, role_id)
);

-- +goose Down
DROP TABLE user_roles;
DROP TABLE roles;
DROP TABLE users;

-- Test-only stand-ins for the Wave 1 masters (WP0 Task 6). WP2 replaces them
-- with the real masters. Column types are INTEGER and TEXT only (CD-34).

-- +goose Up
CREATE TABLE testkit_employee (
    id         INTEGER PRIMARY KEY,
    code       TEXT NOT NULL UNIQUE,
    user_id    INTEGER,
    manager_id INTEGER REFERENCES testkit_employee (id),
    status     TEXT NOT NULL
);

CREATE TABLE testkit_project (
    id      INTEGER PRIMARY KEY,
    code    TEXT NOT NULL UNIQUE,
    name    TEXT NOT NULL,
    enabled INTEGER NOT NULL
);

CREATE TABLE testkit_site (
    id          INTEGER PRIMARY KEY,
    code        TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    branch_code TEXT NOT NULL,
    project_id  INTEGER REFERENCES testkit_project (id),
    enabled     INTEGER NOT NULL
);

-- +goose Down
DROP TABLE testkit_site;
DROP TABLE testkit_project;
DROP TABLE testkit_employee;

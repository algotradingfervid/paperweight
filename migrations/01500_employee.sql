-- Employees and the reporting chain (MST-109…MST-120). disabled mirrors
-- status = 'Left' and is set in the same write.
-- +goose Up
CREATE TABLE employee (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    code           TEXT    NOT NULL UNIQUE CHECK (code = upper(code)),
    name           TEXT    NOT NULL,
    branch_id      INTEGER NOT NULL REFERENCES branch(id) ON DELETE RESTRICT,
    user_id        INTEGER REFERENCES users(id) ON DELETE RESTRICT,
    manager_id     INTEGER REFERENCES employee(id) ON DELETE RESTRICT,
    joining_date   TEXT    NOT NULL,
    birth_date     TEXT,
    relieving_date TEXT,
    status         TEXT    NOT NULL DEFAULT 'Active' CHECK (status IN ('Active', 'Left')),
    disabled       INTEGER NOT NULL DEFAULT 0 CHECK (disabled IN (0, 1)),
    -- users.version right after this employee's leave disabled an enabled user;
    -- re-joining re-enables that user only while it still matches (CD-189).
    user_version_on_leave INTEGER,
    version        INTEGER NOT NULL DEFAULT 1,
    created_at     TEXT    NOT NULL,
    created_by     INTEGER,
    updated_at     TEXT    NOT NULL,
    updated_by     INTEGER,
    CHECK (disabled = (status = 'Left')) -- disabled mirrors status
) STRICT;

-- A user links to at most one active employee (CD-39, MST-111).
CREATE UNIQUE INDEX employee_active_user ON employee (user_id) WHERE status = 'Active' AND user_id IS NOT NULL;

-- +goose Down
DROP TABLE employee;

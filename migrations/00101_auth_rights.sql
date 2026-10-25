-- +goose Up
CREATE TABLE role_doctypes (
    role_id    INTEGER NOT NULL REFERENCES roles (id) ON DELETE CASCADE,
    doctype    TEXT NOT NULL,
    customised INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (role_id, doctype)
);

CREATE TABLE role_rights (
    role_id    INTEGER NOT NULL,
    doctype    TEXT NOT NULL,
    right_name TEXT NOT NULL CHECK (right_name IN (
        'read', 'create', 'write', 'submit', 'cancel', 'amend',
        'delete', 'approve', 'print', 'export', 'import')),
    scope      TEXT NOT NULL CHECK (scope IN ('own', 'team', 'all')),
    PRIMARY KEY (role_id, doctype, right_name),
    FOREIGN KEY (role_id, doctype) REFERENCES role_doctypes (role_id, doctype) ON DELETE CASCADE
);

-- No FK to the Project and Site masters: they are MST's tables (CD-13).
CREATE TABLE user_restrictions (
    user_id   INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    dimension TEXT NOT NULL CHECK (dimension IN ('project', 'site')),
    value_id  INTEGER NOT NULL,
    PRIMARY KEY (user_id, dimension, value_id)
);

CREATE TABLE auth_meta (
    id           INTEGER PRIMARY KEY CHECK (id = 1),
    perm_version INTEGER NOT NULL
);
INSERT INTO auth_meta (id, perm_version) VALUES (1, 1);

-- +goose Down
DROP TABLE auth_meta;
DROP TABLE user_restrictions;
DROP TABLE role_rights;
DROP TABLE role_doctypes;

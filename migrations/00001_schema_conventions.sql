-- +goose Up
CREATE TABLE app_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
INSERT INTO app_meta (key, value) VALUES ('schema_conventions', '1');

-- +goose Down
DROP TABLE app_meta;

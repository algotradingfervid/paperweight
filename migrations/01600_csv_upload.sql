-- Files waiting between the Upload and Import steps of the master import
-- wizard (CD-201): only the token goes in the session, because sessions load
-- on every request and csvio allows 5 MB. claimed_at is set while an Import
-- runs, so two posts can't import one file twice.
-- +goose Up
CREATE TABLE csv_upload (
    token      TEXT    PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at TEXT    NOT NULL,
    claimed_at TEXT    NULL,
    data       BLOB    NOT NULL
);
CREATE INDEX csv_upload_user_id ON csv_upload (user_id);

-- +goose Down
DROP TABLE csv_upload;

-- +goose Up
CREATE TABLE attachment (
  id             INTEGER PRIMARY KEY,
  parent_doctype TEXT    NOT NULL,
  parent_id      INTEGER NOT NULL,
  file_name      TEXT    NOT NULL CHECK (length(file_name) BETWEEN 1 AND 480),
  size           INTEGER NOT NULL CHECK (size > 0),
  content_type   TEXT    NOT NULL,
  sha256         TEXT    NOT NULL CHECK (length(sha256) = 64),
  uploaded_by    INTEGER NOT NULL,
  uploaded_at    TEXT    NOT NULL
) STRICT;
CREATE INDEX attachment_parent ON attachment(parent_doctype, parent_id, uploaded_at DESC);
CREATE INDEX attachment_sha    ON attachment(sha256);
-- +goose Down
DROP TABLE attachment;

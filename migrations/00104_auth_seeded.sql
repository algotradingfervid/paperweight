-- +goose Up
-- seeded is 1 once SeedDefaults has created the canonical roles; after that it
-- never re-creates a role an administrator deleted (WP1-AUTH Task 17).
ALTER TABLE auth_meta ADD COLUMN seeded INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE auth_meta DROP COLUMN seeded;

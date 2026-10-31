-- sqlc refuses a package with no queries. This one keeps modkit's dbq
-- generating until settings.sql (M0-7) and comments.sql (M0-14) exist;
-- delete it then.

-- name: KitPlaceholder :one
SELECT count(*) FROM business_settings;

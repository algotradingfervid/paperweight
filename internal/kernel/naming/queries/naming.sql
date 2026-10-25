-- name: GetSeries :one
SELECT * FROM naming_series WHERE doctype = ?;

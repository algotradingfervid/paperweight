-- name: GetSeries :one
SELECT * FROM naming_series WHERE doctype = ?;

-- name: NextCounter :one
INSERT INTO naming_counters(doctype, prefix, last, created_at, updated_at) VALUES (?, ?, 1, ?, ?)
ON CONFLICT(doctype, prefix) DO UPDATE SET last = last + 1, version = version + 1, updated_at = excluded.updated_at
RETURNING last;

-- name: GetCounterLast :one
SELECT last FROM naming_counters WHERE doctype = ? AND prefix = ?;

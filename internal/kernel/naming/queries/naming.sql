-- name: GetSeries :one
SELECT * FROM naming_series WHERE doctype = ?;

-- name: NextCounter :one
INSERT INTO naming_counters(doctype, prefix, last, created_at, updated_at) VALUES (?, ?, 1, ?, ?)
ON CONFLICT(doctype, prefix) DO UPDATE SET last = last + 1, version = version + 1, updated_at = excluded.updated_at
RETURNING last;

-- name: GetCounterLast :one
SELECT last FROM naming_counters WHERE doctype = ? AND prefix = ?;

-- name: InsertSeriesIfMissing :exec
INSERT INTO naming_series(doctype, label, template, created_at, updated_at) VALUES (?, ?, ?, ?, ?)
ON CONFLICT(doctype) DO NOTHING;

-- name: InsertIssued :exec
INSERT INTO naming_issued(doctype, doc_id, number, prefix, seq, root, amendment_no, issued_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: CountIssuedForDoc :one
SELECT COUNT(*) FROM naming_issued WHERE doctype = ? AND doc_id = ?;

-- name: ListSeries :many
SELECT * FROM naming_series ORDER BY label;

-- name: ListCounters :many
SELECT * FROM naming_counters ORDER BY doctype, prefix;

-- name: UpdateSeriesTemplate :exec
UPDATE naming_series SET template = ?, version = version + 1, updated_at = ?, updated_by = ? WHERE id = ?;

-- name: HighestIssued :one
SELECT CAST(COALESCE(MAX(seq), 0) AS INTEGER) FROM naming_issued
WHERE doctype = ? AND prefix = ? AND amendment_no = 0;

-- name: SetCounterLast :one
INSERT INTO naming_counters(doctype, prefix, last, created_at, created_by, updated_at, updated_by)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(doctype, prefix) DO UPDATE SET last = excluded.last, version = version + 1,
    updated_at = excluded.updated_at, updated_by = excluded.updated_by
RETURNING id;

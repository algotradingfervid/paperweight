-- name: RequestForValidate :one
-- The request's Validate: the header fields its rules read (P5). Blank links
-- and dates read 0 and ''.
SELECT state,
       CAST(COALESCE(site_id, 0) AS INTEGER) AS site_id,
       CAST(COALESCE(project_id, 0) AS INTEGER) AS project_id,
       CAST(COALESCE(request_date, '') AS TEXT) AS request_date,
       CAST(COALESCE(required_by, '') AS TEXT) AS required_by
FROM purchase_request
WHERE id = ?;

-- name: RequestLinesForValidate :many
-- The request's Validate: each line as stored (P5). Blank links read 0.
SELECT id, line_no,
       CAST(COALESCE(item_id, 0) AS INTEGER) AS item_id,
       CAST(COALESCE(uom_id, 0) AS INTEGER) AS uom_id,
       CAST(COALESCE(qty, 0) AS INTEGER) AS qty,
       description
FROM purchase_request_line
WHERE parent_id = ?
ORDER BY line_no;

-- name: SetRequestProject :exec
-- Validate's draft write of the project default from the site (P5). A plain
-- UPDATE inside DOC's write.
UPDATE purchase_request SET project_id = sqlc.narg(project_id) WHERE id = sqlc.arg(id);

-- name: SetRequestLineDefaults :exec
-- Validate's draft write of a line's description and unit defaults from its
-- item (P5). A plain UPDATE inside DOC's write.
UPDATE purchase_request_line
SET description = sqlc.arg(description),
    uom_id = sqlc.narg(uom_id)
WHERE id = sqlc.arg(id);

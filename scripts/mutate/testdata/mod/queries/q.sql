-- Items (fixture).

-- name: OpenItems :many
SELECT DISTINCT id
FROM item
WHERE owner = sqlc.arg(owner)
  AND state IN ('open', 'held')
ORDER BY id DESC
LIMIT 10;

-- name: MineOrShared :many
SELECT id
FROM item
WHERE owner = sqlc.arg(owner)
  OR shared_by = sqlc.arg(owner)
ORDER BY id;

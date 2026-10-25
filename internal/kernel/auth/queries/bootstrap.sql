-- name: UsersExist :one
SELECT EXISTS (SELECT 1 FROM users);

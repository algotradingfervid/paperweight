-- name: LockoutRecordFailure :exec
-- A missing or expired row restarts at count 1; otherwise count goes up and
-- the expiry stays. The SET expressions all read the old row.
INSERT INTO login_attempts (key, count, first_failure_at, expires_at)
VALUES (sqlc.arg(key), 1, sqlc.arg(now), sqlc.arg(expires_at))
ON CONFLICT (key) DO UPDATE SET
    count = CASE WHEN login_attempts.expires_at <= excluded.first_failure_at THEN 1 ELSE login_attempts.count + 1 END,
    first_failure_at = CASE WHEN login_attempts.expires_at <= excluded.first_failure_at THEN excluded.first_failure_at ELSE login_attempts.first_failure_at END,
    expires_at = CASE WHEN login_attempts.expires_at <= excluded.first_failure_at THEN excluded.expires_at ELSE login_attempts.expires_at END;

-- name: LockoutIsLocked :one
SELECT count(*) FROM login_attempts
WHERE key = sqlc.arg(key) AND count >= sqlc.arg(limit) AND expires_at > sqlc.arg(now);

-- name: LockoutDelete :exec
DELETE FROM login_attempts WHERE key = ?;

-- name: LockoutSweep :execrows
DELETE FROM login_attempts WHERE expires_at <= ?;

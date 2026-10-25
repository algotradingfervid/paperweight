package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Lockout limits (plan Global Constraints): failures per account key or per
// IP within LockWindow, which starts at the first failure.
const (
	AccountLimit = 5
	IPLimit      = 50
	LockWindow   = 15 * time.Minute
)

// emailHash is hex(SHA-256) of the normalised email; the plain email is
// never stored.
func emailHash(email string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	return hex.EncodeToString(sum[:])
}

// AccountKey is the login_attempts key of email's account.
func AccountKey(email string) string { return "acct:" + emailHash(email) }

// IPKey is the login_attempts key of ip.
func IPKey(ip string) string { return "ip:" + ip }

// countAccountFailure records a failed sign-in for email's account.
func (a *Auth) countAccountFailure(ctx context.Context, email string) error {
	return a.recordFailure(ctx, AccountKey(email))
}

// recordFailure counts one failure for key in a single upsert. A missing or
// expired row restarts the window at now.
func (a *Auth) recordFailure(ctx context.Context, key string) error {
	now := a.clock.Now()
	return a.db.Write(ctx, func(tx *db.Tx) error {
		return dbq.New(tx.SQL()).LockoutRecordFailure(ctx, dbq.LockoutRecordFailureParams{
			Key: key, Now: FormatTime(now), ExpiresAt: FormatTime(now.Add(LockWindow)),
		})
	})
}

// locked reports whether key has at least limit failures in an unexpired
// window.
func (a *Auth) locked(ctx context.Context, key string, limit int) (bool, error) {
	var n int64
	err := a.db.Read(ctx, func(tx *db.Tx) error {
		var err error
		n, err = dbq.New(tx.SQL()).LockoutIsLocked(ctx, dbq.LockoutIsLockedParams{
			Key: key, Limit: int64(limit), Now: FormatTime(a.clock.Now()),
		})
		return err
	})
	return n > 0, err
}

// SweepLockouts deletes expired counters and returns how many it deleted.
func (a *Auth) SweepLockouts(ctx context.Context) (int, error) {
	var n int64
	err := a.db.Write(ctx, func(tx *db.Tx) error {
		var err error
		n, err = dbq.New(tx.SQL()).LockoutSweep(ctx, FormatTime(a.clock.Now()))
		return err
	})
	return int(n), err
}

package authtest

import (
	"context"
	"sync"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// defaultPassword is the canonical test password; its hash is computed once
// per process.
const defaultPassword = "Correct-Horse-42"

var defaultHash = sync.OnceValues(func() (string, error) { return auth.Hash(defaultPassword) })

// SetPassword stores the hash of pw as email's password.
func (w *World) SetPassword(t testing.TB, email, pw string) {
	t.Helper()
	u := w.user(t, email)
	var hash string
	var err error
	if pw == defaultPassword {
		hash, err = defaultHash()
	} else {
		hash, err = auth.Hash(pw)
	}
	if err != nil {
		t.Fatalf("authtest.SetPassword(%q): %v", email, err)
	}
	ctx := context.Background()
	if err := w.DB.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, hash, int64(u))
		return err
	}); err != nil {
		t.Fatalf("authtest.SetPassword(%q): %v", email, err)
	}
}

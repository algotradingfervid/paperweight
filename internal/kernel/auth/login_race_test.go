package auth_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// raceAuth returns an Auth whose Deps.Verify runs during and then accepts the
// password and asks for a rehash, standing in for a concurrent admin action
// between verify and save.
func raceAuth(w *authtest.World, during func()) *auth.Auth {
	return auth.New(auth.Deps{
		DB: w.DB, Clock: w.Clock, Directory: w.Dir, Audit: w.Audit, Refs: w.Refs, Info: w.Info,
		Verify: func(_, _ string) (ok, rehash bool, err error) {
			during()
			return true, true, nil
		},
	})
}

func raceExec(t *testing.T, w *authtest.World, query string, args ...any) {
	t.Helper()
	ctx := context.Background()
	if err := w.DB.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, query, args...)
		return err
	}); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

// raceUser returns email's stored last_login_at and password_hash.
func raceUser(t *testing.T, w *authtest.World, email string) (lastLogin sql.NullString, hash string) {
	t.Helper()
	if err := w.DB.SQL().QueryRowContext(context.Background(),
		`SELECT last_login_at, password_hash FROM users WHERE email = ?`, email).Scan(&lastLogin, &hash); err != nil {
		t.Fatalf("read user %q: %v", email, err)
	}
	return lastLogin, hash
}

func TestLoginRace_DisabledDuringVerify(t *testing.T) {
	const email = "priya@vardhan.test"
	w := authtest.NewWorld(t)
	w.SetPassword(t, email, t8Password)
	a := raceAuth(w, func() { raceExec(t, w, `UPDATE users SET enabled = 0 WHERE email = ?`, email) })
	if _, err := t8SignIn(a, email, t8Password); !errors.Is(err, auth.ErrAccountDisabled) {
		t.Fatalf("SignIn: error = %v, want ErrAccountDisabled", err)
	}
	if last, _ := raceUser(t, w, email); last.Valid {
		t.Errorf("last_login_at = %q, want unset", last.String)
	}
	if n, _, _, _ := t8Attempt(t, w, auth.AccountKey(email)); n != 1 {
		t.Errorf("account failures = %d, want 1", n)
	}
}

func TestLoginRace_HashChangedDuringVerify(t *testing.T) {
	const email = "priya@vardhan.test"
	w := authtest.NewWorld(t)
	w.SetPassword(t, email, t8Password)
	reset, err := auth.Hash("Reset-By-Admin-77")
	if err != nil {
		t.Fatal(err)
	}
	a := raceAuth(w, func() { raceExec(t, w, `UPDATE users SET password_hash = ? WHERE email = ?`, reset, email) })
	if _, err := t8SignIn(a, email, t8Password); !errors.Is(err, auth.ErrLoginInvalid) {
		t.Fatalf("SignIn: error = %v, want ErrLoginInvalid", err)
	}
	last, hash := raceUser(t, w, email)
	if last.Valid {
		t.Errorf("last_login_at = %q, want unset", last.String)
	}
	if hash != reset {
		t.Errorf("password_hash = %q, want the concurrent reset's hash", hash)
	}
	if n, _, _, _ := t8Attempt(t, w, auth.AccountKey(email)); n != 1 {
		t.Errorf("account failures = %d, want 1", n)
	}
}

func TestLoginRace_FailureAfterExpiredWindowRestarts(t *testing.T) {
	const email = "priya@vardhan.test"
	w := authtest.NewWorld(t)
	w.SetPassword(t, email, t8Password)
	w.Clock.Set(t8Start)
	for i := range 5 {
		if _, err := t8SignIn(w.Auth, email, t8Wrong); !errors.Is(err, auth.ErrLoginInvalid) {
			t.Fatalf("failure %d: error = %v, want ErrLoginInvalid", i+1, err)
		}
	}
	w.Clock.Advance(auth.LockWindow)
	now := w.Clock.Now()
	if _, err := t8SignIn(w.Auth, email, t8Wrong); !errors.Is(err, auth.ErrLoginInvalid) {
		t.Fatalf("failure after window: error = %v, want ErrLoginInvalid", err)
	}
	count, first, expires, ok := t8Attempt(t, w, auth.AccountKey(email))
	if !ok || count != 1 {
		t.Fatalf("account row ok=%v count=%d, want count 1", ok, count)
	}
	if !first.Equal(now) {
		t.Errorf("first_failure_at = %s, want %s", first, now)
	}
	if want := now.Add(auth.LockWindow); !expires.Equal(want) {
		t.Errorf("expires_at = %s, want %s", expires, want)
	}
	if _, err := t8SignIn(w.Auth, email, t8Password); err != nil {
		t.Errorf("correct password after restart: error = %v, want nil (not locked)", err)
	}
}

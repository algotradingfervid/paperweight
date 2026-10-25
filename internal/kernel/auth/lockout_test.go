package auth_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
)

// t8Attempt returns the stored login_attempts row for key; ok is false when
// there is none.
func t8Attempt(t *testing.T, w *authtest.World, key string) (count int, first, expires time.Time, ok bool) {
	t.Helper()
	var f, e string
	err := w.DB.SQL().QueryRowContext(context.Background(),
		`SELECT count, first_failure_at, expires_at FROM login_attempts WHERE key = ?`, key).Scan(&count, &f, &e)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, time.Time{}, time.Time{}, false
		}
		t.Fatalf("read login_attempts %q: %v", key, err)
	}
	if first, err = auth.ParseTime(f); err != nil {
		t.Fatalf("first_failure_at %q: %v", f, err)
	}
	if expires, err = auth.ParseTime(e); err != nil {
		t.Fatalf("expires_at %q: %v", e, err)
	}
	return count, first, expires, true
}

// t8Outcome is how an error looks to the person signing in.
type t8Outcome struct {
	Status int
	Msg    string
}

func t8Describe(err error) t8Outcome {
	var p *auth.Problem
	if errors.As(err, &p) {
		return t8Outcome{p.Status(), p.Error()}
	}
	if err == nil {
		return t8Outcome{0, "<nil>"}
	}
	return t8Outcome{-1, err.Error()}
}

// AUTH-010
func TestLockout_UnlocksAfterWindow(t *testing.T) {
	w := authtest.NewWorld(t)
	w.SetPassword(t, "priya@vardhan.test", t8Password)
	for i := range 5 {
		w.Clock.Set(t8Start.Add(time.Duration(i) * time.Minute))
		if _, err := t8SignIn(w.Auth, "priya@vardhan.test", t8Wrong); !errors.Is(err, auth.ErrLoginInvalid) {
			t.Fatalf("failure %d at %s: error = %v, want ErrLoginInvalid", i+1, w.Clock.Now().Format(time.TimeOnly), err)
		}
	}
	w.Clock.Set(time.Date(2026, 10, 24, 4, 44, 59, 0, time.UTC))
	if _, err := t8SignIn(w.Auth, "priya@vardhan.test", t8Password); !errors.Is(err, auth.ErrAccountLocked) {
		t.Errorf("correct password at 04:44:59Z: error = %v, want ErrAccountLocked", err)
	}
	w.Clock.Set(time.Date(2026, 10, 24, 4, 45, 1, 0, time.UTC))
	got, err := t8SignIn(w.Auth, "priya@vardhan.test", t8Password)
	if err != nil {
		t.Fatalf("correct password at 04:45:01Z: error = %v, want nil", err)
	}
	if want := w.Users["priya@vardhan.test"]; got.User != want {
		t.Errorf("SignIn.User = %d, want %d (priya)", got.User, want)
	}
	if _, _, _, ok := t8Attempt(t, w, auth.AccountKey("priya@vardhan.test")); ok {
		t.Errorf("login_attempts row for priya's account key still present after a successful sign-in")
	}
}

// AUTH-011
func TestLockout_SuccessResetsCounter(t *testing.T) {
	w := authtest.NewWorld(t)
	w.Clock.Set(t8Start)
	w.SetPassword(t, "priya@vardhan.test", t8Password)
	fail4 := func(round string) {
		t.Helper()
		for i := range 4 {
			if _, err := t8SignIn(w.Auth, "priya@vardhan.test", t8Wrong); !errors.Is(err, auth.ErrLoginInvalid) {
				t.Fatalf("%s failure %d: error = %v, want ErrLoginInvalid", round, i+1, err)
			}
			w.Clock.Advance(time.Minute)
		}
	}
	fail4("first")
	if _, err := t8SignIn(w.Auth, "priya@vardhan.test", t8Password); err != nil {
		t.Fatalf("correct sign-in after 4 failures: error = %v, want nil", err)
	}
	fail4("second")
	if n, _, _, ok := t8Attempt(t, w, auth.AccountKey("priya@vardhan.test")); !ok || n != 4 {
		t.Errorf("account counter = (%d, present=%v), want (4, true)", n, ok)
	}
	if _, err := t8SignIn(w.Auth, "priya@vardhan.test", t8Password); err != nil {
		t.Errorf("correct sign-in after 4+success+4 failures: error = %v, want nil (count 4, not 8)", err)
	}
}

// AUTH-012
func TestLockout_CounterAlwaysExpires(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	w.Clock.Set(t8Start)
	key := auth.AccountKey("priya@vardhan.test")
	if err := auth.RecordFailure(w.Auth, ctx, key); err != nil {
		t.Fatalf("RecordFailure(%q): %v", key, err)
	}
	n, first, expires, ok := t8Attempt(t, w, key)
	if !ok {
		t.Fatalf("no login_attempts row for %q after RecordFailure", key)
	}
	if n != 1 || !first.Equal(t8Start) {
		t.Errorf("row = (count %d, first %s), want (1, %s)", n, first, t8Start)
	}
	if d := expires.Sub(first); d <= 0 || d > auth.LockWindow {
		t.Errorf("expires_at - first_failure_at = %s, want in (0, %s]", d, auth.LockWindow)
	}
	if auth.LockWindow != 15*time.Minute {
		t.Errorf("LockWindow = %s, want 15m", auth.LockWindow)
	}

	w.Clock.Advance(16 * time.Minute)
	fresh := auth.AccountKey("ghost@vardhan.test")
	if err := auth.RecordFailure(w.Auth, ctx, fresh); err != nil {
		t.Fatalf("RecordFailure(%q): %v", fresh, err)
	}
	deleted, err := w.Auth.SweepLockouts(ctx)
	if err != nil || deleted != 1 {
		t.Errorf("SweepLockouts = (%d, %v), want (1, nil)", deleted, err)
	}
	if _, _, _, ok := t8Attempt(t, w, key); ok {
		t.Errorf("expired row %q still present after SweepLockouts", key)
	}
	if _, _, _, ok := t8Attempt(t, w, fresh); !ok {
		t.Errorf("unexpired row %q removed by SweepLockouts", fresh)
	}

	_, err = w.DB.SQL().ExecContext(ctx,
		`INSERT INTO login_attempts (key, count, first_failure_at, expires_at) VALUES (?, 1, ?, NULL)`,
		"acct:noexpiry", auth.FormatTime(t8Start))
	if err == nil || !strings.Contains(err.Error(), "NOT NULL") {
		t.Errorf("INSERT with NULL expires_at: error = %v, want a NOT NULL constraint error", err)
	}
}

// AUTH-013
func TestLockout_ConcurrentFailuresCounted(t *testing.T) {
	w := authtest.NewWorld(t)
	w.Clock.Set(t8Start)
	w.SetPassword(t, "priya@vardhan.test", t8Password)
	ctx := context.Background()
	key := auth.AccountKey("priya@vardhan.test")
	const n = 20
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = auth.RecordFailure(w.Auth, ctx, key)
		}()
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: RecordFailure error: %v", i, err)
		}
	}
	if got, _, _, ok := t8Attempt(t, w, key); !ok || got != n {
		t.Errorf("stored count = (%d, present=%v), want (%d, true)", got, ok, n)
	}
	if _, err := t8SignIn(w.Auth, "priya@vardhan.test", t8Password); !errors.Is(err, auth.ErrAccountLocked) {
		t.Errorf("SignIn(priya, correct) after %d failures: error = %v, want ErrAccountLocked", n, err)
	}
}

// AUTH-014
func TestLockout_UnknownEmailIndistinguishable(t *testing.T) {
	w := authtest.NewWorld(t)
	w.Clock.Set(t8Start)
	w.SetPassword(t, "priya@vardhan.test", t8Password)
	var want []t8Outcome
	for range 5 {
		want = append(want, t8Describe(auth.ErrLoginInvalid))
	}
	want = append(want, t8Describe(auth.ErrAccountLocked))

	seq := func(email string) []t8Outcome {
		var out []t8Outcome
		for range 6 {
			_, err := t8SignIn(w.Auth, email, t8Wrong)
			out = append(out, t8Describe(err))
			w.Clock.Advance(10 * time.Second)
		}
		return out
	}
	ghost := seq("ghost@vardhan.test")
	priya := seq("priya@vardhan.test")
	if !reflect.DeepEqual(ghost, want) {
		t.Errorf("ghost sequence = %v, want %v", ghost, want)
	}
	if !reflect.DeepEqual(priya, want) {
		t.Errorf("priya sequence = %v, want %v", priya, want)
	}

	sum := sha256.Sum256([]byte("ghost@vardhan.test"))
	ghostKey := "acct:" + hex.EncodeToString(sum[:])
	if got := auth.AccountKey("ghost@vardhan.test"); got != ghostKey {
		t.Errorf("AccountKey(ghost) = %q, want %q", got, ghostKey)
	}
	rows, err := w.DB.SQL().QueryContext(context.Background(), `SELECT key FROM login_attempts`)
	if err != nil {
		t.Fatalf("SELECT key: %v", err)
	}
	defer func() { _ = rows.Close() }()
	keys := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatalf("scan key: %v", err)
		}
		keys[k] = true
		if strings.Contains(k, "@") {
			t.Errorf("login_attempts key %q contains a plain email", k)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if !keys[ghostKey] {
		t.Errorf("login_attempts keys %v lack the ghost account key %q", keys, ghostKey)
	}
	if ipKey := auth.IPKey(t8IP); ipKey != "ip:"+t8IP || !keys[ipKey] {
		t.Errorf("IPKey(%q) = %q, want %q present in login_attempts keys %v", t8IP, ipKey, "ip:"+t8IP, keys)
	}
}

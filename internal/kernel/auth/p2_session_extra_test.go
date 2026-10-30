package auth_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Carry-overs from the Task 8 and Task 9 reviews (Task 16).

// t16Expiring signs in four sessions, then moves the clock to now and sets
// their stored times so that:
//
//	idleGone  last used 12 h 0 m 1 s ago      (expired)
//	idleLive  last used exactly 12 h ago      (live)
//	absGone   created 7 d 0 h 0 m 1 s ago     (expired, used just now)
//	absLive   created exactly 7 d ago         (live, used just now)
//
// It returns the token hash of each.
func t16Expiring(t *testing.T, e *sessionEnv) (now time.Time, idleGone, idleLive, absGone, absLive string) {
	t.Helper()
	priya := e.w.Users["priya@vardhan.test"]
	e.w.Clock.Set(authtest.T0)
	idleGone = auth.HashToken(e.login(t, priya, "10.0.0.11"))
	idleLive = auth.HashToken(e.login(t, priya, "10.0.0.12"))
	absGone = auth.HashToken(e.login(t, priya, "10.0.0.13"))
	absLive = auth.HashToken(e.login(t, priya, "10.0.0.14"))

	now = authtest.T0.Add(8 * 24 * time.Hour)
	e.w.Clock.Set(now)
	set := func(hash string, created, lastSeen time.Time) {
		raceExec(t, e.w, `UPDATE sessions SET created_at = ?, last_seen_at = ? WHERE token_hash = ?`,
			auth.FormatTime(created), auth.FormatTime(lastSeen), hash)
	}
	day := 24 * time.Hour
	set(idleGone, now.Add(-day), now.Add(-12*time.Hour-time.Second))
	set(idleLive, now.Add(-day), now.Add(-12*time.Hour))
	set(absGone, now.Add(-7*day-time.Second), now)
	set(absLive, now.Add(-7*day), now)
	return now, idleGone, idleLive, absGone, absLive
}

// t16Hashes returns every token_hash in sessions, sorted.
func t16Hashes(t *testing.T, e *sessionEnv) []string {
	t.Helper()
	ctx := context.Background()
	var out []string
	if err := e.w.DB.Read(ctx, func(tx *db.Tx) error {
		rows, err := tx.SQL().QueryContext(ctx, `SELECT token_hash FROM sessions ORDER BY token_hash`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var h string
			if err := rows.Scan(&h); err != nil {
				return err
			}
			out = append(out, h)
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("list token hashes: %v", err)
	}
	return out
}

func t16Sorted(a, b string) []string {
	if a > b {
		a, b = b, a
	}
	return []string{a, b}
}

func TestSession_SweepDeletesOnlyExpired(t *testing.T) {
	e := newSessionEnv(t)
	_, idleGone, idleLive, absGone, absLive := t16Expiring(t, e)
	if n := len(t16Hashes(t, e)); n != 4 {
		t.Fatalf("sessions before Sweep = %d, want 4", n)
	}

	n, err := e.st.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if n != 2 {
		t.Errorf("Sweep() = %d, want 2 (idle-expired and absolute-expired)", n)
	}
	got := t16Hashes(t, e)
	if want := t16Sorted(idleLive, absLive); !reflect.DeepEqual(got, want) {
		t.Errorf("sessions after Sweep = %v, want the two live ones %v (gone: idle %s, absolute %s)", got, want, idleGone, absGone)
	}

	n, err = e.st.Sweep(context.Background())
	if err != nil || n != 0 {
		t.Errorf("second Sweep() = %d, %v; want 0, nil", n, err)
	}
}

func TestSession_AllCtxOmitsExpired(t *testing.T) {
	e := newSessionEnv(t)
	_, _, idleLive, _, absLive := t16Expiring(t, e)

	all, err := e.st.AllCtx(context.Background())
	if err != nil {
		t.Fatalf("AllCtx: %v", err)
	}
	ctx := context.Background()
	want := map[string][]byte{}
	for _, h := range []string{idleLive, absLive} {
		var data []byte
		if err := e.w.DB.Read(ctx, func(tx *db.Tx) error {
			return tx.SQL().QueryRowContext(ctx, `SELECT data FROM sessions WHERE token_hash = ?`, h).Scan(&data)
		}); err != nil {
			t.Fatalf("read data of %s: %v", h, err)
		}
		want[h] = data
	}
	if !reflect.DeepEqual(all, want) {
		keys := make([]string, 0, len(all))
		for k := range all {
			keys = append(keys, k)
		}
		t.Errorf("AllCtx() keys = %v, want only the live %v", keys, t16Sorted(idleLive, absLive))
	}
}

func TestSession_AbsoluteLimitWhileActive(t *testing.T) {
	// A read-only request reaches the store only as Middleware's touch (CD-223).
	e := newServedSessionEnv(t)
	priya := e.w.Users["priya@vardhan.test"]
	e.w.Clock.Set(authtest.T0)
	raw := e.login(t, priya, "10.0.0.4")

	for h := 1; h <= 7*24; h++ { // used every hour, up to exactly 7 days
		e.w.Clock.Advance(time.Hour)
		if got := e.uidSeen(t, raw); got != int64(priya) {
			t.Fatalf("at T0+%dh: uid = %d, want %d", h, got, priya)
		}
	}
	e.w.Clock.Advance(time.Second) // 7 d 0 h 0 m 1 s after sign-in, last used 1 s ago
	if got := e.uidSeen(t, raw); got != 0 {
		t.Errorf("7 days and 1 s after sign-in: uid = %d, want 0 (signed out)", got)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM sessions WHERE token_hash = ?`, auth.HashToken(raw)); n != 0 {
		t.Errorf("absolute-expired session rows = %d, want 0 (deleted)", n)
	}
}

func TestSession_UpdateKeepsCreatedAt(t *testing.T) {
	e := newSessionEnv(t)
	priya := e.w.Users["priya@vardhan.test"]
	e.w.Clock.Set(authtest.T0)
	raw := e.login(t, priya, "10.0.0.4")
	hash := auth.HashToken(raw)

	e.w.Clock.Advance(3 * time.Hour)
	// A real commit on the presented token (CD-223: a read-only GET commits nothing).
	if body, _ := e.get(t, fmt.Sprintf("/login?uid=%d", priya), raw, "10.0.0.4"); body != strconv.FormatInt(int64(priya), 10) {
		t.Fatalf("after 3 h: uid = %q, want %d", body, priya)
	}
	if got := e.str(t, `SELECT created_at FROM sessions WHERE token_hash = ?`, hash); got != auth.FormatTime(authtest.T0) {
		t.Errorf("created_at after an update = %q, want %q (unchanged)", got, auth.FormatTime(authtest.T0))
	}
	if got := e.str(t, `SELECT last_seen_at FROM sessions WHERE token_hash = ?`, hash); got != auth.FormatTime(authtest.T0.Add(3*time.Hour)) {
		t.Errorf("last_seen_at after an update = %q, want %q", got, auth.FormatTime(authtest.T0.Add(3*time.Hour)))
	}
	if n := e.count(t, `SELECT COUNT(*) FROM sessions`); n != 1 {
		t.Errorf("sessions rows = %d, want 1 (updated in place)", n)
	}
}

// t16PrefillFailures records two wrong-password failures for email at
// t8Start and t8Start+1m and leaves the clock at t8Start+2m.
func t16PrefillFailures(t *testing.T, w *authtest.World, email string) {
	t.Helper()
	w.Clock.Set(t8Start)
	for i := range 2 {
		if _, err := t8SignIn(w.Auth, email, t8Wrong); !errors.Is(err, auth.ErrLoginInvalid) {
			t.Fatalf("prefill failure %d: error = %v, want ErrLoginInvalid", i+1, err)
		}
		w.Clock.Advance(time.Minute)
	}
}

// t16WantCounters asserts the account and IP counters both hold 3 failures in
// the window that started at t8Start.
func t16WantCounters(t *testing.T, w *authtest.World, email string) {
	t.Helper()
	for _, key := range []string{auth.AccountKey(email), auth.IPKey(t8IP)} {
		count, first, expires, ok := t8Attempt(t, w, key)
		if !ok {
			t.Errorf("%s: no login_attempts row, want count 3 (the counter was reset)", key)
			continue
		}
		if count != 3 {
			t.Errorf("%s: count = %d, want 3", key, count)
		}
		if !first.Equal(t8Start) {
			t.Errorf("%s: first_failure_at = %s, want %s (window kept)", key, first, t8Start)
		}
		if want := t8Start.Add(auth.LockWindow); !expires.Equal(want) {
			t.Errorf("%s: expires_at = %s, want %s", key, expires, want)
		}
	}
}

func TestLogin_RaceDisabledDuringVerifyCounts(t *testing.T) {
	const email = "priya@vardhan.test"
	w := authtest.NewWorld(t)
	w.SetPassword(t, email, t8Password)
	t16PrefillFailures(t, w, email)
	a := raceAuth(w, func() { raceExec(t, w, `UPDATE users SET enabled = 0 WHERE email = ?`, email) })

	if _, err := t8SignIn(a, email, t8Password); !errors.Is(err, auth.ErrAccountDisabled) {
		t.Fatalf("SignIn: error = %v, want ErrAccountDisabled", err)
	}
	t16WantCounters(t, w, email)
	if last, _ := raceUser(t, w, email); last.Valid {
		t.Errorf("last_login_at = %q, want unset", last.String)
	}
}

func TestLogin_RaceHashChangedDuringVerifyCounts(t *testing.T) {
	const email = "priya@vardhan.test"
	w := authtest.NewWorld(t)
	w.SetPassword(t, email, t8Password)
	t16PrefillFailures(t, w, email)
	reset, err := auth.Hash("Reset-By-Admin-77")
	if err != nil {
		t.Fatal(err)
	}
	a := raceAuth(w, func() { raceExec(t, w, `UPDATE users SET password_hash = ? WHERE email = ?`, reset, email) })

	if _, err := t8SignIn(a, email, t8Password); !errors.Is(err, auth.ErrLoginInvalid) {
		t.Fatalf("SignIn: error = %v, want ErrLoginInvalid", err)
	}
	t16WantCounters(t, w, email)
	last, hash := raceUser(t, w, email)
	if last.Valid {
		t.Errorf("last_login_at = %q, want unset", last.String)
	}
	if hash != reset {
		t.Errorf("password_hash = %q, want the concurrent reset's hash", hash)
	}
}

func TestLogin_PasswordOverByteCap(t *testing.T) {
	w := authtest.NewWorld(t)
	w.SetPassword(t, "priya@vardhan.test", t8Password)
	a, spy := t8SpyAuth(w)

	// 120 Hangul syllables in NFD: 360 jamo, 1080 bytes, but only 120 runes
	// after NFC, so only the byte cap rejects it.
	long := norm.NFD.String(strings.Repeat("한", 120))
	if len(long) <= 1024 || utf8.RuneCountInString(norm.NFC.String(long)) > 256 {
		t.Fatalf("test setup: %d bytes, %d runes after NFC; want > 1024 bytes and <= 256 runes",
			len(long), utf8.RuneCountInString(norm.NFC.String(long)))
	}
	for _, email := range []string{"priya@vardhan.test", "nobody@vardhan.test"} {
		if _, err := t8SignIn(a, email, long); !errors.Is(err, auth.ErrPasswordTooLong) {
			t.Errorf("SignIn(%q, %d bytes) error = %v, want ErrPasswordTooLong", email, len(long), err)
		}
	}
	if n := len(spy.calls()); n != 0 {
		t.Errorf("Verify called %d times for passwords over 1024 bytes, want 0", n)
	}

	// Exactly 1024 bytes (256 four-byte runes) is within both limits.
	edge := strings.Repeat("\U0001F600", 256)
	if _, err := t8SignIn(a, "priya@vardhan.test", edge); !errors.Is(err, auth.ErrLoginInvalid) {
		t.Errorf("SignIn(priya, 1024 bytes) error = %v, want ErrLoginInvalid (verified, wrong)", err)
	}
	if n := len(spy.calls()); n != 1 {
		t.Errorf("Verify calls after a 1024-byte password = %d, want 1", n)
	}
}

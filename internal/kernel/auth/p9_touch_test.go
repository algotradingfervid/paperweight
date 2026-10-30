package auth_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	authweb "github.com/vardhan/internaltools/internal/kernel/auth/web"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/views"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// P9 (CD-223): SessionStore.Touch on the real store. The overlap and page
// behaviour through Server.Middleware is in web/p9_session_test.go.

const (
	p9Deadline = 5 * time.Second
	p9Stale    = 2 * time.Minute // past the touch interval
	p9IP1      = "203.0.113.7"
	p9IP2      = "203.0.113.8"
	p9UA1      = "agent-one"
	p9UA2      = "agent-two"
	p9Refused  = "p9 refused the update"
)

// newServedSessionEnv is newSessionEnv with the production path in front:
// Server.Middleware instead of a bare LoadAndSave, so a request touches the
// session the way a real one does. Routes: /login?uid=N signs in with a CSRF
// token, as sign-in does, and / writes the uid it sees.
func newServedSessionEnv(t *testing.T) *sessionEnv {
	t.Helper()
	w := authtest.NewWorld(t)
	cfg := auth.DefaultSessionConfig(false)
	st := auth.NewSessionStore(w.DB, w.Clock, cfg)
	sm := auth.NewSessionManager(st, cfg)
	srv := authweb.New(w.Auth, st, sm, views.Default{}, authweb.Config{})

	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(rw http.ResponseWriter, r *http.Request) {
		uid, err := strconv.ParseInt(r.URL.Query().Get("uid"), 10, 64)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusBadRequest)
			return
		}
		sm.Put(r.Context(), auth.SessionKeyUser, uid)
		authweb.CSRFToken(r.Context())
		_, _ = io.WriteString(rw, strconv.FormatInt(uid, 10))
	})
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(rw, strconv.FormatInt(sm.GetInt64(r.Context(), auth.SessionKeyUser), 10))
	})
	hs := httptest.NewServer(srv.Middleware(mux))
	t.Cleanup(hs.Close)
	return &sessionEnv{w: w, st: st, sm: sm, srv: hs}
}

// p9Row is the part of a sessions row a touch may or may not change.
type p9Row struct {
	data                          []byte
	created, lastSeen, ip, userAg string
}

func (e *sessionEnv) p9Session(t *testing.T, hash string) p9Row {
	t.Helper()
	var r p9Row
	ctx := context.Background()
	if err := e.w.DB.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx,
			`SELECT data, created_at, last_seen_at, ip, user_agent FROM sessions WHERE token_hash = ?`, hash).
			Scan(&r.data, &r.created, &r.lastSeen, &r.ip, &r.userAg)
	}); err != nil {
		t.Fatalf("read session %s: %v", hash, err)
	}
	return r
}

// p9LogUpdates counts every UPDATE of a sessions row.
func p9LogUpdates(t *testing.T, e *sessionEnv) {
	t.Helper()
	raceExec(t, e.w, `CREATE TABLE p9_updates (id INTEGER PRIMARY KEY)`)
	raceExec(t, e.w, `CREATE TRIGGER p9_log_update AFTER UPDATE ON sessions BEGIN INSERT INTO p9_updates (id) VALUES (NULL); END`)
}

func (e *sessionEnv) p9Updates(t *testing.T) int {
	t.Helper()
	return e.count(t, `SELECT COUNT(*) FROM p9_updates`)
}

// p9Signed signs priya in at T0 with p9IP1 and p9UA1 on the row.
func p9Signed(t *testing.T, e *sessionEnv) (hash string) {
	t.Helper()
	e.w.Clock.Set(authtest.T0)
	hash = auth.HashToken(e.login(t, e.w.Users["priya@vardhan.test"], "10.0.0.4"))
	raceExec(t, e.w, `UPDATE sessions SET ip = ?, user_agent = ? WHERE token_hash = ?`, p9IP1, p9UA1, hash)
	return hash
}

func p9Meta(hash, ip, ua string) context.Context {
	return auth.WithRequestMeta(context.Background(), hash, ip, ua)
}

func p9Recv[T any](t *testing.T, what string, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(p9Deadline):
		t.Fatalf("%s: nothing within %s", what, p9Deadline)
		panic("unreachable")
	}
}

// p9WriteLock is a write transaction the test holds open. Readers carry on
// under WAL; every other db.Write waits on BEGIN IMMEDIATE.
type p9WriteLock struct {
	finish chan struct{}
	done   chan error
	once   sync.Once
	err    error
}

// p9HoldWrite opens the transaction; apply, if any, runs in it on release.
func p9HoldWrite(t *testing.T, w *authtest.World, apply func(tx *db.Tx) error) *p9WriteLock {
	t.Helper()
	l := &p9WriteLock{finish: make(chan struct{}), done: make(chan error, 1)}
	locked := make(chan struct{})
	go func() {
		l.done <- w.DB.Write(context.Background(), func(tx *db.Tx) error {
			close(locked)
			select {
			case <-l.finish:
			case <-time.After(2 * p9Deadline):
				return errors.New("write lock held past its deadline")
			}
			if apply != nil {
				return apply(tx)
			}
			return nil
		})
	}()
	t.Cleanup(func() { _ = l.commit() })
	p9Recv(t, "take the write lock", locked)
	return l
}

func (l *p9WriteLock) commit() error {
	l.once.Do(func() {
		close(l.finish)
		l.err = <-l.done
	})
	return l.err
}

// p9TouchWriters counts goroutines inside a db.Write called from Touch. While
// the test holds the write lock such a goroutine has done Touch's read and is
// parked on BEGIN IMMEDIATE.
func p9TouchWriters() int {
	buf := make([]byte, 8<<20)
	buf = buf[:runtime.Stack(buf, true)]
	n := 0
	for g := range strings.SplitSeq(string(buf), "\n\n") {
		if strings.Contains(g, "auth.(*SessionStore).Touch") && strings.Contains(g, "db.(*DB).Write") {
			n++
		}
	}
	return n
}

func p9WaitTouchWriters(t *testing.T, n int) {
	t.Helper()
	deadline := time.After(p9Deadline)
	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	for p9TouchWriters() < n {
		select {
		case <-tick.C:
		case <-deadline:
			t.Fatalf("%d of %d calls reached Touch's db.Write within %s", p9TouchWriters(), n, p9Deadline)
		}
	}
}

// CD-223: scs must not re-commit every loaded session, and Lifetime stays.
func TestNewSessionManager_NoIdleTimeout(t *testing.T) {
	w := authtest.NewWorld(t)
	cfg := auth.DefaultSessionConfig(false)
	sm := auth.NewSessionManager(auth.NewSessionStore(w.DB, w.Clock, cfg), cfg)
	if sm.IdleTimeout != 0 {
		t.Errorf("IdleTimeout = %s, want 0", sm.IdleTimeout)
	}
	if sm.Lifetime != cfg.Absolute {
		t.Errorf("Lifetime = %s, want %s", sm.Lifetime, cfg.Absolute)
	}
}

// (b) Inside the minute Touch only reads: it returns with no change and never
// asks for the write lock, which the test is holding.
func TestTouch_WithinMinuteTakesNoWriteLock(t *testing.T) {
	e := newSessionEnv(t)
	hash := p9Signed(t, e)
	p9LogUpdates(t, e)
	before := e.p9Session(t, hash)

	e.w.Clock.Set(authtest.T0.Add(time.Minute - time.Millisecond))
	lock := p9HoldWrite(t, e.w, nil)
	done := make(chan error, 1)
	go func() { done <- e.st.Touch(p9Meta(hash, p9IP2, p9UA2), hash) }()
	if err := p9Recv(t, "Touch returns with the write lock held", done); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	if err := lock.commit(); err != nil {
		t.Fatalf("release the write lock: %v", err)
	}

	if got := e.p9Session(t, hash); !bytes.Equal(got.data, before.data) || got.lastSeen != before.lastSeen || got.ip != p9IP1 || got.userAg != p9UA1 {
		t.Errorf("row after Touch = %+v, want unchanged %+v", got, before)
	}
	if n := e.p9Updates(t); n != 0 {
		t.Errorf("updates = %d, want 0", n)
	}
}

// (c) Past the minute: last_seen_at, ip and user_agent move, data and
// created_at do not, and the row is updated in place.
func TestTouch_StaleMovesSeenIPAgentOnly(t *testing.T) {
	e := newSessionEnv(t)
	hash := p9Signed(t, e)
	p9LogUpdates(t, e)
	before := e.p9Session(t, hash)

	at := authtest.T0.Add(time.Minute + time.Millisecond)
	e.w.Clock.Set(at)
	if err := e.st.Touch(p9Meta(hash, p9IP2, p9UA2), hash); err != nil {
		t.Fatalf("Touch: %v", err)
	}

	got := e.p9Session(t, hash)
	if !bytes.Equal(got.data, before.data) {
		t.Errorf("data = %x, want byte-identical %x", got.data, before.data)
	}
	if got.created != before.created || got.lastSeen != auth.FormatTime(at) || got.ip != p9IP2 || got.userAg != p9UA2 {
		t.Errorf("row = %+v, want created_at %q, last_seen_at %q, ip %q, user_agent %q",
			got, before.created, auth.FormatTime(at), p9IP2, p9UA2)
	}
	if n := e.p9Updates(t); n != 1 {
		t.Errorf("updates = %d, want 1", n)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM sessions`); n != 1 {
		t.Errorf("sessions rows = %d, want 1", n)
	}
}

// (c) The pinned boundary: exactly one minute old is not moved, because the
// UPDATE is `last_seen_at < cutoff` with cutoff = now - 1 minute.
func TestTouch_ExactlyOneMinuteIsNotMoved(t *testing.T) {
	e := newSessionEnv(t)
	hash := p9Signed(t, e)
	p9LogUpdates(t, e)
	before := e.p9Session(t, hash)

	e.w.Clock.Set(authtest.T0.Add(time.Minute))
	if err := e.st.Touch(p9Meta(hash, p9IP2, p9UA2), hash); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	if got := e.p9Session(t, hash); got.lastSeen != before.lastSeen || got.ip != p9IP1 || got.userAg != p9UA1 {
		t.Errorf("row = %+v, want unchanged %+v", got, before)
	}
	if n := e.p9Updates(t); n != 0 {
		t.Errorf("updates = %d, want 0", n)
	}
}

// (g) A token with no row: no error, and no row appears.
func TestTouch_UnknownHashIsNoError(t *testing.T) {
	e := newSessionEnv(t)
	hash := auth.HashToken("no-such-token")
	if err := e.st.Touch(p9Meta(hash, p9IP1, p9UA1), hash); err != nil {
		t.Errorf("Touch(unknown) = %v, want nil", err)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM sessions`); n != 0 {
		t.Errorf("sessions rows = %d, want 0", n)
	}
}

// (g) Touch has read a stale row and waits on the write lock while a revoke
// holds it: 0 rows is not an error, and the row stays gone.
func TestTouch_RacingRevokeNoRowComesBack(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		revoke func(e *sessionEnv, hash string) func(tx *db.Tx) error
	}{
		{"RevokeAll", func(e *sessionEnv, _ string) func(tx *db.Tx) error {
			return func(tx *db.Tx) error { return e.st.RevokeAll(ctx, tx, e.w.Users["priya@vardhan.test"], "") }
		}},
		{"row deleted by hash", func(_ *sessionEnv, hash string) func(tx *db.Tx) error {
			return func(tx *db.Tx) error {
				_, err := tx.SQL().ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hash)
				return err
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newSessionEnv(t)
			hash := p9Signed(t, e)
			e.w.Clock.Set(authtest.T0.Add(p9Stale))

			lock := p9HoldWrite(t, e.w, tc.revoke(e, hash))
			done := make(chan error, 1)
			go func() { done <- e.st.Touch(p9Meta(hash, p9IP2, p9UA2), hash) }()
			p9WaitTouchWriters(t, 1)
			if err := lock.commit(); err != nil {
				t.Fatalf("revoke: %v", err)
			}
			if err := p9Recv(t, "Touch ends", done); err != nil {
				t.Errorf("Touch after the revoke = %v, want nil", err)
			}
			if n := e.count(t, `SELECT COUNT(*) FROM sessions WHERE token_hash = ?`, hash); n != 0 {
				t.Errorf("session rows = %d, want 0", n)
			}
		})
	}
}

// (h) Both calls are past their read and parked on the write lock, so only the
// cutoff in the UPDATE's WHERE can stop the second write.
func TestTouch_ConcurrentStaleMakeOneWrite(t *testing.T) {
	e := newSessionEnv(t)
	hash := p9Signed(t, e)
	p9LogUpdates(t, e)
	data := e.p9Session(t, hash).data

	for round := range 5 {
		e.w.Clock.Advance(p9Stale)
		updates := e.p9Updates(t)
		lock := p9HoldWrite(t, e.w, nil)
		done := make(chan error, 2)
		for _, m := range [][2]string{{p9IP1, p9UA1}, {p9IP2, p9UA2}} {
			go func() { done <- e.st.Touch(p9Meta(hash, m[0], m[1]), hash) }()
		}
		p9WaitTouchWriters(t, 2)
		if err := lock.commit(); err != nil {
			t.Fatalf("round %d: release the write lock: %v", round, err)
		}
		for range 2 {
			if err := p9Recv(t, "a Touch ends", done); err != nil {
				t.Errorf("round %d: Touch = %v, want nil (0 rows is not an error)", round, err)
			}
		}
		if n := e.p9Updates(t) - updates; n != 1 {
			t.Errorf("round %d: writes from two stale calls = %d, want 1", round, n)
		}
		got := e.p9Session(t, hash)
		if got.lastSeen != auth.FormatTime(e.w.Clock.Now()) {
			t.Errorf("round %d: last_seen_at = %q, want %q", round, got.lastSeen, auth.FormatTime(e.w.Clock.Now()))
		}
		if !slices.Contains([]string{p9IP1, p9IP2}, got.ip) {
			t.Errorf("round %d: ip = %q, want one of the two calls'", round, got.ip)
		}
		if !bytes.Equal(got.data, data) {
			t.Errorf("round %d: data changed", round)
		}
	}
}

// (h) The cutoff is a minute back, not "now" and not absent. A commit lands
// 30 s before now while Touch waits on the write lock: last_seen_at is older
// than now but newer than the cutoff, so the UPDATE matches nothing and the
// commit's row stands.
func TestTouch_CutoffInWhereIsOneMinuteBack(t *testing.T) {
	ctx := context.Background()
	e := newSessionEnv(t)
	hash := p9Signed(t, e)
	now := authtest.T0.Add(p9Stale)
	e.w.Clock.Set(now)
	commit := auth.FormatTime(now.Add(-30 * time.Second))

	lock := p9HoldWrite(t, e.w, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, `UPDATE sessions SET last_seen_at = ? WHERE token_hash = ?`, commit, hash)
		return err
	})
	done := make(chan error, 1)
	go func() { done <- e.st.Touch(p9Meta(hash, p9IP2, p9UA2), hash) }()
	p9WaitTouchWriters(t, 1)
	if err := lock.commit(); err != nil {
		t.Fatalf("the commit: %v", err)
	}
	if err := p9Recv(t, "Touch ends", done); err != nil {
		t.Fatalf("Touch = %v, want nil", err)
	}
	if got := e.p9Session(t, hash); got.lastSeen != commit || got.ip != p9IP1 || got.userAg != p9UA1 {
		t.Errorf("row = %+v, want last_seen_at %q and the sign-in's ip and user_agent: the touch overwrote a newer write", got, commit)
	}
}

// (j) Touch returns the store's failure; the middleware is what forgives it.
func TestTouch_ReturnsStoreFailure(t *testing.T) {
	e := newSessionEnv(t)
	hash := p9Signed(t, e)
	raceExec(t, e.w, fmt.Sprintf(`CREATE TRIGGER p9_refuse BEFORE UPDATE ON sessions BEGIN SELECT RAISE(ABORT, '%s'); END`, p9Refused))

	e.w.Clock.Set(authtest.T0.Add(p9Stale))
	err := e.st.Touch(p9Meta(hash, p9IP2, p9UA2), hash)
	if err == nil || !strings.Contains(err.Error(), p9Refused) {
		t.Errorf("Touch = %v, want the store's error %q", err, p9Refused)
	}
}

package authweb_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	authweb "github.com/vardhan/internaltools/internal/kernel/auth/web"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/webtest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/types"
)

// P9 (CD-223): sessions are committed only when they change, and the
// middleware touches last_seen_at at most once a minute. Everything here runs
// on the real SessionStore with auth.NewSessionManager's settings, through
// Server.Middleware. Overlaps are forced: a held handler or a held write
// transaction, released by the test; every wait has p9Deadline.

const (
	p9Deadline = 5 * time.Second
	p9Rounds   = 40
	p9Stale    = 2 * time.Minute // past the touch interval
	p9DraftKey = "mst_wizard:Employee:1"
	p9Refused  = "p9 refused the update"
	p9UA1      = "Firefox on Linux (p9-1)"
	p9UA2      = "Safari on iPhone (p9-2)"
	p9IP1      = "203.0.113.7"
	p9IP2      = "203.0.113.8"
)

// p9Env is t10Harness with the routes the overlap tests need:
//
//	GET  /p9/hold    read-only; signals entered, waits for release, then writes
//	GET  /p9/value   the session string at ?key=
//	POST /p9/put     stores form value at form key
//	POST /p9/flash   adds a flash with form text
//	GET  /p9/flash   takes the flashes and writes their text
type p9Env struct {
	w       *authtest.World
	h       *webtest.Harness
	entered chan struct{}
	release chan struct{}
}

func p9New(t *testing.T, opts ...webtest.Option) *p9Env {
	t.Helper()
	e := &p9Env{w: authtest.NewWorld(t), entered: make(chan struct{}), release: make(chan struct{})}
	routes := webtest.WithRoutes(func(r chi.Router, s *authweb.Server) {
		r.Method(http.MethodGet, "/p9/hold", s.Self(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			select {
			case e.entered <- struct{}{}:
			case <-time.After(p9Deadline):
				return
			}
			select {
			case <-e.release:
			case <-time.After(p9Deadline):
			}
			_, _ = rw.Write([]byte("HELD"))
		})))
		r.Method(http.MethodGet, "/p9/value", s.Self(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			_, _ = rw.Write([]byte(e.h.SM.GetString(r.Context(), r.URL.Query().Get("key")))) //nolint:gosec // test-only route; the test reads this back
		})))
		r.Method(http.MethodPost, "/p9/put", s.Self(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			e.h.SM.Put(r.Context(), r.PostFormValue("key"), r.PostFormValue("value"))
			_, _ = rw.Write([]byte("PUT"))
		})))
		r.Method(http.MethodPost, "/p9/flash", s.Self(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			web.Flash(r.Context(), "info", r.PostFormValue("text"))
			_, _ = rw.Write([]byte("FLASHED"))
		})))
		r.Method(http.MethodGet, "/p9/flash", s.Self(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			var text []string
			for _, n := range web.TakeFlash(r.Context()) {
				text = append(text, n.Text)
			}
			_, _ = rw.Write([]byte(strings.Join(text, "|")))
		})))
	})
	e.h, _ = t10Harness(t, e.w, append(opts, routes)...)
	return e
}

// p9Reply is one raw response. Raw requests are safe off the test goroutine,
// where webtest.Client's t.Fatalf is not, and they replay a cookie the jar has
// dropped.
type p9Reply struct {
	status     int
	body       string
	setCookies []string
	err        error
}

func (e *p9Env) send(path, cookie string, header map[string]string) p9Reply {
	ctx, cancel := context.WithTimeout(context.Background(), 2*p9Deadline)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.h.HTTP.URL+path, nil)
	if err != nil {
		return p9Reply{err: err}
	}
	req.AddCookie(&http.Cookie{Name: e.h.SM.Cookie.Name, Value: cookie}) //nolint:gosec // client-side request cookie
	for k, v := range header {
		req.Header.Set(k, v)
	}
	hc := *e.h.HTTP.Client()
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := hc.Do(req)
	if err != nil {
		return p9Reply{err: err}
	}
	defer func() { _ = resp.Body.Close() }()
	var b bytes.Buffer
	if _, err := b.ReadFrom(resp.Body); err != nil {
		return p9Reply{err: err}
	}
	return p9Reply{status: resp.StatusCode, body: b.String(), setCookies: resp.Header.Values("Set-Cookie")}
}

// p9Recv receives from ch within p9Deadline.
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

func p9Send(t *testing.T, what string, ch chan<- struct{}) {
	t.Helper()
	select {
	case ch <- struct{}{}:
	case <-time.After(p9Deadline):
		t.Fatalf("%s: nobody took it within %s", what, p9Deadline)
	}
}

// p9WantReply fails unless r is status with body.
func p9WantReply(t *testing.T, what string, r p9Reply, status int, body string) {
	t.Helper()
	if r.err != nil {
		t.Fatalf("%s: %v", what, r.err)
	}
	if r.status != status || r.body != body {
		t.Fatalf("%s: status %d body %q, want %d %q", what, r.status, r.body, status, body)
	}
}

// p9Row is one sessions row.
type p9Row struct {
	data                          []byte
	created, lastSeen, ip, userAg string
}

func p9Session(t *testing.T, w *authtest.World, hash string) p9Row {
	t.Helper()
	var r p9Row
	err := w.DB.SQL().QueryRowContext(context.Background(),
		`SELECT data, created_at, last_seen_at, ip, user_agent FROM sessions WHERE token_hash = ?`, hash).
		Scan(&r.data, &r.created, &r.lastSeen, &r.ip, &r.userAg)
	if err != nil {
		t.Fatalf("read session %s: %v", hash, err)
	}
	return r
}

func p9Rows(t *testing.T, w *authtest.World, hash string) int {
	t.Helper()
	return t11Int(t, w, `SELECT count(*) FROM sessions WHERE token_hash = ?`, hash)
}

func p9Exec(t *testing.T, w *authtest.World, query string, args ...any) {
	t.Helper()
	ctx := context.Background()
	if err := w.DB.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, query, args...)
		return err
	}); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

// p9LogWrites counts every row written to sessions, by whoever wrote it: the
// real store runs unchanged and the trigger is the witness.
func p9LogWrites(t *testing.T, w *authtest.World) {
	t.Helper()
	p9Exec(t, w, `CREATE TABLE p9_writes (id INTEGER PRIMARY KEY, kind TEXT NOT NULL)`)
	p9Exec(t, w, `CREATE TRIGGER p9_log_insert AFTER INSERT ON sessions BEGIN INSERT INTO p9_writes (kind) VALUES ('insert'); END`)
	p9Exec(t, w, `CREATE TRIGGER p9_log_update AFTER UPDATE ON sessions BEGIN INSERT INTO p9_writes (kind) VALUES ('update'); END`)
	p9Exec(t, w, `CREATE TRIGGER p9_log_delete AFTER DELETE ON sessions BEGIN INSERT INTO p9_writes (kind) VALUES ('delete'); END`)
}

func p9Writes(t *testing.T, w *authtest.World, kind string) int {
	t.Helper()
	return t11Int(t, w, `SELECT count(*) FROM p9_writes WHERE kind = ?`, kind)
}

// p9Probe is a write probe on its own connection: PRAGMA data_version changes
// when another connection commits to the database.
func p9Probe(t *testing.T, w *authtest.World) func() int64 {
	t.Helper()
	ctx := context.Background()
	conn, err := w.DB.SQL().Conn(ctx)
	if err != nil {
		t.Fatalf("probe connection: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return func() int64 {
		t.Helper()
		var v int64
		if err := conn.QueryRowContext(ctx, `PRAGMA data_version`).Scan(&v); err != nil {
			t.Fatalf("PRAGMA data_version: %v", err)
		}
		return v
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

// p9TouchWriters counts goroutines that are inside a db.Write called from
// SessionStore.Touch. While the test holds the write lock such a goroutine has
// done Touch's read and is parked on BEGIN IMMEDIATE.
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

// p9WaitTouchWriters returns once n goroutines are parked in Touch's write.
func p9WaitTouchWriters(t *testing.T, n int) {
	t.Helper()
	deadline := time.After(p9Deadline)
	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	for p9TouchWriters() < n {
		select {
		case <-tick.C:
		case <-deadline:
			t.Fatalf("%d of %d requests reached Touch's db.Write within %s", p9TouchWriters(), n, p9Deadline)
		}
	}
}

// (a) A read-only request holds the session it loaded while another request
// changes the session; the read-only request's end loses nothing. A stale
// request also touches, which must not put its loaded blob back either.
func TestTouch_OverlapKeepsConcurrentWrites(t *testing.T) {
	writes := []struct {
		name  string
		write func(c *webtest.Client, round int) (want string, r webtest.Response)
		read  func(c *webtest.Client) webtest.Response
	}{
		{
			name: "session value",
			write: func(c *webtest.Client, round int) (string, webtest.Response) {
				v := fmt.Sprintf("value-%d", round)
				return v, c.Post("/p9/put", url.Values{"key": {"p9"}, "value": {v}})
			},
			read: func(c *webtest.Client) webtest.Response { return c.Get("/p9/value?key=p9") },
		},
		{
			name: "wizard draft",
			write: func(c *webtest.Client, round int) (string, webtest.Response) {
				v := fmt.Sprintf("draft-%d-%s", round, strings.Repeat("d", 8<<10))
				return v, c.Post("/p9/put", url.Values{"key": {p9DraftKey}, "value": {v}})
			},
			read: func(c *webtest.Client) webtest.Response {
				return c.Get("/p9/value?key=" + url.QueryEscape(p9DraftKey))
			},
		},
		{
			name: "flash",
			write: func(c *webtest.Client, round int) (string, webtest.Response) {
				v := fmt.Sprintf("Saved %d.", round)
				return v, c.Post("/p9/flash", url.Values{"text": {v}})
			},
			read: func(c *webtest.Client) webtest.Response { return c.Get("/p9/flash") },
		},
	}
	for _, wr := range writes {
		for _, stale := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s stale=%v", wr.name, stale), func(t *testing.T) {
				e := p9New(t)
				c := signedIn(t, e.h, t10Priya)
				raw := c.Cookie().Value

				lost := 0
				for round := range p9Rounds {
					if stale {
						e.w.Clock.Advance(p9Stale)
					}
					done := make(chan p9Reply, 1)
					go func() { done <- e.send("/p9/hold", raw, nil) }()
					p9Recv(t, "request A reaches its handler", e.entered)

					want, r := wr.write(c, round)
					wantStatus(t, "request B writes", r, http.StatusOK)

					p9Send(t, "release request A", e.release)
					p9WantReply(t, "request A", p9Recv(t, "request A ends", done), http.StatusOK, "HELD")

					got := wr.read(c)
					wantStatus(t, "read back", got, http.StatusOK)
					if got.Body != want {
						lost++
					}
				}
				if lost != 0 {
					t.Errorf("request B's write lost in %d of %d rounds", lost, p9Rounds)
				}
			})
		}
	}
}

// (b) A GET within the minute writes nothing at all.
func TestTouch_WithinMinuteWritesNothing(t *testing.T) {
	e := p9New(t)
	c := signedIn(t, e.h, t10Priya)
	hash := t16Session(t, "after sign-in", c)
	p9LogWrites(t, e.w)
	before := p9Session(t, e.w, hash)
	probe := p9Probe(t, e.w)
	version := probe()

	for _, at := range []time.Duration{0, time.Second, 30 * time.Second, time.Minute - time.Millisecond} {
		e.w.Clock.Set(authtest.T0.Add(at))
		r := c.Get("/")
		wantStatus(t, fmt.Sprintf("GET / at T0+%s", at), r, http.StatusOK)
		if r.Body != "HOME" {
			t.Fatalf("GET / at T0+%s: body %q, want HOME", at, r.Body)
		}
		after := p9Session(t, e.w, hash)
		if !bytes.Equal(after.data, before.data) {
			t.Errorf("at T0+%s: session data rewrote from %x to %x", at, before.data, after.data)
		}
		if after.lastSeen != before.lastSeen {
			t.Errorf("at T0+%s: last_seen_at = %q, want %q", at, after.lastSeen, before.lastSeen)
		}
		if got := probe(); got != version {
			t.Errorf("at T0+%s: PRAGMA data_version moved from %d to %d: the GET wrote to the database", at, version, got)
			version = got
		}
		for _, kind := range []string{"insert", "update", "delete"} {
			if n := p9Writes(t, e.w, kind); n != 0 {
				t.Errorf("at T0+%s: %d %s on sessions, want 0", at, n, kind)
			}
		}
	}

	// The probe does see a commit, so the checks above can fail.
	p9Exec(t, e.w, `INSERT INTO p9_writes (kind) VALUES ('control')`)
	if got := probe(); got == version {
		t.Errorf("PRAGMA data_version stayed %d after a commit: the probe is blind", got)
	}
}

// (c) A GET after the minute moves last_seen_at, ip and user_agent and leaves
// the rest of the row byte-identical.
func TestTouch_AfterMinuteMovesSeenIPAgent(t *testing.T) {
	e := p9New(t, webtest.WithTrustProxy())
	first := e.h.Client().WithHeader("User-Agent", p9UA1).WithHeader("X-Forwarded-For", p9IP1)
	e.w.SetPassword(t, t10Priya, t10Password)
	t12LoginOK(t, "sign in", first.Login(t10Priya, t10Password))
	hash := t16Session(t, "after sign-in", first)
	before := p9Session(t, e.w, hash)
	if before.ip != p9IP1 || before.userAg != p9UA1 {
		t.Fatalf("after sign-in: ip %q, user_agent %q, want %q, %q", before.ip, before.userAg, p9IP1, p9UA1)
	}
	p9LogWrites(t, e.w)

	moved := first.WithHeader("User-Agent", p9UA2).WithHeader("X-Forwarded-For", p9IP2)
	at := authtest.T0.Add(61 * time.Second)
	e.w.Clock.Set(at)
	wantStatus(t, "GET / at T0+61s", moved.Get("/"), http.StatusOK)

	after := p9Session(t, e.w, hash)
	want := p9Row{data: before.data, created: before.created, lastSeen: auth.FormatTime(at), ip: p9IP2, userAg: p9UA2}
	if !bytes.Equal(after.data, want.data) {
		t.Errorf("data = %x, want byte-identical %x", after.data, want.data)
	}
	if after.created != want.created || after.lastSeen != want.lastSeen || after.ip != want.ip || after.userAg != want.userAg {
		t.Errorf("session row = %+v, want %+v", after, want)
	}
	if n := p9Writes(t, e.w, "update"); n != 1 {
		t.Errorf("updates on sessions = %d, want 1", n)
	}
}

// (c) The pinned boundary. A session last seen exactly one minute ago is not
// moved: the plan's UPDATE is `last_seen_at < cutoff` with cutoff = now - 1
// minute. One millisecond later (the stored precision) it is moved.
func TestTouch_ExactlyOneMinuteIsNotMoved(t *testing.T) {
	e := p9New(t, webtest.WithTrustProxy())
	first := e.h.Client().WithHeader("User-Agent", p9UA1).WithHeader("X-Forwarded-For", p9IP1)
	e.w.SetPassword(t, t10Priya, t10Password)
	t12LoginOK(t, "sign in", first.Login(t10Priya, t10Password))
	hash := t16Session(t, "after sign-in", first)
	p9LogWrites(t, e.w)
	moved := first.WithHeader("User-Agent", p9UA2).WithHeader("X-Forwarded-For", p9IP2)

	e.w.Clock.Set(authtest.T0.Add(time.Minute))
	wantStatus(t, "GET / at exactly T0+1m", moved.Get("/"), http.StatusOK)
	got := p9Session(t, e.w, hash)
	if got.lastSeen != auth.FormatTime(authtest.T0) || got.ip != p9IP1 || got.userAg != p9UA1 {
		t.Errorf("at exactly one minute: last_seen_at %q, ip %q, user_agent %q; want the sign-in's %q, %q, %q",
			got.lastSeen, got.ip, got.userAg, auth.FormatTime(authtest.T0), p9IP1, p9UA1)
	}
	if n := p9Writes(t, e.w, "update"); n != 0 {
		t.Errorf("at exactly one minute: updates = %d, want 0", n)
	}

	at := authtest.T0.Add(time.Minute + time.Millisecond)
	e.w.Clock.Set(at)
	wantStatus(t, "GET / at T0+1m0.001s", moved.Get("/"), http.StatusOK)
	got = p9Session(t, e.w, hash)
	if got.lastSeen != auth.FormatTime(at) || got.ip != p9IP2 || got.userAg != p9UA2 {
		t.Errorf("one millisecond past the minute: last_seen_at %q, ip %q, user_agent %q; want %q, %q, %q",
			got.lastSeen, got.ip, got.userAg, auth.FormatTime(at), p9IP2, p9UA2)
	}
	if n := p9Writes(t, e.w, "update"); n != 1 {
		t.Errorf("one millisecond past the minute: updates = %d, want 1", n)
	}
}

// (c) A guest session holds values too, so it is touched like any other.
func TestTouch_GuestSessionIsTouched(t *testing.T) {
	e := p9New(t)
	c := e.h.Client()
	wantStatus(t, "GET /signin", c.Get("/signin"), http.StatusOK)
	hash := t16Session(t, "guest", c)
	before := p9Session(t, e.w, hash)

	at := authtest.T0.Add(p9Stale)
	e.w.Clock.Set(at)
	r := c.Get("/signin")
	wantStatus(t, "GET /signin again", r, http.StatusOK)
	after := p9Session(t, e.w, hash)
	if after.lastSeen != auth.FormatTime(at) {
		t.Errorf("guest last_seen_at = %q, want %q", after.lastSeen, auth.FormatTime(at))
	}
	if !bytes.Equal(after.data, before.data) {
		t.Errorf("guest data = %x, want byte-identical %x", after.data, before.data)
	}
}

// A real commit still sets last_seen_at, inside the minute too (CD-223).
func TestTouch_RealCommitSetsLastSeen(t *testing.T) {
	e := p9New(t)
	c := signedIn(t, e.h, t10Priya)
	hash := t16Session(t, "after sign-in", c)

	at := authtest.T0.Add(10 * time.Second)
	e.w.Clock.Set(at)
	wantStatus(t, "POST /p9/put", c.Post("/p9/put", url.Values{"key": {"p9"}, "value": {"kept"}}), http.StatusOK)
	if got := p9Session(t, e.w, hash).lastSeen; got != auth.FormatTime(at) {
		t.Errorf("last_seen_at after a real commit = %q, want %q", got, auth.FormatTime(at))
	}
	if r := c.Get("/p9/value?key=p9"); r.Body != "kept" {
		t.Errorf("value after the commit = %q, want kept", r.Body)
	}
}

// p9WantSignedOut asserts raw is refused the way an expired session is, and
// that its row is gone.
func p9WantSignedOut(t *testing.T, e *p9Env, what, raw string) {
	t.Helper()
	r := e.send("/", raw, map[string]string{"HX-Request": "true"})
	if r.err != nil {
		t.Fatalf("%s: %v", what, r.err)
	}
	if r.status != http.StatusUnauthorized {
		t.Errorf("%s: HTMX GET / status %d, want 401", what, r.status)
	}
	if strings.Contains(r.body, "HOME") {
		t.Errorf("%s: refused request reached the handler: %q", what, r.body)
	}
	if n := p9Rows(t, e.w, auth.HashToken(raw)); n != 0 {
		t.Errorf("%s: session rows = %d, want 0", what, n)
	}
}

// (d) Used every 11 h, a session outlives 12 h; each use is a touch.
func TestTouch_UsedEvery11HoursStaysAlive(t *testing.T) {
	e := p9New(t)
	c := signedIn(t, e.h, t10Priya)
	hash := t16Session(t, "after sign-in", c)

	for i := 1; i <= 4; i++ {
		at := authtest.T0.Add(time.Duration(i*11) * time.Hour)
		e.w.Clock.Set(at)
		r := c.Get("/")
		wantStatus(t, fmt.Sprintf("GET / at T0+%dh", i*11), r, http.StatusOK)
		if r.Body != "HOME" {
			t.Fatalf("GET / at T0+%dh: body %q, want HOME", i*11, r.Body)
		}
		if got := p9Session(t, e.w, hash).lastSeen; got != auth.FormatTime(at) {
			t.Fatalf("at T0+%dh: last_seen_at = %q, want %q", i*11, got, auth.FormatTime(at))
		}
	}
}

// (d) AUTH-037's boundaries on the real path: exactly 12 h past the last touch
// is accepted, and past it is refused and the row deleted.
func TestTouch_IdleBoundaryFromLastTouch(t *testing.T) {
	for _, tc := range []struct {
		name   string
		past   time.Duration
		status int
	}{
		{"exactly 12 h", 0, http.StatusOK},
		{"12 h 1 ms", time.Millisecond, http.StatusUnauthorized},
		{"12 h 1 s", time.Second, http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := p9New(t)
			c := signedIn(t, e.h, t10Priya)
			raw := c.Cookie().Value

			touched := authtest.T0.Add(5 * time.Hour)
			e.w.Clock.Set(touched)
			wantStatus(t, "the touch", c.Get("/"), http.StatusOK)

			e.w.Clock.Set(touched.Add(12*time.Hour + tc.past))
			r := c.HTMX().Get("/")
			wantStatus(t, "after the idle gap", r, tc.status)
			if tc.status == http.StatusOK {
				if n := p9Rows(t, e.w, auth.HashToken(raw)); n != 1 {
					t.Errorf("session rows = %d, want 1", n)
				}
				return
			}
			if !strings.Contains(messageOf(r), t16Expired) {
				t.Errorf("body %q, HX-Trigger %q, want %q", r.Body, r.Header.Get("HX-Trigger"), t16Expired)
			}
			if n := p9Rows(t, e.w, auth.HashToken(raw)); n != 0 {
				t.Errorf("expired session rows = %d, want 0 (deleted)", n)
			}
		})
	}
}

// (d) A plain browser GET of an idle-expired session goes to sign-in (CD-17).
func TestTouch_IdleExpiredBrowserGet(t *testing.T) {
	e := p9New(t)
	c := signedIn(t, e.h, t10Priya)
	hash := t16Session(t, "after sign-in", c)
	p9LogWrites(t, e.w)

	e.w.Clock.Set(authtest.T0.Add(12*time.Hour + time.Second))
	wantRedirect(t, "GET / after 12 h 1 s", c.Get("/"), http.StatusSeeOther, "/signin?next=%2F")
	if n := p9Rows(t, e.w, hash); n != 0 {
		t.Errorf("expired session rows = %d, want 0", n)
	}
	if n := p9Writes(t, e.w, "insert"); n != 0 {
		t.Errorf("inserts on sessions = %d, want 0", n)
	}
	if n := p9Writes(t, e.w, "update"); n != 0 {
		t.Errorf("updates on sessions = %d, want 0: an expired session is not touched", n)
	}
}

// (d) Accepted in CD-223: idle time runs from the last touch, so a request
// inside the minute after it does not extend the limit.
func TestTouch_IdleRunsFromTouchNotLastRequest(t *testing.T) {
	e := p9New(t)
	c := signedIn(t, e.h, t10Priya)
	raw := c.Cookie().Value

	touched := authtest.T0.Add(time.Hour)
	e.w.Clock.Set(touched)
	wantStatus(t, "the touch", c.Get("/"), http.StatusOK)
	e.w.Clock.Set(touched.Add(59 * time.Second))
	wantStatus(t, "a request inside the minute", c.Get("/"), http.StatusOK)

	e.w.Clock.Set(touched.Add(12*time.Hour + time.Millisecond))
	p9WantSignedOut(t, e, "12 h 1 ms past the touch", raw)
}

// (e) Touching does not extend the 7 day limit.
func TestTouch_AbsoluteLimitUnaffected(t *testing.T) {
	e := p9New(t)
	c := signedIn(t, e.h, t10Priya)
	raw := c.Cookie().Value
	hash := auth.HashToken(raw)

	for i := 1; i*11 < 7*24; i++ {
		e.w.Clock.Set(authtest.T0.Add(time.Duration(i*11) * time.Hour))
		wantStatus(t, fmt.Sprintf("GET / at T0+%dh", i*11), c.Get("/"), http.StatusOK)
	}
	e.w.Clock.Set(authtest.T0.Add(7 * 24 * time.Hour))
	wantStatus(t, "GET / at exactly 7 days", c.Get("/"), http.StatusOK)
	if got := p9Session(t, e.w, hash).created; got != auth.FormatTime(authtest.T0) {
		t.Errorf("created_at after touches = %q, want %q (unchanged)", got, auth.FormatTime(authtest.T0))
	}

	e.w.Clock.Set(authtest.T0.Add(7*24*time.Hour + time.Millisecond))
	p9WantSignedOut(t, e, "7 days 1 ms after sign-in, last used 1 ms ago", raw)
}

// (f) My Sessions shows the touched time, which is the last touch and not the
// last request, and marks the right session as current.
func TestTouch_MySessionsShowsTouchedTime(t *testing.T) {
	e := p9New(t)
	e.w.SetPassword(t, t10Priya, t10Password)
	a := e.h.Client().WithHeader("User-Agent", t16Chrome)
	t12LoginOK(t, "priya on Chrome", a.Login(t10Priya, t10Password))
	b := e.h.Client().WithHeader("User-Agent", t16Safari)
	t12LoginOK(t, "priya on Safari", b.Login(t10Priya, t10Password))

	// 10:35 IST: Safari is used. 10:39:30: Chrome is touched. 10:40:10: Chrome
	// opens the page, 40 s after its touch, so its row still says 10:39.
	safariUsed := authtest.T0.Add(5 * time.Minute)
	e.w.Clock.Set(safariUsed)
	wantStatus(t, "Safari GET /", b.Get("/"), http.StatusOK)
	chromeUsed := authtest.T0.Add(9*time.Minute + 30*time.Second)
	e.w.Clock.Set(chromeUsed)
	wantStatus(t, "Chrome GET /", a.Get("/"), http.StatusOK)
	e.w.Clock.Set(authtest.T0.Add(10*time.Minute + 10*time.Second))

	page := a.Get("/account/sessions")
	wantStatus(t, "GET /account/sessions", page, http.StatusOK)
	chrome, safari := t16Row(page.Body, t16Chrome), t16Row(page.Body, t16Safari)
	if chrome == "" || safari == "" {
		t.Fatalf("sessions page lacks a row: %q", page.Body)
	}
	if want := types.FormatIST(chromeUsed); !strings.Contains(chrome, want) {
		t.Errorf("Chrome's row does not show its touch %q: %q", want, chrome)
	}
	if notNow := types.FormatIST(authtest.T0.Add(10 * time.Minute)); strings.Contains(chrome, notNow) {
		t.Errorf("Chrome's row shows %q, a request inside the minute after its touch: %q", notNow, chrome)
	}
	if want := types.FormatIST(safariUsed); !strings.Contains(safari, want) {
		t.Errorf("Safari's row does not show its touch %q: %q", want, safari)
	}
	if !strings.Contains(chrome, "This session") || strings.Contains(chrome, "End session") {
		t.Errorf("Chrome's row is not the current session: %q", chrome)
	}
	if strings.Contains(safari, "This session") || !strings.Contains(safari, "End session") {
		t.Errorf("Safari's row should be another session: %q", safari)
	}

	// The page's own request is the touch when the session is stale.
	opened := authtest.T0.Add(15 * time.Minute)
	e.w.Clock.Set(opened)
	page = b.Get("/account/sessions")
	wantStatus(t, "Safari GET /account/sessions", page, http.StatusOK)
	safari = t16Row(page.Body, t16Safari)
	if want := types.FormatIST(opened); !strings.Contains(safari, want) {
		t.Errorf("Safari's row does not show this request's touch %q: %q", want, safari)
	}
	if !strings.Contains(safari, "This session") {
		t.Errorf("Safari's row, from Safari, is not the current session: %q", safari)
	}
}

// (g) However a session is ended, a stale replay of its cookie brings nothing
// back. Each ending goes through its real route.
func TestTouch_RevokedStaysGone(t *testing.T) {
	for _, tc := range []struct {
		name string
		end  func(t *testing.T, e *p9Env, a, b *webtest.Client)
	}{
		{"End session", func(t *testing.T, e *p9Env, a, b *webtest.Client) {
			priya := e.w.Users[t10Priya]
			id, ok := t16SessionIDs(t, e.w, priya)[t16Session(t, "B", b)]
			if !ok {
				t.Fatal("no row for session B")
			}
			t12Changed(t, "End session", a.Post(fmt.Sprintf("/account/sessions/%d/revoke", id), url.Values{}))
		}},
		{"End all other sessions", func(t *testing.T, _ *p9Env, a, _ *webtest.Client) {
			t12Changed(t, "End all other sessions", a.Post("/account/sessions/revoke-others", url.Values{}))
		}},
		{"sign-out", func(t *testing.T, _ *p9Env, _, b *webtest.Client) {
			wantRedirect(t, "sign-out", b.Post("/signout", url.Values{}), http.StatusSeeOther, "/signin")
		}},
		{"password change", func(t *testing.T, _ *p9Env, a, _ *webtest.Client) {
			t12Changed(t, "password change", a.Post("/account/password", t12ChangeForm(t10Password, t12New, t12New)))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := p9New(t)
			a := signedIn(t, e.h, t10Priya)
			b := e.h.Client()
			t12LoginOK(t, "session B", b.Login(t10Priya, t10Password))
			rawB := b.Cookie().Value
			p9LogWrites(t, e.w)

			tc.end(t, e, a, b)
			if n := p9Rows(t, e.w, auth.HashToken(rawB)); n != 0 {
				t.Fatalf("after the ending: rows = %d, want 0", n)
			}
			e.w.Clock.Advance(p9Stale)
			inserts := p9Writes(t, e.w, "insert")
			p9WantSignedOut(t, e, "first replay", rawB)
			e.w.Clock.Advance(p9Stale)
			p9WantSignedOut(t, e, "second replay", rawB)
			if n := p9Writes(t, e.w, "insert") - inserts; n != 0 {
				t.Errorf("inserts during the replays = %d, want 0", n)
			}
		})
	}
}

// (g) The race: a stale request has done Touch's read and waits on the write
// lock while a revoke holds it. The touch finds no row, and none comes back.
func TestTouch_RacingRevokeLeavesNoRow(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		revoke func(e *p9Env, st *auth.SessionStore, hash string) func(tx *db.Tx) error
	}{
		{"RevokeAll", func(e *p9Env, st *auth.SessionStore, _ string) func(tx *db.Tx) error {
			return func(tx *db.Tx) error { return st.RevokeAll(ctx, tx, e.w.Users[t10Priya], "") }
		}},
		{"row deleted by hash", func(_ *p9Env, _ *auth.SessionStore, hash string) func(tx *db.Tx) error {
			return func(tx *db.Tx) error {
				_, err := tx.SQL().ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hash)
				return err
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := p9New(t)
			c := signedIn(t, e.h, t10Priya)
			raw := c.Cookie().Value
			hash := auth.HashToken(raw)
			p9LogWrites(t, e.w)
			e.w.Clock.Advance(p9Stale)

			cfg := auth.DefaultSessionConfig(false)
			st := auth.NewSessionStore(e.w.DB, e.w.Clock, cfg)
			lock := p9HoldWrite(t, e.w, tc.revoke(e, st, hash))
			done := make(chan p9Reply, 1)
			go func() { done <- e.send("/", raw, nil) }()
			p9WaitTouchWriters(t, 1)

			if err := lock.commit(); err != nil {
				t.Fatalf("revoke: %v", err)
			}
			// The session was live when the request loaded it, and a touch
			// that finds no row is not an error.
			p9WantReply(t, "the racing request", p9Recv(t, "the racing request ends", done), http.StatusOK, "HOME")

			if n := p9Rows(t, e.w, hash); n != 0 {
				t.Errorf("rows after the racing touch = %d, want 0", n)
			}
			if n := p9Writes(t, e.w, "insert"); n != 0 {
				t.Errorf("inserts = %d, want 0", n)
			}
			if n := p9Writes(t, e.w, "update"); n != 0 {
				t.Errorf("updates = %d, want 0", n)
			}
			p9WantSignedOut(t, e, "the next request", raw)
		})
	}
}

// (h) Two stale requests have both done Touch's read and wait on the write
// lock, so only the cutoff in the UPDATE's WHERE can stop the second write.
func TestTouch_TwoStaleRequestsMakeOneWrite(t *testing.T) {
	e := p9New(t, webtest.WithTrustProxy())
	c := signedIn(t, e.h, t10Priya)
	raw := c.Cookie().Value
	hash := auth.HashToken(raw)
	p9LogWrites(t, e.w)
	data := p9Session(t, e.w, hash).data

	for round := range 5 {
		e.w.Clock.Advance(p9Stale)
		updates := p9Writes(t, e.w, "update")
		lock := p9HoldWrite(t, e.w, nil)
		replies := make(chan p9Reply, 2)
		for _, ip := range []string{p9IP1, p9IP2} {
			go func() { replies <- e.send("/", raw, map[string]string{"X-Forwarded-For": ip}) }()
		}
		p9WaitTouchWriters(t, 2)
		if err := lock.commit(); err != nil {
			t.Fatalf("round %d: release the write lock: %v", round, err)
		}
		for range 2 {
			p9WantReply(t, "a stale request", p9Recv(t, "a stale request ends", replies), http.StatusOK, "HOME")
		}

		if n := p9Writes(t, e.w, "update") - updates; n != 1 {
			t.Errorf("round %d: writes from two stale requests = %d, want 1", round, n)
		}
		row := p9Session(t, e.w, hash)
		if row.lastSeen != auth.FormatTime(e.w.Clock.Now()) {
			t.Errorf("round %d: last_seen_at = %q, want %q", round, row.lastSeen, auth.FormatTime(e.w.Clock.Now()))
		}
		if !slices.Contains([]string{p9IP1, p9IP2}, row.ip) {
			t.Errorf("round %d: ip = %q, want one of the two requests'", round, row.ip)
		}
		if !bytes.Equal(row.data, data) {
			t.Errorf("round %d: data = %x, want byte-identical %x", round, row.data, data)
		}
	}
}

// (i) An unmodified GET sets no cookie, inside the minute or as a touch, and
// the cookie keeps working.
func TestTouch_UnmodifiedGetSetsNoCookie(t *testing.T) {
	e := p9New(t)
	c := signedIn(t, e.h, t10Priya)
	token := c.Cookie().Value

	for _, at := range []time.Duration{10 * time.Second, p9Stale, p9Stale + time.Second} {
		e.w.Clock.Set(authtest.T0.Add(at))
		r := c.Get("/")
		wantStatus(t, fmt.Sprintf("GET / at T0+%s", at), r, http.StatusOK)
		if got := r.Header.Values("Set-Cookie"); len(got) != 0 {
			t.Errorf("GET / at T0+%s: Set-Cookie = %q, want none", at, got)
		}
		if r.Body != "HOME" {
			t.Errorf("GET / at T0+%s: body %q, want HOME (the cookie still works)", at, r.Body)
		}
		if got := c.Cookie(); got == nil || got.Value != token {
			t.Errorf("GET / at T0+%s: jar cookie = %+v, want the sign-in token", at, got)
		}
	}
}

// (i) A real change sets the cookie, with the same token.
func TestTouch_ModifiedRequestSetsCookie(t *testing.T) {
	e := p9New(t)
	c := signedIn(t, e.h, t10Priya)
	token := c.Cookie().Value

	r := c.Post("/p9/put", url.Values{"key": {"p9"}, "value": {"v"}})
	wantStatus(t, "POST /p9/put", r, http.StatusOK)
	if got := r.Header.Values("Set-Cookie"); len(got) != 1 {
		t.Fatalf("POST /p9/put: Set-Cookie = %q, want one", got)
	}
	if got := c.Cookie(); got == nil || got.Value != token {
		t.Errorf("after a real change: cookie = %+v, want the same token", got)
	}
}

// (i) Sign-in still rotates the token, through the real route.
func TestTouch_SignInRotatesToken(t *testing.T) {
	e := p9New(t)
	e.w.SetPassword(t, t10Priya, t10Password)
	c := e.h.Client()
	page := c.Get("/signin")
	wantStatus(t, "GET /signin", page, http.StatusOK)
	guest := c.Cookie()
	if guest == nil {
		t.Fatal("no guest cookie from GET /signin")
	}
	in := c.NoCSRF().Post("/signin", url.Values{
		"email": {t10Priya}, "password": {t10Password}, "csrf_token": {t11FormCSRF(t, "GET /signin", page)},
	})
	wantRedirect(t, "POST /signin", in, http.StatusSeeOther, "/")

	signed := c.Cookie()
	if signed == nil || signed.Value == guest.Value {
		t.Fatalf("signed-in cookie = %+v, want a new token (guest %q)", signed, guest.Value)
	}
	if n := p9Rows(t, e.w, auth.HashToken(guest.Value)); n != 0 {
		t.Errorf("rows for the guest token = %d, want 0", n)
	}
	if n := t11Int(t, e.w, `SELECT count(*) FROM sessions WHERE token_hash = ? AND user_id = ?`,
		auth.HashToken(signed.Value), int64(e.w.Users[t10Priya])); n != 1 {
		t.Errorf("rows for the new token and priya = %d, want 1", n)
	}
	home := c.Get("/")
	wantStatus(t, "GET / after sign-in", home, http.StatusOK)
	if home.Body != "HOME" {
		t.Errorf("GET / after sign-in: body %q, want HOME", home.Body)
	}
	p9WantSignedOut(t, e, "the old guest token", guest.Value)
}

// (j) A failed touch must not fail a read-only page. The failure comes through
// the public path with no seam: a trigger makes SQLite refuse every UPDATE on
// sessions, so the real store's real UPDATE returns an error.
func TestTouch_FailureStillServesPage(t *testing.T) {
	logs := t16CaptureLogs(t)
	e := p9New(t)
	c := signedIn(t, e.h, t10Priya)
	raw := c.Cookie().Value
	hash := auth.HashToken(raw)
	before := p9Session(t, e.w, hash)
	p9Exec(t, e.w, fmt.Sprintf(`CREATE TRIGGER p9_refuse BEFORE UPDATE ON sessions BEGIN SELECT RAISE(ABORT, '%s'); END`, p9Refused))

	e.w.Clock.Advance(p9Stale)
	r := c.Get("/")
	wantStatus(t, "GET / with the touch failing", r, http.StatusOK)
	if r.Body != "HOME" {
		t.Errorf("GET / with the touch failing: body %q, want HOME", r.Body)
	}
	if got := r.Header.Values("Set-Cookie"); len(got) != 0 {
		t.Errorf("GET / with the touch failing: Set-Cookie = %q, want none", got)
	}
	after := p9Session(t, e.w, hash)
	if after.lastSeen != before.lastSeen || !bytes.Equal(after.data, before.data) {
		t.Errorf("session row changed by a refused touch: %+v, was %+v", after, before)
	}
	out := logs.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, p9Refused) {
		t.Errorf("log = %q, want a WARN carrying the store's error %q", out, p9Refused)
	}
	if strings.Contains(out, raw) {
		t.Error("the log contains the raw session token")
	}

	// The next good request recovers.
	p9Exec(t, e.w, `DROP TRIGGER p9_refuse`)
	wantStatus(t, "GET / after the failure clears", c.Get("/"), http.StatusOK)
	if got := p9Session(t, e.w, hash).lastSeen; got != auth.FormatTime(e.w.Clock.Now()) {
		t.Errorf("last_seen_at after recovery = %q, want %q", got, auth.FormatTime(e.w.Clock.Now()))
	}
}

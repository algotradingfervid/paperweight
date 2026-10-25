package auth_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// sessionEnv is a World with a SessionStore and session manager served by
// httptest. Routes: /login?uid=N Puts uid; / writes the uid it sees;
// /block signals entered, waits for release, then writes the uid.
type sessionEnv struct {
	w       *authtest.World
	st      *auth.SessionStore
	sm      *scs.SessionManager
	srv     *httptest.Server
	entered chan struct{}
	release chan struct{}
}

const sessionCookie = "session" // cookie name over HTTP

func newSessionEnv(t *testing.T) *sessionEnv {
	t.Helper()
	w := authtest.NewWorld(t)
	cfg := auth.DefaultSessionConfig(false)
	st := auth.NewSessionStore(w.DB, w.Clock, cfg)
	sm := auth.NewSessionManager(st, cfg)
	e := &sessionEnv{w: w, st: st, sm: sm, entered: make(chan struct{}), release: make(chan struct{})}

	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(rw http.ResponseWriter, r *http.Request) {
		uid, err := strconv.ParseInt(r.URL.Query().Get("uid"), 10, 64)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusBadRequest)
			return
		}
		sm.Put(r.Context(), auth.SessionKeyUser, uid)
		_, _ = io.WriteString(rw, strconv.FormatInt(uid, 10))
	})
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(rw, strconv.FormatInt(sm.GetInt64(r.Context(), auth.SessionKeyUser), 10))
	})
	mux.HandleFunc("/block", func(rw http.ResponseWriter, r *http.Request) {
		sm.Put(r.Context(), auth.SessionKeyCSRF, "csrf-auth-9")
		e.entered <- struct{}{}
		<-e.release
		_, _ = io.WriteString(rw, strconv.FormatInt(sm.GetInt64(r.Context(), auth.SessionKeyUser), 10))
	})

	// The wrapper outside LoadAndSave sets the request meta the store reads.
	inner := sm.LoadAndSave(mux)
	h := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		presented := ""
		if c, err := r.Cookie(sessionCookie); err == nil {
			presented = auth.HashToken(c.Value)
		}
		ctx := auth.WithRequestMeta(r.Context(), presented, r.Header.Get("X-Test-IP"), r.UserAgent())
		inner.ServeHTTP(rw, r.WithContext(ctx))
	})
	e.srv = httptest.NewServer(h)
	t.Cleanup(e.srv.Close)
	return e
}

// get requests path with the cookie value (none when ""), and returns the
// body and the session cookie value the response set ("" when none).
func (e *sessionEnv) get(t *testing.T, path, cookie, ip string) (body, setCookie string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, e.srv.URL+path, nil)
	if err != nil {
		t.Fatalf("NewRequest(%s): %v", path, err)
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie}) //nolint:gosec // client-side request cookie
	}
	req.Header.Set("X-Test-IP", ip)
	req.Header.Set("User-Agent", "agent-"+ip)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("GET %s: read body: %v", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d, body %q", path, resp.StatusCode, b)
	}
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookie {
			setCookie = c.Value
		}
	}
	return string(b), setCookie
}

// login signs u in and returns the raw cookie value.
func (e *sessionEnv) login(t *testing.T, u auth.UserID, ip string) string {
	t.Helper()
	body, c := e.get(t, fmt.Sprintf("/login?uid=%d", u), "", ip)
	if c == "" {
		t.Fatalf("login(%d): no %q cookie set", u, sessionCookie)
	}
	if body != strconv.FormatInt(int64(u), 10) {
		t.Fatalf("login(%d): body %q", u, body)
	}
	return c
}

// uidSeen returns the uid the server sees with cookie.
func (e *sessionEnv) uidSeen(t *testing.T, cookie string) int64 {
	t.Helper()
	body, _ := e.get(t, "/", cookie, "10.0.0.1")
	n, err := strconv.ParseInt(body, 10, 64)
	if err != nil {
		t.Fatalf("uid body %q: %v", body, err)
	}
	return n
}

func (e *sessionEnv) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	ctx := context.Background()
	if err := e.w.DB.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, query, args...).Scan(&n)
	}); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}

func (e *sessionEnv) str(t *testing.T, query string, args ...any) string {
	t.Helper()
	var s string
	ctx := context.Background()
	if err := e.w.DB.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, query, args...).Scan(&s)
	}); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return s
}

// AUTH-035
func TestSession_TokenHashedAtRest(t *testing.T) {
	e := newSessionEnv(t)
	priya := e.w.Users["priya@vardhan.test"]
	raw := e.login(t, priya, "10.0.0.4")

	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("cookie %q is not base64url: %v", raw, err)
	}
	if len(b) != 32 {
		t.Errorf("cookie decodes to %d bytes, want 32", len(b))
	}

	sum := sha256.Sum256([]byte(raw))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if got := auth.HashToken(raw); got != want {
		t.Errorf("HashToken(raw) = %q, want base64url(SHA-256(raw)) %q", got, want)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM sessions`); n != 1 {
		t.Fatalf("sessions rows = %d, want 1", n)
	}
	if got := e.str(t, `SELECT token_hash FROM sessions WHERE user_id = ?`, int64(priya)); got != want {
		t.Errorf("sessions.token_hash = %q, want %q", got, want)
	}

	// No TEXT or BLOB column of any table holds the raw token.
	ctx := context.Background()
	type col struct{ table, name string }
	var cols []col
	if err := e.w.DB.Read(ctx, func(tx *db.Tx) error {
		rows, err := tx.SQL().QueryContext(ctx,
			`SELECT m.name, p.name FROM sqlite_master m, pragma_table_info(m.name) p
			 WHERE m.type = 'table' AND (upper(p.type) = 'TEXT' OR upper(p.type) = 'BLOB')`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var c col
			if err := rows.Scan(&c.table, &c.name); err != nil {
				return err
			}
			cols = append(cols, c)
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("list columns: %v", err)
	}
	if !slices.Contains(cols, col{"sessions", "token_hash"}) || !slices.Contains(cols, col{"sessions", "data"}) {
		t.Fatalf("column scan missed sessions.token_hash or sessions.data: %v", cols)
	}
	for _, c := range cols {
		q := fmt.Sprintf(`SELECT COUNT(*) FROM "%s" WHERE instr(CAST("%s" AS BLOB), CAST(? AS BLOB)) > 0`, c.table, c.name)
		if n := e.count(t, q, raw); n != 0 {
			t.Errorf("%s.%s holds the raw token in %d rows", c.table, c.name, n)
		}
	}
}

// AUTH-036
func TestSession_HashNotReplayable(t *testing.T) {
	e := newSessionEnv(t)
	priya := e.w.Users["priya@vardhan.test"]
	raw := e.login(t, priya, "10.0.0.4")

	if got := e.uidSeen(t, raw); got != int64(priya) {
		t.Errorf("raw cookie: uid = %d, want %d", got, priya)
	}
	if got := e.uidSeen(t, auth.HashToken(raw)); got != 0 {
		t.Errorf("hash as cookie: uid = %d, want 0 (signed out)", got)
	}
}

// AUTH-037
func TestSession_IdleExpiry(t *testing.T) {
	e := newSessionEnv(t)
	t0 := authtest.T0 // 24-10-2026 10:30:00 IST
	e.w.Clock.Set(t0)
	priya := e.w.Users["priya@vardhan.test"]
	raw := e.login(t, priya, "10.0.0.4")
	hash := auth.HashToken(raw)

	t1 := t0.Add(11*time.Hour + 59*time.Minute + 59*time.Second) // 22:29:59 IST
	e.w.Clock.Set(t1)
	if got := e.uidSeen(t, raw); got != int64(priya) {
		t.Fatalf("at %s: uid = %d, want %d", t1, got, priya)
	}
	if got := e.str(t, `SELECT last_seen_at FROM sessions WHERE token_hash = ?`, hash); got != auth.FormatTime(t1) {
		t.Errorf("last_seen_at = %q, want %q", got, auth.FormatTime(t1))
	}

	t2 := t1.Add(12 * time.Hour) // 10:29:59 IST on 25-10-2026: exactly 12 h idle
	e.w.Clock.Set(t2)
	if got := e.uidSeen(t, raw); got != int64(priya) {
		t.Fatalf("at %s (exactly 12 h idle): uid = %d, want %d", t2, got, priya)
	}

	t3 := t2.Add(12*time.Hour + time.Second) // 12 h 0 m 1 s idle
	e.w.Clock.Set(t3)
	if got := e.uidSeen(t, raw); got != 0 {
		t.Errorf("at %s (12 h 0 m 1 s idle): uid = %d, want 0 (signed out)", t3, got)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM sessions WHERE token_hash = ?`, hash); n != 0 {
		t.Errorf("expired session rows = %d, want 0 (deleted)", n)
	}
}

// AUTH-043
func TestSession_DeletedUserInvalid(t *testing.T) {
	e := newSessionEnv(t)
	kiran := e.w.AddUser(t, "kiran@vardhan.test", "Kiran Joshi", "Employee")
	raw := e.login(t, kiran, "10.0.0.9")
	if got := e.uidSeen(t, raw); got != int64(kiran) {
		t.Fatalf("before delete: uid = %d, want %d", got, kiran)
	}

	ctx := context.Background()
	if err := e.w.DB.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, `DELETE FROM users WHERE id = ?`, int64(kiran))
		return err
	}); err != nil {
		t.Fatalf("delete kiran: %v", err)
	}

	if got := e.uidSeen(t, raw); got != 0 {
		t.Errorf("after delete: uid = %d, want 0 (signed out)", got)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM sessions WHERE user_id = ?`, int64(kiran)); n != 0 {
		t.Errorf("sessions rows for kiran = %d, want 0", n)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM sessions WHERE token_hash = ?`, auth.HashToken(raw)); n != 0 {
		t.Errorf("sessions rows for kiran's token = %d, want 0", n)
	}
}

// Review Focus 2: a revocation during a request is not undone by that
// request's end-of-request commit.
func TestSession_RevokedNotResurrectedByInFlightCommit(t *testing.T) {
	e := newSessionEnv(t)
	deepak := e.w.Users["deepak@vardhan.test"]
	raw := e.login(t, deepak, "10.0.0.7")
	if n := e.count(t, `SELECT COUNT(*) FROM sessions WHERE user_id = ?`, int64(deepak)); n != 1 {
		t.Fatalf("sessions rows for deepak = %d, want 1", n)
	}

	type result struct {
		status int
		err    error
	}
	done := make(chan result, 1)
	go func() {
		req, err := http.NewRequest(http.MethodGet, e.srv.URL+"/block", nil)
		if err != nil {
			done <- result{err: err}
			return
		}
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: raw}) //nolint:gosec // client-side request cookie
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			done <- result{err: err}
			return
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		done <- result{status: resp.StatusCode}
	}()

	var once sync.Once
	release := func() { once.Do(func() { close(e.release) }) }
	t.Cleanup(release) // never leave the handler blocked (srv.Close waits for it)

	select {
	case <-e.entered: // the request has loaded its session and is running
	case r := <-done:
		t.Fatalf("in-flight request ended before reaching the handler: status %d, err %v", r.status, r.err)
	}
	ctx := context.Background()
	if err := e.w.DB.Write(ctx, func(tx *db.Tx) error {
		return e.st.RevokeAll(ctx, tx, deepak, "")
	}); err != nil {
		release()
		<-done
		t.Fatalf("RevokeAll: %v", err)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM sessions WHERE user_id = ?`, int64(deepak)); n != 0 {
		t.Errorf("after RevokeAll, before commit: sessions rows = %d, want 0", n)
	}
	release()
	r := <-done
	if r.err != nil || r.status != http.StatusOK {
		t.Fatalf("in-flight request: status %d, err %v; want 200", r.status, r.err)
	}

	if n := e.count(t, `SELECT COUNT(*) FROM sessions WHERE user_id = ?`, int64(deepak)); n != 0 {
		t.Errorf("after in-flight commit: sessions rows for deepak = %d, want 0", n)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM sessions WHERE token_hash = ?`, auth.HashToken(raw)); n != 0 {
		t.Errorf("after in-flight commit: rows with the revoked token = %d, want 0", n)
	}
	if got := e.uidSeen(t, raw); got != 0 {
		t.Errorf("revoked cookie: uid = %d, want 0 (signed out)", got)
	}
}

func TestSession_ListAndRevoke(t *testing.T) {
	e := newSessionEnv(t)
	priya := e.w.Users["priya@vardhan.test"]
	ravi := e.w.Users["ravi@vardhan.test"]
	rawA := e.login(t, priya, "10.0.0.41")
	e.w.Clock.Advance(time.Minute)
	rawB := e.login(t, priya, "10.0.0.42")
	_ = e.login(t, ravi, "10.0.0.3")
	idA := int64(e.count(t, `SELECT id FROM sessions WHERE token_hash = ?`, auth.HashToken(rawA)))
	idB := int64(e.count(t, `SELECT id FROM sessions WHERE token_hash = ?`, auth.HashToken(rawB)))

	ctx := context.Background()
	got, err := e.st.ListForUser(ctx, priya, auth.HashToken(rawA))
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	for i := range got {
		got[i].CreatedAt, got[i].LastSeenAt = got[i].CreatedAt.UTC(), got[i].LastSeenAt.UTC()
	}
	slices.SortFunc(got, func(a, b auth.SessionInfo) int { return int(a.ID - b.ID) })
	t1 := authtest.T0.Add(time.Minute)
	want := []auth.SessionInfo{
		{ID: idA, CreatedAt: authtest.T0, LastSeenAt: authtest.T0, IP: "10.0.0.41", UserAgent: "agent-10.0.0.41", Current: true},
		{ID: idB, CreatedAt: t1, LastSeenAt: t1, IP: "10.0.0.42", UserAgent: "agent-10.0.0.42", Current: false},
	}
	slices.SortFunc(want, func(a, b auth.SessionInfo) int { return int(a.ID - b.ID) })
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListForUser(priya, A) =\n%+v\nwant\n%+v", got, want)
	}

	// Ravi cannot revoke Priya's session.
	if err := e.st.RevokeForUser(ctx, ravi, idB); !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("RevokeForUser(ravi, priya's session) = %v, want ErrNotFound", err)
	}
	if got := e.uidSeen(t, rawB); got != int64(priya) {
		t.Errorf("after a refused revoke: uid = %d, want %d", got, priya)
	}

	if err := e.st.RevokeForUser(ctx, priya, idB); err != nil {
		t.Fatalf("RevokeForUser(priya, B): %v", err)
	}
	if got := e.uidSeen(t, rawB); got != 0 {
		t.Errorf("revoked session B: uid = %d, want 0", got)
	}
	if got := e.uidSeen(t, rawA); got != int64(priya) {
		t.Errorf("session A after revoking B: uid = %d, want %d", got, priya)
	}
	left, err := e.st.ListForUser(ctx, priya, auth.HashToken(rawA))
	if err != nil {
		t.Fatalf("ListForUser after revoke: %v", err)
	}
	if len(left) != 1 || left[0].ID != idA || !left[0].Current {
		t.Errorf("ListForUser after revoke = %+v, want only session %d, current", left, idA)
	}
}

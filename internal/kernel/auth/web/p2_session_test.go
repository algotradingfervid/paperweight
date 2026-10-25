package authweb_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/webtest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Task 16 (P2) sign-in, password and session cases over HTTP.

const (
	t16Admin    = "admin@vardhan.test"
	t16IPBlock  = "Too many failed sign-in attempts from your network. Try again in 15 minutes."
	t16Expired  = "Your session has expired. Sign in again."
	t16Chrome   = "Chrome on Windows"
	t16Safari   = "Safari on iPhone"
	t16RaviUA   = "Firefox on Ubuntu"
	t16GhostPw  = "Ghost-Guess-2026"
	t16Ghost    = "ghost@vardhan.test"
	t16LoggedUA = "Firefox on Linux (auth-16)"
)

// t16Session returns the token hash of c's current session cookie.
func t16Session(t *testing.T, what string, c *webtest.Client) string {
	t.Helper()
	ck := c.Cookie()
	if ck == nil || ck.Value == "" {
		t.Fatalf("%s: client has no session cookie", what)
	}
	return auth.HashToken(ck.Value)
}

// t16SessionIDs returns u's session ids by token hash.
func t16SessionIDs(t *testing.T, w *authtest.World, u auth.UserID) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	rows, err := w.DB.SQL().QueryContext(context.Background(), `SELECT token_hash, id FROM sessions WHERE user_id = ?`, int64(u))
	if err != nil {
		t.Fatalf("list sessions of %d: %v", u, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var h string
		var id int64
		if err := rows.Scan(&h, &id); err != nil {
			t.Fatal(err)
		}
		out[h] = id
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// AUTH-015
func TestLockout_PerIPBlock(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w, webtest.WithTrustProxy())
	w.SetPassword(t, t10Priya, t10Password)
	w.Clock.Set(authtest.T0)

	from7 := h.Client().WithHeader("X-Forwarded-For", "203.0.113.7")
	for i := 1; i <= 50; i++ {
		r := t11SignIn(t, from7, fmt.Sprintf("u%d@vardhan.test", i), t11Wrong, "")
		wantStatus(t, fmt.Sprintf("failure %d from 203.0.113.7", i), r, http.StatusUnauthorized)
		if i%10 == 0 {
			w.Clock.Advance(time.Minute) // all 50 inside 15 minutes
		}
	}

	r := t11SignIn(t, from7, t10Priya, t10Password, "")
	wantStatus(t, "attempt 51 from 203.0.113.7 (correct password)", r, http.StatusTooManyRequests)
	if !strings.Contains(r.Body, t16IPBlock) {
		t.Errorf("attempt 51: body %q, want %q", r.Body, t16IPBlock)
	}
	if n := t11SignedInSessions(t, w); n != 0 {
		t.Errorf("sessions with a user after the blocked attempt = %d, want 0", n)
	}

	from8 := h.Client().WithHeader("X-Forwarded-For", "203.0.113.8")
	wantRedirect(t, "sign-in from 203.0.113.8", t11SignIn(t, from8, t10Priya, t10Password, ""), http.StatusSeeOther, "/")
}

// AUTH-016
func TestLockout_AdminUnlock(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	w.SetPassword(t, t10Priya, t10Password)
	priya := w.Users[t10Priya]
	anil := w.Users[t16Admin]
	ctx := context.Background()

	for i := 1; i <= auth.AccountLimit; i++ {
		_, err := w.Auth.SignIn(ctx, auth.SignInInput{Email: t10Priya, Password: t11Wrong, IP: "198.51.100.4", UserAgent: "auth-16"})
		if err == nil {
			t.Fatalf("wrong password %d signed in", i)
		}
	}
	locked := t11SignIn(t, h.Client(), t10Priya, t10Password, "")
	wantStatus(t, "priya's correct password while locked", locked, http.StatusTooManyRequests)

	admin := signedIn(t, h, t16Admin)
	r := admin.Post(fmt.Sprintf("/users/%d/unlock", priya), url.Values{})
	wantStatus(t, "POST /users/{priya}/unlock", r, http.StatusSeeOther)
	if n := t11Int(t, w, `SELECT count(*) FROM login_attempts WHERE key = ?`, auth.AccountKey(t10Priya)); n != 0 {
		t.Errorf("login_attempts rows for priya's account after unlock = %d, want 0", n)
	}

	wantRedirect(t, "priya signs in after unlock", t11SignIn(t, h.Client(), t10Priya, t10Password, ""), http.StatusSeeOther, "/")

	var unlocks []audit.Entry
	for _, e := range w.Audit.Entries() {
		if e.Action == "unlock" {
			unlocks = append(unlocks, e)
		}
	}
	if len(unlocks) != 1 {
		t.Fatalf("audit unlock entries = %+v, want exactly 1", unlocks)
	}
	e := unlocks[0]
	got := []any{e.DocType, e.DocID, e.Action, e.Reason, e.By}
	want := []any{"User", int64(priya), "unlock", "unlocked by Anil Kapoor", types.UserID(anil)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("unlock audit entry (DocType, DocID, Action, Reason, By) = %v, want %v", got, want)
	}
}

// t16LogBuffer captures slog's default logger output until the test ends.
type t16LogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *t16LogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *t16LogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func t16CaptureLogs(t *testing.T) *t16LogBuffer {
	t.Helper()
	b := &t16LogBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(b, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return b
}

// t16TextColumns lists every TEXT or BLOB column of every table.
func t16TextColumns(t *testing.T, w *authtest.World) [][2]string {
	t.Helper()
	ctx := context.Background()
	var cols [][2]string
	if err := w.DB.Read(ctx, func(tx *db.Tx) error {
		rows, err := tx.SQL().QueryContext(ctx,
			`SELECT m.name, p.name FROM sqlite_master m, pragma_table_info(m.name) p
			 WHERE m.type = 'table' AND (upper(p.type) = 'TEXT' OR upper(p.type) = 'BLOB')`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var c [2]string
			if err := rows.Scan(&c[0], &c[1]); err != nil {
				return err
			}
			cols = append(cols, c)
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("list columns: %v", err)
	}
	return cols
}

// t16Holding returns "table.column" for each column holding s.
func t16Holding(t *testing.T, w *authtest.World, cols [][2]string, s string) []string {
	t.Helper()
	var out []string
	for _, c := range cols {
		q := fmt.Sprintf(`SELECT count(*) FROM "%s" WHERE instr(CAST("%s" AS BLOB), CAST(? AS BLOB)) > 0`, c[0], c[1])
		if t11Int(t, w, q, s) > 0 {
			out = append(out, c[0]+"."+c[1])
		}
	}
	return out
}

type t16LogRow struct {
	At, Event string
	UserID    sql.NullInt64
	EmailHash sql.NullString
	IP, UA    string
}

// AUTH-019
func TestAuthLog_NoSecrets(t *testing.T) {
	logs := t16CaptureLogs(t)
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	w.SetPassword(t, t10Priya, t10Password)
	priya := w.Users[t10Priya]
	t1 := authtest.T0
	t2 := t1.Add(time.Minute)
	w.Clock.Set(t1)

	// Success: priya.
	ok := h.Client().WithHeader("User-Agent", t16LoggedUA)
	page := ok.Get("/signin")
	wantStatus(t, "GET /signin", page, http.StatusOK)
	formCSRF := t11FormCSRF(t, "GET /signin", page)
	guestToken := ok.Cookie()
	in := ok.NoCSRF().Post("/signin", url.Values{"email": {t10Priya}, "password": {t10Password}, "csrf_token": {formCSRF}})
	wantRedirect(t, "priya signs in", in, http.StatusSeeOther, "/")
	signedToken := ok.Cookie()
	if signedToken == nil {
		t.Fatal("no session cookie after sign-in")
	}
	csrfPage := ok.Get("/_test/csrf")
	wantStatus(t, "GET /_test/csrf", csrfPage, http.StatusOK)

	// Failure: an unknown email.
	w.Clock.Set(t2)
	bad := h.Client().WithHeader("User-Agent", t16LoggedUA+" 2")
	page2 := bad.Get("/signin")
	formCSRF2 := t11FormCSRF(t, "GET /signin (2)", page2)
	guestToken2 := bad.Cookie()
	fail := bad.NoCSRF().Post("/signin", url.Values{"email": {t16Ghost}, "password": {t16GhostPw}, "csrf_token": {formCSRF2}})
	wantStatus(t, "ghost signs in", fail, http.StatusUnauthorized)

	var got []t16LogRow
	rows, err := w.DB.SQL().QueryContext(context.Background(),
		`SELECT at, event, user_id, email_hash, ip, user_agent FROM auth_log ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var r t16LogRow
		if err := rows.Scan(&r.At, &r.Event, &r.UserID, &r.EmailHash, &r.IP, &r.UA); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	sum := sha256.Sum256([]byte(t16Ghost))
	want := []t16LogRow{
		{At: auth.FormatTime(t1), Event: "login_success", UserID: sql.NullInt64{Int64: int64(priya), Valid: true}, IP: t11LoopbackIP, UA: t16LoggedUA},
		{At: auth.FormatTime(t2), Event: "login_failed", EmailHash: sql.NullString{String: hex.EncodeToString(sum[:]), Valid: true}, IP: t11LoopbackIP, UA: t16LoggedUA + " 2"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("auth_log rows =\n%+v\nwant\n%+v", got, want)
	}
	for _, r := range got {
		if at, err := auth.ParseTime(r.At); err != nil || at.Location() != time.UTC {
			t.Errorf("auth_log at %q: parsed %v (err %v), want a UTC time", r.At, at, err)
		}
	}

	type secret struct{ what, value string }
	secrets := []secret{
		{"priya's password", t10Password},
		{"the failed password", t16GhostPw},
		{"the signed-in session token", signedToken.Value},
	}
	if guestToken != nil {
		secrets = append(secrets, secret{"the guest session token", guestToken.Value})
	}
	if guestToken2 != nil {
		secrets = append(secrets, secret{"the second guest session token", guestToken2.Value})
	}
	csrfs := []secret{
		{"the sign-in form CSRF token", formCSRF},
		{"the second sign-in form CSRF token", formCSRF2},
		{"the signed-in CSRF token", csrfPage.Body},
	}
	cols := t16TextColumns(t, w)
	for _, s := range secrets {
		if s.value == "" {
			t.Fatalf("test setup: %s is empty", s.what)
		}
		if at := t16Holding(t, w, cols, s.value); len(at) != 0 {
			t.Errorf("%s is stored in %v", s.what, at)
		}
	}
	// The synchronizer token lives in the session data by design (CD-15);
	// it must not be anywhere else.
	for _, s := range csrfs {
		if s.value == "" {
			t.Fatalf("test setup: %s is empty", s.what)
		}
		var at []string
		for _, c := range t16Holding(t, w, cols, s.value) {
			if c != "sessions.data" {
				at = append(at, c)
			}
		}
		if len(at) != 0 {
			t.Errorf("%s is stored in %v", s.what, at)
		}
	}
	if at := t16Holding(t, w, cols, t16Ghost); len(at) != 0 {
		t.Errorf("the unknown email is stored in plain text in %v", at)
	}
	out := logs.String()
	for _, s := range append(secrets, csrfs...) {
		if strings.Contains(out, s.value) {
			t.Errorf("the application log contains %s", s.what)
		}
	}
}

// AUTH-038
func TestSession_AbsoluteExpiry(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	w.Clock.Set(authtest.T0) // 24-10-2026 10:30 IST
	c := signedIn(t, h, t10Priya)

	for i := 1; i <= 7*24; i++ { // every hour up to 31-10-2026 10:30:00 IST
		w.Clock.Advance(time.Hour)
		r := c.HTMX().Get("/")
		wantStatus(t, fmt.Sprintf("HTMX GET / at T0+%dh", i), r, http.StatusOK)
	}
	w.Clock.Advance(time.Second) // 31-10-2026 10:30:01 IST
	r := c.HTMX().Get("/")
	wantStatus(t, "HTMX GET / at 31-10-2026 10:30:01 IST", r, http.StatusUnauthorized)
	if !strings.Contains(messageOf(r), t16Expired) {
		t.Errorf("body %q, HX-Trigger %q, want %q", r.Body, r.Header.Get("HX-Trigger"), t16Expired)
	}
	if strings.Contains(r.Body, "HOME") {
		t.Errorf("expired session reached the handler: %q", r.Body)
	}
}

// AUTH-041
func TestLogout_PostOnly(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	c := signedIn(t, h, t10Priya)
	priya := w.Users[t10Priya]
	hash := t16Session(t, "after sign-in", c)

	r := c.Get("/signout")
	wantStatus(t, "GET /signout", r, http.StatusMethodNotAllowed)
	if n := t11Int(t, w, `SELECT count(*) FROM sessions WHERE token_hash = ? AND user_id = ?`, hash, int64(priya)); n != 1 {
		t.Errorf("priya's session rows after GET /signout = %d, want 1", n)
	}
	home := c.Get("/")
	wantStatus(t, "GET / after GET /signout", home, http.StatusOK)
	if !strings.Contains(home.Body, "HOME") {
		t.Errorf("GET / after GET /signout: body %q, want HOME (still signed in)", home.Body)
	}
}

// AUTH-044 (level integration)
func TestSessions_UserRevokesOthers(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	w.SetPassword(t, t10Priya, t10Password)
	w.SetPassword(t, t10Ravi, t10Password)
	priya := w.Users[t10Priya]
	ravi := w.Users[t10Ravi]
	w.Clock.Set(authtest.T0) // 24-10-2026 10:30 IST

	a := h.Client().WithHeader("User-Agent", t16Chrome)
	t12LoginOK(t, "priya on Chrome", a.Login(t10Priya, t10Password))
	b := h.Client().WithHeader("User-Agent", t16Safari)
	t12LoginOK(t, "priya on Safari", b.Login(t10Priya, t10Password))
	rv := h.Client().WithHeader("User-Agent", t16RaviUA)
	t12LoginOK(t, "ravi", rv.Login(t10Ravi, t10Password))

	w.Clock.Advance(time.Hour) // 11:30 IST: Safari is used again
	wantStatus(t, "Safari GET / at 11:30", b.Get("/"), http.StatusOK)

	page := a.Get("/account/sessions")
	wantStatus(t, "GET /account/sessions", page, http.StatusOK)
	for _, ua := range []string{t16Chrome, t16Safari} {
		if !strings.Contains(page.Body, ua) {
			t.Errorf("sessions page does not list %q: %q", ua, page.Body)
		}
	}
	if strings.Contains(page.Body, t16RaviUA) {
		t.Errorf("sessions page lists Ravi's session: %q", page.Body)
	}
	started := types.FormatIST(authtest.T0)
	if started != "24-10-2026 10:30" {
		t.Fatalf("types.FormatIST(T0) = %q, want 24-10-2026 10:30", started)
	}
	if n := strings.Count(page.Body, started); n < 2 {
		t.Errorf("sessions page shows %q %d times, want at least 2 (each session's start): %q", started, n, page.Body)
	}
	if used := types.FormatIST(authtest.T0.Add(time.Hour)); !strings.Contains(page.Body, used) {
		t.Errorf("sessions page does not show Safari's last use %q: %q", used, page.Body)
	}
	if strings.Contains(page.Body, "24-10-2026 05:00") {
		t.Errorf("sessions page shows the UTC time 24-10-2026 05:00, want IST (CD-33)")
	}

	r := a.Post("/account/sessions/revoke-others", url.Values{})
	t12Changed(t, "POST /account/sessions/revoke-others", r)
	aHash := t16Session(t, "Chrome", a)
	if got, want := t16SessionIDs(t, w, priya), 1; len(got) != want {
		t.Errorf("priya's sessions after revoke-others = %v, want only the current one", got)
	} else if _, ok := got[aHash]; !ok {
		t.Errorf("priya's remaining session %v is not the current (Chrome) one", got)
	}
	wantStatus(t, "Chrome GET / after revoke-others", a.Get("/"), http.StatusOK)
	wantStatus(t, "Safari HTMX GET / after revoke-others", b.HTMX().Get("/"), http.StatusUnauthorized)

	// Another user's session id is not found, and stays.
	raviIDs := t16SessionIDs(t, w, ravi)
	if len(raviIDs) != 1 {
		t.Fatalf("ravi's sessions = %v, want 1", raviIDs)
	}
	for _, id := range raviIDs {
		r := a.Post(fmt.Sprintf("/account/sessions/%d/revoke", id), url.Values{})
		wantStatus(t, "POST /account/sessions/{ravi's id}/revoke", r, http.StatusNotFound)
	}
	if got := t16SessionIDs(t, w, ravi); !reflect.DeepEqual(got, raviIDs) {
		t.Errorf("ravi's sessions after priya's revoke attempt = %v, want %v", got, raviIDs)
	}
	wantStatus(t, "ravi GET / after priya's revoke attempt", rv.Get("/"), http.StatusOK)

	// Priya's own other session is revoked by id.
	t12LoginOK(t, "priya on Safari again", b.Login(t10Priya, t10Password))
	bID, ok := t16SessionIDs(t, w, priya)[t16Session(t, "Safari again", b)]
	if !ok {
		t.Fatal("no session row for Safari's new session")
	}
	r = a.Post(fmt.Sprintf("/account/sessions/%d/revoke", bID), url.Values{})
	t12Changed(t, "POST /account/sessions/{own other id}/revoke", r)
	wantStatus(t, "Safari HTMX GET / after its revoke", b.HTMX().Get("/"), http.StatusUnauthorized)
	wantStatus(t, "Chrome GET / after revoking Safari", a.Get("/"), http.StatusOK)
}

// AUTH-045
func TestSessions_MultipleAllowed(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	priya := w.Users[t10Priya]
	a := signedIn(t, h, t10Priya)
	b := h.Client()
	t12LoginOK(t, "priya in browser B", b.Login(t10Priya, t10Password))

	for _, tc := range []struct {
		what string
		c    *webtest.Client
	}{{"browser A", a}, {"browser B", b}} {
		r := tc.c.Get("/")
		wantStatus(t, tc.what+" GET /", r, http.StatusOK)
		if !strings.Contains(r.Body, "HOME") {
			t.Errorf("%s GET /: body %q, want HOME", tc.what, r.Body)
		}
	}
	if n := t11Int(t, w, `SELECT count(*) FROM sessions WHERE user_id = ?`, int64(priya)); n != 2 {
		t.Errorf("priya's sessions = %d, want 2", n)
	}
}

// AUTH-047
func TestCSRF_LoginForm(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	w.SetPassword(t, t10Priya, t10Password)
	creds := url.Values{"email": {t10Priya}, "password": {t10Password}}

	// No session at all.
	fresh := h.Client()
	r := fresh.NoCSRF().Post("/signin", creds)
	wantStatus(t, "POST /signin with no session and no token", r, http.StatusForbidden)
	if !strings.Contains(r.Body, auth.ErrCSRF.Msg) {
		t.Errorf("body %q, want %q", r.Body, auth.ErrCSRF.Msg)
	}

	// A guest session from the sign-in page, but the token left out.
	guest := h.Client()
	wantStatus(t, "GET /signin", guest.Get("/signin"), http.StatusOK)
	r = guest.NoCSRF().Post("/signin", creds)
	wantStatus(t, "POST /signin without the page's token", r, http.StatusForbidden)

	if n := t11SignedInSessions(t, w); n != 0 {
		t.Errorf("sessions with a user = %d, want 0", n)
	}
	if n := t11Int(t, w, `SELECT count(*) FROM auth_log WHERE event = 'login_success'`); n != 0 {
		t.Errorf("login_success rows = %d, want 0", n)
	}
	if n := t11Int(t, w, `SELECT count(*) FROM users WHERE email = ? AND last_login_at IS NOT NULL`, t10Priya); n != 0 {
		t.Errorf("priya's last_login_at was set")
	}
	if home := guest.Get("/"); home.Status != http.StatusSeeOther {
		t.Errorf("GET / after the refused sign-in: status %d, want 303 (not signed in)", home.Status)
	}
}

// Carry-over (Task 12): a failed change never echoes a submitted password.
func TestChangePassword_FailureEchoesNoPassword(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	c := signedIn(t, h, t10Priya)

	for _, tc := range []struct {
		what                   string
		current, next, confirm string
		msg                    string
	}{
		{"wrong current password", t12Wrong, t12New, t12New, t12BadCur},
		{"mismatched confirmation", t10Password, t12New, t12Confirm2, t12Mismatch},
	} {
		r := c.Post("/account/password", t12ChangeForm(tc.current, tc.next, tc.confirm))
		t12WantMsg(t, tc.what, r, http.StatusBadRequest, tc.msg)
		for _, pw := range []string{tc.current, tc.next, tc.confirm} {
			if strings.Contains(r.Body, pw) {
				t.Errorf("%s: response body contains the submitted password %q", tc.what, pw)
			}
		}
	}
}

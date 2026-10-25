package authweb_test

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	authweb "github.com/vardhan/internaltools/internal/kernel/auth/web"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/webtest"
)

const (
	t11Wrong      = "Correct-Horse-43"
	t11Old        = "old@vardhan.test"
	t11Nobody     = "nobody@vardhan.test"
	t11Invalid    = "Invalid email or password."
	t11Disabled   = "Your account is disabled. Ask your administrator to enable it."
	t11Locked     = "Too many failed sign-in attempts. Try again in 15 minutes, or ask your administrator to unlock your account."
	t11LoopbackIP = "127.0.0.1"
)

var (
	t11InputRe = regexp.MustCompile(`<input[^>]*name="csrf_token"[^>]*>`)
	t11ValueRe = regexp.MustCompile(`value="([^"]*)"`)
)

// t11FormCSRF returns the value of the page's hidden csrf_token input.
func t11FormCSRF(t *testing.T, what string, r webtest.Response) string {
	t.Helper()
	tag := t11InputRe.FindString(r.Body)
	if tag == "" {
		t.Fatalf("%s: no <input name=\"csrf_token\"> in body %q", what, r.Body)
	}
	m := t11ValueRe.FindStringSubmatch(tag)
	if m == nil || m[1] == "" {
		t.Fatalf("%s: csrf_token input %q has no value", what, tag)
	}
	return m[1]
}

// t11SignIn loads GET /signin (creating the anonymous session and its CSRF
// token), then POSTs the form with that token. Fields: email, password,
// csrf_token, and next when non-empty.
func t11SignIn(t *testing.T, c *webtest.Client, email, pw, next string) webtest.Response {
	t.Helper()
	path := "/signin"
	if next != "" {
		path += "?next=" + url.QueryEscape(next)
	}
	page := c.Get(path)
	wantStatus(t, "GET "+path, page, http.StatusOK)
	form := url.Values{
		"email":      {email},
		"password":   {pw},
		"csrf_token": {t11FormCSRF(t, "GET "+path, page)},
	}
	if next != "" {
		form.Set("next", next)
	}
	return c.NoCSRF().Post("/signin", form)
}

// t11SetCookie returns the last Set-Cookie for name in r, or nil.
func t11SetCookie(r webtest.Response, name string) *http.Cookie {
	var got *http.Cookie
	for _, ck := range (&http.Response{Header: r.Header}).Cookies() {
		if ck.Name == name {
			got = ck
		}
	}
	return got
}

// t11RawSetCookie returns the raw Set-Cookie header for name in r, or "".
func t11RawSetCookie(r webtest.Response, name string) string {
	var got string
	for _, v := range r.Header.Values("Set-Cookie") {
		if strings.HasPrefix(v, name+"=") {
			got = v
		}
	}
	return got
}

// t11Int runs a one-int query.
func t11Int(t *testing.T, w *authtest.World, query string, args ...any) int {
	t.Helper()
	var n int
	if err := w.DB.SQL().QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("%s %v: %v", query, args, err)
	}
	return n
}

// t11SignedInSessions counts sessions rows that carry a user.
func t11SignedInSessions(t *testing.T, w *authtest.World) int {
	t.Helper()
	return t11Int(t, w, `SELECT count(*) FROM sessions WHERE user_id IS NOT NULL`)
}

type t11Replayed struct {
	Status int
	Header http.Header
	Body   string
}

// t11Replay sends GET path with only the given session cookie value, from a
// client with no jar that does not follow redirects.
func t11Replay(t *testing.T, h *webtest.Harness, cookieValue, path string, htmx bool) t11Replayed {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, h.HTTP.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Cookie", "session="+cookieValue)
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	cl := &http.Client{
		Transport:     h.HTTP.Client().Transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := cl.Do(req)
	if err != nil {
		t.Fatalf("replay GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("replay GET %s: read body: %v", path, err)
	}
	return t11Replayed{Status: resp.StatusCode, Header: resp.Header, Body: html.UnescapeString(string(b))}
}

// AUTH-001
func TestLogin_ValidCredentials(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	w.SetPassword(t, t10Priya, t10Password)
	priya := w.Users[t10Priya]
	c := h.Client()

	r := t11SignIn(t, c, t10Priya, t10Password, "")
	wantRedirect(t, "POST /signin", r, http.StatusSeeOther, "/")

	ck := t11SetCookie(r, "session")
	if ck == nil {
		t.Fatalf("POST /signin set no session cookie; Set-Cookie %q", r.Header.Values("Set-Cookie"))
	}
	if ck.Value == "" {
		t.Error("session cookie value is empty")
	}
	if !ck.HttpOnly {
		t.Error("session cookie HttpOnly = false, want true")
	}
	if ck.SameSite != http.SameSiteLaxMode {
		t.Errorf("session cookie SameSite = %v, want Lax", ck.SameSite)
	}
	if !ck.Expires.IsZero() || ck.RawExpires != "" || ck.MaxAge != 0 {
		t.Errorf("session cookie Expires %v (raw %q), MaxAge %d; want no Expires (browser session, CD-14)", ck.Expires, ck.RawExpires, ck.MaxAge)
	}

	if n := t11Int(t, w, `SELECT count(*) FROM sessions WHERE user_id = ?`, int64(priya)); n != 1 {
		t.Errorf("sessions rows for priya = %d, want 1", n)
	}

	var last *string
	if err := w.DB.SQL().QueryRowContext(context.Background(), `SELECT last_login_at FROM users WHERE id = ?`, int64(priya)).Scan(&last); err != nil {
		t.Fatal(err)
	}
	if last == nil || *last != auth.FormatTime(authtest.T0) || *last != "2026-10-24T05:00:00.000Z" {
		t.Errorf("users.last_login_at = %v, want %q", last, "2026-10-24T05:00:00.000Z")
	}

	rows, err := w.DB.SQL().QueryContext(context.Background(), `SELECT CAST(coalesce(user_id, 0) AS INTEGER), ip FROM auth_log WHERE event = 'login_success'`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var uid int64
		var ip string
		if err := rows.Scan(&uid, &ip); err != nil {
			t.Fatal(err)
		}
		got = append(got, fmt.Sprintf("%d %s", uid, ip))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if want := []string{fmt.Sprintf("%d %s", priya, t11LoopbackIP)}; len(got) != 1 || got[0] != want[0] {
		t.Errorf("auth_log login_success rows (user_id ip) = %q, want %q", got, want)
	}

	home := c.Get("/")
	wantStatus(t, "GET / after sign-in", home, http.StatusOK)
	if !strings.Contains(home.Body, "HOME") {
		t.Errorf("GET / after sign-in: body %q, want HOME", home.Body)
	}
}

// AUTH-003
func TestLogin_WrongPassword(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	w.SetPassword(t, t10Priya, t10Password)
	priya := w.Users[t10Priya]

	r := t11SignIn(t, h.Client(), t10Priya, t11Wrong, "")
	wantStatus(t, "POST /signin wrong password", r, http.StatusUnauthorized)
	if !strings.Contains(r.Body, t11Invalid) {
		t.Errorf("body %q, want %q", r.Body, t11Invalid)
	}
	if n := t11SignedInSessions(t, w); n != 0 {
		t.Errorf("sessions rows with a user = %d, want 0", n)
	}
	if n := t11Int(t, w, `SELECT count(*) FROM auth_log WHERE event = 'login_failed' AND user_id = ?`, int64(priya)); n != 1 {
		t.Errorf("auth_log login_failed rows for priya = %d, want 1", n)
	}
	if n := t11Int(t, w, `SELECT count(*) FROM auth_log WHERE event = 'login_success'`); n != 0 {
		t.Errorf("auth_log login_success rows = %d, want 0", n)
	}

	rows, err := w.DB.SQL().QueryContext(context.Background(),
		`SELECT at, event, CAST(coalesce(user_id, '') AS TEXT), coalesce(email_hash, ''), ip, user_agent FROM auth_log`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		cols := make([]string, 6)
		if err := rows.Scan(&cols[0], &cols[1], &cols[2], &cols[3], &cols[4], &cols[5]); err != nil {
			t.Fatal(err)
		}
		for _, v := range cols {
			if strings.Contains(v, t11Wrong) {
				t.Errorf("auth_log row %q contains the password", cols)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

// AUTH-004
func TestLogin_UnknownEmailSameResponse(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	w.SetPassword(t, t10Priya, t10Password)

	wrong := t11SignIn(t, h.Client(), t10Priya, t11Wrong, "")
	unknown := t11SignIn(t, h.Client(), t11Nobody, t11Wrong, "")

	wantStatus(t, "wrong password", wrong, http.StatusUnauthorized)
	wantStatus(t, "unknown email", unknown, http.StatusUnauthorized)
	for _, r := range []webtest.Response{wrong, unknown} {
		if !strings.Contains(r.Body, t11Invalid) {
			t.Errorf("body %q, want %q", r.Body, t11Invalid)
		}
	}

	mask := func(what string, r webtest.Response) string {
		return strings.ReplaceAll(r.Body, t11FormCSRF(t, what, r), "X")
	}
	if a, b := mask("wrong password", wrong), mask("unknown email", unknown); a != b {
		t.Errorf("bodies differ beyond the CSRF token:\nwrong password: %q\nunknown email:  %q", a, b)
	}

	keys := func(hd http.Header) []string {
		var ks []string
		for k := range hd {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		return ks
	}
	ka, kb := keys(wrong.Header), keys(unknown.Header)
	if strings.Join(ka, ",") != strings.Join(kb, ",") {
		t.Errorf("header keys differ: wrong password %q, unknown email %q", ka, kb)
	}
	for _, k := range ka {
		if k == "Set-Cookie" || k == "Date" || k == "X-Test-Csrf" { // X-Test-Csrf: webtest harness only (CD-136)
			continue
		}
		if va, vb := wrong.Header.Values(k), unknown.Header.Values(k); strings.Join(va, "\x00") != strings.Join(vb, "\x00") {
			t.Errorf("header %s differs: wrong password %q, unknown email %q", k, va, vb)
		}
	}
}

// AUTH-007
func TestLogin_DisabledUserCorrectPassword(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	w.SetPassword(t, t11Old, t10Password)

	r := t11SignIn(t, h.Client(), t11Old, t10Password, "")
	wantStatus(t, "POST /signin disabled user", r, http.StatusForbidden)
	if !strings.Contains(r.Body, t11Disabled) {
		t.Errorf("body %q, want %q", r.Body, t11Disabled)
	}
	if n := t11SignedInSessions(t, w); n != 0 {
		t.Errorf("sessions rows with a user = %d, want 0", n)
	}
	if n := t11Int(t, w, `SELECT coalesce((SELECT count FROM login_attempts WHERE key = ?), 0)`, auth.AccountKey(t11Old)); n != 1 {
		t.Errorf("account failure count = %d, want 1", n)
	}
}

// AUTH-009
func TestLockout_LocksAfterFiveFailures(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	w.SetPassword(t, t10Priya, t10Password)
	w.Clock.Set(time.Date(2026, 10, 24, 4, 30, 0, 0, time.UTC)) // 10:30 IST
	c := h.Client()

	for i := 1; i <= 5; i++ {
		r := t11SignIn(t, c, t10Priya, t11Wrong, "")
		wantStatus(t, fmt.Sprintf("attempt %d (wrong)", i), r, http.StatusUnauthorized)
		if !strings.Contains(r.Body, t11Invalid) {
			t.Fatalf("attempt %d: body %q, want %q", i, r.Body, t11Invalid)
		}
		w.Clock.Advance(time.Minute)
	}

	// 10:35 IST, correct password.
	r := t11SignIn(t, c, t10Priya, t10Password, "")
	wantStatus(t, "attempt 6 (correct)", r, http.StatusTooManyRequests)
	if !strings.Contains(r.Body, t11Locked) {
		t.Errorf("attempt 6: body %q, want %q", r.Body, t11Locked)
	}
	if n := t11SignedInSessions(t, w); n != 0 {
		t.Errorf("sessions rows with a user = %d, want 0", n)
	}
	if home := c.Get("/"); home.Status != http.StatusSeeOther {
		t.Errorf("GET / after the locked attempt: status %d, want 303 (not signed in)", home.Status)
	}
}

// AUTH-018
func TestLogin_NextParamLocalOnly(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	w.SetPassword(t, t10Priya, t10Password)

	for _, tc := range []struct{ next, loc string }{
		{"/po/123", "/po/123"},
		{"https://evil.example/", "/"},
		{"//evil.example", "/"},
		{`/\evil.example`, "/"},
		{"javascript:alert(1)", "/"},
	} {
		r := t11SignIn(t, h.Client(), t10Priya, t10Password, tc.next)
		wantRedirect(t, "sign in with next="+tc.next, r, http.StatusSeeOther, tc.loc)
		if got := authweb.SafeNext(tc.next); got != tc.loc {
			t.Errorf("SafeNext(%q) = %q, want %q", tc.next, got, tc.loc)
		}
	}
}

// AUTH-040
func TestLogout_DeletesSession(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	w.SetPassword(t, t10Priya, t10Password)
	priya := w.Users[t10Priya]
	c := h.Client()

	in := t11SignIn(t, c, t10Priya, t10Password, "")
	wantRedirect(t, "POST /signin", in, http.StatusSeeOther, "/")
	ck := t11SetCookie(in, "session")
	if ck == nil || ck.Value == "" {
		t.Fatalf("sign-in set no session cookie: %q", in.Header.Values("Set-Cookie"))
	}
	old := ck.Value

	out := c.Post("/signout", url.Values{})
	wantRedirect(t, "POST /signout", out, http.StatusSeeOther, "/signin")
	raw := t11RawSetCookie(out, "session")
	if !strings.Contains(raw, "Max-Age=0") {
		t.Errorf("sign-out Set-Cookie for session = %q, want Max-Age=0", raw)
	}
	if n := t11Int(t, w, `SELECT count(*) FROM sessions WHERE user_id = ?`, int64(priya)); n != 0 {
		t.Errorf("sessions rows for priya after sign-out = %d, want 0", n)
	}
	if n := t11Int(t, w, `SELECT count(*) FROM sessions WHERE token_hash = ?`, auth.HashToken(old)); n != 0 {
		t.Errorf("sessions rows for the old token after sign-out = %d, want 0", n)
	}
	if n := t11Int(t, w, `SELECT count(*) FROM auth_log WHERE event = 'logout' AND user_id = ?`, int64(priya)); n != 1 {
		t.Errorf("auth_log logout rows for priya = %d, want 1", n)
	}

	hx := t11Replay(t, h, old, "/", true)
	if hx.Status != http.StatusUnauthorized {
		t.Errorf("replayed old cookie, HTMX GET /: status %d, want 401 (body %q)", hx.Status, hx.Body)
	}
	if strings.Contains(hx.Body, "HOME") {
		t.Errorf("replayed old cookie, HTMX GET /: body leaks HOME")
	}
	if msg := hx.Body + "\n" + hx.Header.Get("HX-Trigger"); !strings.Contains(msg, auth.ErrSessionExpired.Msg) {
		t.Errorf("replayed old cookie, HTMX GET /: body %q, HX-Trigger %q, want %q", hx.Body, hx.Header.Get("HX-Trigger"), auth.ErrSessionExpired.Msg)
	}

	plain := t11Replay(t, h, old, "/", false)
	if plain.Status != http.StatusSeeOther || plain.Header.Get("Location") != "/signin?next=%2F" {
		t.Errorf("replayed old cookie, GET /: status %d Location %q, want 303 /signin?next=%%2F", plain.Status, plain.Header.Get("Location"))
	}
}

// Review Focus 1
func TestLogin_RenewsSessionToken(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	w.SetPassword(t, t10Priya, t10Password)
	priya := w.Users[t10Priya]
	c := h.Client()

	page := c.Get("/signin")
	wantStatus(t, "GET /signin", page, http.StatusOK)
	pre := t11SetCookie(page, "session")
	if pre == nil || pre.Value == "" {
		t.Fatalf("GET /signin set no anonymous session cookie: %q", page.Header.Values("Set-Cookie"))
	}
	r := c.NoCSRF().Post("/signin", url.Values{
		"email":      {t10Priya},
		"password":   {t10Password},
		"csrf_token": {t11FormCSRF(t, "GET /signin", page)},
	})
	wantRedirect(t, "POST /signin", r, http.StatusSeeOther, "/")
	post := t11SetCookie(r, "session")
	if post == nil || post.Value == "" {
		t.Fatalf("POST /signin set no session cookie: %q", r.Header.Values("Set-Cookie"))
	}
	if post.Value == pre.Value {
		t.Fatalf("session token unchanged by sign-in (%q): session fixation", pre.Value)
	}

	if n := t11Int(t, w, `SELECT count(*) FROM sessions WHERE token_hash = ? AND user_id IS NOT NULL`, auth.HashToken(pre.Value)); n != 0 {
		t.Errorf("pre-sign-in token has %d signed-in rows, want 0", n)
	}
	if n := t11Int(t, w, `SELECT count(*) FROM sessions WHERE token_hash = ? AND user_id = ?`, auth.HashToken(post.Value), int64(priya)); n != 1 {
		t.Errorf("post-sign-in token rows for priya = %d, want 1", n)
	}

	planted := t11Replay(t, h, pre.Value, "/", false)
	if planted.Status != http.StatusSeeOther || strings.Contains(planted.Body, "HOME") {
		t.Errorf("pre-sign-in token, GET /: status %d body %q, want 303 to /signin (resolves to nobody)", planted.Status, planted.Body)
	}
	if got := c.Get("/"); got.Status != http.StatusOK || !strings.Contains(got.Body, "HOME") {
		t.Errorf("signed-in client GET /: status %d body %q, want 200 HOME", got.Status, got.Body)
	}
}

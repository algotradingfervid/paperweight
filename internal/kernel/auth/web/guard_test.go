package authweb_test

import (
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	authweb "github.com/vardhan/internaltools/internal/kernel/auth/web"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/webtest"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

const (
	t10Password    = "Correct-Horse-42"
	t10Priya       = "priya@vardhan.test"
	t10Ravi        = "ravi@vardhan.test"
	t10Deepak      = "deepak@vardhan.test"
	t10DenyReadPO  = "You don't have permission to read Purchase Order. Ask your administrator for a role that allows it."
	t10InternalMsg = "Something went wrong. Try again, and if it keeps happening tell the administrator."
	t10AdminMsg    = "Only an administrator can manage users."
)

// t10Stubs records what the stub handlers saw.
type t10Stubs struct {
	mu        sync.Mutex
	saves     int
	docSaves  int
	lastForm  url.Values
	lastPForm url.Values
}

func (s *t10Stubs) Saves() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saves
}

func (s *t10Stubs) DocSaves() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.docSaves
}

// t10Harness builds the Task 10 harness with the plan's stub routes:
// GET /po/list (Right read) → "PO LIST"; POST /po/{id}/save (Right write);
// GET /api/po/{id} (Right read); GET /users (Admin); GET / (Self);
// GET /doc/{doctype}/list and POST /doc/{doctype}/{id}/save (CheckedInHandler,
// the handler calls Can itself and answers through WriteError).
func t10Harness(t *testing.T, w *authtest.World, opts ...webtest.Option) (*webtest.Harness, *t10Stubs) {
	t.Helper()
	st := &t10Stubs{}
	text := func(s string) http.HandlerFunc {
		return func(rw http.ResponseWriter, _ *http.Request) { _, _ = rw.Write([]byte(s)) }
	}
	routes := webtest.WithRoutes(func(r chi.Router, s *authweb.Server) {
		r.Method(http.MethodGet, "/po/list", s.Right(authtest.DTOrder, auth.RightRead, text("PO LIST")))
		r.Method(http.MethodPost, "/po/{id}/save", s.Right(authtest.DTOrder, auth.RightWrite, http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			st.mu.Lock()
			st.saves++
			st.lastPForm = cloneValues(r.PostForm)
			st.lastForm = cloneValues(r.Form)
			st.mu.Unlock()
			_, _ = rw.Write([]byte("SAVED"))
		})))
		r.Method(http.MethodGet, "/api/po/{id}", s.Right(authtest.DTOrder, auth.RightRead, text(`{"id":1}`)))
		r.Method(http.MethodGet, "/users", s.Admin(t10AdminMsg, text("USERS")))
		r.Method(http.MethodGet, "/", s.Self(text("HOME")))
		r.Method(http.MethodGet, "/doc/{doctype}/list", s.CheckedInHandler(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			p, _ := auth.PrincipalFrom(r.Context())
			dt := chi.URLParam(r, "doctype")
			if err := w.Auth.Can(r.Context(), p, dt, auth.RightRead, nil); err != nil {
				s.WriteError(rw, r, err)
				return
			}
			_, _ = rw.Write([]byte("DOC LIST " + dt)) //nolint:gosec // test stub handler, plain text
		})))
		r.Method(http.MethodPost, "/doc/{doctype}/{id}/save", s.CheckedInHandler(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			st.mu.Lock()
			st.docSaves++
			st.mu.Unlock()
			_, _ = rw.Write([]byte("DOC SAVED"))
		})))
	})
	h := webtest.New(t, w, append([]webtest.Option{routes}, opts...)...)
	return h, st
}

func cloneValues(v url.Values) url.Values {
	if v == nil {
		return nil
	}
	out := url.Values{}
	for k, vs := range v {
		out[k] = append([]string(nil), vs...)
	}
	return out
}

// signedIn sets email's password and returns a client signed in as email.
func signedIn(t *testing.T, h *webtest.Harness, email string) *webtest.Client {
	t.Helper()
	h.W.SetPassword(t, email, t10Password)
	c := h.Client()
	c.Login(email, t10Password)
	return c
}

// messageOf returns the body plus HX-Trigger, where an htmx error carries its
// text (platform/web Error, CD-47).
func messageOf(r webtest.Response) string {
	return r.Body + "\n" + r.Header.Get("HX-Trigger")
}

func wantStatus(t *testing.T, what string, r webtest.Response, status int) {
	t.Helper()
	if r.Status != status {
		t.Fatalf("%s: status = %d, want %d (body %q)", what, r.Status, status, r.Body)
	}
}

func wantRedirect(t *testing.T, what string, r webtest.Response, status int, loc string) {
	t.Helper()
	wantStatus(t, what, r, status)
	if got := r.Header.Get("Location"); got != loc {
		t.Fatalf("%s: Location = %q, want %q", what, got, loc)
	}
}

// AUTH-039
func TestSession_ExpiredHTMXRedirect(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	c := signedIn(t, h, t10Priya)

	before := c.Get("/po/list?status=draft")
	wantStatus(t, "GET before expiry", before, http.StatusOK)
	if !strings.Contains(before.Body, "PO LIST") {
		t.Fatalf("GET before expiry: body %q lacks PO LIST", before.Body)
	}

	w.Clock.Advance(12*time.Hour + time.Second)

	const next = "/signin?next=%2Fpo%2Flist%3Fstatus%3Ddraft"
	r := c.HTMX().Get("/po/list?status=draft")
	wantStatus(t, "HTMX GET after expiry", r, http.StatusUnauthorized)
	if got := r.Header.Get("HX-Redirect"); got != next {
		t.Fatalf("HX-Redirect = %q, want %q", got, next)
	}
	if !strings.Contains(messageOf(r), auth.ErrSessionExpired.Msg) {
		t.Fatalf("HTMX response lacks %q: body %q, HX-Trigger %q", auth.ErrSessionExpired.Msg, r.Body, r.Header.Get("HX-Trigger"))
	}
	if auth.ErrSessionExpired.Msg != "Your session has expired. Sign in again." {
		t.Fatalf("ErrSessionExpired text = %q", auth.ErrSessionExpired.Msg)
	}
	for _, leak := range []string{"PO LIST", "goroutine"} {
		if strings.Contains(r.Body, leak) {
			t.Fatalf("HTMX body leaks %q: %q", leak, r.Body)
		}
	}

	plain := c.Get("/po/list?status=draft")
	wantRedirect(t, "plain GET after expiry", plain, http.StatusSeeOther, next)
	if strings.Contains(plain.Body, "PO LIST") {
		t.Fatalf("plain GET body leaks PO LIST: %q", plain.Body)
	}
}

// AUTH-107
func TestRoutes_AllDeclarePermission(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	ok := http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) { rw.WriteHeader(http.StatusOK) })

	if got := authweb.CheckRoutes(h.Router); len(got) != 0 {
		t.Fatalf("CheckRoutes(harness router) = %q, want none", got)
	}

	reported := func(problems []string, route string) bool {
		for _, p := range problems {
			if strings.HasPrefix(p, route+":") {
				return true
			}
		}
		return false
	}

	h.Router.Get("/oops", ok)
	h.Router.Method(http.MethodGet, "/x", authweb.Public(ok))
	h.Router.Method(http.MethodGet, "/healthz", authweb.Public(ok))
	h.Router.Method(http.MethodGet, "/signin", authweb.Public(ok))
	got := authweb.CheckRoutes(h.Router)
	for _, route := range []string{"GET /oops", "GET /x"} {
		if !reported(got, route) {
			t.Errorf("CheckRoutes = %q, want an entry starting %q", got, route+":")
		}
	}
	for _, route := range []string{"GET /healthz", "GET /signin", "GET /po/list", "GET /doc/{doctype}/list"} {
		if reported(got, route) {
			t.Errorf("CheckRoutes = %q, must not report %q", got, route)
		}
	}
	if len(got) != 2 {
		t.Errorf("CheckRoutes = %q, want exactly the 2 bad routes", got)
	}

	// CD-46: CheckedInHandler only under /doc/.
	r := chi.NewRouter()
	r.Method(http.MethodGet, "/doc/{doctype}/list", h.Srv.CheckedInHandler(ok))
	if got := authweb.CheckRoutes(r); len(got) != 0 {
		t.Errorf("CheckedInHandler at /doc/{doctype}/list: CheckRoutes = %q, want none", got)
	}
	r = chi.NewRouter()
	r.Method(http.MethodGet, "/po/{id}/other", h.Srv.CheckedInHandler(ok))
	if got := authweb.CheckRoutes(r); len(got) != 1 || !reported(got, "GET /po/{id}/other") {
		t.Errorf("CheckedInHandler at /po/{id}/other: CheckRoutes = %q, want one entry for it", got)
	}

	// CD-87: /ui/validate/* takes Self, never CheckedInHandler.
	r = chi.NewRouter()
	r.Method(http.MethodPost, "/ui/validate/{field}", h.Srv.Self(ok))
	if got := authweb.CheckRoutes(r); len(got) != 0 {
		t.Errorf("Self at POST /ui/validate/{field}: CheckRoutes = %q, want none", got)
	}
	r = chi.NewRouter()
	r.Method(http.MethodPost, "/ui/validate/{field}", h.Srv.CheckedInHandler(ok))
	if got := authweb.CheckRoutes(r); len(got) != 1 || !reported(got, "POST /ui/validate/{field}") {
		t.Errorf("CheckedInHandler at POST /ui/validate/{field}: CheckRoutes = %q, want one entry for it", got)
	}

	if !reflect.DeepEqual(authweb.PublicPaths, []string{"/signin", "/setup", "/static/*", "/healthz"}) {
		t.Errorf("PublicPaths = %q", authweb.PublicPaths)
	}
	if !reflect.DeepEqual(authweb.CheckedInHandlerPrefixes, []string{"/doc/"}) {
		t.Errorf("CheckedInHandlerPrefixes = %q, want [/doc/] (CD-87)", authweb.CheckedInHandlerPrefixes)
	}
}

// AUTH-115
func TestGuest_OnlyPublicRoutes(t *testing.T) {
	w := authtest.NewWorld(t)
	h, st := t10Harness(t, w)
	c := h.Client()

	for _, tc := range []struct{ path, loc string }{
		{"/", "/signin?next=%2F"},
		{"/po/list", "/signin?next=%2Fpo%2Flist"},
		{"/doc/authtest.Order/list", "/signin?next=%2Fdoc%2Fauthtest.Order%2Flist"},
		{"/users", "/signin?next=%2Fusers"},
	} {
		r := c.Get(tc.path)
		wantRedirect(t, "guest GET "+tc.path, r, http.StatusSeeOther, tc.loc)
		for _, leak := range []string{"HOME", "PO LIST", "DOC LIST", "USERS"} {
			if strings.Contains(r.Body, leak) {
				t.Errorf("guest GET %s: body leaks %q", tc.path, leak)
			}
		}
	}

	api := c.Get("/api/po/1")
	wantStatus(t, "guest GET /api/po/1", api, http.StatusUnauthorized)
	if !strings.Contains(api.Body, auth.ErrSignInRequired.Msg) || strings.Contains(api.Body, `"id"`) {
		t.Errorf("guest GET /api/po/1: body %q, want %q and no data", api.Body, auth.ErrSignInRequired.Msg)
	}

	hx := c.HTMX().Get("/po/list")
	wantStatus(t, "guest HTMX GET /po/list", hx, http.StatusUnauthorized)
	if got := hx.Header.Get("HX-Redirect"); got != "/signin?next=%2Fpo%2Flist" {
		t.Errorf("guest HTMX HX-Redirect = %q", got)
	}
	if !strings.Contains(messageOf(hx), auth.ErrSignInRequired.Msg) || strings.Contains(hx.Body, "PO LIST") {
		t.Errorf("guest HTMX: body %q, HX-Trigger %q, want %q and no data", hx.Body, hx.Header.Get("HX-Trigger"), auth.ErrSignInRequired.Msg)
	}

	post := c.Post("/po/1/save", url.Values{"name": {"x"}})
	wantStatus(t, "guest POST /po/1/save", post, http.StatusUnauthorized)
	if st.Saves() != 0 {
		t.Errorf("guest POST reached the handler %d times", st.Saves())
	}

	wantStatus(t, "guest GET /static/app.css", c.Get("/static/app.css"), http.StatusOK)
}

func TestGuard_CheckedInHandler(t *testing.T) {
	w := authtest.NewWorld(t)
	h, st := t10Harness(t, w)

	deepak := signedIn(t, h, t10Deepak)
	r := deepak.Get("/doc/authtest.Order/list")
	wantStatus(t, "deepak GET /doc/authtest.Order/list", r, http.StatusForbidden)
	if !strings.Contains(r.Body, t10DenyReadPO) || strings.Contains(r.Body, "DOC LIST") {
		t.Fatalf("deepak body = %q, want %q and no data", r.Body, t10DenyReadPO)
	}

	priya := signedIn(t, h, t10Priya)
	r = priya.Get("/doc/authtest.Order/list")
	wantStatus(t, "priya GET /doc/authtest.Order/list", r, http.StatusOK)
	if !strings.Contains(r.Body, "DOC LIST authtest.Order") {
		t.Fatalf("priya body = %q, want DOC LIST authtest.Order", r.Body)
	}

	r = priya.NoCSRF().Post("/doc/authtest.Order/1/save", url.Values{"name": {"x"}})
	wantStatus(t, "NoCSRF POST to CheckedInHandler", r, http.StatusForbidden)
	if !strings.Contains(r.Body, auth.ErrCSRF.Msg) {
		t.Fatalf("NoCSRF POST body = %q, want %q", r.Body, auth.ErrCSRF.Msg)
	}
	if st.DocSaves() != 0 {
		t.Fatalf("NoCSRF POST reached the handler %d times", st.DocSaves())
	}
}

func TestWriteError_HTTPError(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)

	denied := &auth.DeniedError{Right: auth.RightRead, Label: "Purchase Order", Plural: "Purchase Orders"}
	for _, tc := range []struct {
		name   string
		err    error
		status int
		want   string
		absent string
	}{
		{"busy", fmt.Errorf("x: %w", db.ErrBusy), http.StatusServiceUnavailable, "The system is busy. Please try again in a moment.", ""},
		{"not found", fmt.Errorf("x: %w", doctype.ErrNotFound), http.StatusNotFound, auth.MsgNotFound, ""},
		{"denied", denied, http.StatusForbidden, t10DenyReadPO, ""},
		{"other", errors.New("boom"), http.StatusInternalServerError, t10InternalMsg, "boom"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/po/list", nil)
			h.Srv.WriteError(rec, req, tc.err)
			body := html.UnescapeString(rec.Body.String())
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tc.status, body)
			}
			if !strings.Contains(body, tc.want) {
				t.Fatalf("body = %q, want %q", body, tc.want)
			}
			if tc.absent != "" && strings.Contains(body, tc.absent) {
				t.Fatalf("body = %q leaks %q", body, tc.absent)
			}
		})
	}
	if auth.MsgNotFound != "This record doesn't exist or you don't have access to it." {
		t.Fatalf("MsgNotFound = %q", auth.MsgNotFound)
	}
}

// Review Focus 4
func TestClientIP_TrustedProxyOnly(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/signin", nil)
	r.RemoteAddr = "10.0.0.2:5555"
	r.Header.Set("X-Forwarded-For", "203.0.113.7")
	if got := authweb.ClientIP(r, false); got != "10.0.0.2" {
		t.Errorf("ClientIP(untrusted) = %q, want 10.0.0.2", got)
	}
	if got := authweb.ClientIP(r, true); got != "203.0.113.7" {
		t.Errorf("ClientIP(trusted) = %q, want 203.0.113.7", got)
	}

	r.Header.Set("X-Forwarded-For", "198.51.100.9, 203.0.113.7")
	if got := authweb.ClientIP(r, true); got != "203.0.113.7" {
		t.Errorf("ClientIP(trusted, two entries) = %q, want the last entry 203.0.113.7", got)
	}
	if got := authweb.ClientIP(r, false); got != "10.0.0.2" {
		t.Errorf("ClientIP(untrusted, two entries) = %q, want 10.0.0.2", got)
	}
}

func TestSafeNext(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"/po/list?status=draft", "/po/list?status=draft"},
		{"/", "/"},
		{"", "/"},
		{"po/list", "/"},
		{"//evil.example/x", "/"},
		{`/\evil.example/x`, "/"},
		{"https://evil.example/x", "/"},
		{"javascript:alert(1)", "/"},
	} {
		if got := authweb.SafeNext(tc.in); got != tc.want {
			t.Errorf("SafeNext(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestGuard_StripsOwnerFields(t *testing.T) {
	w := authtest.NewWorld(t)
	h, st := t10Harness(t, w)
	c := signedIn(t, h, t10Priya)

	r := c.Post("/po/1/save", url.Values{
		"name":       {"x"},
		"created_by": {"3"},
		"created_at": {"2026-10-24T05:00:00.000Z"},
		"updated_by": {"3"},
		"updated_at": {"2026-10-24T05:00:00.000Z"},
	})
	wantStatus(t, "POST /po/1/save", r, http.StatusOK)
	if st.Saves() != 1 {
		t.Fatalf("handler calls = %d, want 1", st.Saves())
	}
	st.mu.Lock()
	pf, f := st.lastPForm, st.lastForm
	st.mu.Unlock()
	for _, k := range []string{"created_by", "created_at", "updated_by", "updated_at"} {
		if pf.Get(k) != "" || f.Get(k) != "" {
			t.Errorf("handler sees %s: PostForm %q, Form %q; want stripped", k, pf.Get(k), f.Get(k))
		}
	}
	if pf.Get("name") != "x" || f.Get("name") != "x" {
		t.Errorf("handler sees name PostForm %q, Form %q; want x kept", pf.Get("name"), f.Get("name"))
	}
}

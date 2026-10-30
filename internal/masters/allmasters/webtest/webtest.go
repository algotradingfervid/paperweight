// Package webtest serves master screens in tests (MST Task 22): the routes one
// <pkg>web.Routes returns, mounted on platform/web's router over msttest's
// database and real kernel services. Each request carries the given principal
// on its context, where AUTH's session middleware puts it in production
// (auth.WithPrincipal, CD-12), and an scs session reachable by web.Flash
// (CD-80, CD-150). It imports no internal/ui package (CD-89): it reads the
// HTML only through goquery.
package webtest

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/platform/web"
)

// Harness is one master's screens over a msttest.Env.
type Harness struct {
	Env     *msttest.Env
	Router  chi.Router
	handler http.Handler
}

// New builds msttest.New(t), then mounts routes(env.Kit) on web.NewRouter
// with an scs session manager and no AUTH middleware (the principal is put on
// each request by Get and Post). Register the masters under test (import
// their packages) before calling it, so msttest grants their rights.
func New(t testing.TB, routes func(k mstkit.Env) func(chi.Router)) *Harness {
	t.Helper()
	return NewSessions(t, func(k mstkit.Env, _ *scs.SessionManager) func(chi.Router) { return routes(k) })
}

// NewSessions is New for routes that use the router's session manager
// themselves. A test of the production wiring calls allmasters.New(k).Routes
// inside routes, with sm in its WebDeps; this package can't call it, because
// allmasters reaches internal/ui (CD-89). The manager and store are AUTH's, so
// overlapping requests behave as in production (CD-223).
func NewSessions(t testing.TB, routes func(k mstkit.Env, sm *scs.SessionManager) func(chi.Router)) *Harness {
	t.Helper()
	env := msttest.New(t)
	cfg := auth.DefaultSessionConfig(false)
	sm := auth.NewSessionManager(auth.NewSessionStore(env.DB, env.Clock, cfg), cfg)
	r := web.NewRouter(web.Options{Sessions: sm})
	routes(env.Kit, sm)(r)
	m := metaRouter{Router: r, cookie: sm.Cookie.Name}
	return &Harness{Env: env, Router: m, handler: m}
}

// metaRouter tells AUTH's store which token the request presented, as AUTH's
// middleware does around the session load (CD-223).
type metaRouter struct {
	chi.Router
	cookie string
}

func (m metaRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var hash string
	if c, err := r.Cookie(m.cookie); err == nil && c.Value != "" {
		hash = auth.HashToken(c.Value)
	}
	m.Router.ServeHTTP(w, r.WithContext(auth.WithRequestMeta(r.Context(), hash, "", r.UserAgent())))
}

// Response is one recorded answer.
type Response struct {
	Status   int
	Location string
	Header   http.Header
	Body     string
	Doc      *goquery.Document
}

// Get sends a full-page GET as p.
func (h *Harness) Get(t testing.TB, p auth.Principal, target string) Response {
	t.Helper()
	return h.do(t, p, http.MethodGet, target, nil, false, nil)
}

// Follow GETs r's Location as p, sending the session cookie r set, as a
// browser does after a 303: the next page shows the flash the write put in
// the session (CD-80). It fails the test unless r is a redirect that set a
// cookie.
func (h *Harness) Follow(t testing.TB, p auth.Principal, r Response) Response {
	t.Helper()
	if r.Location == "" {
		t.Fatalf("follow: response %d has no Location\nbody: %s", r.Status, Squash(r.Body))
	}
	cookies := (&http.Response{Header: r.Header}).Cookies()
	if len(cookies) == 0 {
		t.Fatalf("follow %s: the response set no cookie, so the session (and its flash) is lost", r.Location)
	}
	return h.do(t, p, http.MethodGet, r.Location, nil, false, cookies)
}

// GetHTMX sends a GET as p with HX-Request: true (pickers, fragments).
func (h *Harness) GetHTMX(t testing.TB, p auth.Principal, target string) Response {
	t.Helper()
	return h.do(t, p, http.MethodGet, target, nil, true, nil)
}

// Post sends form as p, form-encoded, as a full-page (no-JS) submit.
func (h *Harness) Post(t testing.TB, p auth.Principal, target string, form url.Values) Response {
	t.Helper()
	if form == nil {
		form = url.Values{}
	}
	return h.do(t, p, http.MethodPost, target, form, false, nil)
}

// Browser is one browser's cookie jar: its requests carry the session the
// earlier ones set, which a wizard's step values live in (CD-50).
type Browser struct {
	h       *Harness
	p       auth.Principal
	cookies map[string]*http.Cookie
}

// Browser is a new browser with no cookies, signed in as p.
func (h *Harness) Browser(p auth.Principal) *Browser {
	return &Browser{h: h, p: p, cookies: map[string]*http.Cookie{}}
}

// As is the same browser, with its cookies, signed in as p.
func (b *Browser) As(p auth.Principal) *Browser {
	return &Browser{h: b.h, p: p, cookies: b.cookies}
}

// Clone is a second tab: the same session, with cookies of its own, so two tabs
// can post at once.
func (b *Browser) Clone() *Browser {
	return &Browser{h: b.h, p: b.p, cookies: maps.Clone(b.cookies)}
}

// Get sends a full-page GET.
func (b *Browser) Get(t testing.TB, target string) Response {
	t.Helper()
	return b.send(t, http.MethodGet, target, nil, false)
}

// Post sends form as a full-page (no-JS) submit.
func (b *Browser) Post(t testing.TB, target string, form url.Values) Response {
	t.Helper()
	return b.send(t, http.MethodPost, target, form, false)
}

// PostHTMX sends form with HX-Request: true, as the wizard's forms do.
func (b *Browser) PostHTMX(t testing.TB, target string, form url.Values) Response {
	t.Helper()
	return b.send(t, http.MethodPost, target, form, true)
}

func (b *Browser) send(t testing.TB, method, target string, form url.Values, htmx bool) Response {
	t.Helper()
	jar := make([]*http.Cookie, 0, len(b.cookies))
	for _, c := range b.cookies {
		jar = append(jar, c)
	}
	r := b.h.do(t, b.p, method, target, form, htmx, jar)
	for _, c := range (&http.Response{Header: r.Header}).Cookies() {
		b.cookies[c.Name] = c
	}
	return r
}

func (h *Harness) do(t testing.TB, p auth.Principal, method, target string, form url.Values, htmx bool, cookies []*http.Cookie) Response {
	t.Helper()
	var req *http.Request
	if form != nil {
		req = httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	req = req.WithContext(auth.WithPrincipal(req.Context(), p))
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rec.Body.String()))
	if err != nil {
		t.Fatalf("%s %s: parse body: %v", method, target, err)
	}
	return Response{
		Status:   rec.Code,
		Location: rec.Header().Get("Location"),
		Header:   rec.Header(),
		Body:     rec.Body.String(),
		Doc:      doc,
	}
}

// Squash collapses runs of white space into one space and trims the ends.
func Squash(s string) string { return strings.Join(strings.Fields(s), " ") }

// Text is the page's visible text, white space collapsed.
func (r Response) Text() string { return Squash(r.Doc.Text()) }

// WantStatus fails the test unless r has status code.
func WantStatus(t testing.TB, r Response, code int) {
	t.Helper()
	if r.Status != code {
		t.Fatalf("status = %d; want %d\nbody: %s", r.Status, code, Squash(r.Body))
	}
}

// WantRedirect fails the test unless r is a 303 See Other to location.
func WantRedirect(t testing.TB, r Response, location string) {
	t.Helper()
	if r.Status != http.StatusSeeOther || r.Location != location {
		t.Fatalf("got %d Location %q; want 303 to %q\nbody: %s", r.Status, r.Location, location, Squash(r.Body))
	}
}

// WantText fails the test unless the page text contains text.
func WantText(t testing.TB, r Response, text string) {
	t.Helper()
	if !strings.Contains(r.Text(), text) {
		t.Errorf("page text lacks %q\ntext: %s", text, r.Text())
	}
}

// WantNoText fails the test if the page text contains text.
func WantNoText(t testing.TB, r Response, text string) {
	t.Helper()
	if strings.Contains(r.Text(), text) {
		t.Errorf("page text contains %q; want it absent\ntext: %s", text, r.Text())
	}
}

// Banners is the text of each banner in the page's main region (flash
// messages and page banners, design-system §7), in page order.
func (r Response) Banners() []string {
	var out []string
	r.Doc.Find("#main .banner").Each(func(_ int, s *goquery.Selection) {
		out = append(out, Squash(s.Text()))
	})
	return out
}

// WantBanner fails the test unless a banner in the main region shows msg.
func WantBanner(t testing.TB, r Response, msg string) {
	t.Helper()
	for _, b := range r.Banners() {
		if strings.Contains(b, msg) {
			return
		}
	}
	t.Errorf("no banner shows %q; banners: %q\ntext: %s", msg, r.Banners(), r.Text())
}

// FieldMessage is the text of the message region tied to the form control
// named name through aria-describedby (design-system §7 Field), and whether
// such a control exists. It also reports whether that control is marked
// aria-invalid="true".
func (r Response) FieldMessage(name string) (msg string, invalid, found bool) {
	var parts []string
	r.Doc.Find(`[name="` + name + `"]`).Each(func(_ int, s *goquery.Selection) {
		ids, ok := s.Attr("aria-describedby")
		if !ok {
			return
		}
		found = true
		if v, _ := s.Attr("aria-invalid"); v == "true" {
			invalid = true
		}
		for _, id := range strings.Fields(ids) {
			parts = append(parts, r.Doc.Find(`[id="`+id+`"]`).Text())
		}
	})
	return Squash(strings.Join(parts, " ")), invalid, found
}

// WantFieldError fails the test unless the control named field shows msg in
// its own message region and is marked aria-invalid (the error is next to
// its field, not only in the page).
func WantFieldError(t testing.TB, r Response, field, msg string) {
	t.Helper()
	got, invalid, found := r.FieldMessage(field)
	switch {
	case !found:
		t.Errorf("no control named %q with aria-describedby; want %q next to it\ntext: %s", field, msg, r.Text())
	case !strings.Contains(got, msg):
		t.Errorf("message next to %q = %q; want %q", field, got, msg)
	case !invalid:
		t.Errorf("control %q shows %q but is not aria-invalid=\"true\"", field, msg)
	}
}

// WantFieldErrorID fails the test unless the control with element id id has a
// message region reading exactly msg and is marked aria-invalid. A form with
// two pickers has two search inputs named q, so WantFieldError can't tell them
// apart. Callers pass components.FieldID(field), which this package can't
// import (CD-89).
func WantFieldErrorID(t testing.TB, r Response, id, msg string) {
	t.Helper()
	s := r.Doc.Find(`[id="` + id + `"]`).First()
	refs, ok := s.Attr("aria-describedby")
	if !ok {
		t.Errorf("no control %s with aria-describedby; want %q next to it\ntext: %s", id, msg, r.Text())
		return
	}
	var got []string
	for _, ref := range strings.Fields(refs) {
		got = append(got, Squash(r.Doc.Find(`[id="`+ref+`"]`).Text()))
	}
	if !slices.Contains(got, msg) {
		t.Errorf("messages next to %s = %q; want %q", id, got, msg)
	}
	if v, _ := s.Attr("aria-invalid"); v != "true" {
		t.Errorf("control %s shows %q but is not aria-invalid", id, msg)
	}
}

// HasControl reports whether the page has a form control named name.
func (r Response) HasControl(name string) bool {
	return r.Doc.Find(`input[name="`+name+`"], select[name="`+name+`"], textarea[name="`+name+`"]`).Length() > 0
}

// Value is the value of the first input or textarea named name (a select's
// selected option value), and whether it exists.
func (r Response) Value(name string) (string, bool) {
	s := r.Doc.Find(`input[name="` + name + `"]:not([type="checkbox"]):not([type="radio"])`).First()
	if s.Length() > 0 {
		v, _ := s.Attr("value")
		return v, true
	}
	if s = r.Doc.Find(`textarea[name="` + name + `"]`).First(); s.Length() > 0 {
		return s.Text(), true
	}
	if s = r.Doc.Find(`select[name="` + name + `"]`).First(); s.Length() > 0 {
		v, _ := s.Find("option[selected]").First().Attr("value")
		return v, true
	}
	return "", false
}

// FormValue is the value of the input named name in the form posting to
// action, and whether it exists. A page can have several such inputs: the save
// form and each More action carry their own hidden version.
func (r Response) FormValue(action, name string) (string, bool) {
	return r.Doc.Find(`form[action="` + action + `"] input[name="` + name + `"]`).First().Attr("value")
}

// CheckboxValue is the value a ticked checkbox named name posts ("on" when it
// has no value attribute, as in a browser), and whether it exists.
func (r Response) CheckboxValue(name string) (string, bool) {
	s := r.Doc.Find(`input[type="checkbox"][name="` + name + `"]`).First()
	if s.Length() == 0 {
		return "", false
	}
	if v, ok := s.Attr("value"); ok {
		return v, true
	}
	return "on", true
}

// Choice is one option of a select or radio group: what it posts and what it
// shows.
type Choice struct{ Value, Label string }

// Choices lists the options of the select or radio group named name in page
// order. A select's blank placeholder option (value "") is skipped.
func (r Response) Choices(name string) []Choice {
	var out []Choice
	r.Doc.Find(`select[name="` + name + `"] option`).Each(func(_ int, s *goquery.Selection) {
		v, _ := s.Attr("value")
		if v == "" {
			return
		}
		out = append(out, Choice{Value: v, Label: Squash(s.Text())})
	})
	r.Doc.Find(`input[type="radio"][name="` + name + `"]`).Each(func(_ int, s *goquery.Selection) {
		v, _ := s.Attr("value")
		label := ""
		if id, ok := s.Attr("id"); ok {
			label = Squash(r.Doc.Find(`label[for="` + id + `"]`).Text())
		}
		if label == "" {
			label = Squash(s.Closest("label").Text())
		}
		out = append(out, Choice{Value: v, Label: label})
	})
	return out
}

// PickRow is one picker result: the id it posts and its visible text.
type PickRow struct{ Value, Text string }

// PickRows lists the selectable picker results (role="option" elements that
// are not aria-disabled, i.e. not the "No matches" row) in page order.
func (r Response) PickRows() []PickRow {
	var out []PickRow
	r.Doc.Find(`[role="option"]`).Each(func(_ int, s *goquery.Selection) {
		if v, _ := s.Attr("aria-disabled"); v == "true" {
			return
		}
		v, _ := s.Attr("data-value")
		out = append(out, PickRow{Value: v, Text: Squash(s.Text())})
	})
	return out
}

// RowLink is the text of the list row linking to href, and whether the page
// has one (§8.1: the whole card is one link to the record).
func (r Response) RowLink(href string) (string, bool) {
	s := r.Doc.Find(`a[href="` + href + `"]`)
	if s.Length() == 0 {
		return "", false
	}
	return Squash(s.First().Text()), true
}

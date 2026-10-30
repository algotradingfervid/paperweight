// Package webtest serves master screens in tests (MST Task 22): the routes one
// <pkg>web.Routes returns, mounted on platform/web's router over msttest's
// database and real kernel services. Each request carries the given principal
// on its context, where AUTH's session middleware puts it in production
// (auth.WithPrincipal, CD-12), and an scs session reachable by web.Flash
// (CD-80, CD-150). It imports no internal/ui package (CD-89): it reads the
// HTML only through goquery.
package webtest

import (
	"net/http"
	"net/http/httptest"
	"net/url"
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
// their packages) before calling it, so msttest grants their rights. A test
// of the production wiring passes allmasters.New(k).Routes as routes; this
// package can't call it, because allmasters reaches internal/ui (CD-89).
func New(t testing.TB, routes func(k mstkit.Env) func(chi.Router)) *Harness {
	t.Helper()
	env := msttest.New(t)
	r := web.NewRouter(web.Options{Sessions: scs.New()})
	routes(env.Kit)(r)
	return &Harness{Env: env, Router: r, handler: r}
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

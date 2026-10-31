package docscreen_test

import (
	"context"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
)

// M0-17 (CD-263 G6, CD-280 D5; hooks spec §4.1; purchase UX §5.6, §5.7):
// a Body returning no component keeps the generic step, nav=stay saves and
// redraws the same step at its bare URL, Step.Bar changes the step's action
// bar, and Step.Top renders above the generic body. Every test runs on
// docscreentest.Hooked on a fixture database, the real engine and the real
// AUTH service (m02Env, CD-125).
//
// The items fixture is UX §5.7's Add from requests: the Items Body shows a
// custom view only for ?from=requests (or a post of its hidden m17_from), and
// otherwise returns no component, so the generic Items body shows.

// m17Long is a ref one character over Hooked's MaxLen 12: the engine's 422.
const m17Long = "THIRTEEN-CHAR"

// m17NoTick is the custom view's Save.Errors text (UX §5.7).
const m17NoTick = "Tick at least one line to add."

// m17Raw is a component writing s.
func m17Raw(s string) templ.Component {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, s)
		return err
	})
}

// m17Requests reports whether a request is for the custom Add from requests
// view: ?from=requests on a GET, or the view's own hidden input on a POST.
func m17Requests(c docscreen.Ctx) bool {
	return c.Req.URL.Query().Get("from") == "requests" || c.Req.PostFormValue("m17_from") == "requests"
}

// m17Call is what one Body call saw.
type m17Call struct {
	Method, URL string
	Posted      bool
}

// m17Log records Body calls.
type m17Log struct {
	mu    sync.Mutex
	calls []m17Call
}

func (l *m17Log) saw(c docscreen.Ctx, s docscreen.StepState) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, m17Call{Method: c.Req.Method, URL: c.Req.URL.String(), Posted: s.Posted})
}

// take returns the calls since the last take and forgets them.
func (l *m17Log) take() []m17Call {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.calls
	l.calls = nil
	return out
}

// nilBody is a Body that always returns no component.
func (l *m17Log) nilBody(c docscreen.Ctx, s docscreen.StepState) (templ.Component, error) {
	l.saw(c, s)
	return nil, nil
}

// itemsBody is the Add from requests view for m17Requests, else no
// component: #m17-requests with data-posted, its hidden m17_from and a
// quantity input m17_qty showing the typed value on a redraw.
func (l *m17Log) itemsBody(c docscreen.Ctx, s docscreen.StepState) (templ.Component, error) {
	l.saw(c, s)
	if !m17Requests(c) {
		return nil, nil
	}
	qty := ""
	if s.Posted {
		qty = c.Req.PostFormValue("m17_qty")
	}
	return m17Raw(`<section id="m17-requests" data-posted="` + strconv.FormatBool(s.Posted) + `">` +
		`<h2>Add from requests</h2><input type="hidden" name="m17_from" value="requests"/>` +
		`<label for="m17-qty">Quantity to order</label><input id="m17-qty" name="m17_qty" value="` + html.EscapeString(qty) + `"/>` +
		`</section>`), nil
}

// m17ItemsSave is the custom view's Save: no quantity is Save.Errors; a
// quantity keeps the loaded lines (by id) and adds one BOLT-M12 line. Outside
// the view it keeps the stored lines.
func m17ItemsSave(e *hookedEnv) func(c docscreen.Ctx) (docscreen.Save, error) {
	return func(c docscreen.Ctx) (docscreen.Save, error) {
		if !m17Requests(c) {
			return docscreen.Save{}, nil
		}
		qty := c.Req.PostFormValue("m17_qty")
		if qty == "" {
			return docscreen.Save{Errors: []doctype.FieldError{{Message: m17NoTick}}}, nil
		}
		var lines []map[string]string
		for _, l := range c.View.Lines {
			lines = append(lines, map[string]string{"id": l.ID})
		}
		m := e.fx.M
		lines = append(lines, map[string]string{"project_id": itoa(m.Project["PRJ-METRO"]), "item_id": itoa(m.Item["BOLT-M12"]), "qty": qty, "note": "m17 added"})
		return docscreen.Save{Lines: lines}, nil
	}
}

// m17RequestsBar is the custom view's bar (UX §5.7): Add selected posting
// nav=stay, and a Back to items link; the zero StepBar elsewhere.
func m17RequestsBar(base string) func(c docscreen.Ctx, s docscreen.StepState) (docscreen.StepBar, error) {
	return func(c docscreen.Ctx, _ docscreen.StepState) (docscreen.StepBar, error) {
		if !m17Requests(c) {
			return docscreen.StepBar{}, nil
		}
		return docscreen.StepBar{Primary: "Add selected", Stay: true, Back: &components.Link{Label: "Back to items", Href: base + "/edit/items"}}, nil
	}
}

// m17Hdr is the htmx request header when hx is set.
func m17Hdr(hx bool) []string {
	if hx {
		return []string{"HX-Request", "true"}
	}
	return nil
}

// m17What names a request for messages.
func m17What(what string, hx bool) string {
	return what + " (htmx " + strconv.FormatBool(hx) + ")"
}

// m17Location is a 303's raw Location, query included.
func m17Location(t *testing.T, rec *httptest.ResponseRecorder, what string) string {
	t.Helper()
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("%s = %d, want 303; body %.500s", what, rec.Code, rec.Body)
	}
	return rec.Header().Get("Location")
}

// m17Primary is the step's primary button: the bar's one .btn--primary.
func m17Primary(t *testing.T, page *goquery.Document, what string) *goquery.Selection {
	t.Helper()
	p := page.Find("#wizard .actionbar .btn--primary")
	if p.Length() != 1 {
		t.Fatalf("%s: %d primary buttons in the action bar, want 1", what, p.Length())
	}
	return p
}

// m17WantStayPrimary fails unless the primary is a submit labelled label
// that posts nav=stay.
func m17WantStayPrimary(t *testing.T, page *goquery.Document, label, what string) {
	t.Helper()
	p := m17Primary(t, page, what)
	if goquery.NodeName(p) != "button" || m02Attr(p, "type") != "submit" {
		t.Errorf("%s: primary is <%s type=%s>, want a submit button", what, goquery.NodeName(p), m02Attr(p, "type"))
	}
	if got := norm(p.Text()); got != label {
		t.Errorf("%s: primary = %q, want %q", what, got, label)
	}
	if n, v := m02Attr(p, "name"), m02Attr(p, "value"); n != "nav" || v != "stay" {
		t.Errorf("%s: primary posts %s=%s, want nav=stay", what, n, v)
	}
}

// m17WantPlainPrimary fails unless the primary is labelled label and posts
// no nav (the default: save and continue).
func m17WantPlainPrimary(t *testing.T, page *goquery.Document, label, what string) {
	t.Helper()
	p := m17Primary(t, page, what)
	if got := norm(p.Text()); got != label {
		t.Errorf("%s: primary = %q, want %q", what, got, label)
	}
	if n := m02Attr(p, "name"); n != "<none>" {
		t.Errorf("%s: primary has name %q, want none (the default nav)", what, n)
	}
}

// m17WantBackLink fails unless the bar has Back link label → href in place
// of the Back submit, and keeps Save and finish later.
func m17WantBackLink(t *testing.T, page *goquery.Document, label, href, what string) {
	t.Helper()
	if n := page.Find(`button[name="nav"][value="back"]`).Length(); n != 0 {
		t.Errorf("%s: %d Back submit buttons, want none: the Back link replaces it", what, n)
	}
	links := page.Find("#wizard .actionbar a").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return norm(s.Text()) == label
	})
	if links.Length() != 1 {
		t.Errorf("%s: %d %q links in the action bar, want 1", what, links.Length(), label)
	} else if got := m02Attr(links, "href"); got != href {
		t.Errorf("%s: %q link goes to %q, want %q", what, label, got, href)
	}
	m17WantFinishLater(t, page, what)
}

// m17WantFinishLater fails unless Save and finish later shows once.
func m17WantFinishLater(t *testing.T, page *goquery.Document, what string) {
	t.Helper()
	fin := page.Find(`button[name="nav"][value="finish"]`)
	if fin.Length() != 1 || norm(fin.Text()) != "Save and finish later" {
		t.Errorf("%s: %d Save and finish later buttons (%q), want 1", what, fin.Length(), norm(fin.Text()))
	}
}

// m17WantGenericItems fails unless page shows docscreen's generic Items
// body and not the custom view.
func m17WantGenericItems(t *testing.T, page *goquery.Document, what string) {
	t.Helper()
	if n := page.Find("#m17-requests").Length(); n != 0 {
		t.Errorf("%s: the custom Add from requests view shows, want the generic Items body", what)
	}
	if n := page.Find(`#wizard #grid-alerts`).Length(); n != 1 {
		t.Errorf("%s: %d #grid-alerts, want the generic lines region once", what, n)
	}
	if n := page.Find(`#wizard a[href$="/lines/new"]`).Length(); n == 0 {
		t.Errorf("%s: no Add an item link, want the generic lines region", what)
	}
}

// m17SetTop sets s.Top to top by name, so this file compiles before
// Step.Top (CD-280 D5) exists: until then every TestModule_StepTop case
// fails here, naming the missing field.
func m17SetTop(t *testing.T, s docscreen.Step, top func(docscreen.Ctx) (templ.Component, error)) docscreen.Step {
	t.Helper()
	f := reflect.ValueOf(&s).Elem().FieldByName("Top")
	if !f.IsValid() {
		t.Fatalf("docscreen.Step has no Top field; want Top func(c Ctx) (templ.Component, error) (CD-280 D5)")
	}
	v := reflect.ValueOf(top)
	if !v.Type().AssignableTo(f.Type()) {
		t.Fatalf("docscreen.Step.Top is %s, want func(c Ctx) (templ.Component, error) (CD-280 D5)", f.Type())
	}
	f.Set(v)
	return s
}

// m17Top is a Top returning #m17-top.
func m17Top(docscreen.Ctx) (templ.Component, error) {
	return m17Raw(`<div id="m17-top"><a href="/m17/requests">Add from requests (2 open)</a></div>`), nil
}

// m17Before reports whether the first match of a comes before the first
// match of b in sel's HTML; false when either is missing.
func m17Before(sel *goquery.Selection, a, b string) bool {
	h, err := goquery.OuterHtml(sel)
	if err != nil {
		return false
	}
	i, j := strings.Index(h, a), strings.Index(h, b)
	return i >= 0 && j >= 0 && i < j
}

// HOOK-026: a Body that returns a nil component keeps docscreen's generic
// body for that render: the step's inputs with the stored values, a 422
// redraw with the typed value, and on the Items step the generic lines
// region (M.After) with a line error linking to the line editor. The same
// Body returning a component replaces them as before.
func TestModule_StepBodyFallback(t *testing.T) {
	e, _, id := m02Env(t)
	log := &m17Log{}
	m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{
		"terms": {Body: log.nilBody},
		"items": {Body: log.itemsBody},
	}})
	base := m02URL(id)

	t.Run("terms", func(t *testing.T) {
		for _, hx := range []bool{false, true} {
			what := m17What("GET terms with a nil Body", hx)
			log.take()
			page := m02Page(t, e.get(t, base+"/edit/terms", m17Hdr(hx)...), http.StatusOK, what)
			if len(log.take()) == 0 {
				t.Errorf("%s: the Body was never called", what)
			}
			form := stepForm(page, base+"/edit/terms")
			if form.Length() != 1 {
				t.Fatalf("%s: no step form posting to %s/edit/terms", what, base)
			}
			if got := m02Attr(form.Find(`input[name="ref"]`), "value"); got != "HK-REF" {
				t.Errorf("%s: generic ref input = %q, want the stored HK-REF in the step form", what, got)
			}
			if n := m02Named(form, "remarks"); n != 1 {
				t.Errorf("%s: %d generic remarks inputs in the step form, want 1", what, n)
			}
			if strings.Contains(norm(page.Find("#wizard").Text()), "Nothing needed here") {
				t.Errorf("%s: shows Nothing needed here, want the generic inputs", what)
			}
		}
	})

	t.Run("terms 422 keeps the typed value", func(t *testing.T) {
		before := m02Rec(t, e, id)
		audits := len(e.fx.Audit(t, docscreentest.Hooked, id))
		for _, hx := range []bool{false, true} {
			what := m17What("POST terms with a ref too long and a nil Body", hx)
			form := url.Values{"version": {strconv.Itoa(before.Version)}, "ref": {m17Long}, "remarks": {"m17 remarks"}}
			page := m02Page(t, e.post(t, base+"/edit/terms", form, m17Hdr(hx)...), http.StatusUnprocessableEntity, what)
			if got := m02Attr(stepForm(page, base+"/edit/terms").Find(`input[name="ref"]`), "value"); got != m17Long {
				t.Errorf("%s: generic ref input = %q, want the typed %q", what, got, m17Long)
			}
			if page.Find(".error-summary").Length() == 0 {
				t.Errorf("%s: no error summary", what)
			}
		}
		m02WantUnchanged(t, e, id, before, audits, "the refused terms save")
	})

	t.Run("items", func(t *testing.T) {
		for _, hx := range []bool{false, true} {
			what := m17What("GET items with a nil Body", hx)
			page := m02Page(t, e.get(t, base+"/edit/items", m17Hdr(hx)...), http.StatusOK, what)
			m17WantGenericItems(t, page, what)
			if strings.Contains(norm(page.Find("#wizard").Text()), "Nothing needed here") {
				t.Errorf("%s: shows Nothing needed here, want the generic Items body", what)
			}

			what = m17What("GET items?from=requests", hx)
			page = m02Page(t, e.get(t, base+"/edit/items?from=requests", m17Hdr(hx)...), http.StatusOK, what)
			if n := stepForm(page, base+"/edit/items").Find("#m17-requests").Length(); n != 1 {
				t.Errorf("%s: %d custom views in the step form, want 1: the Body's component replaces the generic body", what, n)
			}
			if n := page.Find(`#grid-alerts, a[href$="/lines/new"]`).Length(); n != 0 {
				t.Errorf("%s: %d generic lines elements beside the custom view, want none", what, n)
			}
		}
	})

	t.Run("items line error links to the line editor", func(t *testing.T) {
		// The Save sends line 1 with a quantity the engine refuses (qty is
		// Positive), so the 422 carries a line error.
		bad := func(c docscreen.Ctx) (docscreen.Save, error) {
			return docscreen.Save{Lines: []map[string]string{{"id": c.View.Lines[0].ID, "qty": "-1.000"}, {"id": c.View.Lines[1].ID}}}, nil
		}
		m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"items": {Body: log.itemsBody, Save: bad}}})
		before := m02Rec(t, e, id)
		audits := len(e.fx.Audit(t, docscreentest.Hooked, id))
		rec := e.post(t, base+"/edit/items", url.Values{"version": {strconv.Itoa(before.Version)}})
		page := m02Page(t, rec, http.StatusUnprocessableEntity, "POST items with a negative quantity on line 1 and a nil Body")
		m02WantUnchanged(t, e, id, before, audits, "the refused items save")
		m17WantGenericItems(t, page, "the items 422")
		var hrefs []string
		page.Find(".error-summary a").Each(func(_ int, s *goquery.Selection) { hrefs = append(hrefs, m02Attr(s, "href")) })
		if !slices.Contains(hrefs, "#line-editor") {
			t.Errorf("error summary links = %q, want the line error linked to #line-editor, the generic grid editor that shows", hrefs)
		}
	})
}

// HOOK-027: nav=stay saves the step and redraws the same step at its bare
// URL: a 303 there, or for htmx the step's #wizard with HX-Push-Url to it.
// return=check isn't followed, a query on the step URL is dropped (UX §5.7:
// Add selected returns to the generic Items view), and a 422 keeps the typed
// values with nothing written.
func TestModule_StepStay(t *testing.T) {
	e, _, id := m02Env(t)
	base := m02URL(id)

	t.Run("generic step", func(t *testing.T) {
		for _, hx := range []bool{false, true} {
			what := m17What("POST terms nav=stay with return=check", hx)
			before := m02Rec(t, e, id)
			ref := "M17-STAY-" + strconv.FormatBool(hx)[:1]
			form := url.Values{"version": {strconv.Itoa(before.Version)}, "ref": {ref}, "remarks": {"stayed"}, "nav": {"stay"}, "return": {"check"}}
			rec := e.post(t, base+"/edit/terms?return=check", form, m17Hdr(hx)...)
			if !hx {
				if got := m17Location(t, rec, what); got != base+"/edit/terms" {
					t.Errorf("%s went to %q, want the bare terms URL %s/edit/terms", what, got, base)
				}
			} else {
				page := m02Page(t, rec, http.StatusOK, what)
				if got := rec.Header().Get("HX-Push-Url"); got != base+"/edit/terms" {
					t.Errorf("%s: HX-Push-Url = %q, want the bare terms URL %s/edit/terms", what, got, base)
				}
				if got := rootIDs(t, rec); !slices.Equal(got, []string{"wizard"}) {
					t.Errorf("%s: roots = %q, want only #wizard", what, got)
				}
				if got := norm(page.Find("h1#step-title").Text()); got != "Terms" {
					t.Errorf("%s: heading = %q, want the same step, Terms", what, got)
				}
				if got := stepCount(page.Selection); got != "Step 2 of 4" {
					t.Errorf("%s: stepper = %q, want Step 2 of 4", what, got)
				}
				f := stepForm(page, base+"/edit/terms")
				if got := m02Attr(f.Find(`input[name="ref"]`), "value"); got != ref {
					t.Errorf("%s: redrawn ref = %q, want the saved %q", what, got, ref)
				}
				if got := m02Attr(f.Find("#doc-version"), "value"); got != strconv.Itoa(before.Version+1) {
					t.Errorf("%s: #doc-version = %q, want the saved version %d", what, got, before.Version+1)
				}
			}
			after := m02Rec(t, e, id)
			if after.Version != before.Version+1 || after.Values["ref"] != ref || after.Values["remarks"] != "stayed" {
				t.Errorf("%s: stored version %d ref %q remarks %q, want %d, %s, stayed: stay saves", what, after.Version, after.Values["ref"], after.Values["remarks"], before.Version+1, ref)
			}
		}
	})

	t.Run("generic step 422", func(t *testing.T) {
		before := m02Rec(t, e, id)
		audits := len(e.fx.Audit(t, docscreentest.Hooked, id))
		for _, hx := range []bool{false, true} {
			what := m17What("POST terms nav=stay with a ref too long", hx)
			form := url.Values{"version": {strconv.Itoa(before.Version)}, "ref": {m17Long}, "remarks": {"m17 typed"}, "nav": {"stay"}}
			page := m02Page(t, e.post(t, base+"/edit/terms", form, m17Hdr(hx)...), http.StatusUnprocessableEntity, what)
			f := stepForm(page, base+"/edit/terms")
			if got := m02Attr(f.Find(`input[name="ref"]`), "value"); got != m17Long {
				t.Errorf("%s: ref = %q, want the typed %q", what, got, m17Long)
			}
			if got := f.Find(`[name="remarks"]`).Text() + m02Attr(f.Find(`input[name="remarks"]`), "value"); !strings.Contains(got, "m17 typed") {
				t.Errorf("%s: remarks = %q, want the typed m17 typed", what, got)
			}
		}
		m02WantUnchanged(t, e, id, before, audits, "the refused stay")
	})

	t.Run("custom view returns to the generic step", func(t *testing.T) {
		log := &m17Log{}
		m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{
			"items": {Body: log.itemsBody, Save: m17ItemsSave(e), Bar: m17RequestsBar(base)},
		}})
		for _, hx := range []bool{false, true} {
			what := m17What("POST items from the custom view with nav=stay", hx)
			before := m02Rec(t, e, id)
			form := url.Values{"version": {strconv.Itoa(before.Version)}, "m17_from": {"requests"}, "m17_qty": {"4.000"}, "nav": {"stay"}}
			log.take()
			rec := e.post(t, base+"/edit/items?from=requests", form, m17Hdr(hx)...)
			if !hx {
				if got := m17Location(t, rec, what); got != base+"/edit/items" {
					t.Errorf("%s went to %q, want the bare items URL %s/edit/items", what, got, base)
				}
			} else {
				page := m02Page(t, rec, http.StatusOK, what)
				if got := rec.Header().Get("HX-Push-Url"); got != base+"/edit/items" {
					t.Errorf("%s: HX-Push-Url = %q, want the bare items URL %s/edit/items", what, got, base)
				}
				if got := norm(page.Find("h1#step-title").Text()); got != "Items" {
					t.Errorf("%s: heading = %q, want the same step, Items", what, got)
				}
				m17WantGenericItems(t, page, what+" redraw")
				m17WantPlainPrimary(t, page, "Save and continue", what+" redraw")
				for _, c := range log.take() {
					if c.Posted {
						t.Errorf("%s: redraw Body call %+v, want Posted false: the step as its bare URL shows it", what, c)
					}
				}
			}
			after := m02Rec(t, e, id)
			if after.Version != before.Version+1 || len(after.Lines) != len(before.Lines)+1 || after.Lines[len(after.Lines)-1]["note"] != "m17 added" {
				t.Errorf("%s: stored version %d with lines %v, want version %d with one m17 added line appended", what, after.Version, after.Lines, before.Version+1)
			}
		}
	})

	t.Run("custom view 422", func(t *testing.T) {
		before := m02Rec(t, e, id)
		audits := len(e.fx.Audit(t, docscreentest.Hooked, id))
		for _, hx := range []bool{false, true} {
			what := m17What("POST items from the custom view with nothing ticked", hx)
			form := url.Values{"version": {strconv.Itoa(before.Version)}, "m17_from": {"requests"}, "nav": {"stay"}}
			page := m02Page(t, e.post(t, base+"/edit/items", form, m17Hdr(hx)...), http.StatusUnprocessableEntity, what)
			v := stepForm(page, base+"/edit/items").Find("#m17-requests")
			if v.Length() != 1 || m02Attr(v, "data-posted") != "true" {
				t.Errorf("%s: %d custom views (posted %s), want the view redrawn from the post", what, v.Length(), m02Attr(v, "data-posted"))
			}
			if got := norm(page.Find(".error-summary").Text()); !strings.Contains(got, m17NoTick) {
				t.Errorf("%s: error summary = %q, want %q", what, got, m17NoTick)
			}
		}
		m02WantUnchanged(t, e, id, before, audits, "the refused add")
	})
}

// HOOK-028: Step.Bar changes the step's action bar on every render: Primary
// relabels the primary, Stay makes it post nav=stay, and Back is a link in
// place of the Back submit, saving nothing. Save and finish later stays, and
// with a Body the primary is still the bar's, the form's first submit, so
// Enter presses it. A zero StepBar is byte-identical to no hook; an error is
// a 500.
func TestModule_StepBar(t *testing.T) {
	e, _, id := m02Env(t)
	base := m02URL(id)
	log := &m17Log{}
	items := docscreen.Step{Body: log.itemsBody, Save: m17ItemsSave(e), Bar: m17RequestsBar(base)}

	t.Run("custom view", func(t *testing.T) {
		m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"items": items}})
		for _, hx := range []bool{false, true} {
			what := m17What("GET items?from=requests", hx)
			page := m02Page(t, e.get(t, base+"/edit/items?from=requests", m17Hdr(hx)...), http.StatusOK, what)
			form := stepForm(page, base+"/edit/items")
			if form.Find("#m17-requests").Length() != 1 {
				t.Fatalf("%s: no custom view in the step form", what)
			}
			m17WantStayPrimary(t, page, "Add selected", what)
			m17WantBackLink(t, page, "Back to items", base+"/edit/items", what)
			if first := form.Find(`button[type="submit"]`).First(); norm(first.Text()) != "Add selected" {
				t.Errorf("%s: the step form's first submit is %q, want the primary Add selected (Enter presses it)", what, norm(first.Text()))
			}
		}
	})

	t.Run("422 redraw keeps the bar", func(t *testing.T) {
		form := url.Values{"version": {m02Version(t, e, id)}, "m17_from": {"requests"}, "nav": {"stay"}}
		page := m02Page(t, e.post(t, base+"/edit/items", form), http.StatusUnprocessableEntity, "POST items with nothing ticked")
		m17WantStayPrimary(t, page, "Add selected", "the 422 redraw")
		m17WantBackLink(t, page, "Back to items", base+"/edit/items", "the 422 redraw")
	})

	t.Run("primary on a generic step", func(t *testing.T) {
		m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"terms": {Bar: func(docscreen.Ctx, docscreen.StepState) (docscreen.StepBar, error) {
			return docscreen.StepBar{Primary: "Save terms"}, nil
		}}}})
		for _, hx := range []bool{false, true} {
			what := m17What("GET terms with Bar.Primary", hx)
			page := m02Page(t, e.get(t, base+"/edit/terms", m17Hdr(hx)...), http.StatusOK, what)
			m17WantPlainPrimary(t, page, "Save terms", what)
			if n := stepForm(page, base+"/edit/terms").Find(`button[name="nav"][value="back"]`).Length(); n != 1 {
				t.Errorf("%s: %d Back submit buttons, want docscreen's one (Back nil keeps it)", what, n)
			}
			if n := m02Named(page.Selection, "ref"); n != 1 {
				t.Errorf("%s: %d generic ref inputs, want 1", what, n)
			}
			m17WantFinishLater(t, page, what)
		}
	})

	t.Run("back link on a generic step", func(t *testing.T) {
		m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"terms": {Bar: func(docscreen.Ctx, docscreen.StepState) (docscreen.StepBar, error) {
			return docscreen.StepBar{Back: &components.Link{Label: "Back to details", Href: base + "/edit/details"}}, nil
		}}}})
		for _, hx := range []bool{false, true} {
			what := m17What("GET terms with Bar.Back", hx)
			page := m02Page(t, e.get(t, base+"/edit/terms", m17Hdr(hx)...), http.StatusOK, what)
			m17WantPlainPrimary(t, page, "Save and continue", what)
			m17WantBackLink(t, page, "Back to details", base+"/edit/details", what)
		}
	})

	t.Run("stay on the generic items step", func(t *testing.T) {
		// Without a Body the bar sits after the lines region, outside the
		// step form, and posts it through its form attribute.
		m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"items": {Bar: func(docscreen.Ctx, docscreen.StepState) (docscreen.StepBar, error) {
			return docscreen.StepBar{Primary: "Add selected", Stay: true}, nil
		}}}})
		page := m02Page(t, e.get(t, base+"/edit/items"), http.StatusOK, "GET generic items with Bar.Stay")
		m17WantStayPrimary(t, page, "Add selected", "generic items")
		if got := m02Attr(m17Primary(t, page, "generic items"), "form"); got != "wizard-form" {
			t.Errorf("generic items: primary form = %q, want wizard-form (the bar is outside the step form)", got)
		}
	})

	t.Run("zero StepBar is byte-identical", func(t *testing.T) {
		zero := func(docscreen.Ctx, docscreen.StepState) (docscreen.StepBar, error) { return docscreen.StepBar{}, nil }
		paths := []string{"/edit/details", "/edit/terms", "/edit/items"}
		render := func(m docscreen.Module) map[string]string {
			m02Mount(e, m)
			out := map[string]string{}
			for _, p := range paths {
				for _, hx := range []bool{false, true} {
					rec := e.get(t, base+p, m17Hdr(hx)...)
					if rec.Code != http.StatusOK {
						t.Fatalf("GET %s = %d, want 200; body %.300s", p, rec.Code, rec.Body)
					}
					out[m17What(p, hx)] = rec.Body.String()
				}
			}
			return out
		}
		plain := render(docscreen.Module{})
		hooked := render(docscreen.Module{Steps: map[string]docscreen.Step{"details": {Bar: zero}, "terms": {Bar: zero}, "items": {Bar: zero}}})
		for k, want := range plain {
			if hooked[k] != want {
				t.Errorf("GET %s with a zero StepBar differs from no hook:\n got %.400s\nwant %.400s", k, hooked[k], want)
			}
		}
	})

	t.Run("error is a 500", func(t *testing.T) {
		m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"terms": {Bar: func(docscreen.Ctx, docscreen.StepState) (docscreen.StepBar, error) {
			return docscreen.StepBar{}, errFor("m17 bar broke")
		}}}})
		for _, hx := range []bool{false, true} {
			if rec := e.get(t, base+"/edit/terms", m17Hdr(hx)...); rec.Code != http.StatusInternalServerError {
				t.Errorf("%s = %d, want 500; body %.300s", m17What("GET terms with a failing Bar", hx), rec.Code, rec.Body)
			}
		}
	})
}

// HOOK-043 (CD-280 D5): Step.Top renders above the generic step body, inside
// the step form, when Body is unset or returns nil, on every render
// (including /new and a 422 redraw); not when Body returns a component. An
// error is a 500. Step.Top is set by reflection (m17SetTop), so until it is
// declared each case fails naming the missing field.
func TestModule_StepTop(t *testing.T) {
	e, _, id := m02Env(t)
	base := m02URL(id)
	log := &m17Log{}

	wantTop := func(t *testing.T, page *goquery.Document, action, next, what string) {
		t.Helper()
		form := stepForm(page, action)
		if form.Length() != 1 {
			t.Fatalf("%s: no step form posting to %s", what, action)
		}
		if n := page.Find("#m17-top").Length(); n != 1 {
			t.Fatalf("%s: %d Tops, want 1", what, n)
		}
		if form.Find("#m17-top").Length() != 1 {
			t.Errorf("%s: Top is outside the step form", what)
		}
		if !m17Before(form, `id="m17-top"`, next) {
			t.Errorf("%s: Top is not above the generic body (%s)", what, next)
		}
	}

	t.Run("no Body", func(t *testing.T) {
		m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"terms": m17SetTop(t, docscreen.Step{}, m17Top)}})
		for _, hx := range []bool{false, true} {
			what := m17What("GET terms with Top", hx)
			page := m02Page(t, e.get(t, base+"/edit/terms", m17Hdr(hx)...), http.StatusOK, what)
			wantTop(t, page, base+"/edit/terms", `name="ref"`, what)
			if got := m02Attr(stepForm(page, base+"/edit/terms").Find(`input[name="ref"]`), "value"); got != "HK-REF" {
				t.Errorf("%s: generic ref = %q, want the stored HK-REF", what, got)
			}
		}
	})

	t.Run("Body returns nil", func(t *testing.T) {
		m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"terms": m17SetTop(t, docscreen.Step{Body: log.nilBody}, m17Top)}})
		for _, hx := range []bool{false, true} {
			what := m17What("GET terms with Top and a nil Body", hx)
			page := m02Page(t, e.get(t, base+"/edit/terms", m17Hdr(hx)...), http.StatusOK, what)
			wantTop(t, page, base+"/edit/terms", `name="ref"`, what)
		}
	})

	t.Run("Body returns a component", func(t *testing.T) {
		m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"items": m17SetTop(t, docscreen.Step{Body: log.itemsBody}, m17Top)}})
		for _, hx := range []bool{false, true} {
			what := m17What("GET items?from=requests with Top", hx)
			page := m02Page(t, e.get(t, base+"/edit/items?from=requests", m17Hdr(hx)...), http.StatusOK, what)
			if n := page.Find("#m17-requests").Length(); n != 1 {
				t.Errorf("%s: %d custom views, want the Body's", what, n)
			}
			if n := page.Find("#m17-top").Length(); n != 0 {
				t.Errorf("%s: %d Tops beside a Body's component, want none", what, n)
			}
		}
		// The same step without the query: Body returns nil, so Top shows,
		// in the step form above the generic lines region.
		for _, hx := range []bool{false, true} {
			what := m17What("GET items with Top and a nil Body", hx)
			page := m02Page(t, e.get(t, base+"/edit/items", m17Hdr(hx)...), http.StatusOK, what)
			wantTop(t, page, base+"/edit/items", `name="version"`, what)
			m17WantGenericItems(t, page, what)
			if !m17Before(page.Find("#wizard"), `id="m17-top"`, `id="grid-alerts"`) {
				t.Errorf("%s: Top is not above the generic lines region", what)
			}
		}
	})

	t.Run("422 redraw and /new", func(t *testing.T) {
		m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{
			"details": m17SetTop(t, docscreen.Step{}, m17Top),
			"terms":   m17SetTop(t, docscreen.Step{}, m17Top),
		}})
		form := url.Values{"version": {m02Version(t, e, id)}, "ref": {m17Long}, "remarks": {"x"}}
		page := m02Page(t, e.post(t, base+"/edit/terms", form), http.StatusUnprocessableEntity, "POST terms with a ref too long")
		wantTop(t, page, base+"/edit/terms", `name="ref"`, "the 422 redraw")
		page = m02Page(t, e.get(t, m02NewURL), http.StatusOK, "GET /new")
		wantTop(t, page, m02NewURL, `name="supplier_id"`, "GET /new")
	})

	t.Run("error is a 500", func(t *testing.T) {
		broke := func(docscreen.Ctx) (templ.Component, error) { return nil, errFor("m17 top broke") }
		m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"terms": m17SetTop(t, docscreen.Step{}, broke)}})
		for _, hx := range []bool{false, true} {
			if rec := e.get(t, base+"/edit/terms", m17Hdr(hx)...); rec.Code != http.StatusInternalServerError {
				t.Errorf("%s = %d, want 500; body %.300s", m17What("GET terms with a failing Top", hx), rec.Code, rec.Body)
			}
		}
	})
}

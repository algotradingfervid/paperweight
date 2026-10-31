package docscreen_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// M0-50 (CD-323 (1)): a step's Save, on /new or on a draft's step, refused
// with a 403 that carries field errors (CD-240's CheckLinkTx refusal for a
// posted record the viewer can't read, which a module puts on its field)
// redraws the step as a 422, the status htmx swaps: the errors go through the
// hide filter and the page keeps the posted values and version (CD-94). A
// 403 with no field errors is still the problem page. Every test runs on
// Hooked with the real engine and the real AUTH service: the restricted user
// (TECH-SITE) posts the open user's METRO-SITE draft, which they can't read.

const m50Ref = "M50-TYPED" // the typed reference the redraw keeps

// m50Refusal is a module's refusal with status, put on its field.
type m50Refusal struct {
	status int
	field  string
	err    error
}

func (e m50Refusal) Error() string { return e.err.Error() }
func (e m50Refusal) Unwrap() error { return e.err }
func (e m50Refusal) Status() int   { return e.status }
func (e m50Refusal) FieldErrors() []doctype.FieldError {
	return []doctype.FieldError{{Field: e.field, Message: e.err.Error()}}
}

// m50Env is the Hooked harness as the restricted user, with the open user's
// METRO-SITE draft (other) and the restricted user's own TECH-SITE draft
// (own). Step slug's Save checks the link to other on the real AUTH service
// and returns its refusal on field with status, or as it is when field is "";
// hide are the fields the module hides.
func m50Env(t *testing.T, slug, field string, status int, hide ...string) (e *hookedEnv, own int64, linkMsg string) {
	t.Helper()
	e = newHookedEnv(t)
	other, err := e.draft(t, e.open, "METRO-SITE")
	if err != nil {
		t.Fatalf("saving the open user's draft: %v", err)
	}
	mine, err := e.draft(t, e.restricted, "TECH-SITE")
	if err != nil {
		t.Fatalf("saving the restricted user's draft: %v", err)
	}
	linkErr := e.d.Auth.CheckLink(t.Context(), e.restricted, docscreentest.Hooked, other.ID)
	if linkErr == nil {
		t.Fatalf("CheckLink(restricted, the METRO-SITE draft) = nil, want CD-240's refusal")
	}
	save := func(c docscreen.Ctx) (docscreen.Save, error) {
		err := c.Deps.Auth.CheckLink(c.Req.Context(), c.Principal, docscreentest.Hooked, other.ID)
		if err == nil {
			return docscreen.Save{}, nil
		}
		if field == "" {
			return docscreen.Save{}, err
		}
		return docscreen.Save{}, m50Refusal{status: status, field: field, err: err}
	}
	m := docscreen.Module{Steps: map[string]docscreen.Step{slug: {Save: save}}}
	if len(hide) > 0 {
		m.Hide = m01bHide(hide...)
	}
	m01bMount(e, docscreentest.Hooked, m)
	e.who = e.restricted
	return e, mine.ID, linkErr.Error()
}

// m50Value is the value the page's input for name shows: a select's chosen
// option, or the input's value.
func m50Value(page *goquery.Document, name string) string {
	in := page.Find(`[name="` + name + `"]`).First()
	if goquery.NodeName(in) == "select" {
		v, _ := in.Find("option[selected]").Attr("value")
		return v
	}
	if goquery.NodeName(in) == "textarea" {
		return in.Text()
	}
	v, _ := in.Attr("value")
	return v
}

// m50Hdrs are the two ways a step posts: a plain form and htmx.
var m50Hdrs = []struct {
	name string
	hdr  []string
}{{"plain", nil}, {"htmx", []string{"HX-Request", "true"}}}

// m50Redrawn checks a 422 redraw of the step form posting to action: the
// error summary shows msg, linked to field's input (or plain when field is
// hidden), and the form keeps the posted values.
func m50Redrawn(t *testing.T, rec *httptest.ResponseRecorder, action, field string, linked bool, msg string, posted map[string]string) {
	t.Helper()
	page := m04Page(t, rec, http.StatusUnprocessableEntity, "POST "+action)
	if stepForm(page, action).Length() != 1 {
		t.Fatalf("POST %s: the 422 has no step form posting to %s; body %.600s", action, action, rec.Body)
	}
	if got := norm(page.Find(".error-summary").Text()); !strings.Contains(got, msg) {
		t.Errorf("error summary = %q, want %q", got, msg)
	}
	links := summaryLinks(page)
	if has := slices.Contains(links, "#"+components.FieldID(field)); has != linked {
		t.Errorf("error summary links %q; a link to %s = %v, want %v", links, field, has, linked)
	}
	if !linked && m01bNamed(page, field) != 0 {
		t.Errorf("the redraw renders the hidden %s", field)
	}
	for name, want := range posted {
		if got := m50Value(page, name); got != want {
			t.Errorf("redrawn %s = %q, want the posted %q", name, got, want)
		}
	}
	wantNoDeadLinks(t, rec)
}

// m50Problem checks a 403 problem page with msg and no step form.
func m50Problem(t *testing.T, rec *httptest.ResponseRecorder, what, msg string) {
	t.Helper()
	page := m04Page(t, rec, http.StatusForbidden, what)
	if page.Find("#wizard-form").Length() != 0 || page.Find(`#wizard form[method="post"]`).Length() != 0 {
		t.Errorf("%s: got the step form, want the problem page", what)
	}
	if got := norm(page.Find("#main").Text()); !strings.Contains(got, msg) {
		t.Errorf("%s: page text = %q, want %q", what, got, msg)
	}
}

// newPost: the details step's Save refused with a 403 on a field.
func TestForbid50_NewPost(t *testing.T) {
	cases := []struct {
		name, field string
		hide        []string
		linked      bool
	}{
		{"field", "supplier_id", nil, true},
		{"hidden field", "site_id", []string{"site_id"}, false},
	}
	for _, c := range cases {
		for _, h := range m50Hdrs {
			t.Run(c.name+"/"+h.name, func(t *testing.T) {
				e, _, msg := m50Env(t, "details", c.field, http.StatusForbidden, c.hide...)
				supplier := itoa(e.fx.M.Party["RAJKOT"])
				form := url.Values{"supplier_id": {supplier}}
				if !c.linked {
					form.Set("site_id", itoa(e.site["TECH-SITE"]))
				}
				count := m02Count(t, e)
				rec := e.post(t, m02NewURL, form, h.hdr...)
				m50Redrawn(t, rec, m02NewURL, c.field, c.linked, msg, map[string]string{"supplier_id": supplier})
				if h.hdr != nil && strings.Contains(rec.Body.String(), "<html") {
					t.Errorf("htmx 422 drew the whole page, want the wizard fragment")
				}
				if got := m02Count(t, e); got != count {
					t.Errorf("Hooked documents = %d, want %d: nothing created", got, count)
				}
			})
		}
	}
}

// stepPost: the terms step's Save refused with a 403 on a field; the page
// keeps the posted version, never the stored one (CD-94).
func TestForbid50_StepPost(t *testing.T) {
	cases := []struct {
		name, field string
		hide        []string
		linked      bool
	}{
		{"field", "ref", nil, true},
		{"hidden field", "remarks", []string{"remarks"}, false},
	}
	for _, c := range cases {
		for _, h := range m50Hdrs {
			t.Run(c.name+"/"+h.name, func(t *testing.T) {
				e, own, msg := m50Env(t, "terms", c.field, http.StatusForbidden, c.hide...)
				action := m01bURL(own) + "/edit/terms"
				before := m01bRecord(t, e, own)
				rec := e.post(t, action, url.Values{"version": {"7"}, "ref": {m50Ref}, "remarks": {"m50 remarks"}}, h.hdr...)
				posted := map[string]string{"ref": m50Ref, "version": "7"}
				m50Redrawn(t, rec, action, c.field, c.linked, msg, posted)
				if h.hdr != nil && strings.Contains(rec.Body.String(), "<html") {
					t.Errorf("htmx 422 drew the whole page, want the wizard fragment")
				}
				if after := m01bRecord(t, e, own); after.Version != before.Version || after.Values["ref"] != before.Values["ref"] {
					t.Errorf("draft after the refusal = v%d ref %q, want v%d ref %q unchanged", after.Version, after.Values["ref"], before.Version, before.Values["ref"])
				}
			})
		}
	}
}

// A 403 with no field errors stays the problem page, from the Save (the
// refusal as AUTH gives it) and from the engine (the restricted user posting
// a site they may not use: AUTH's CheckValuesTx refusal).
func TestForbid50_NoFieldsIsProblemPage(t *testing.T) {
	t.Run("new/save", func(t *testing.T) {
		e, _, msg := m50Env(t, "details", "", 0)
		rec := e.post(t, m02NewURL, url.Values{"supplier_id": {itoa(e.fx.M.Party["RAJKOT"])}})
		m50Problem(t, rec, "POST /new", msg)
	})
	t.Run("step/save", func(t *testing.T) {
		e, own, msg := m50Env(t, "terms", "", 0)
		rec := e.post(t, m01bURL(own)+"/edit/terms", url.Values{"version": {m01bVersion(t, e, own)}, "ref": {m50Ref}})
		m50Problem(t, rec, "POST terms", msg)
	})
	t.Run("new/engine", func(t *testing.T) {
		e, _, _ := m50Env(t, "terms", "", 0)
		rec := e.post(t, m02NewURL, url.Values{"supplier_id": {itoa(e.fx.M.Party["RAJKOT"])}, "site_id": {itoa(e.site["METRO-SITE"])}})
		m50Problem(t, rec, "POST /new on METRO-SITE", "")
	})
	t.Run("step/engine", func(t *testing.T) {
		e, own, _ := m50Env(t, "terms", "", 0)
		rec := e.post(t, m01bURL(own)+"/edit/details", url.Values{
			"version": {m01bVersion(t, e, own)}, "supplier_id": {itoa(e.fx.M.Party["RAJKOT"])}, "site_id": {itoa(e.site["METRO-SITE"])},
		})
		m50Problem(t, rec, "POST details on METRO-SITE", "")
	})
}

// Only a 403 redraws: a Save's 409 on a field stays the problem page.
func TestForbid50_OtherStatusIsProblemPage(t *testing.T) {
	t.Run("new", func(t *testing.T) {
		e, _, msg := m50Env(t, "details", "supplier_id", http.StatusConflict)
		rec := e.post(t, m02NewURL, url.Values{"supplier_id": {itoa(e.fx.M.Party["RAJKOT"])}})
		if stepForm(m04Page(t, rec, http.StatusConflict, "POST /new"), m02NewURL).Length() != 0 {
			t.Errorf("POST /new with a 409 on a field redrew the step, want the problem page")
		}
		m50ProblemText(t, rec, "POST /new", msg)
	})
	t.Run("step", func(t *testing.T) {
		e, own, msg := m50Env(t, "terms", "ref", http.StatusConflict)
		action := m01bURL(own) + "/edit/terms"
		rec := e.post(t, action, url.Values{"version": {m01bVersion(t, e, own)}, "ref": {m50Ref}})
		if stepForm(m04Page(t, rec, http.StatusConflict, "POST terms"), action).Length() != 0 {
			t.Errorf("POST terms with a 409 on a field redrew the step, want the problem page")
		}
		m50ProblemText(t, rec, "POST terms", msg)
	})
}

// m50ProblemText fails unless the page's #main says msg.
func m50ProblemText(t *testing.T, rec *httptest.ResponseRecorder, what, msg string) {
	t.Helper()
	if got := norm(uitest.Doc(t, rec).Find("#main").Text()); !strings.Contains(got, msg) {
		t.Errorf("%s: page text = %q, want %q", what, got, msg)
	}
}

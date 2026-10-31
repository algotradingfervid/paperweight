package docscreen_test

import (
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
)

// m17FixNewStep is a new draft's bare first-step URL.
var m17FixNewStep = regexp.MustCompile(`^/doc/` + regexp.QuoteMeta(docscreentest.Hooked) + `/[0-9]+/edit/details$`)

// M0-17 review (AD ruling): nav=stay on /new creates the draft and lands on
// the new draft's own first-step URL, a 303 there, or for htmx the #wizard
// frame with HX-Push-Url to it, drawn as a plain GET of it (Posted false, no
// query): return=check isn't followed.
func TestModule_StepStayOnNew(t *testing.T) {
	e, _, _ := m02Env(t)
	log := &m17Log{}
	save := func(c docscreen.Ctx) (docscreen.Save, error) {
		return docscreen.Save{Values: map[string]string{
			"supplier_id": itoa(e.fx.M.Party["SHREE"]), "site_id": itoa(e.site["METRO-SITE"]), "ref": c.Req.PostFormValue("ref"),
		}}, nil
	}
	m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"details": {Body: log.nilBody, Save: save}}})
	for _, hx := range []bool{false, true} {
		what := m17What("POST /new nav=stay with return=check", hx)
		count := m02Count(t, e)
		form := url.Values{"ref": {"M17-NEW"}, "nav": {"stay"}, "return": {"check"}}
		log.take()
		rec := e.post(t, m02NewURL+"?return=check", form, m17Hdr(hx)...)
		if got := m02Count(t, e); got != count+1 {
			t.Errorf("%s: %d Hooked documents, want %d: stay creates the draft", what, got, count+1)
		}
		if !hx {
			if got := m17Location(t, rec, what); !m17FixNewStep.MatchString(got) {
				t.Errorf("%s went to %q, want the new draft's bare details URL", what, got)
			}
			continue
		}
		page := m02Page(t, rec, http.StatusOK, what)
		push := rec.Header().Get("HX-Push-Url")
		if !m17FixNewStep.MatchString(push) {
			t.Fatalf("%s: HX-Push-Url = %q, want the new draft's bare details URL", what, push)
		}
		if got := rootIDs(t, rec); !slices.Equal(got, []string{"wizard"}) {
			t.Errorf("%s: roots = %q, want only #wizard", what, got)
		}
		if got := stepCount(page.Selection); got != "Step 1 of 4" {
			t.Errorf("%s: stepper = %q, want Step 1 of 4", what, got)
		}
		f := stepForm(page, push)
		if f.Length() != 1 {
			t.Fatalf("%s: no step form posting to %s", what, push)
		}
		id, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(push, "/doc/"+docscreentest.Hooked+"/"), "/edit/details"), 10, 64)
		if err != nil {
			t.Fatalf("%s: HX-Push-Url %q has no draft id", what, push)
		}
		if rec := m02Rec(t, e, id); rec.Values["ref"] != "M17-NEW" {
			t.Errorf("%s: stored ref = %q, want M17-NEW", what, rec.Values["ref"])
		}
		if got, want := m02Attr(f.Find("#doc-version"), "value"), m02Version(t, e, id); got != want {
			t.Errorf("%s: #doc-version = %q, want the new draft's version %s", what, got, want)
		}
		if n := f.Find(`input[name="return"]`).Length(); n != 0 {
			t.Errorf("%s: %d return inputs, want none: stay drops return=check", what, n)
		}
		calls := log.take()
		var redraw []m17Call
		for _, c := range calls {
			if c.Method == http.MethodGet {
				redraw = append(redraw, c)
			}
		}
		if len(redraw) == 0 {
			t.Errorf("%s: Body calls %+v, want a redraw as a GET", what, calls)
		}
		for _, c := range redraw {
			if c.Posted || c.URL != push {
				t.Errorf("%s: redraw Body call %+v, want Posted false at %s", what, c, push)
			}
		}
	}
}

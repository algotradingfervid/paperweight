package docscreen_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// M0-3 (hooks spec §4.2; CD-256 (5), (6), (7), (9), (10), (11)): Module.Line,
// CardsOnly, NoLinePages, the line page's Back label and the line hooks' Ctx.
// Every test runs on docscreentest.Hooked on a fixture database, the real
// engine and the real AUTH service (CD-125), through the m01b harness.

const (
	m03Marker   = "M03 custom line body"
	m03NewNote  = "m03 new line"
	m03EditNote = "m03 edited line"
)

// m03Call is one call of a Line hook.
type m03Call struct {
	Hook      string // "body" or "save"
	Method    string
	No        int
	State     docscreen.StepState
	ViewID    int64 // 0 when c.View is nil
	DT        string
	Principal auth.UserID
}

// m03Hooks is a Line whose Body renders the qty FieldInput (typed value when
// Posted, else the stored one) inside #m03-body, and whose Save parses qty
// and returns this line's values: qty and note for an existing line, every
// field for a new one. A qty that doesn't parse is a Save.Errors.
type m03Hooks struct {
	e       *hookedEnv
	bodyErr error
	saveErr error
	lines   []map[string]string // returned as Save.Lines when non-nil

	mu    sync.Mutex
	calls []m03Call
}

func (h *m03Hooks) record(hook string, c docscreen.Ctx, no int, s docscreen.StepState) {
	call := m03Call{Hook: hook, No: no, State: s, Principal: c.Principal.ID}
	if c.Req != nil {
		call.Method = c.Req.Method
	}
	if c.View != nil {
		call.ViewID = c.View.Info.ID
	}
	if c.DT != nil {
		call.DT = c.DT.Name
	}
	h.mu.Lock()
	h.calls = append(h.calls, call)
	h.mu.Unlock()
}

// taken returns the calls so far and forgets them.
func (h *m03Hooks) taken() []m03Call {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := h.calls
	h.calls = nil
	return out
}

func (h *m03Hooks) line() *docscreen.Line {
	return &docscreen.Line{Body: h.body, Save: h.save}
}

func (h *m03Hooks) body(c docscreen.Ctx, no int, s docscreen.StepState) (templ.Component, error) {
	h.record("body", c, no, s)
	if h.bodyErr != nil {
		return nil, h.bodyErr
	}
	value := ""
	switch {
	case s.Posted:
		value = c.Req.PostFormValue("qty")
	case no > 0 && c.View != nil && no <= len(c.View.Lines):
		value = c.View.Lines[no-1].Inputs["qty"]
	}
	in := docscreen.FieldInput(m03Field("qty"), components.FieldID("qty"), value, lineErrorOf(s.Errors, "qty"), c.Deps)
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, `<div id="m03-body"><p>`+m03Marker+`</p>`); err != nil {
			return err
		}
		if err := in.Render(ctx, w); err != nil {
			return err
		}
		_, err := io.WriteString(w, `</div>`)
		return err
	}), nil
}

func (h *m03Hooks) save(c docscreen.Ctx, no int) (docscreen.Save, error) {
	h.record("save", c, no, docscreen.StepState{})
	if h.saveErr != nil {
		return docscreen.Save{}, h.saveErr
	}
	plain, fe := docscreen.ToPlain(m03Field("qty"), c.Req.PostFormValue("qty"))
	if fe != nil {
		return docscreen.Save{Errors: []doctype.FieldError{*fe}}, nil
	}
	s := docscreen.Save{Values: map[string]string{"qty": plain, "note": m03EditNote}, Lines: h.lines}
	if no == 0 {
		m := h.e.fx.M
		s.Values = map[string]string{
			"project_id": itoa(m.Project["PRJ-TECHPARK"]), "item_id": itoa(m.Item["BOLT-M12"]), "qty": plain, "note": m03NewNote,
		}
	}
	return s, nil
}

func lineErrorOf(errs []doctype.FieldError, name string) string {
	for _, e := range errs {
		if e.Field == name {
			return e.Message
		}
	}
	return ""
}

// m03Field is Hooked's field name.
func m03Field(name string) doctype.Field {
	docscreentest.RegisterHooked()
	dt, _ := doctype.Lookup(docscreentest.Hooked)
	f, _ := dt.Field(name)
	return f
}

// m03Env is m01bHooked with a Line module built from m03Hooks; adjust sets
// the hooks' failure modes and any other Module field before mounting.
func m03Env(t *testing.T, adjust func(h *m03Hooks, m *docscreen.Module)) (*hookedEnv, int64, *m03Hooks) {
	t.Helper()
	e, id := m01bHooked(t, docscreen.Module{})
	h := &m03Hooks{e: e}
	m := docscreen.Module{Line: h.line()}
	if adjust != nil {
		adjust(h, &m)
	}
	m01bMount(e, docscreentest.Hooked, m)
	return e, id, h
}

// m03Unchanged fails unless Hooked id is still before (nothing written).
func m03Unchanged(t *testing.T, e *hookedEnv, id int64, before doc.Record, what string) {
	t.Helper()
	after := m01bRecord(t, e, id)
	if after.Version != before.Version || !reflect.DeepEqual(after.Lines, before.Lines) || !reflect.DeepEqual(after.Values, before.Values) {
		t.Errorf("%s wrote: version %d → %d, lines %q → %q", what, before.Version, after.Version, before.Lines, after.Lines)
	}
}

// m03Status fails unless rec has status want.
func m03Status(t *testing.T, code, want int, what, body string) {
	t.Helper()
	if code != want {
		t.Fatalf("%s = %d, want %d; body %.400s", what, code, want, body)
	}
}

// m03InForm fails unless #m03-body is inside the form posting to action, and
// that form carries the version want. It returns the qty input's value.
func m03InForm(t *testing.T, page *goquery.Document, action, version string) string {
	t.Helper()
	body := page.Find("#m03-body")
	if body.Length() != 1 || !strings.Contains(norm(body.Text()), m03Marker) {
		t.Fatalf("found %d #m03-body with the marker, want the Line.Body once", body.Length())
	}
	form := body.Closest("form")
	if got, _ := form.Attr("action"); got != action {
		t.Errorf("Line.Body sits in a form posting to %q, want %q", got, action)
	}
	if got, _ := form.Find(`input[name="version"]`).Attr("value"); got != version {
		t.Errorf("the line form's version = %q, want %q", got, version)
	}
	v, _ := body.Find(`[name="qty"]`).Attr("value")
	return v
}

// m03Back fails unless the page's Back link reads label and goes to href.
func m03Back(t *testing.T, page *goquery.Document, label, href string) {
	t.Helper()
	back := page.Find("main a.back-link")
	if back.Length() != 1 {
		t.Fatalf("found %d Back links, want 1", back.Length())
	}
	if got := norm(back.Text()); got != label {
		t.Errorf("Back link reads %q, want %q", got, label)
	}
	if got, _ := back.Attr("href"); got != href {
		t.Errorf("Back link goes to %q, want %q", got, href)
	}
}

// m03NoGeneric fails when the generic line inputs render beside Line.Body.
func m03NoGeneric(t *testing.T, page *goquery.Document, what string) {
	t.Helper()
	for _, name := range []string{"project_id", "item_id", "note"} {
		if n := m01bNamed(page, name); n != 0 {
			t.Errorf("%s renders %d generic %s inputs, want only Line.Body's", what, n, name)
		}
	}
}

// HOOK-009: Line.Body and Line.Save replace linePage and postedLine for new
// and existing lines; Save's values are merged into the loaded lines as
// linePost does and saved with the posted version; docscreen keeps the URLs,
// version, Back, backToItems and remove.
func TestModule_LinePage(t *testing.T) {
	t.Run("new", func(t *testing.T) {
		e, id, h := m03Env(t, nil)
		base := m01bURL(id)
		before := m01bRecord(t, e, id)
		version := strconv.Itoa(before.Version)

		page := m01bOK(t, e.get(t, base+"/lines/new"), "GET /lines/new")
		if v := m03InForm(t, page, base+"/lines/new", version); v != "" {
			t.Errorf("new line's qty = %q, want empty", v)
		}
		m03NoGeneric(t, page, "GET /lines/new")
		m03Back(t, page, "Items", base+"/edit/items")
		want := []m03Call{{Hook: "body", Method: http.MethodGet, No: 0, ViewID: id, DT: docscreentest.Hooked, Principal: e.open.ID}}
		if got := h.taken(); !reflect.DeepEqual(got, want) {
			t.Errorf("hook calls on GET = %+v, want %+v", got, want)
		}

		form := url.Values{"version": {version}, "qty": {"3"}, "note": {"crafted"}, "item_id": {itoa(e.fx.M.Item["WIRE-18G"])}}
		if got := location(t, e.post(t, base+"/lines/new", form)); got != base+"/edit/items" {
			t.Errorf("new line save went to %q, want the items step", got)
		}
		want = []m03Call{{Hook: "save", Method: http.MethodPost, No: 0, ViewID: id, DT: docscreentest.Hooked, Principal: e.open.ID}}
		if got := h.taken(); !reflect.DeepEqual(got, want) {
			t.Errorf("hook calls on POST = %+v, want %+v", got, want)
		}
		after := m01bRecord(t, e, id)
		if len(after.Lines) != 3 {
			t.Fatalf("stored lines = %q, want 3", after.Lines)
		}
		m := e.fx.M
		l := after.Lines[2]
		if l["project_id"] != itoa(m.Project["PRJ-TECHPARK"]) || l["item_id"] != itoa(m.Item["BOLT-M12"]) || l["qty"] != "3.000" || l["note"] != m03NewNote {
			t.Errorf("new line = %q, want Line.Save's PRJ-TECHPARK, BOLT-M12, 3.000, %q (posted fields dropped)", l, m03NewNote)
		}
		if !reflect.DeepEqual(after.Lines[:2], before.Lines) {
			t.Errorf("lines 1 and 2 = %q, want unchanged %q", after.Lines[:2], before.Lines)
		}
		if after.Version == before.Version {
			t.Errorf("version %d unchanged after a save", after.Version)
		}
	})

	t.Run("edit", func(t *testing.T) {
		e, id, h := m03Env(t, nil)
		base := m01bURL(id)
		before := m01bRecord(t, e, id)
		version := strconv.Itoa(before.Version)
		v, err := e.d.View(t.Context(), e.open, docscreentest.Hooked, id)
		if err != nil {
			t.Fatalf("View: %v", err)
		}

		page := m01bOK(t, e.get(t, base+"/lines/1"), "GET /lines/1")
		if got, want := m03InForm(t, page, base+"/lines/1", version), v.Lines[0].Inputs["qty"]; got != want || want == "" {
			t.Errorf("line 1's qty in Line.Body = %q, want the stored %q", got, want)
		}
		m03NoGeneric(t, page, "GET /lines/1")
		m03Back(t, page, "Items", base+"/edit/items")
		want := []m03Call{{Hook: "body", Method: http.MethodGet, No: 1, ViewID: id, DT: docscreentest.Hooked, Principal: e.open.ID}}
		if got := h.taken(); !reflect.DeepEqual(got, want) {
			t.Errorf("hook calls on GET = %+v, want %+v", got, want)
		}

		m := e.fx.M
		form := url.Values{"version": {version}, "qty": {"7"}, "note": {"crafted"}, "item_id": {itoa(m.Item["WIRE-18G"])}}
		if got := location(t, e.post(t, base+"/lines/1", form)); got != base+"/edit/items" {
			t.Errorf("line 1 save went to %q, want the items step", got)
		}
		want = []m03Call{{Hook: "save", Method: http.MethodPost, No: 1, ViewID: id, DT: docscreentest.Hooked, Principal: e.open.ID}}
		if got := h.taken(); !reflect.DeepEqual(got, want) {
			t.Errorf("hook calls on POST = %+v, want %+v", got, want)
		}
		after := m01bRecord(t, e, id)
		if len(after.Lines) != 2 {
			t.Fatalf("stored lines = %q, want 2", after.Lines)
		}
		l := after.Lines[0]
		if l["project_id"] != itoa(m.Project["PRJ-METRO"]) || l["item_id"] != itoa(m.Item["CEMENT-OPC"]) {
			t.Errorf("line 1 project %q, item %q; want the loaded PRJ-METRO and CEMENT-OPC kept (merged, posted item dropped)", l["project_id"], l["item_id"])
		}
		if l["qty"] != "7.000" || l["note"] != m03EditNote {
			t.Errorf("line 1 qty %q, note %q; want Line.Save's 7.000 and %q", l["qty"], l["note"], m03EditNote)
		}
		if l["id"] != before.Lines[0]["id"] {
			t.Errorf("line 1 id %q → %q, want it updated in place", before.Lines[0]["id"], l["id"])
		}
		if !reflect.DeepEqual(after.Lines[1], before.Lines[1]) {
			t.Errorf("line 2 = %q, want unchanged %q", after.Lines[1], before.Lines[1])
		}
	})

	t.Run("Save.Errors is 422", func(t *testing.T) {
		e, id, h := m03Env(t, nil)
		base := m01bURL(id)
		before := m01bRecord(t, e, id)
		version := strconv.Itoa(before.Version)
		_, fe := docscreen.ToPlain(m03Field("qty"), "abc")
		if fe == nil {
			t.Fatal("ToPlain(qty, abc) accepted it: the case below would prove nothing")
		}
		for _, path := range []string{"/lines/1", "/lines/new"} {
			no := 1
			if path == "/lines/new" {
				no = 0
			}
			rec := e.post(t, base+path, url.Values{"version": {version}, "qty": {"abc"}})
			m03Status(t, rec.Code, http.StatusUnprocessableEntity, "POST "+path+" with qty abc", rec.Body.String())
			m03Unchanged(t, e, id, before, "a 422 on "+path)
			page := uitest.Doc(t, rec)
			if got := m03InForm(t, page, base+path, version); got != "abc" {
				t.Errorf("%s 422: qty in Line.Body = %q, want the typed abc", path, got)
			}
			m03NoGeneric(t, page, path+" 422")
			if got := norm(page.Find(".error-summary").Text()); !strings.Contains(got, fe.Message) {
				t.Errorf("%s 422: error summary = %q, want %q", path, got, fe.Message)
			}
			wantNoDeadLinks(t, rec)
			want := []m03Call{
				{Hook: "save", Method: http.MethodPost, No: no, ViewID: id, DT: docscreentest.Hooked, Principal: e.open.ID},
				{Hook: "body", Method: http.MethodPost, No: no, State: docscreen.StepState{Posted: true, Errors: []doctype.FieldError{*fe}}, ViewID: id, DT: docscreentest.Hooked, Principal: e.open.ID},
			}
			if got := h.taken(); !reflect.DeepEqual(got, want) {
				t.Errorf("%s 422: hook calls = %+v, want %+v", path, got, want)
			}
		}
	})

	t.Run("stale version is 409", func(t *testing.T) {
		e, id, h := m03Env(t, nil)
		base := m01bURL(id)
		old := m01bVersion(t, e, id)
		location(t, e.post(t, base+"/lines/2", url.Values{"version": {old}, "qty": {"6"}}))
		h.taken()
		before := m01bRecord(t, e, id)
		for _, path := range []string{"/lines/1", "/lines/new"} {
			no := 1
			if path == "/lines/new" {
				no = 0
			}
			rec := e.post(t, base+path, url.Values{"version": {old}, "qty": {"9"}})
			m03Status(t, rec.Code, http.StatusConflict, "POST "+path+" with a stale version", rec.Body.String())
			m03Unchanged(t, e, id, before, "a stale "+path)
			page := uitest.Doc(t, rec)
			body := page.Find("#m03-body")
			if body.Length() != 1 {
				t.Fatalf("%s 409: found %d #m03-body, want Line.Body redrawn", path, body.Length())
			}
			if got, _ := body.Find(`[name="qty"]`).Attr("value"); got != "9" {
				t.Errorf("%s 409: qty in Line.Body = %q, want the typed 9", path, got)
			}
			if got := strings.Join(bannerTexts(page.Selection, "danger"), " "); !strings.Contains(got, "Someone else changed") {
				t.Errorf("%s 409: danger banners = %q, want DOC's stale text", path, got)
			}
			calls := h.taken()
			if n := len(calls); n == 0 || calls[n-1].Hook != "body" || !calls[n-1].State.Posted || calls[n-1].No != no {
				t.Errorf("%s 409: hook calls = %+v, want Line.Body redrawn last with Posted true and no %d", path, calls, no)
			}
		}
	})

	t.Run("Save.Lines is 500", func(t *testing.T) {
		for _, c := range []struct {
			name  string
			lines []map[string]string
		}{
			{"one line", []map[string]string{{"qty": "1.000"}}},
			{"empty non-nil", []map[string]string{}},
		} {
			t.Run(c.name, func(t *testing.T) {
				e, id, _ := m03Env(t, func(h *m03Hooks, _ *docscreen.Module) { h.lines = c.lines })
				base := m01bURL(id)
				before := m01bRecord(t, e, id)
				for _, path := range []string{"/lines/1", "/lines/new"} {
					rec := e.post(t, base+path, url.Values{"version": {strconv.Itoa(before.Version)}, "qty": {"4"}})
					m03Status(t, rec.Code, http.StatusInternalServerError, "POST "+path+" with Save.Lines set", rec.Body.String())
					m03Unchanged(t, e, id, before, "Save.Lines on "+path)
				}
			})
		}
	})

	t.Run("Body error is 500", func(t *testing.T) {
		e, id, _ := m03Env(t, func(h *m03Hooks, _ *docscreen.Module) { h.bodyErr = errors.New("m03 body failed") })
		base := m01bURL(id)
		for _, path := range []string{"/lines/1", "/lines/new"} {
			rec := e.get(t, base+path)
			m03Status(t, rec.Code, http.StatusInternalServerError, "GET "+path+" with a failing Body", rec.Body.String())
		}
	})

	t.Run("Save error is 500", func(t *testing.T) {
		e, id, _ := m03Env(t, func(h *m03Hooks, _ *docscreen.Module) { h.saveErr = errors.New("m03 save failed") })
		base := m01bURL(id)
		before := m01bRecord(t, e, id)
		for _, path := range []string{"/lines/1", "/lines/new"} {
			rec := e.post(t, base+path, url.Values{"version": {strconv.Itoa(before.Version)}, "qty": {"4"}})
			m03Status(t, rec.Code, http.StatusInternalServerError, "POST "+path+" with a failing Save", rec.Body.String())
			m03Unchanged(t, e, id, before, "a failing Save on "+path)
		}
	})

	t.Run("remove", func(t *testing.T) {
		e, id, h := m03Env(t, nil)
		base := m01bURL(id)
		before := m01bRecord(t, e, id)
		page := m01bOK(t, e.get(t, base+"/lines/1/remove"), "GET /lines/1/remove")
		if !strings.Contains(norm(page.Find("main").Text()), "Remove line 1") {
			t.Errorf("remove page = %q, want docscreen's Remove line 1 question", norm(page.Find("main").Text()))
		}
		if page.Find("#m03-body").Length() != 0 {
			t.Errorf("remove page renders Line.Body, want docscreen's remove page")
		}
		m03Back(t, page, "Items", base+"/edit/items")
		rec := e.post(t, base+"/lines/1/remove", url.Values{"version": {strconv.Itoa(before.Version)}})
		if got := location(t, rec); got != base+"/edit/items" {
			t.Errorf("remove went to %q, want the items step", got)
		}
		after := m01bRecord(t, e, id)
		if len(after.Lines) != 1 || after.Lines[0]["note"] != "second" {
			t.Errorf("stored lines after removing line 1 = %q, want only the second", after.Lines)
		}
		if calls := h.taken(); len(calls) != 0 {
			t.Errorf("remove called the Line hooks: %+v", calls)
		}
	})

	t.Run("back", func(t *testing.T) {
		e, id, _ := m03Env(t, nil)
		base := m01bURL(id)
		for _, path := range []string{"/lines/1", "/lines/new"} {
			page := m01bOK(t, e.get(t, base+path), "GET "+path)
			m03Back(t, page, "Items", base+"/edit/items")
			href, _ := page.Find("main a.back-link").Attr("href")
			items := m01bOK(t, e.get(t, href), "GET the Back link")
			if items.Find(`[href="`+base+`/lines/new"]`).Length() == 0 {
				t.Errorf("%s: the Back link's page has no Add an item link: not the Items step", path)
			}
		}
	})
}

// m03LineRoutes are the 7 line routes (routes.go) on draft base, with line 1.
func m03LineRoutes(base string) []struct{ method, path string } {
	return []struct{ method, path string }{
		{http.MethodGet, base + "/lines/new"},
		{http.MethodPost, base + "/lines/new"},
		{http.MethodGet, base + "/lines/1"},
		{http.MethodPost, base + "/lines/1"},
		{http.MethodGet, base + "/lines/1/remove"},
		{http.MethodPost, base + "/lines/1/remove"},
		{http.MethodPost, base + "/lines/1/move?dir=down"},
	}
}

// m03NoGrid fails when the Items step has any desktop grid markup, or lacks
// the cards and Add an item.
func m03NoGrid(t *testing.T, page *goquery.Document, base string) {
	t.Helper()
	for _, sel := range []string{"#line-editor", ".line-editor", ".linegrid", ".linelist--grid", "form.linegrid-form", `[id^="grid-row-"]`} {
		if n := page.Find(sel).Length(); n != 0 {
			t.Errorf("Items step has %d %s, want no grid", n, sel)
		}
	}
	if n := page.Find(".linelist .lines-card").Length(); n != 2 {
		t.Errorf("Items step has %d line cards, want 2", n)
	}
	if page.Find(`.linelist a[href="`+base+`/lines/new"]`).Length() != 1 {
		t.Errorf("Items step has no Add an item link to %s/lines/new", base)
	}
	for _, no := range []string{"1", "2"} {
		if page.Find(`.lines-card a[href="`+base+`/lines/`+no+`"]`).Length() != 1 {
			t.Errorf("Items step has no Change link to line %s", no)
		}
	}
}

// HOOK-018: NoLinePages answers 404 on every line route before any version
// check, and the Items step links to none; CardsOnly, and Module.Line, leave
// the Items step without the desktop grid but with its cards and Add, and
// the error summary links only to what is on the page.
func TestModule_LineFlags(t *testing.T) {
	t.Run("NoLinePages", func(t *testing.T) {
		e, id := m01bHooked(t, docscreen.Module{NoLinePages: true})
		base := m01bURL(id)
		before := m01bRecord(t, e, id)
		current := strconv.Itoa(before.Version)
		for _, rt := range m03LineRoutes(base) {
			versions := map[string]url.Values{"": nil}
			if rt.method == http.MethodPost {
				form := e.m01bLine1(current)
				stale := e.m01bLine1(strconv.Itoa(before.Version + 5))
				missing := e.m01bLine1(current)
				missing.Del("version")
				bad := e.m01bLine1("x")
				versions = map[string]url.Values{"current version": form, "stale version": stale, "missing version": missing, "non-numeric version": bad}
			}
			for name, form := range versions {
				rec := e.do(t, rt.method, rt.path, form)
				if rec.Code != http.StatusNotFound {
					t.Errorf("%s %s (%s) = %d, want 404; body %.200s", rt.method, rt.path, name, rec.Code, rec.Body)
				}
			}
		}
		m03Unchanged(t, e, id, before, "the line routes under NoLinePages")

		page := m01bOK(t, e.get(t, base+"/edit/items"), "GET items")
		main, err := page.Find("main").Html()
		if err != nil {
			t.Fatal(err)
		}
		if i := strings.Index(main, "/lines/"); i >= 0 {
			t.Errorf("Items step links to a line URL under NoLinePages: …%.300s", main[max(0, i-150):])
		}
		if n := page.Find("[data-add-line]").Length(); n != 0 {
			t.Errorf("Items step has %d Add an item buttons, want none", n)
		}
	})

	for _, c := range []struct {
		name string
		mod  func() docscreen.Module
	}{
		{"CardsOnly", func() docscreen.Module { return docscreen.Module{CardsOnly: true} }},
		{"Line without CardsOnly", func() docscreen.Module { return docscreen.Module{Line: (&m03Hooks{}).line()} }},
	} {
		t.Run(c.name, func(t *testing.T) {
			e, id := m01bHooked(t, c.mod())
			base := m01bURL(id)
			m03NoGrid(t, m01bOK(t, e.get(t, base+"/edit/items"), "GET items"), base)

			// A draft whose line has no item: Submit refuses on the Items step
			// with a line error, whose summary link must target the page.
			res, err := e.engine.Do(t.Context(), e.open, docscreentest.Hooked, 0, doc.ActionSave, doc.ActionInput{
				Values: map[string]string{"supplier_id": itoa(e.fx.M.Party["SHREE"]), "site_id": itoa(e.site["METRO-SITE"])},
				Lines: []map[string]string{
					{"project_id": itoa(e.fx.M.Project["PRJ-METRO"]), "item_id": itoa(e.fx.M.Item["CEMENT-OPC"]), "qty": "1.000"},
					{"project_id": itoa(e.fx.M.Project["PRJ-METRO"]), "qty": "2.000"},
				},
			})
			if err != nil {
				t.Fatalf("saving a draft with an item-less line: %v", err)
			}
			nb := m01bURL(res.ID)
			rec := e.post(t, nb+"/submit", url.Values{"version": {strconv.Itoa(res.Version)}})
			m03Status(t, rec.Code, http.StatusUnprocessableEntity, "submit with an item-less line", rec.Body.String())
			wantHeading(t, rec, "Items")
			page := uitest.Doc(t, rec)
			m03NoGrid(t, page, nb)
			if links := summaryLinks(page); len(links) == 0 {
				t.Errorf("the Items step's error summary has no link; want one per error")
			}
			wantNoDeadLinks(t, rec)
		})
	}
}

// CD-256 (9): StepLabels["items"] is the line page's Back link, on the
// generic line page and on Line.Body's, on GET and on a 422 redraw.
func TestModule_LineBackLabel(t *testing.T) {
	const label = "Received quantities"
	labels := map[string]string{"items": label}
	for _, c := range []struct {
		name string
		line bool
	}{{"generic line page", false}, {"Line.Body", true}} {
		t.Run(c.name, func(t *testing.T) {
			e, id := m01bHooked(t, docscreen.Module{})
			m := docscreen.Module{StepLabels: labels}
			if c.line {
				m.Line = (&m03Hooks{e: e}).line()
			}
			m01bMount(e, docscreentest.Hooked, m)
			base := m01bURL(id)
			for _, path := range []string{"/lines/1", "/lines/new"} {
				m03Back(t, m01bOK(t, e.get(t, base+path), "GET "+path), label, base+"/edit/items")
			}
			rec := e.post(t, base+"/lines/1", url.Values{"version": {m01bVersion(t, e, id)}, "qty": {"abc"}})
			m03Status(t, rec.Code, http.StatusUnprocessableEntity, "POST /lines/1 with qty abc", rec.Body.String())
			m03Back(t, uitest.Doc(t, rec), label, base+"/edit/items")
		})
	}
}

// CD-256 (10), CD-217/228, CD-233: a Line.Body rendering FieldInput with
// c.Deps shows a stored line link's label without a read check, while a
// posted id goes through LinkRead: a readable one shows its label, a hidden
// one shows the raw id, exactly as an id with no record.
func TestModule_LineCtxStored(t *testing.T) {
	const hiddenID, unknownID = 9001, 9002
	e, id := m01bHooked(t, docscreen.Module{})
	m := e.fx.M
	e.fx.Exec(t, `INSERT INTO fixture_project (id, code, name, created_at, created_by, updated_at, updated_by)
		VALUES (?, 'PRJ-HIDDEN', 'Hidden Depot', '2026-10-24T00:00:00Z', 1, '2026-10-24T00:00:00Z', 1)`, hiddenID)
	readable := m.Project["PRJ-TECHPARK"]
	var mu sync.Mutex
	var asked []int64
	e.d.LinkRead = func(_ context.Context, _ *db.Tx, _ auth.Principal, _ *doctype.DocType, target int64) error {
		mu.Lock()
		asked = append(asked, target)
		mu.Unlock()
		if target == readable {
			return nil
		}
		return auth.ErrForbidden
	}
	project := m03Field("project_id")
	refused := doctype.FieldError{Field: "project_id", Message: "m03 refused"}
	line := &docscreen.Line{
		Body: func(c docscreen.Ctx, no int, s docscreen.StepState) (templ.Component, error) {
			value := ""
			switch {
			case s.Posted:
				value = c.Req.PostFormValue("project_id")
			case no > 0 && c.View != nil:
				value = c.View.Lines[no-1].Inputs["project_id"]
			}
			in := docscreen.FieldInput(project, components.FieldID("project_id"), value, lineErrorOf(s.Errors, "project_id"), c.Deps)
			return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
				if _, err := io.WriteString(w, `<div id="m03-probe">`); err != nil {
					return err
				}
				if err := in.Render(ctx, w); err != nil {
					return err
				}
				_, err := io.WriteString(w, `</div>`)
				return err
			}), nil
		},
		Save: func(docscreen.Ctx, int) (docscreen.Save, error) {
			return docscreen.Save{Errors: []doctype.FieldError{refused}}, nil
		},
	}
	m01bMount(e, docscreentest.Hooked, docscreen.Module{Line: line})
	base := m01bURL(id)

	// Without the stored values PRJ-METRO shows no label: LinkRead refuses it.
	var plain strings.Builder
	d := docscreen.Deps{DB: e.fx.DB, LinkRead: e.d.LinkRead}
	if err := docscreen.FieldInput(project, "m03-plain", itoa(m.Project["PRJ-METRO"]), "", d).Render(t.Context(), &plain); err != nil {
		t.Fatalf("FieldInput: %v", err)
	}
	if strings.Contains(plain.String(), "PRJ-METRO") {
		t.Fatalf("FieldInput without stored values shows PRJ-METRO: the stored case below would prove nothing")
	}

	probe := func(page *goquery.Document) (string, string) {
		t.Helper()
		sel := page.Find("#m03-probe")
		if sel.Length() != 1 {
			t.Fatalf("found %d #m03-probe, want Line.Body once", sel.Length())
		}
		html, err := sel.Html()
		if err != nil {
			t.Fatal(err)
		}
		return norm(sel.Text()), html
	}
	posted := func(value int64) (string, string) {
		t.Helper()
		rec := e.post(t, base+"/lines/1", url.Values{"version": {m01bVersion(t, e, id)}, "project_id": {itoa(value)}})
		m03Status(t, rec.Code, http.StatusUnprocessableEntity, "POST /lines/1 with project "+itoa(value), rec.Body.String())
		return probe(uitest.Doc(t, rec))
	}

	if text, _ := probe(m01bOK(t, e.get(t, base+"/lines/1"), "GET /lines/1")); !strings.Contains(text, "PRJ-METRO") {
		t.Errorf("stored line link in Line.Body = %q, want its label PRJ-METRO (c.Deps is WithStored of the line)", text)
	}

	mu.Lock()
	asked = nil
	mu.Unlock()
	if text, _ := posted(readable); !strings.Contains(text, "PRJ-TECHPARK") {
		t.Errorf("posted readable project in Line.Body = %q, want its label PRJ-TECHPARK through LinkRead", text)
	}
	mu.Lock()
	if !slices.Contains(asked, readable) {
		t.Errorf("LinkRead was asked for %v, want the posted id %d (c.Deps keeps Deps.LinkRead)", asked, readable)
	}
	mu.Unlock()

	hiddenText, hiddenHTML := posted(hiddenID)
	if strings.Contains(hiddenText, "PRJ-HIDDEN") || strings.Contains(hiddenText, "Hidden Depot") || !strings.Contains(hiddenText, itoa(hiddenID)) {
		t.Errorf("posted hidden project in Line.Body = %q, want only the raw id %d", hiddenText, hiddenID)
	}
	_, unknownHTML := posted(unknownID)
	if got, want := hiddenHTML, strings.ReplaceAll(unknownHTML, itoa(unknownID), itoa(hiddenID)); got != want {
		t.Errorf("a hidden id's input differs from an unknown id's but for the id:\nhidden  %s\nunknown %s", got, want)
	}
}

// CD-256 (1), (11): Module.LineCard replaces each line's card on the Items
// step too, with docscreen's Change, Remove and move links on it; on a draft
// CD-242's clearing of Received still applies after the override. Check and
// document page: TestModule_LineCardOverride.
func TestModule_LineCardThreePlaces(t *testing.T) {
	const received = "Received 4 of 10"
	card := func(_ docscreen.Ctx, l docscreen.LineView) components.LineCard {
		c := l.Card
		c.Item, c.Code, c.Received = "M03 card "+l.Inputs["note"], "M03-CODE", received
		return c
	}
	e, id := m01bHooked(t, docscreen.Module{LineCard: card})
	base := m01bURL(id)
	want := []string{"M03 card first M03-CODE", "M03 card second M03-CODE"}
	page := m01bOK(t, e.get(t, base+"/edit/items"), "GET items")
	cards := page.Find(".linelist .lines-card")
	if got := cards.Find(".lines-name").Map(func(_ int, s *goquery.Selection) string { return norm(s.Text()) }); !slices.Equal(got, want) {
		t.Errorf("Items step cards = %q, want %q", got, want)
	}
	if strings.Contains(norm(cards.Text()), received) {
		t.Errorf("draft Items step shows %q: CD-242's clearing must run after the override", received)
	}
	for n, no := range []string{"1", "2"} {
		c := cards.Eq(n)
		if c.Find(`a[href="`+base+`/lines/`+no+`"]`).Length() != 1 || c.Find(`a[href="`+base+`/lines/`+no+`/remove"]`).Length() != 1 {
			t.Errorf("Items step card %s has no Change and Remove links to its line", no)
		}
	}
	if cards.Eq(0).Find(`form[action="`+base+`/lines/1/move?dir=down"]`).Length() != 1 {
		t.Errorf("Items step card 1 has no Move down form")
	}
}

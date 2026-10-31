package docscreen_test

import (
	"context"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
)

// M0-56: docscreen checks the load, the right, the posted version and the
// editable state before a module hook runs or a posted value is parsed
// (CD-335, CD-259); a LineCard gets the line's Ctx (CD-339); and an htmx
// write refused with a status htmx doesn't swap also sends its message as an
// alert notice (CD-348).

// m56Hooks counts the module's Step.Save and Line.Save calls.
type m56Hooks struct {
	mu          sync.Mutex
	steps, line int
}

func (h *m56Hooks) stepSave(docscreen.Ctx) (docscreen.Save, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.steps++
	return docscreen.Save{Errors: []doctype.FieldError{{Field: "ref", Message: "m56 hook parse error"}}}, nil
}

func (h *m56Hooks) lineSave(docscreen.Ctx, int) (docscreen.Save, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.line++
	return docscreen.Save{Errors: []doctype.FieldError{{Field: "qty", Message: "m56 hook parse error"}}}, nil
}

func (h *m56Hooks) calls() (steps, line int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.steps, h.line
}

func m56Body(docscreen.Ctx, int, docscreen.StepState) (templ.Component, error) {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, `<p id="m56-line-body">m56</p>`)
		return err
	}), nil
}

// m56Module is Hooked with a custom save on terms and a custom line page,
// both counted by h.
func m56Module(h *m56Hooks) docscreen.Module {
	return docscreen.Module{
		Steps: map[string]docscreen.Step{"terms": {Save: h.stepSave}},
		Line:  &docscreen.Line{Body: m56Body, Save: h.lineSave},
	}
}

// m56Refusal is the engine's own answer to a save of Hooked id with version:
// DOC's text, asked of the real engine directly.
func m56Refusal(t *testing.T, e *hookedEnv, id int64, version int) string {
	t.Helper()
	_, err := e.engine.Do(t.Context(), e.open, docscreentest.Hooked, id, doc.ActionSave, doc.ActionInput{Version: version, Only: []string{}})
	if err == nil {
		t.Fatalf("the engine saved Hooked %d at version %d, want its refusal", id, version)
	}
	return err.Error()
}

// m56Bump saves remarks on Hooked id, so the version a page posted before is
// stale. It returns the new version.
func m56Bump(t *testing.T, e *hookedEnv, id int64) int {
	t.Helper()
	rec := m02Rec(t, e, id)
	res, err := e.engine.Do(t.Context(), e.open, docscreentest.Hooked, id, doc.ActionSave, doc.ActionInput{
		Version: rec.Version, Values: map[string]string{"remarks": "m56 bumped"}, Lines: rec.Lines, Only: []string{"remarks"},
	})
	if err != nil {
		t.Fatalf("bumping Hooked %d: %v", id, err)
	}
	return res.Version
}

// m56Posts are the write paths that run a hook or parse posted values, each
// with a value no parser (or hook) accepts.
var m56Posts = []struct {
	name, path string
	form       url.Values
	hooked     bool // with m56Module: the path runs a hook
}{
	{"generic step", "/edit/details", url.Values{"supplier_id": {"m56-not-an-id"}}, false},
	{"check step", "/edit/check", url.Values{}, false},
	{"step with Step.Save", "/edit/terms", url.Values{"ref": {"M56"}}, true},
	{"generic line save", "/lines/1", url.Values{"qty": {"m56-not-a-qty"}}, false},
	{"generic new line", "/lines/new", url.Values{"qty": {"m56-not-a-qty"}}, false},
	{"line save with Line.Save", "/lines/1", url.Values{"qty": {"1"}}, true},
	{"new line with Line.Save", "/lines/new", url.Values{"qty": {"1"}}, true},
	{"generic line save, htmx", "/lines/1", url.Values{"qty": {"m56-not-a-qty"}}, false},
}

// HOOK-056a (CD-335): a stale page posting a bad value to a step or line
// gets DOC's 409 with DOC's text, never a 422; no hook runs and nothing is
// written. The same post with the current version still gets the 422, so
// the order, not the parse, decides.
func TestStale56_ConflictBeforeParseAndHooks(t *testing.T) {
	for _, p := range m56Posts {
		t.Run(p.name, func(t *testing.T) {
			e, _, id := m02Env(t)
			h := &m56Hooks{}
			if p.hooked {
				m02Mount(e, m56Module(h))
			}
			stale := m02Rec(t, e, id).Version
			current := m56Bump(t, e, id)
			want := m56Refusal(t, e, id, stale)
			before := m02Rec(t, e, id)
			audits := len(e.fx.Audit(t, docscreentest.Hooked, id))

			post := func(version int) *httptest.ResponseRecorder {
				f := url.Values{}
				for k, v := range p.form {
					f[k] = v
				}
				f.Set("version", strconv.Itoa(version))
				if strings.HasSuffix(p.name, "htmx") {
					return e.post(t, m02URL(id)+p.path, f, "HX-Request", "true")
				}
				return e.post(t, m02URL(id)+p.path, f)
			}
			rec := post(stale)
			if rec.Code != http.StatusConflict {
				t.Fatalf("stale POST %s = %d, want DOC's 409; body %.400s", p.path, rec.Code, rec.Body)
			}
			if got := norm(html.UnescapeString(rec.Body.String())); !strings.Contains(got, want) {
				t.Errorf("stale POST %s lacks DOC's %q; body %.600s", p.path, want, got)
			}
			if s, l := h.calls(); s+l != 0 {
				t.Errorf("stale POST %s ran the module's save %d times, want none before the version check", p.path, s+l)
			}
			m02WantUnchanged(t, e, id, before, audits, "stale POST "+p.path)
			// The conflict redraw keeps what the user typed, unparsed.
			if q := p.form.Get("qty"); !p.hooked && q != "" && !strings.Contains(rec.Body.String(), `value="`+q+`"`) {
				t.Errorf("stale POST %s redraw lacks the typed qty %q", p.path, q)
			}

			if p.path == "/edit/check" {
				return // the check step posts no values: nothing to refuse
			}
			if rec := post(current); rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("current POST %s = %d, want the parse's 422; body %.400s", p.path, rec.Code, rec.Body)
			}
		})
	}
}

// HOOK-056b (CD-335): a document past draft refuses a step or line save
// with DOC's state text (409) even at its current version, before any hook
// or parse.
func TestStale56_StateBeforeParseAndHooks(t *testing.T) {
	for _, p := range m56Posts {
		t.Run(p.name, func(t *testing.T) {
			e, _, id := m02Env(t)
			h := &m56Hooks{}
			if p.hooked {
				m02Mount(e, m56Module(h))
			}
			res, err := e.engine.Do(t.Context(), e.open, docscreentest.Hooked, id, doc.ActionSubmit, doc.ActionInput{Version: m02Rec(t, e, id).Version})
			if err != nil {
				t.Fatalf("submitting Hooked %d: %v", id, err)
			}
			want := m56Refusal(t, e, id, res.Version)
			before := m02Rec(t, e, id)
			audits := len(e.fx.Audit(t, docscreentest.Hooked, id))
			f := url.Values{}
			for k, v := range p.form {
				f[k] = v
			}
			f.Set("version", strconv.Itoa(res.Version))
			var rec *httptest.ResponseRecorder
			if strings.HasSuffix(p.name, "htmx") {
				rec = e.post(t, m02URL(id)+p.path, f, "HX-Request", "true")
			} else {
				rec = e.post(t, m02URL(id)+p.path, f)
			}
			if s, l := h.calls(); s+l != 0 {
				t.Errorf("POST %s on a submitted document ran the module's save %d times, want none", p.path, s+l)
			}
			m02WantUnchanged(t, e, id, before, audits, "POST "+p.path+" on a submitted document")
			// A line page's refusal on a document past draft goes to its
			// document page, as it did; a step answers 409 with DOC's text.
			if strings.HasPrefix(p.path, "/lines/") {
				if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != m02URL(id) {
					t.Errorf("POST %s on a submitted document = %d to %q, want 303 to %s", p.path, rec.Code, rec.Header().Get("Location"), m02URL(id))
				}
				return
			}
			if rec.Code != http.StatusConflict {
				t.Fatalf("POST %s on a submitted document = %d, want DOC's 409; body %.400s", p.path, rec.Code, rec.Body)
			}
			if got := norm(html.UnescapeString(rec.Body.String())); !strings.Contains(got, want) {
				t.Errorf("POST %s lacks DOC's %q; body %.600s", p.path, want, got)
			}
		})
	}
}

// HOOK-056c (CD-339): a module's LineCard gets the line's Ctx, whose Deps
// carry that line's stored inputs; its Hide sees a line-only value (the
// item's label, CD-217) there and not with the header's Ctx.
func TestStale56_LineCardGetsLineCtx(t *testing.T) {
	order, _ := doctype.Lookup(fixture.Order)
	itemField, _ := order.Field("item_id")
	const note = "m56 line ctx"
	hide := func(c docscreen.Ctx, f doctype.Field) bool {
		if f.Name != "m56-probe" {
			return false
		}
		var b strings.Builder
		if err := docscreen.FieldInput(itemField, "m56-probe", c.View.Lines[0].Inputs["item_id"], "", c.Deps).Render(c.Req.Context(), &b); err != nil {
			t.Errorf("FieldInput: %v", err)
		}
		return strings.Contains(b.String(), "CEMENT-OPC")
	}
	card := func(c docscreen.Ctx, l docscreen.LineView) components.LineCard {
		lc := l.Card
		if hide(c, doctype.Field{Name: "m56-probe"}) {
			lc.Note = note
		}
		return lc
	}
	e, id := m46Order(t, docscreen.Module{Hide: hide, LineCard: card})
	page := m01bOK(t, e.get(t, m46URL(id)+"/edit/check"), "GET check")
	if got := norm(page.Find(".lines-card").Text()); !strings.Contains(got, note) {
		t.Errorf("check page card = %q, want %q: LineCard's Ctx must carry the line's stored inputs", got, note)
	}
}

// m56Notice is the response's HX-Trigger notice, failing unless it has one.
func m56Notice(t *testing.T, rec *httptest.ResponseRecorder, what string) web.Notice {
	t.Helper()
	var m map[string]web.Notice
	raw := rec.Header().Get("HX-Trigger")
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("%s: HX-Trigger %q is not a notice event: %v", what, raw, err)
	}
	n, ok := m["notice"]
	if !ok {
		t.Fatalf("%s: HX-Trigger %q has no notice", what, raw)
	}
	return n
}

// HOOK-056d (CD-348): an htmx write a reader posts to a step, a line, a
// remove, a move or a state action is refused with AUTH's 403, which htmx
// doesn't swap, so the response also carries AUTH's text as an error
// notice. The status stays 403. Real auth.Service. A full-page POST carries
// no notice: its page shows the refusal.
func TestStale56_UnswappedRefusalNotice(t *testing.T) {
	e := m15NewEnv(t)
	u := m15DocURL(docscreentest.Hooked, e.id)
	version := strconv.Itoa(m02Rec(t, e.hookedEnv, e.id).Version)
	e.who = e.reader
	for _, path := range []string{"/edit/details", "/edit/terms", "/edit/check", "/lines/new", "/lines/1", "/lines/1/remove", "/lines/1/move?dir=down", "/submit", "/delete"} {
		what := "reader htmx POST " + path
		rec := e.post(t, u+path, url.Values{"version": {version}}, "HX-Request", "true")
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s = %d, want AUTH's 403", what, rec.Code)
			continue
		}
		want := m15HookedDenied
		if verb, ok := map[string]string{"/submit": "submit", "/delete": "delete"}[path]; ok {
			want = strings.Replace(want, "change", verb, 1) // AUTH's text for that right
		}
		if n := m56Notice(t, rec, what); n.Level != "error" || n.Text != want {
			t.Errorf("%s notice = %+v, want error %q", what, n, want)
		}
		if rec := e.post(t, u+path, url.Values{"version": {version}}); rec.Header().Get("HX-Trigger") != "" {
			t.Errorf("full-page POST %s sent HX-Trigger %q, want none", path, rec.Header().Get("HX-Trigger"))
		}
	}
}

// HOOK-056e (CD-348, CD-346): a refusal htmx swaps (409, 422) is shown by
// the swap and sends no notice.
func TestStale56_SwappedRefusalNoNotice(t *testing.T) {
	e, _, id := m02Env(t)
	stale := m02Rec(t, e, id).Version
	m56Bump(t, e, id)
	for _, c := range []struct {
		what    string
		version int
		code    int
	}{{"stale", stale, http.StatusConflict}, {"bad value", m02Rec(t, e, id).Version, http.StatusUnprocessableEntity}} {
		rec := e.post(t, m02URL(id)+"/edit/details", url.Values{"supplier_id": {"m56-x"}, "version": {strconv.Itoa(c.version)}}, "HX-Request", "true")
		if rec.Code != c.code {
			t.Errorf("%s htmx POST = %d, want %d", c.what, rec.Code, c.code)
		}
		if got := rec.Header().Get("HX-Trigger"); got != "" {
			t.Errorf("%s htmx POST sent HX-Trigger %q, want none: the swap shows it", c.what, got)
		}
	}
}

// m56HandOver gives Purchase User write (own) on fixture.Order and hands
// Priya's draft over to a second Purchase User; Priya keeps read. It returns
// the order's version after the hand-over.
func m56HandOver(t *testing.T, e *m15OrderEnv) int {
	t.Helper()
	ctx := t.Context()
	if err := e.a.SetRights(ctx, e.fx.Admin, "Purchase User", fixture.Order, map[auth.Right]auth.Scope{
		"read": auth.ScopeAll, "create": auth.ScopeAll, "write": auth.ScopeOwn, "submit": auth.ScopeOwn,
		"delete": auth.ScopeOwn, "print": auth.ScopeAll, "export": auth.ScopeAll,
	}); err != nil {
		t.Fatalf("SetRights(Purchase User, write own): %v", err)
	}
	other := m15User(t, e.a, e.fx.Admin, "m56-buyer@vardhan.test", "Purchase User")
	priya := m15Load(t, e.a, e.fx.Priya.ID)
	rec := e.fx.Record(t, fixture.Order, e.id)
	if err := e.fx.Engine.Reassign(ctx, priya, fixture.Order, e.id, rec.Version, other.ID); err != nil {
		t.Fatalf("handing the order over: %v", err)
	}
	e.who = priya
	return e.fx.Record(t, fixture.Order, e.id).Version
}

// HOOK-056f (CD-348, PUX-008): after a hand-over, the former owner's htmx
// step save gets AUTH's 403 with its own-scope text, and the same text as an
// error notice; nothing is written. Real auth.Service and engine.
func TestStale56_HandOverStepSaveNotice(t *testing.T) {
	e := m15NewOrderEnv(t)
	version := m56HandOver(t, e)
	before := e.fx.Record(t, fixture.Order, e.id)
	rec := e.post(t, m15DocURL(fixture.Order, e.id)+"/edit/details", url.Values{"remarks": {"m56 after hand-over"}, "version": {strconv.Itoa(version)}}, "HX-Request", "true")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("former owner's htmx step save = %d, want AUTH's 403; body %.400s", rec.Code, rec.Body)
	}
	if n := m56Notice(t, rec, "former owner's htmx step save"); n.Level != "error" || n.Text != m15OrderOwnOnly {
		t.Errorf("notice = %+v, want error %q", n, m15OrderOwnOnly)
	}
	if got := e.fx.Record(t, fixture.Order, e.id); got.Version != before.Version || got.Values["remarks"] != before.Values["remarks"] {
		t.Errorf("the refused save wrote the order: version %d remarks %q, want %d %q", got.Version, got.Values["remarks"], before.Version, before.Values["remarks"])
	}
}

// HOOK-056h (CD-348): a user who can't see the draft (restricted to
// TECH-SITE, the draft on METRO-SITE) gets a 404 on an htmx step save, line
// save or state action, with the refusal's own message as an error notice,
// the same message the full page shows. Real auth.Service.
func TestStale56_HiddenRefusalNotice(t *testing.T) {
	e, _, id := m02Env(t)
	e.who = e.restricted
	version := strconv.Itoa(m02Rec(t, e, id).Version)
	for _, path := range []string{"/edit/details", "/lines/1", "/submit"} {
		what := "hidden htmx POST " + path
		rec := e.post(t, m02URL(id)+path, url.Values{"version": {version}}, "HX-Request", "true")
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", what, rec.Code)
			continue
		}
		n := m56Notice(t, rec, what)
		full := e.post(t, m02URL(id)+path, url.Values{"version": {version}})
		if n.Level != "error" || n.Text == "" || !strings.Contains(norm(html.UnescapeString(full.Body.String())), n.Text) {
			t.Errorf("%s notice = %+v, want error with the full page's message; full page %.400s", what, n, full.Body)
		}
	}
	// Only writes: an htmx GET's refusal keeps today's answer, no notice.
	if rec := e.get(t, m02URL(id)+"/edit/details", "HX-Request", "true"); rec.Code != http.StatusNotFound || rec.Header().Get("HX-Trigger") != "" {
		t.Errorf("hidden htmx GET = %d with HX-Trigger %q, want 404 and none", rec.Code, rec.Header().Get("HX-Trigger"))
	}
}

// m56GoneAfter is the engine with Load answering doc.Gone after its first
// call: a draft deleted between the step POST's view and its load.
type m56GoneAfter struct {
	doc.Engine
	mu    sync.Mutex
	loads int
}

func (g *m56GoneAfter) Load(ctx context.Context, tx *db.Tx, name string, id int64) (doc.Record, error) {
	g.mu.Lock()
	g.loads++
	n := g.loads
	g.mu.Unlock()
	if n > 1 {
		return doc.Record{}, doc.Gone()
	}
	return g.Engine.Load(ctx, tx, name, id)
}

// HOOK-056i (CD-335): a draft gone between the step POST's view and its load
// answers DOC's missing text (404); nothing is saved or created.
func TestStale56_StepPostGoneOnLoad(t *testing.T) {
	e, _, id := m02Env(t)
	version := m02Version(t, e, id)
	count := m02Count(t, e)
	e.d.Engine = &m56GoneAfter{Engine: e.d.Engine}
	e.remount()
	rec := e.post(t, m02URL(id)+"/edit/terms", url.Values{"version": {version}, "ref": {"M56"}})
	if rec.Code != http.StatusNotFound {
		t.Errorf("POST terms with the draft gone on load = %d, want 404; body %.300s", rec.Code, rec.Body)
	}
	if got := m02Count(t, e); got != count {
		t.Errorf("Hooked documents = %d, want %d: nothing created", got, count)
	}
}

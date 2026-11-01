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
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
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
			e, spy, id := m02Env(t)
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
			// The check never asks the engine at the stored version, which a
			// save could pass (CD-335).
			for _, c := range spy.take() {
				if c.ID == id && c.In.Version == res.Version {
					t.Errorf("POST %s asked the engine at the stored version %d: %+v", p.path, res.Version, c)
				}
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

// m56AprEnv is the docscreen harness on fixture.Order with the real AUTH
// (default rights) and the real APR behind the real engine, and the fixture
// approval rules: orders from Rs 50,000 need a Purchase Manager, from
// Rs 5,00,000 a Director.
func m56AprEnv(t *testing.T) *m15OrderEnv {
	t.Helper()
	ctx := t.Context()
	h := fixturetest.New(t)
	deps := doc.Deps{DB: h.DB, Clock: h.Clock, Naming: h.Naming, Numbers: fixturetest.Numbers(), Files: h.Files}
	info := doc.NewEngine(deps) // AUTH and APR need an InfoSource before the engine on them exists
	dir := testkit.Directory(h.F)
	a := auth.New(auth.Deps{DB: h.DB, Clock: h.Clock, Directory: dir, Audit: auditfake.New(), Refs: &authtest.Refs{}, Info: info})
	if err := a.SeedDefaults(ctx, auth.SeedOptions{}); err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}
	rule := `INSERT INTO approval_rules (doctype, from_amount, approver_type, approver_role, fallback_role, created_at, created_by, updated_at, updated_by)
		VALUES (?, ?, 'role', ?, '', '2026-10-24T05:00:00.000Z', 1, '2026-10-24T05:00:00.000Z', 1)`
	h.Exec(t, rule, fixture.Order, 50_000_00, "Purchase Manager")
	h.Exec(t, rule, fixture.Order, 5_00_000_00, "Director")
	deps.Auth, deps.Approval = a, approval.New(approval.Deps{DB: h.DB, Clock: h.Clock, Auth: a, Info: info, Directory: dir})
	h.Engine = doc.NewEngine(deps)
	return &m15OrderEnv{scopeEnv: scopeMount(t, h, h.Engine, a, h.Priya), a: a}
}

// m56Order is a fixture.Order by Priya of amount (its approval amount),
// submitted: approved below Rs 50,000, pending from there. It returns its id.
func m56Order(t *testing.T, e *m15OrderEnv, amount string, want doctype.State) int64 {
	t.Helper()
	priya := m15Load(t, e.a, e.fx.Priya.ID)
	res := e.fx.NewOrderWith(t, priya, map[string]string{"amount": amount}, fixturetest.OL{Item: "CEMENT-OPC", Qty: "10.000", Rate: "350.0000"})
	sub, err := e.fx.Engine.Do(t.Context(), priya, fixture.Order, res.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version})
	if err != nil || sub.State != want {
		t.Fatalf("submitting order %d: state %s, err %v; want %s", res.ID, sub.State, err, want)
	}
	return res.ID
}

// HOOK-056j (CD-354, CD-259): a reason post with no reason goes to the
// engine, which checks the right, the approver, the state and the version
// before it refuses the empty reason; only that last refusal shows as "Enter
// a reason." (422). Real AUTH, APR and engine on fixture.Order, for reject,
// cancel and close: a read-only user is refused (403), an approver APR finds
// ineligible (a Rs 7,00,000 order needs a Director; Ravi is a Purchase
// Manager) gets APR's refusal, a stale page and a document in another state
// DOC's 409, and nothing is written.
func TestStale56_EmptyReasonOrder(t *testing.T) {
	for _, c := range []struct {
		action string
		act    doctype.Action
		amount string        // the order the action fits
		state  doctype.State // its state
		other  string        // an order in another state
		otherS doctype.State
		who    func(e *m15OrderEnv) auth.UserID // a user who may take the action
	}{
		{"reject", doctype.ActionReject, "70000.00", doctype.StatePending, "3500.00", doctype.StateApproved, func(e *m15OrderEnv) auth.UserID { return e.fx.Ravi.ID }},
		{"cancel", doctype.ActionCancel, "3500.00", doctype.StateApproved, "70000.00", doctype.StatePending, func(e *m15OrderEnv) auth.UserID { return e.fx.Admin.ID }},
		{"close", doctype.ActionClose, "3500.00", doctype.StateApproved, "70000.00", doctype.StatePending, func(e *m15OrderEnv) auth.UserID { return e.fx.Admin.ID }},
	} {
		t.Run(c.action, func(t *testing.T) {
			e := m56AprEnv(t)
			ctx := t.Context()
			if err := e.a.CreateRole(ctx, e.fx.Admin, "M56 Reader"); err != nil {
				t.Fatalf("CreateRole: %v", err)
			}
			if err := e.a.SetRights(ctx, e.fx.Admin, "M56 Reader", fixture.Order, map[auth.Right]auth.Scope{"read": auth.ScopeAll}); err != nil {
				t.Fatalf("SetRights: %v", err)
			}
			reader := m15User(t, e.a, e.fx.Admin, "m56-reader@vardhan.test", "M56 Reader")
			actor := m15Load(t, e.a, c.who(e))
			id := m56Order(t, e, c.amount, c.state)
			other := m56Order(t, e, c.other, c.otherS)
			u := m15DocURL(fixture.Order, id) + "/" + c.action
			before := e.fx.Record(t, fixture.Order, id)
			empty := func(path string, version int) *httptest.ResponseRecorder {
				return e.post(t, path, url.Values{"version": {strconv.Itoa(version)}, "reason": {"  "}})
			}
			// The engine's own refusal of the action as p, with a reason.
			refusal := func(p auth.Principal, docID int64, version int) string {
				_, err := e.fx.Engine.Do(ctx, p, fixture.Order, docID, c.act, doc.ActionInput{Version: version, Reason: "m56 reason"})
				if err == nil {
					t.Fatalf("the engine took %s on order %d at version %d, want its refusal", c.action, docID, version)
				}
				return err.Error()
			}
			want := func(rec *httptest.ResponseRecorder, code int, text, what string) {
				t.Helper()
				if rec.Code != code {
					t.Errorf("%s = %d, want %d; body %.400s", what, rec.Code, code, rec.Body)
					return
				}
				page := uitest.Doc(t, rec)
				if got := norm(page.Text()); !strings.Contains(got, text) {
					t.Errorf("%s lacks %q; page %.600s", what, text, got)
				}
				if page.Find(`textarea[name="reason"]`).Length() != 1 {
					t.Errorf("%s doesn't redraw the reason page", what)
				}
			}

			e.who = reader
			want(empty(u, before.Version), http.StatusForbidden, refusal(reader, id, before.Version), "read-only user's empty "+c.action)
			e.who = actor
			want(empty(u, before.Version-1), http.StatusConflict, refusal(actor, id, before.Version-1), "stale empty "+c.action)
			otherVersion := e.fx.Record(t, fixture.Order, other).Version
			want(empty(m15DocURL(fixture.Order, other)+"/"+c.action, otherVersion), http.StatusConflict, refusal(actor, other, otherVersion), "empty "+c.action+" on a document "+string(c.otherS))
			if c.act == doctype.ActionReject {
				big := m56Order(t, e, "700000.00", doctype.StatePending) // Rs 7,00,000: a Director's
				bigVersion := e.fx.Record(t, fixture.Order, big).Version
				want(empty(m15DocURL(fixture.Order, big)+"/reject", bigVersion), http.StatusForbidden, refusal(actor, big, bigVersion), "Ravi's empty reject of a Director's order")
			}
			rec := empty(u, before.Version)
			want(rec, http.StatusUnprocessableEntity, "Enter a reason.", "empty "+c.action)
			if got := norm(uitest.Doc(t, rec).Find("#" + components.FieldID("reason") + "-msg").Text()); got != "Enter a reason." {
				t.Errorf("empty %s: reason field message = %q, want Enter a reason.", c.action, got)
			}
			// A reason the engine refuses for another cause keeps its own text.
			long := e.post(t, u, url.Values{"version": {strconv.Itoa(before.Version)}, "reason": {strings.Repeat("m", 1001)}})
			if long.Code != http.StatusUnprocessableEntity || strings.Contains(norm(long.Body.String()), "Enter a reason.") {
				t.Errorf("over-long %s = %d with %q, want the engine's own 422 text", c.action, long.Code, norm(uitest.Doc(t, long).Find("#"+components.FieldID("reason")+"-msg").Text()))
			}
			if got := e.fx.Record(t, fixture.Order, id); got.Version != before.Version || got.Info.State != before.Info.State {
				t.Errorf("the refused %s posts wrote: version %d state %s, want %d %s", c.action, got.Version, got.Info.State, before.Version, before.Info.State)
			}
		})
	}
}

// HOOK-056k (CD-335): a user who can't see the document posting an empty
// reason gets the missing record's 404 first, before the page's right: this
// one (read only, restricted to TECH-SITE; the draft is on METRO-SITE) holds
// none of the actions' rights. Real auth.Service.
func TestStale56_EmptyReasonHidden(t *testing.T) {
	e, _, id := m02Env(t)
	a, ok := e.d.Auth.(*auth.Auth)
	if !ok {
		t.Fatalf("Auth is %T, want the real *auth.Auth", e.d.Auth)
	}
	ctx := t.Context()
	if err := a.CreateRole(ctx, e.fx.Admin, "M56 Viewer"); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	if err := a.SetRights(ctx, e.fx.Admin, "M56 Viewer", docscreentest.Hooked, map[auth.Right]auth.Scope{"read": auth.ScopeAll}); err != nil {
		t.Fatalf("SetRights: %v", err)
	}
	viewer := m15User(t, a, e.fx.Admin, "m56-viewer@vardhan.test", "M56 Viewer")
	if err := a.ChangeRestrictions(ctx, e.fx.Admin, viewer.ID, auth.DimSite, []int64{e.site["TECH-SITE"]}, nil, nil); err != nil {
		t.Fatalf("ChangeRestrictions: %v", err)
	}
	e.who = m15Load(t, a, viewer.ID)
	version := strconv.Itoa(m02Rec(t, e, id).Version)
	for _, action := range []string{"close", "cancel", "reject"} {
		rec := e.post(t, m02URL(id)+"/"+action, url.Values{"version": {version}, "reason": {""}})
		if rec.Code != http.StatusNotFound {
			t.Errorf("hidden empty %s = %d, want 404; body %.300s", action, rec.Code, rec.Body)
		}
	}
}

// CD-348, CD-346: a 5xx refusal (DOC's busy 503) sends no notice; the
// network banner answers it.
func TestStale56_BusyNoNotice(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusServiceUnavailable} {
		e := newDocEnv(t, allRights...)
		e.seed(doctype.StateDraft)
		e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSubmit, doc.Result{}, statusErr{status: status, msg: "m56 server"})
		rec := e.post(t, doc42+"/submit", url.Values{}, "HX-Request", "true")
		if rec.Code != status {
			t.Fatalf("htmx submit refused with %d = %d", status, rec.Code)
		}
		if got := rec.Header().Get("HX-Trigger"); got != "" {
			t.Errorf("htmx submit refused with %d sent HX-Trigger %q, want none", status, got)
		}
	}
}

// CD-335: a posted version above the stored one (forged) is DOC's stale 409,
// and the engine is never asked at that version, so a save racing up to it
// can't save stale lines.
func TestStale56_ForgedVersion(t *testing.T) {
	e, spy, id := m02Env(t)
	stored := m02Rec(t, e, id)
	forged := stored.Version + 1
	want := m56Refusal(t, e, id, stored.Version-1)
	audits := len(e.fx.Audit(t, docscreentest.Hooked, id))
	spy.take()
	for _, path := range []string{"/edit/details", "/lines/1"} {
		rec := e.post(t, m02URL(id)+path, url.Values{"version": {strconv.Itoa(forged)}, "qty": {"m56-x"}, "supplier_id": {"m56-x"}})
		if rec.Code != http.StatusConflict {
			t.Errorf("forged POST %s = %d, want DOC's 409", path, rec.Code)
		} else if got := norm(html.UnescapeString(rec.Body.String())); !strings.Contains(got, want) {
			t.Errorf("forged POST %s lacks DOC's %q", path, want)
		}
	}
	for _, c := range spy.take() {
		if c.In.Version >= forged {
			t.Errorf("engine asked at version %d, want below the forged %d: %+v", c.In.Version, forged, c)
		}
	}
	m02WantUnchanged(t, e, id, stored, audits, "forged POSTs")
}

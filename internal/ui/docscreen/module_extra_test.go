package docscreen_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// M0-4 (hooks spec §3, §4.4; CD-256): Module.Check, Module.Page and
// Module.Reason, on the real engine and the real AUTH service (CD-125). The
// check page runs on fixture.Order, the registered DocType with a grand
// total (Hooked has none); the document and reason pages run on Hooked.

// Test values: what the hooks return.
const (
	m04Preview   = "This order goes to Ravi Kulkarni for approval."
	m04Note1     = "Reverse charge applies to this bill."
	m04Note2     = "Place of supply is Gujarat."
	m04Warn1     = "This bill number is on another order."
	m04Warn2     = "Delivery is more than ordered."
	m04LessLabel = "Less advance"
	m04LessValue = "₹500.00"
	m04PayLabel  = "To pay"
	m04PayValue  = "₹1,000.00"
	m04LineNote  = "Pending 3 of 5 on line 2."
	m04SiteLabel = "Site"
	m04SiteValue = "Metro site, shown by the module"
	m04Card1     = "Bill details card"
	m04Card2     = "Linked receipts card"
	m04Reason    = "Closing stops any further receipts on this order."
	m04Hint      = "Say why the rest won't come."
	m04Amount    = "1500.00"
)

var errM04Hook = errors.New("m04: the hook failed")

// m04Calls records each call of a hook and the Ctx it got.
type m04Calls struct {
	n       int
	ctx     docscreen.Ctx
	actions []doctype.Action
}

func (c *m04Calls) saw(x docscreen.Ctx) { c.n++; c.ctx = x }

// m04WantCtx fails unless the hook's last Ctx is a request on dt by p
// with the View of record id.
func m04WantCtx(t *testing.T, c *m04Calls, dt string, id int64, p auth.Principal) {
	t.Helper()
	x := c.ctx
	if x.Req == nil || x.DT == nil || x.DT.Name != dt || x.View == nil || x.View.Info.ID != id || x.Principal.ID != p.ID {
		var got string
		if x.DT != nil {
			got = x.DT.Name
		}
		t.Errorf("hook Ctx: Req set %v, DT %q, View %v, Principal %d; want a request on %s, the View of %d, principal %d",
			x.Req != nil, got, x.View != nil, x.Principal.ID, dt, id, p.ID)
	}
}

// m04Mount gives dt the module m and rebuilds the router.
func m04Mount(e *hookedEnv, dt string, m docscreen.Module) {
	e.d.Modules = map[string]docscreen.Module{dt: m}
	e.remount()
}

// m04Auth is the env's real AUTH service.
func m04Auth(t *testing.T, e *hookedEnv) *auth.Auth {
	t.Helper()
	a, ok := e.d.Auth.(*auth.Auth)
	if !ok {
		t.Fatalf("Deps.Auth is %T, want the real *auth.Auth", e.d.Auth)
	}
	return a
}

// m04User creates a user holding a new role named role with rights on
// each DocType, optionally restricted to one project, and loads it.
func m04User(t *testing.T, e *hookedEnv, email, role string, rights map[string][]auth.Right, project int64) auth.Principal {
	t.Helper()
	ctx, a, admin := t.Context(), m04Auth(t, e), e.fx.Admin
	if err := a.CreateRole(ctx, admin, role); err != nil {
		t.Fatalf("CreateRole(%s): %v", role, err)
	}
	for dt, rs := range rights {
		m := map[auth.Right]auth.Scope{}
		for _, r := range rs {
			m[r] = auth.ScopeAll
		}
		if err := a.SetRights(ctx, admin, role, dt, m); err != nil {
			t.Fatalf("SetRights(%s, %s): %v", role, dt, err)
		}
	}
	id, _, err := a.CreateUser(ctx, admin, auth.NewUser{Email: email, Name: email, Password: scopePassword, Roles: []string{role}})
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", email, err)
	}
	if project != 0 {
		if err := a.ChangeRestrictions(ctx, admin, id, auth.DimProject, []int64{project}, nil, nil); err != nil {
			t.Fatalf("ChangeRestrictions(%s): %v", email, err)
		}
	}
	p, _, err := a.LoadPrincipal(ctx, id)
	if err != nil {
		t.Fatalf("LoadPrincipal(%s): %v", email, err)
	}
	return p
}

// m04ApprovedHooked saves a Hooked draft as the open user on METRO-SITE
// (two lines) and submits it; Hooked isn't Approvable, so it is approved.
func m04ApprovedHooked(t *testing.T, e *hookedEnv) int64 {
	t.Helper()
	res, err := e.draft(t, e.open, "METRO-SITE")
	if err != nil {
		t.Fatalf("saving a Hooked draft: %v", err)
	}
	sub, err := e.engine.Do(t.Context(), e.open, docscreentest.Hooked, res.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version})
	if err != nil || sub.State != doctype.StateApproved {
		t.Fatalf("submitting Hooked %d: state %s, err %v; want approved", res.ID, sub.State, err)
	}
	return res.ID
}

func m04URL(dt string, id int64) string { return "/doc/" + dt + "/" + itoa(id) }

func m04Version(t *testing.T, e *hookedEnv, dt string, id int64) string {
	t.Helper()
	return strconv.Itoa(e.fx.Record(t, dt, id).Version)
}

// m04Page fails unless rec has status want and returns its page.
func m04Page(t *testing.T, rec *httptest.ResponseRecorder, want int, what string) *goquery.Document {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("%s = %d, want %d; body %.600s", what, rec.Code, want, rec.Body)
	}
	return uitest.Doc(t, rec)
}

// m04Same404 fails unless rec is the 404 a missing record gets: the same
// status and the same #main text.
func m04Same404(t *testing.T, rec, missing *httptest.ResponseRecorder, what string) {
	t.Helper()
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing record = %d, want 404", missing.Code)
	}
	if rec.Code != http.StatusNotFound {
		t.Errorf("%s = %d, want 404 as for a missing record; body %.300s", what, rec.Code, rec.Body)
		return
	}
	if got, want := norm(uitest.Doc(t, rec).Find("#main").Text()), norm(uitest.Doc(t, missing).Find("#main").Text()); got != want {
		t.Errorf("%s #main = %q, want a missing record's %q", what, got, want)
	}
}

// m04Card is a module card: a section carrying text.
func m04Card(text string) templ.Component {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, `<section data-m04-card><p>`+templ.EscapeString(text)+`</p></section>`)
		return err
	})
}

// m04Warnings fails unless sel holds a components.Warnings banner listing
// exactly want, in order.
func m04Warnings(t *testing.T, sel *goquery.Selection, want []string, what string) {
	t.Helper()
	var lists [][]string
	sel.Find(".banner--warning").Each(func(_ int, b *goquery.Selection) {
		if b.ParentsFiltered("template").Length() > 0 {
			return
		}
		lists = append(lists, b.Find(".banner-items li").Map(func(_ int, li *goquery.Selection) string { return norm(li.Text()) }))
	})
	for _, l := range lists {
		if slices.Equal(l, want) {
			return
		}
	}
	t.Errorf("%s: warning banners list %q, want one listing %q (components.Warnings)", what, lists, want)
}

// m04Has is the innermost elements of sel whose text contains text: the
// element showing it, whatever words a component puts beside it.
func m04Has(sel *goquery.Selection, text string) *goquery.Selection {
	return sel.Find("*").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return strings.Contains(norm(s.Text()), text) && s.Children().FilterFunction(func(_ int, c *goquery.Selection) bool {
			return strings.Contains(norm(c.Text()), text)
		}).Length() == 0
	})
}

// --- HOOK-011: check page extras ---------------------------------------

// m04OrderDraft grants the Hooker role (the open user's) read, create,
// write and submit on fixture.Order, and saves an order draft as the open
// user on PRJ-METRO for m04Amount with one line. Its approval preview is
// m04Preview.
func m04OrderDraft(t *testing.T, e *hookedEnv) int64 {
	t.Helper()
	ctx, m := t.Context(), e.fx.M
	rs := map[auth.Right]auth.Scope{"read": auth.ScopeAll, "create": auth.ScopeAll, "write": auth.ScopeAll, "submit": auth.ScopeAll}
	if err := m04Auth(t, e).SetRights(ctx, e.fx.Admin, "Hooker", fixture.Order, rs); err != nil {
		t.Fatalf("SetRights(Hooker, fixture.Order): %v", err)
	}
	res, err := e.engine.Do(ctx, e.open, fixture.Order, 0, doc.ActionSave, doc.ActionInput{
		Values: map[string]string{
			"supplier_id": itoa(m.Party["SHREE"]), "project_id": itoa(m.Project["PRJ-METRO"]), "order_date": "2026-10-24", "amount": m04Amount,
		},
		Lines: []map[string]string{{"item_id": itoa(m.Item["CEMENT-OPC"]), "qty": "2.000", "rate": "750.0000"}},
	})
	if err != nil {
		t.Fatalf("saving a fixture.Order draft: %v", err)
	}
	e.extras.SetApproval(fixture.Order, res.ID, docscreen.ApprovalView{Needed: true, Preview: m04Preview})
	return res.ID
}

// m04CheckModule's Check returns the notes, warnings and totals, or err.
func m04CheckModule(calls *m04Calls, err *error) docscreen.Module {
	return docscreen.Module{Check: func(c docscreen.Ctx) (docscreen.CheckExtra, error) {
		calls.saw(c)
		if *err != nil {
			return docscreen.CheckExtra{}, *err
		}
		return docscreen.CheckExtra{
			Notes:    []string{m04Note1, m04Note2},
			Warnings: []string{m04Warn1, m04Warn2},
			Totals: []components.Pair{
				{Label: m04LessLabel, Value: m04LessValue, Num: true},
				{Label: m04PayLabel, Value: m04PayValue, Num: true},
			},
		}, nil
	}}
}

// m04WantCheckExtras checks the check page's extras: the notes are the
// info banners straight after the approval preview, the warnings are a
// components.Warnings banner, and the totals are summary rows, outside every
// step's section, after the grand total figure and its words (AD ruling
// for M0-4: Expense's Less advance and To pay follow the grand total).
func m04WantCheckExtras(t *testing.T, page *goquery.Document, what string) {
	t.Helper()
	wiz := page.Find("#wizard")
	if wiz.Length() != 1 {
		t.Fatalf("%s: no #wizard", what)
	}
	info := bannerTexts(wiz, "info")
	i := slices.Index(info, m04Preview)
	if i < 0 || len(info) < i+3 || !slices.Equal(info[i+1:i+3], []string{m04Note1, m04Note2}) {
		t.Errorf("%s: info banners = %q, want the approval preview %q followed by the notes %q, %q", what, info, m04Preview, m04Note1, m04Note2)
	}
	m04Warnings(t, wiz, []string{m04Warn1, m04Warn2}, what)

	m, err := types.ParseMoney(m04Amount)
	if err != nil {
		t.Fatal(err)
	}
	grand := wiz.Find("p.num").FilterFunction(func(_ int, s *goquery.Selection) bool { return norm(s.Text()) == types.FormatINR(m) })
	if grand.Length() != 1 {
		t.Fatalf("%s: grand total %s shown %d times, want once", what, types.FormatINR(m), grand.Length())
	}
	words := wiz.Find("p.in-words")
	if words.Length() != 1 || !inOrder(page, grand, words) {
		t.Fatalf("%s: %d amount-in-words lines, want one after the grand total", what, words.Length())
	}
	sections := wiz.Find("section.card")
	if sections.Length() == 0 {
		t.Fatalf("%s: the check page has no step sections", what)
	}
	var rows []*goquery.Selection
	for _, p := range [][2]string{{m04LessLabel, m04LessValue}, {m04PayLabel, m04PayValue}} {
		row := wiz.Find(".summary-row").FilterFunction(func(_ int, s *goquery.Selection) bool {
			return norm(s.Find("dt").Text()) == p[0] && norm(s.Find("dd").Text()) == p[1]
		})
		if row.Length() != 1 {
			t.Errorf("%s: total %s %s shown as a summary row %d times, want once", what, p[0], p[1], row.Length())
			continue
		}
		if row.Closest("section.card").Length() > 0 || !inOrder(page, sections.Last(), row) {
			t.Errorf("%s: total %s sits inside or before a step's section, want after every section", what, p[0])
		}
		if !inOrder(page, words, row) || words.Get(0) == row.Get(0) || words.Contains(row.Get(0)) {
			t.Errorf("%s: total %s is not after the grand total and its words", what, p[0])
		}
		rows = append(rows, row)
	}
	if len(rows) == 2 && !inOrder(page, rows[0], rows[1]) {
		t.Errorf("%s: totals out of order, want %s then %s", what, m04LessLabel, m04PayLabel)
	}
}

// HOOK-011: Check's Notes follow the approval preview, its Warnings use
// components.Warnings and its Totals follow the grand total and its words, on the
// check page's GET and on its redraw after a refused POST (here a stale
// version, 409). Check gets the request's Ctx with the draft's View. A Check
// error is a 500 on both. A user who can't see the draft gets a missing
// record's 404, and Check is never called.
func TestModule_CheckExtra(t *testing.T) {
	e := newHookedEnv(t)
	id := m04OrderDraft(t, e)
	var calls m04Calls
	var fail error
	m04Mount(e, fixture.Order, m04CheckModule(&calls, &fail))
	check := m04URL(fixture.Order, id) + "/edit/check"

	page := m04Page(t, e.get(t, check), http.StatusOK, "GET check")
	m04WantCheckExtras(t, page, "GET check")
	if calls.n == 0 {
		t.Fatal("Check was never called on the check page")
	}
	m04WantCtx(t, &calls, fixture.Order, id, e.open)

	// The redraw after a refused POST (stale version) keeps the extras.
	before := calls.n
	rec := e.post(t, check, url.Values{"version": {"99"}})
	page = m04Page(t, rec, http.StatusConflict, "stale POST check")
	m04WantCheckExtras(t, page, "stale POST check redraw")
	if calls.n == before {
		t.Error("Check was not called on the redraw after a POST")
	}
	// htmx gets the redrawn #wizard with the extras too.
	page = m04Page(t, e.post(t, check, url.Values{"version": {"99"}}, "HX-Request", "true"), http.StatusConflict, "htmx stale POST check")
	m04WantCheckExtras(t, page, "htmx stale POST check redraw")

	// A Check error is a 500, never a page without its extras.
	fail = errM04Hook
	if rec := e.get(t, check); rec.Code != http.StatusInternalServerError {
		t.Errorf("GET check with a failing Check = %d, want 500", rec.Code)
	}
	if rec := e.post(t, check, url.Values{"version": {"99"}}); rec.Code != http.StatusInternalServerError {
		t.Errorf("stale POST check with a failing Check = %d, want 500", rec.Code)
	}
	fail = nil

	// Only after the read check: a user restricted to PRJ-TECHPARK can't
	// see this PRJ-METRO draft.
	hidden := m04User(t, e, "m04-techpark@vardhan.test", "M04TechPark",
		map[string][]auth.Right{fixture.Order: {"read", "create", "write", "submit"}}, e.fx.M.Project["PRJ-TECHPARK"])
	e.who = hidden
	calls = m04Calls{}
	missing := e.get(t, m04URL(fixture.Order, id+1000)+"/edit/check")
	m04Same404(t, e.get(t, check), missing, "hidden user GET check")
	if rec := e.post(t, check, url.Values{"version": {"99"}}); rec.Code == http.StatusOK || rec.Code == http.StatusConflict {
		t.Errorf("hidden user POST check = %d, want a refusal without the page", rec.Code)
	}
	if calls.n != 0 {
		t.Errorf("Check was called %d times for a user who can't see the draft, want 0", calls.n)
	}
}

// --- HOOK-012: document page extras ------------------------------------

// m04PageModule hides site_id and its Page returns the warnings, a note on
// line 2, the site as a Details pair and two cards, or err.
func m04PageModule(calls *m04Calls, err *error) docscreen.Module {
	return docscreen.Module{
		Hide: m04HideSite,
		Page: func(c docscreen.Ctx) (docscreen.PageExtra, error) {
			calls.saw(c)
			if *err != nil {
				return docscreen.PageExtra{}, *err
			}
			return docscreen.PageExtra{
				Warnings:  []string{m04Warn1, m04Warn2},
				LineNotes: map[int]string{2: m04LineNote},
				Pairs:     []components.Pair{{Label: m04SiteLabel, Value: m04SiteValue}},
				Cards:     []templ.Component{m04Card(m04Card1), m04Card(m04Card2)},
			}, nil
		},
	}
}

func m04HideSite(_ docscreen.Ctx, f doctype.Field) bool { return f.Name == "site_id" }

// m04WantPageExtras checks the document page's extras: the warnings banner;
// the note on line 2's card only; the module's Site pair in Details (the
// field itself is hidden); the two cards in order after Lines and before
// History.
func m04WantPageExtras(t *testing.T, page *goquery.Document, what string) {
	t.Helper()
	m04Warnings(t, page.Selection, []string{m04Warn1, m04Warn2}, what)

	lines := checkSection(page.Selection, "Lines")
	cards := lines.Find("li.lines-card")
	if cards.Length() != 2 {
		t.Fatalf("%s: Lines shows %d cards, want 2", what, cards.Length())
	}
	if m04Has(cards.Eq(1), m04LineNote).Length() != 1 {
		t.Errorf("%s: line 2's card %q lacks the note %q", what, norm(cards.Eq(1).Text()), m04LineNote)
	}
	if strings.Contains(norm(cards.Eq(0).Text()), m04LineNote) {
		t.Errorf("%s: line 1's card shows line 2's note", what)
	}

	var site []string
	checkSection(page.Selection, "Details").Find(".summary-row").Each(func(_ int, r *goquery.Selection) {
		if norm(r.Find("dt").Text()) == m04SiteLabel {
			site = append(site, norm(r.Find("dd").Text()))
		}
	})
	if !slices.Equal(site, []string{m04SiteValue}) {
		t.Errorf("%s: Details Site rows = %q, want only the module's pair %q", what, site, m04SiteValue)
	}

	c1, c2 := m04Has(page.Selection, m04Card1), m04Has(page.Selection, m04Card2)
	history := page.Find("details").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return norm(s.ChildrenFiltered("summary").Text()) == "History"
	})
	switch {
	case c1.Length() != 1 || c2.Length() != 1:
		t.Errorf("%s: cards shown %d and %d times, want once each", what, c1.Length(), c2.Length())
	case lines.Length() != 1 || history.Length() != 1:
		t.Errorf("%s: %d Lines cards and %d History disclosures, want one each", what, lines.Length(), history.Length())
	case lines.Contains(c1.Get(0)) || lines.Contains(c2.Get(0)) || !inOrder(page, lines, c1) || !inOrder(page, c1, c2) || !inOrder(page, c2, history):
		t.Errorf("%s: want Lines, then the module's cards in order, then History", what)
	}
}

// HOOK-012: Page's Warnings, LineNotes, Pairs and Cards appear on the
// document page (Cards after Lines, before History) on every pageViewOf
// path: the GET, a refused action's page and an htmx action's page. A Page
// error is a 500 on each. Page runs only after docscreen's read check: a
// user who can't see the record gets a missing record's 404 and Page is
// never called.
func TestModule_PageExtra(t *testing.T) {
	e := newHookedEnv(t)
	id := m04ApprovedHooked(t, e)
	other := m04ApprovedHooked(t, e)
	for _, x := range []int64{id, other} {
		e.extras.SetAudit(docscreentest.Hooked, x, []docscreen.HistoryRow{{At: "24-10-2026 10:00", By: "Priya Shah", Action: "Created"}})
	}
	var calls m04Calls
	var fail error
	m04Mount(e, docscreentest.Hooked, m04PageModule(&calls, &fail))
	base := m04URL(docscreentest.Hooked, id)

	// The page GET.
	page := m04Page(t, e.get(t, base), http.StatusOK, "GET document page")
	m04WantPageExtras(t, page, "GET document page")
	if calls.n == 0 {
		t.Fatal("Page was never called on the document page")
	}
	m04WantCtx(t, &calls, docscreentest.Hooked, id, e.open)

	// A refused action's page: submitting an approved document.
	refusedForm := func() url.Values { return url.Values{"version": {m04Version(t, e, docscreentest.Hooked, id)}} }
	rec := e.post(t, base+"/submit", refusedForm())
	if rec.Code < 400 || rec.Code == http.StatusInternalServerError {
		t.Fatalf("submitting an approved document = %d, want DOC's refusal; body %.300s", rec.Code, rec.Body)
	}
	refusedStatus := rec.Code
	m04WantPageExtras(t, uitest.Doc(t, rec), "refused action's page")

	// An htmx action's page: closing the other document.
	closeForm := func(x int64) url.Values {
		return url.Values{"version": {m04Version(t, e, docscreentest.Hooked, x)}, "reason": {"Balance not needed"}}
	}
	page = m04Page(t, e.post(t, m04URL(docscreentest.Hooked, other)+"/close", closeForm(other), "HX-Request", "true"), http.StatusOK, "htmx close")
	m04WantPageExtras(t, page, "htmx close's page")

	// A Page error is a 500 on each path.
	fail = errM04Hook
	if rec := e.get(t, base); rec.Code != http.StatusInternalServerError {
		t.Errorf("GET document page with a failing Page = %d, want 500", rec.Code)
	}
	if rec := e.post(t, base+"/submit", refusedForm()); rec.Code != http.StatusInternalServerError {
		t.Errorf("refused action's page with a failing Page = %d (%d without), want 500", rec.Code, refusedStatus)
	}
	if rec := e.post(t, base+"/close", closeForm(id), "HX-Request", "true"); rec.Code != http.StatusInternalServerError {
		t.Errorf("htmx close's page with a failing Page = %d, want 500", rec.Code)
	}
	fail = nil

	// Only after the read check: the TECH-SITE user can't see this
	// METRO-SITE document. other is closed by now; id is still approved
	// unless the failing htmx close went through, which doesn't matter here.
	e.who = e.restricted
	calls = m04Calls{}
	missing := e.get(t, m04URL(docscreentest.Hooked, other+1000))
	m04Same404(t, e.get(t, base), missing, "restricted user GET document page")
	if rec := e.post(t, base+"/submit", url.Values{"version": {m04Version(t, e, docscreentest.Hooked, id)}}); rec.Code == http.StatusOK || rec.Code == http.StatusInternalServerError {
		t.Errorf("restricted user POST submit = %d, want a refusal", rec.Code)
	}
	if calls.n != 0 {
		t.Errorf("Page was called %d times for a user who can't see the record, want 0", calls.n)
	}
}

// --- HOOK-013: reason page extras --------------------------------------

// m04ReasonModule's Reason records its action and returns the note and the
// hint, or err.
func m04ReasonModule(calls *m04Calls, err *error) docscreen.Module {
	return docscreen.Module{Reason: func(c docscreen.Ctx, a doctype.Action) (docscreen.ReasonExtra, error) {
		calls.saw(c)
		calls.actions = append(calls.actions, a)
		if *err != nil {
			return docscreen.ReasonExtra{}, *err
		}
		return docscreen.ReasonExtra{Note: m04Reason, Hint: m04Hint}, nil
	}}
}

// m04WantReasonExtras checks the close page: the note above the reason
// field, the hint as the field's hint, and the kernel's field label and
// button label.
func m04WantReasonExtras(t *testing.T, page *goquery.Document, what string) {
	t.Helper()
	id := components.FieldID("reason")
	area := page.Find("textarea#" + id)
	if area.Length() != 1 {
		t.Fatalf("%s: no reason textarea #%s", what, id)
	}
	label := page.Find(`label[for="` + id + `"]`)
	if got := norm(label.Text()); got != "Reason for closing" {
		t.Errorf("%s: field label = %q, want the kernel's Reason for closing", what, got)
	}
	note := m04Has(page.Selection, m04Reason)
	if note.Length() != 1 {
		t.Errorf("%s: note %q shown %d times, want once", what, m04Reason, note.Length())
	} else if !inOrder(page, note, label) {
		t.Errorf("%s: note is not above the reason field", what)
	}
	if got := norm(page.Find("#" + id + "-hint").Text()); got != m04Hint {
		t.Errorf("%s: field hint = %q, want %q", what, got, m04Hint)
	}
	if d := area.AttrOr("aria-describedby", ""); !slices.Contains(strings.Fields(d), id+"-hint") {
		t.Errorf("%s: textarea aria-describedby = %q, want it to name %s-hint", what, d, id)
	}
	if strings.Contains(norm(label.Text()), m04Hint) || strings.Contains(norm(label.Text()), m04Reason) {
		t.Errorf("%s: the field label carries the module's text", what)
	}
	buttons := page.Find("#main button").Map(func(_ int, b *goquery.Selection) string { return norm(b.Text()) })
	if !slices.Contains(buttons, "Close hooked order") {
		t.Errorf("%s: buttons = %q, want the kernel's Close hooked order", what, buttons)
	}
}

// HOOK-013: Reason's Note shows above the reason field and its Hint is the
// field's hint, on the close page's GET and on reasonPost's redraws (a 422
// for an empty reason, a 409 for a stale version); Reason is called with
// the page's action, and the button and field labels stay the kernel's. A
// Reason error is a 500. Reason runs only after docscreen's checks: a user
// who can't see the record gets a missing record's 404, a user without the
// page's right AUTH's 403, and Reason is never called for either.
func TestModule_ReasonExtra(t *testing.T) {
	e := newHookedEnv(t)
	id := m04ApprovedHooked(t, e)
	var calls m04Calls
	var fail error
	m04Mount(e, docscreentest.Hooked, m04ReasonModule(&calls, &fail))
	closeURL := m04URL(docscreentest.Hooked, id) + "/close"

	m04WantReasonExtras(t, m04Page(t, e.get(t, closeURL), http.StatusOK, "GET close"), "GET close")
	if calls.n == 0 {
		t.Fatal("Reason was never called on the close page")
	}
	m04WantCtx(t, &calls, docscreentest.Hooked, id, e.open)
	if !slices.Equal(calls.actions, []doctype.Action{doctype.ActionClose}) {
		t.Errorf("Reason actions = %q, want [close]", calls.actions)
	}

	// reasonPost's 422: an empty reason.
	version := m04Version(t, e, docscreentest.Hooked, id)
	page := m04Page(t, e.post(t, closeURL, url.Values{"version": {version}, "reason": {"  "}}), http.StatusUnprocessableEntity, "POST close without a reason")
	m04WantReasonExtras(t, page, "422 redraw")
	if got := norm(page.Find("#" + components.FieldID("reason") + "-msg").Text()); !strings.Contains(got, "Enter a reason.") {
		t.Errorf("422 redraw field message = %q, want Enter a reason.", got)
	}
	// reasonPost after DOC refuses: a stale version.
	page = m04Page(t, e.post(t, closeURL, url.Values{"version": {"99"}, "reason": {"Balance not needed"}}), http.StatusConflict, "stale POST close")
	m04WantReasonExtras(t, page, "409 redraw")
	if got := e.fx.Record(t, docscreentest.Hooked, id); got.Version != m04Atoi(t, version) {
		t.Fatalf("a refused close wrote: version %d, want %s", got.Version, version)
	}
	if n := len(calls.actions); n != 3 || calls.actions[1] != doctype.ActionClose || calls.actions[2] != doctype.ActionClose {
		t.Errorf("Reason actions = %q, want close on the GET and both redraws", calls.actions)
	}

	// A Reason error is a 500.
	fail = errM04Hook
	if rec := e.get(t, closeURL); rec.Code != http.StatusInternalServerError {
		t.Errorf("GET close with a failing Reason = %d, want 500", rec.Code)
	}
	if rec := e.post(t, closeURL, url.Values{"version": {version}, "reason": {""}}); rec.Code != http.StatusInternalServerError {
		t.Errorf("422 redraw with a failing Reason = %d, want 500", rec.Code)
	}
	fail = nil

	// Only after docscreen's checks.
	calls = m04Calls{}
	e.who = e.restricted
	m04Same404(t, e.get(t, closeURL), e.get(t, m04URL(docscreentest.Hooked, id+1000)+"/close"), "restricted user GET close")
	e.who = m04User(t, e, "m04-reader@vardhan.test", "M04Reader", map[string][]auth.Right{docscreentest.Hooked: {"read"}}, 0)
	if rec := e.get(t, closeURL); rec.Code != http.StatusForbidden {
		t.Errorf("GET close without submit = %d, want AUTH's 403", rec.Code)
	}
	if calls.n != 0 {
		t.Errorf("Reason was called %d times for users the page refuses, want 0", calls.n)
	}
}

func m04Atoi(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

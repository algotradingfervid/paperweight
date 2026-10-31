package docscreen_test

import (
	"context"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// M0-16 (CD-263 G2 to G5, CD-275; hooks spec §7 ruling 5): CheckExtra.Cards
// on the check page, and PageExtra.Notice, More and Primary on the document
// page, on the real engine and the real AUTH service (CD-125). The check
// page runs on fixture.Claim, the registered DocType that is Attachable and
// has a grand total; Notice runs on Hooked; More and Primary run on
// fixture.Order with 00-fixtures' rights (SeedDefaults), where approvals
// give docscreen its own primary.

// Test values: what the hooks return.
const (
	m16Card1      = "Owner card from the module"
	m16Card2      = "Comments card from the module"
	m16CardAction = "/modules/m16/comment"
	m16ClaimTotal = "1500.00"
	m16Notice     = "Owned by Priya Shah. Only Priya Shah can change this draft."
	m16NoticeLink = "Hand over"
	m16NoticeHref = "/modules/m16/handover"
	m16CopyLabel  = "Copy"
	m16CopyHref   = "/modules/m16/copy"
	m16HandLabel  = "Hand over"
	m16HandHref   = "/modules/m16/handover"
	m16CopyParam  = "from"
	m16CopyValue  = "m16-source"
	m16SendLabel  = "Send to vendor"
	m16SendHref   = "/modules/m16/send"
	m16Blocked    = "You submitted this order, so someone else must approve it."
)

var errM16Hook = errors.New("m16: the hook failed")

// m16Calls counts a hook's calls.
type m16Calls struct{ n int }

// m16Card is a module card: a section holding the module's own form, so
// a card rendered inside the step form would nest forms.
func m16Card(text string) templ.Component {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, `<section data-m16-card><form method="post" action="`+m16CardAction+`"><p>`+templ.EscapeString(text)+`</p></form></section>`)
		return err
	})
}

// m16MountOn gives dt the module m on the scope harness and rebuilds the
// router.
func m16MountOn(e *scopeEnv, dt string, m docscreen.Module) {
	e.d.Modules = map[string]docscreen.Module{dt: m}
	e.remount()
}

// m16Status fails unless rec has status want and returns its page.
func m16Status(t *testing.T, rec *httptest.ResponseRecorder, want int, what string) *goquery.Document {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("%s = %d, want %d; body %.600s", what, rec.Code, want, rec.Body)
	}
	return uitest.Doc(t, rec)
}

// m16Banner is the one banner (outside the layout's <template>s) whose
// text contains text; it fails unless there is exactly one.
func m16Banner(t *testing.T, page *goquery.Document, text, what string) *goquery.Selection {
	t.Helper()
	b := page.Find(".banner").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return s.ParentsFiltered("template").Length() == 0 && strings.Contains(norm(s.Text()), text)
	})
	if b.Length() != 1 {
		t.Fatalf("%s: %d banners show %q, want exactly one; main %.600s", what, b.Length(), text, norm(page.Find("#main").Text()))
	}
	return b
}

// --- HOOK-022: check page cards ----------------------------------------

// m16ClaimDraft grants the Hooker role (the open user's) read, create,
// write and submit on fixture.Claim, and saves a claim draft as the open
// user with one line of m16ClaimTotal.
func m16ClaimDraft(t *testing.T, e *hookedEnv) int64 {
	t.Helper()
	ctx := t.Context()
	rs := map[auth.Right]auth.Scope{"read": auth.ScopeAll, "create": auth.ScopeAll, "write": auth.ScopeAll, "submit": auth.ScopeAll}
	if err := m04Auth(t, e).SetRights(ctx, e.fx.Admin, "Hooker", fixture.Claim, rs); err != nil {
		t.Fatalf("SetRights(Hooker, fixture.Claim): %v", err)
	}
	if len(e.fx.F.Employees) == 0 {
		t.Fatal("the fixture has no employees")
	}
	res, err := e.engine.Do(ctx, e.open, fixture.Claim, 0, doc.ActionSave, doc.ActionInput{
		Values: map[string]string{"employee_id": itoa(e.fx.F.Employees[0].ID), "claim_date": "2026-10-24"},
		Lines:  []map[string]string{{"expense_type": "TRAVEL", "amount": m16ClaimTotal}},
	})
	if err != nil {
		t.Fatalf("saving a fixture.Claim draft: %v", err)
	}
	return res.ID
}

// m16CardsModule's Check returns the two cards, or err.
func m16CardsModule(calls *m16Calls, err *error) docscreen.Module {
	return docscreen.Module{Check: func(docscreen.Ctx) (docscreen.CheckExtra, error) {
		calls.n++
		if *err != nil {
			return docscreen.CheckExtra{}, *err
		}
		return docscreen.CheckExtra{Cards: []templ.Component{m16Card(m16Card1), m16Card(m16Card2)}}, nil
	}}
}

// m16WantCheckCards checks the check page: the two cards once each, in
// order, after the Attachments card and before the grand total, outside
// the step form (each card's form is its own, never nested).
func m16WantCheckCards(t *testing.T, page *goquery.Document, what string) {
	t.Helper()
	wiz := page.Find("#wizard")
	if wiz.Length() != 1 {
		t.Fatalf("%s: no #wizard", what)
	}
	attach := checkSection(wiz, "Attachments")
	if attach.Length() != 1 {
		t.Fatalf("%s: %d Attachments cards, want 1 (fixture.Claim is Attachable)", what, attach.Length())
	}
	m, err := types.ParseMoney(m16ClaimTotal)
	if err != nil {
		t.Fatal(err)
	}
	grand := wiz.Find("p.num").FilterFunction(func(_ int, s *goquery.Selection) bool { return norm(s.Text()) == types.FormatINR(m) })
	if grand.Length() != 1 {
		t.Fatalf("%s: grand total %s shown %d times, want once", what, types.FormatINR(m), grand.Length())
	}
	var cards []*goquery.Selection
	for _, text := range []string{m16Card1, m16Card2} {
		c := wiz.Find("section[data-m16-card]").FilterFunction(func(_ int, s *goquery.Selection) bool { return norm(s.Text()) == text })
		if c.Length() != 1 {
			t.Errorf("%s: card %q shown %d times, want once", what, text, c.Length())
			continue
		}
		if n := c.ParentsFiltered("form").Length(); n != 0 {
			t.Errorf("%s: card %q sits inside %d forms, want none (outside the step form, no nested form)", what, text, n)
		}
		if f := c.Find("form"); f.Length() != 1 || f.AttrOr("action", "") != m16CardAction {
			t.Errorf("%s: card %q's own form is missing or changed", what, text)
		}
		if !inOrder(page, attach, c) || attach.Contains(c.Get(0)) {
			t.Errorf("%s: card %q is not after the Attachments card", what, text)
		}
		if !inOrder(page, c, grand) {
			t.Errorf("%s: card %q is not before the grand total", what, text)
		}
		cards = append(cards, c)
	}
	if len(cards) == 2 && !inOrder(page, cards[0], cards[1]) {
		t.Errorf("%s: cards out of order, want %q then %q", what, m16Card1, m16Card2)
	}
}

// HOOK-022: CheckExtra.Cards render after the Attachments card and before
// the grand total, outside the step form, on the check page's GET and on
// its redraws after a refused POST (a stale version; full page and htmx).
// A Check error is a 500. A user who can't see the draft gets a missing
// record's 404 and Check is never called.
func TestModule_CheckCards(t *testing.T) {
	e := newHookedEnv(t)
	id := m16ClaimDraft(t, e)
	var calls m16Calls
	var fail error
	m04Mount(e, fixture.Claim, m16CardsModule(&calls, &fail))
	check := m04URL(fixture.Claim, id) + "/edit/check"

	m16WantCheckCards(t, m16Status(t, e.get(t, check), http.StatusOK, "GET check"), "GET check")
	if calls.n == 0 {
		t.Fatal("Check was never called on the check page")
	}
	m16WantCheckCards(t, m16Status(t, e.post(t, check, url.Values{"version": {"99"}}), http.StatusConflict, "stale POST check"), "stale POST check redraw")
	m16WantCheckCards(t, m16Status(t, e.post(t, check, url.Values{"version": {"99"}}, "HX-Request", "true"), http.StatusConflict, "htmx stale POST check"), "htmx stale POST check redraw")

	fail = errM16Hook
	if rec := e.get(t, check); rec.Code != http.StatusInternalServerError {
		t.Errorf("GET check with a failing Check = %d, want 500", rec.Code)
	}
	fail = nil

	// A user whose read on fixture.Claim is own can't see the open user's
	// draft.
	hidden := m16ScopedUser(t, e, "m16-own@vardhan.test", "M16 Own Claims", fixture.Claim, auth.ScopeOwn)
	e.who = hidden
	calls = m16Calls{}
	missing := e.get(t, m04URL(fixture.Claim, id+1000)+"/edit/check")
	m04Same404(t, e.get(t, check), missing, "a user who can't see the claim: GET check")
	if calls.n != 0 {
		t.Errorf("Check was called %d times for a user who can't see the draft, want 0", calls.n)
	}
}

// m16ScopedUser creates a user holding a new role with read, create, write
// and submit on dt at scope s, and loads it.
func m16ScopedUser(t *testing.T, e *hookedEnv, email, role, dt string, s auth.Scope) auth.Principal {
	t.Helper()
	ctx, a, admin := t.Context(), m04Auth(t, e), e.fx.Admin
	if err := a.CreateRole(ctx, admin, role); err != nil {
		t.Fatalf("CreateRole(%s): %v", role, err)
	}
	rs := map[auth.Right]auth.Scope{"read": s, "create": s, "write": s, "submit": s}
	if err := a.SetRights(ctx, admin, role, dt, rs); err != nil {
		t.Fatalf("SetRights(%s, %s): %v", role, dt, err)
	}
	return m15User(t, a, admin, email, role)
}

// --- HOOK-023: the module's notice -------------------------------------

// m16NoticeModule's Page returns *notice (nil: none), or err.
func m16NoticeModule(calls *m16Calls, notice **components.BannerProps, err *error) docscreen.Module {
	return docscreen.Module{Page: func(docscreen.Ctx) (docscreen.PageExtra, error) {
		calls.n++
		if *err != nil {
			return docscreen.PageExtra{}, *err
		}
		return docscreen.PageExtra{Notice: *notice}, nil
	}}
}

// m16WantNotice checks the document page's notice: one banner of tint
// class kind with the message and its link, after every banner and link
// in before (docscreen's own), and before the page's h1.
func m16WantNotice(t *testing.T, page *goquery.Document, kind string, before []*goquery.Selection, what string) {
	t.Helper()
	n := m16Banner(t, page, m16Notice, what)
	if !n.HasClass("banner--" + kind) {
		t.Errorf("%s: notice classes %q, want banner--%s", what, n.AttrOr("class", ""), kind)
	}
	if a := n.Find("a"); a.Length() != 1 || a.AttrOr("href", "") != m16NoticeHref || norm(a.Text()) != m16NoticeLink {
		t.Errorf("%s: notice links %d, want one %q to %s", what, a.Length(), m16NoticeLink, m16NoticeHref)
	}
	h1 := page.Find("#main h1")
	if h1.Length() != 1 {
		t.Fatalf("%s: %d h1 in #main, want 1", what, h1.Length())
	}
	if !inOrder(page, n, h1) {
		t.Errorf("%s: the notice is not above the title", what)
	}
	for i, b := range before {
		if !inOrder(page, b, n) || b.Get(0) == n.Get(0) {
			t.Errorf("%s: docscreen banner %d (%q) is not before the notice, want the notice last", what, i, norm(b.Text()))
		}
	}
}

// HOOK-023: PageExtra.Notice is the last of docscreen's banners, above the
// title: after a refused action's danger banner, after an htmx action's
// success banner, and after the "Next request" link. A blank Kind renders
// as Info (no panic); a set Kind is kept. A Page error is a 500. A user
// who can't see the record gets a missing record's 404 and Page is never
// called.
func TestModule_PageNotice(t *testing.T) {
	e := newHookedEnv(t)
	id := m04ApprovedHooked(t, e)
	other := m04ApprovedHooked(t, e)
	var calls m16Calls
	var fail error
	notice := &components.BannerProps{Kind: components.Info, Message: m16Notice, Link: &components.Link{Label: m16NoticeLink, Href: m16NoticeHref}}
	m04Mount(e, docscreentest.Hooked, m16NoticeModule(&calls, &notice, &fail))
	base := m04URL(docscreentest.Hooked, id)

	// The page GET: the notice alone, above the title.
	m16WantNotice(t, m16Status(t, e.get(t, base), http.StatusOK, "GET document page"), "info", nil, "GET document page")
	if calls.n == 0 {
		t.Fatal("Page was never called on the document page")
	}

	// After "Next request" (the page after an approve).
	page := m16Status(t, e.get(t, base+"?next=1"), http.StatusOK, "GET document page ?next=1")
	next := page.Find("#main a").FilterFunction(func(_ int, s *goquery.Selection) bool { return norm(s.Text()) == "Next request" })
	if next.Length() != 1 {
		t.Fatalf("?next=1: %d Next request links, want 1", next.Length())
	}
	m16WantNotice(t, page, "info", []*goquery.Selection{next}, "?next=1")

	// After a refused action's danger banner: submitting an approved document.
	rec := e.post(t, base+"/submit", url.Values{"version": {m04Version(t, e, docscreentest.Hooked, id)}})
	if rec.Code < 400 || rec.Code == http.StatusInternalServerError {
		t.Fatalf("submitting an approved document = %d, want DOC's refusal; body %.300s", rec.Code, rec.Body)
	}
	page = uitest.Doc(t, rec)
	danger := page.Find(".banner--danger").FilterFunction(func(_ int, s *goquery.Selection) bool { return s.ParentsFiltered("template").Length() == 0 })
	if danger.Length() == 0 {
		t.Fatal("refused action's page has no danger banner")
	}
	m16WantNotice(t, page, "info", []*goquery.Selection{danger.Last()}, "refused action's page")

	// After an htmx action's success banner (the #main fragment): closing other.
	closeForm := url.Values{"version": {m04Version(t, e, docscreentest.Hooked, other)}, "reason": {"Balance not needed"}}
	page = m16Status(t, e.post(t, m04URL(docscreentest.Hooked, other)+"/close", closeForm, "HX-Request", "true"), http.StatusOK, "htmx close")
	m16WantNotice(t, page, "info", []*goquery.Selection{m16Banner(t, page, "Closed.", "htmx close")}, "htmx close's page")

	// A blank Kind renders as Info; a set Kind is kept.
	notice = &components.BannerProps{Message: m16Notice, Link: &components.Link{Label: m16NoticeLink, Href: m16NoticeHref}}
	m16WantNotice(t, m16Status(t, e.get(t, base), http.StatusOK, "GET with a blank-kind notice"), "info", nil, "blank Kind")
	notice = &components.BannerProps{Kind: components.Warning, Message: m16Notice, Link: &components.Link{Label: m16NoticeLink, Href: m16NoticeHref}}
	m16WantNotice(t, m16Status(t, e.get(t, base), http.StatusOK, "GET with a warning notice"), "warning", nil, "Warning kind")

	// No notice: no module banner.
	notice = nil
	if b := m04Has(m16Status(t, e.get(t, base), http.StatusOK, "GET without a notice").Selection, m16Notice); b.Length() != 0 {
		t.Errorf("Page without a Notice: the page shows %q", m16Notice)
	}

	fail = errM16Hook
	if rec := e.get(t, base); rec.Code != http.StatusInternalServerError {
		t.Errorf("GET document page with a failing Page = %d, want 500", rec.Code)
	}
	fail = nil

	e.who = e.restricted
	calls = m16Calls{}
	m04Same404(t, e.get(t, base), e.get(t, m04URL(docscreentest.Hooked, other+1000)), "restricted user GET document page")
	if calls.n != 0 {
		t.Errorf("Page was called %d times for a user who can't see the record, want 0", calls.n)
	}
}

// --- HOOK-024 and HOOK-025: fixture.Order with 00-fixtures' rights ------

// m16OrderEnv is m15's fixture.Order harness (SeedDefaults, real engine)
// with an approved order and a pending one (the reference PO, which needs
// approval), both by Priya on PRJ-METRO, and a Purchase User restricted to
// PRJ-TECHPARK, who can't see either.
type m16OrderEnv struct {
	*m15OrderEnv
	approved, pending int64
	priya, ravi       auth.Principal
	admin, hidden     auth.Principal
}

func m16NewOrderEnv(t *testing.T) *m16OrderEnv {
	t.Helper()
	e := m15NewOrderEnv(t)
	ctx := t.Context()
	priya := m15Load(t, e.a, e.fx.Priya.ID)
	ok := e.fx.NewOrder(t, priya, fixturetest.OL{Item: "CEMENT-OPC", Qty: "10.000", Rate: "350.0000"})
	if res := e.fx.MustDo(t, ctx, priya, fixture.Order, ok.ID, doc.ActionSubmit, doc.ActionInput{Version: ok.Version}); res.State != doctype.StateApproved {
		t.Fatalf("the small order is %s after submit, want approved", res.State)
	}
	ref := e.fx.F.ReferencePO
	var lines []fixturetest.OL
	for _, l := range ref.Lines {
		lines = append(lines, fixturetest.OL{Item: l.Item, Qty: doc.FormatPlain(doc.Field{Type: doc.Qty}, l.Qty), Rate: doc.FormatPlain(doc.Field{Type: doc.Rate}, l.Rate)})
	}
	big := e.fx.NewOrderWith(t, priya, map[string]string{
		"supplier_id": itoa(e.fx.M.Party[ref.Party]), "amount": doc.FormatPlain(doc.Field{Type: doc.Money}, ref.GrandTotal),
	}, lines...)
	if res := e.fx.MustDo(t, ctx, priya, fixture.Order, big.ID, doc.ActionSubmit, doc.ActionInput{Version: big.Version}); res.State != doctype.StatePending {
		t.Fatalf("the reference order is %s after submit, want pending", res.State)
	}
	hidden := m15User(t, e.a, e.fx.Admin, "m16-techpark@vardhan.test", "Purchase User")
	if err := e.a.ChangeRestrictions(ctx, e.fx.Admin, hidden.ID, auth.DimProject, []int64{e.fx.M.Project["PRJ-TECHPARK"]}, nil, nil); err != nil {
		t.Fatalf("ChangeRestrictions: %v", err)
	}
	return &m16OrderEnv{
		m15OrderEnv: e, approved: ok.ID, pending: big.ID,
		priya: priya, ravi: m15Load(t, e.a, e.fx.Ravi.ID), admin: m15Load(t, e.a, e.fx.Admin.ID),
		hidden: m15Load(t, e.a, hidden.ID),
	}
}

// m16More is one rendered More actions entry: its label, its link or
// form action, whether it posts, and a POST form's hidden inputs.
type m16More struct {
	Label, Href string
	Post        bool
	Hidden      map[string]string
}

// m16MoreOf lists the page's More actions entries in order.
func m16MoreOf(t *testing.T, page *goquery.Document, what string) []m16More {
	t.Helper()
	sec := page.Find("section.more-actions")
	if sec.Length() > 1 {
		t.Fatalf("%s: %d More actions sections, want at most 1", what, sec.Length())
	}
	var out []m16More
	sec.Find(".more-actions-items > li").Each(func(_ int, li *goquery.Selection) {
		if f := li.Find("form"); f.Length() > 0 {
			m := m16More{Label: norm(f.Find("button").Text()), Href: f.AttrOr("action", ""), Post: strings.EqualFold(f.AttrOr("method", ""), "post"), Hidden: map[string]string{}}
			f.Find("input[type=hidden]").Each(func(_ int, in *goquery.Selection) { m.Hidden[in.AttrOr("name", "")] = in.AttrOr("value", "") })
			out = append(out, m)
			return
		}
		a := li.Find("a")
		out = append(out, m16More{Label: norm(a.Text()), Href: a.AttrOr("href", "")})
	})
	return out
}

func m16Labels(ms []m16More) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Label
	}
	return out
}

// m16MoreModule's Page returns *more, or err.
func m16MoreModule(calls *m16Calls, more *[]components.MoreAction, err *error) docscreen.Module {
	return docscreen.Module{Page: func(docscreen.Ctx) (docscreen.PageExtra, error) {
		calls.n++
		if *err != nil {
			return docscreen.PageExtra{}, *err
		}
		return docscreen.PageExtra{More: *more}, nil
	}}
}

// m16ModuleMore is what the More module returns: a POST entry with a
// hidden input and a link entry.
func m16ModuleMore() []components.MoreAction {
	return []components.MoreAction{
		{Label: m16CopyLabel, Href: m16CopyHref, Post: true, Hidden: []components.Param{{Name: m16CopyParam, Value: m16CopyValue}}},
		{Label: m16HandLabel, Href: m16HandHref},
	}
}

// m16WantModuleFirst fails unless got is the module's two entries (the
// POST one with the page's csrf token and its hidden input) followed by
// base, docscreen's own entries, unchanged and in their order.
func m16WantModuleFirst(t *testing.T, got, base []m16More, what string) {
	t.Helper()
	want := slices.Concat([]string{m16CopyLabel, m16HandLabel}, m16Labels(base))
	if !slices.Equal(m16Labels(got), want) {
		t.Fatalf("%s: More actions = %q, want the module's entries first, then docscreen's: %q", what, m16Labels(got), want)
	}
	cp := got[0]
	if !cp.Post || cp.Href != m16CopyHref || cp.Hidden["csrf_token"] != csrfToken || cp.Hidden[m16CopyParam] != m16CopyValue {
		t.Errorf("%s: module POST entry = %+v, want a POST form to %s with csrf_token %q and %s=%s", what, cp, m16CopyHref, csrfToken, m16CopyParam, m16CopyValue)
	}
	if hand := got[1]; hand.Post || hand.Href != m16HandHref {
		t.Errorf("%s: module link entry = %+v, want a link to %s", what, hand, m16HandHref)
	}
	for i, b := range base {
		g := got[i+2]
		if g.Label != b.Label || g.Href != b.Href || g.Post != b.Post || !maps.Equal(g.Hidden, b.Hidden) {
			t.Errorf("%s: docscreen's entry %d = %+v, want it unchanged: %+v", what, i, g, b)
		}
	}
}

// HOOK-024: PageExtra.More entries come before docscreen's own More actions
// entries, whose order and contents are unchanged: on an approved order
// (Close, Cancel, Print for an Administrator) and a pending one (Withdraw,
// a POST with the page's version, then Print). A POST entry gets the
// page's csrf token. A Page error is a 500. A user who can't see the
// order gets a missing record's 404 and Page is never called.
func TestModule_PageMore(t *testing.T) {
	e := m16NewOrderEnv(t)
	e.who = e.admin
	cases := []struct {
		name string
		id   int64
		want []string
	}{
		{"approved", e.approved, []string{"Close", "Cancel", "Print"}},
		{"pending", e.pending, []string{"Withdraw", "Print"}},
	}
	base := map[int64][]m16More{}
	for _, c := range cases {
		u := m15DocURL(fixture.Order, c.id)
		got := m16MoreOf(t, m16Status(t, e.get(t, u), http.StatusOK, c.name+" without a module"), c.name)
		if !slices.Equal(m16Labels(got), c.want) {
			t.Fatalf("%s without a module: More actions = %q, want docscreen's %q", c.name, m16Labels(got), c.want)
		}
		base[c.id] = got
	}

	var calls m16Calls
	var fail error
	more := m16ModuleMore()
	m16MountOn(e.scopeEnv, fixture.Order, m16MoreModule(&calls, &more, &fail))
	for _, c := range cases {
		u := m15DocURL(fixture.Order, c.id)
		got := m16MoreOf(t, m16Status(t, e.get(t, u), http.StatusOK, c.name+" with the module"), c.name)
		m16WantModuleFirst(t, got, base[c.id], c.name)
	}
	if calls.n == 0 {
		t.Fatal("Page was never called on the document page")
	}

	// No module entries: docscreen's alone.
	more = nil
	if got := m16MoreOf(t, m16Status(t, e.get(t, m15DocURL(fixture.Order, e.approved)), http.StatusOK, "no module entries"), "no module entries"); !slices.Equal(m16Labels(got), cases[0].want) {
		t.Errorf("Page without More: More actions = %q, want docscreen's %q", m16Labels(got), cases[0].want)
	}
	more = m16ModuleMore()

	fail = errM16Hook
	if rec := e.get(t, m15DocURL(fixture.Order, e.approved)); rec.Code != http.StatusInternalServerError {
		t.Errorf("GET document page with a failing Page = %d, want 500", rec.Code)
	}
	fail = nil

	e.who = e.hidden
	calls = m16Calls{}
	m04Same404(t, e.get(t, m15DocURL(fixture.Order, e.approved)), e.get(t, m15DocURL(fixture.Order, e.pending+1000)), "PRJ-TECHPARK user GET document page")
	if calls.n != 0 {
		t.Errorf("Page was called %d times for a user who can't see the order, want 0", calls.n)
	}
}

// m16PrimaryModule's Page returns *primary, or err.
func m16PrimaryModule(calls *m16Calls, primary **components.ButtonProps, err *error) docscreen.Module {
	return docscreen.Module{Page: func(docscreen.Ctx) (docscreen.PageExtra, error) {
		calls.n++
		if *err != nil {
			return docscreen.PageExtra{}, *err
		}
		return docscreen.PageExtra{Primary: *primary}, nil
	}}
}

// m16SendLinks is every link to the module's primary on the page.
func m16SendLinks(page *goquery.Document) *goquery.Selection {
	return page.Find(`a[href="` + m16SendHref + `"]`)
}

// m16WantModulePrimary fails unless the bar's first control is the
// module's primary link, shown nowhere else, followed by the labels of
// docscreen's own bar buttons in after.
func m16WantModulePrimary(t *testing.T, page *goquery.Document, after []string, what string) {
	t.Helper()
	bar := page.Find(".actionbar")
	if bar.Length() != 1 {
		t.Fatalf("%s: %d action bars, want 1; main %.600s", what, bar.Length(), norm(page.Find("#main").Text()))
	}
	ctrls := bar.Find("a, button")
	labels := ctrls.Map(func(_ int, s *goquery.Selection) string { return norm(s.Text()) })
	if want := slices.Concat([]string{m16SendLabel}, after); !slices.Equal(labels, want) {
		t.Fatalf("%s: action bar = %q, want %q (the module's primary first)", what, labels, want)
	}
	first := ctrls.First()
	if goquery.NodeName(first) != "a" || first.AttrOr("href", "") != m16SendHref || !first.HasClass("btn-primary") {
		html, _ := goquery.OuterHtml(first)
		t.Errorf("%s: bar's primary = %s, want a primary link to %s", what, html, m16SendHref)
	}
	if n := m16SendLinks(page).Length(); n != 1 {
		t.Errorf("%s: %d links to %s, want only the bar's", what, n, m16SendHref)
	}
}

// m16WantNoModulePrimary fails if the module's primary shows anywhere on
// the page; the bar is docscreen's: labels, or the note.
func m16WantNoModulePrimary(t *testing.T, page *goquery.Document, labels []string, note, what string) {
	t.Helper()
	if n := m16SendLinks(page).Length(); n != 0 || m04Has(page.Selection, m16SendLabel).Length() != 0 {
		t.Errorf("%s: the module's %q shows (%d links), want docscreen's bar only", what, m16SendLabel, n)
	}
	bar := page.Find(".actionbar")
	got := bar.Find("a, button").Map(func(_ int, s *goquery.Selection) string { return norm(s.Text()) })
	if !slices.Equal(got, labels) {
		t.Errorf("%s: action bar = %q, want docscreen's %q", what, got, labels)
	}
	if gotNote := norm(bar.Find(".actionbar-note").Text()); gotNote != note {
		t.Errorf("%s: action bar note = %q, want %q", what, gotNote, note)
	}
}

// HOOK-025: PageExtra.Primary is the bar's primary link only when
// docscreen's bar has no primary and no note: an approved order for Priya
// (no bar buttons) and a pending order for Priya (Withdraw only, which
// stays the secondary after it). When docscreen has its own primary
// (Approve for Ravi, an approver) or a note (APR blocks Ravi), the
// module's isn't shown. A Primary without Href is a 500, as is a Page
// error. A user who can't see the order gets a missing record's 404 and
// Page is never called.
func TestModule_PagePrimary(t *testing.T) {
	e := m16NewOrderEnv(t)
	var calls m16Calls
	var fail error
	primary := &components.ButtonProps{Label: m16SendLabel, Href: m16SendHref, Variant: components.Primary}
	m16MountOn(e.scopeEnv, fixture.Order, m16PrimaryModule(&calls, &primary, &fail))
	approved, pending := m15DocURL(fixture.Order, e.approved), m15DocURL(fixture.Order, e.pending)

	e.who = e.priya
	m16WantModulePrimary(t, m16Status(t, e.get(t, approved), http.StatusOK, "Priya: approved order"), nil, "Priya: approved order")
	if calls.n == 0 {
		t.Fatal("Page was never called on the document page")
	}
	m16WantModulePrimary(t, m16Status(t, e.get(t, pending), http.StatusOK, "Priya: pending order"), []string{"Withdraw"}, "Priya: pending order")

	// docscreen's own primary: Approve for Ravi (Purchase Manager).
	e.who = e.ravi
	m16WantNoModulePrimary(t, m16Status(t, e.get(t, pending), http.StatusOK, "Ravi: pending order"), []string{"Approve", "Reject"}, "", "Ravi, an approver")

	// docscreen's note: APR blocks Ravi from approving.
	e.apr.block = &approval.UserError{Kind: approval.ErrSelfApproval, Msg: m16Blocked}
	m16WantNoModulePrimary(t, m16Status(t, e.get(t, pending), http.StatusOK, "Ravi blocked: pending order"), nil, m16Blocked, "Ravi, blocked by APR")
	e.apr.block = nil

	// A Primary without Href is a 500.
	e.who = e.priya
	primary = &components.ButtonProps{Label: m16SendLabel, Variant: components.Primary}
	if rec := e.get(t, approved); rec.Code != http.StatusInternalServerError {
		t.Errorf("GET approved order with a Primary without Href = %d, want 500", rec.Code)
	}
	primary = &components.ButtonProps{Label: m16SendLabel, Href: m16SendHref, Variant: components.Primary}

	fail = errM16Hook
	if rec := e.get(t, approved); rec.Code != http.StatusInternalServerError {
		t.Errorf("GET document page with a failing Page = %d, want 500", rec.Code)
	}
	fail = nil

	e.who = e.hidden
	calls = m16Calls{}
	m04Same404(t, e.get(t, approved), e.get(t, m15DocURL(fixture.Order, e.pending+1000)), "PRJ-TECHPARK user GET document page")
	if calls.n != 0 {
		t.Errorf("Page was called %d times for a user who can't see the order, want 0", calls.n)
	}
}

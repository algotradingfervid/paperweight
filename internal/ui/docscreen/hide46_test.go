package docscreen_test

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
)

// M0-46 (CD-319): Module.Hide on the redraw paths that missed it: the line
// page's error summary, the refusal page of a user without write, and the
// default line card. Every test runs on the real engine and the real AUTH
// service on a fixture database.

const (
	m46Unit    = "Bags"
	m46UnitMsg = "Line 1: Unit is not stocked."
)

// m46Mount is m01bMount keeping the probe on every request.
func m46Mount(e *hookedEnv, dt string, m docscreen.Module) {
	m01bMount(e, dt, m)
	router := e.h
	e.h = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		router.ServeHTTP(w, r.WithContext(fixture.WithProbe(r.Context(), e.probe)))
	})
}

// m46Order is the Hooked harness with the Hooker role given read, create,
// write, submit, approve, cancel and delete on fixture.Order, and one Order draft by the open user:
// SHREE, 24-10-2026, line 1 CEMENT-OPC 10 Bags at 350. fixture.Order gets m.
func m46Order(t *testing.T, m docscreen.Module) (*hookedEnv, int64) {
	t.Helper()
	e := newHookedEnv(t)
	rs := map[auth.Right]auth.Scope{"read": auth.ScopeAll, "create": auth.ScopeAll, "write": auth.ScopeAll, "submit": auth.ScopeAll, "approve": auth.ScopeAll, "cancel": auth.ScopeAll, "delete": auth.ScopeAll}
	if err := m04Auth(t, e).SetRights(t.Context(), e.fx.Admin, "Hooker", fixture.Order, rs); err != nil {
		t.Fatalf("SetRights(Hooker, %s): %v", fixture.Order, err)
	}
	e.fx.Naming.Prefix[fixture.Order] = "FO"
	fm := e.fx.M
	res, err := e.engine.Do(t.Context(), e.open, fixture.Order, 0, doc.ActionSave, doc.ActionInput{
		Values: map[string]string{"supplier_id": itoa(fm.Party["SHREE"]), "order_date": "2026-10-24"},
		Lines:  []map[string]string{{"item_id": itoa(fm.Item["CEMENT-OPC"]), "qty": "10", "rate": "350", "uom": m46Unit}},
	})
	if err != nil {
		t.Fatalf("saving an Order draft: %v", err)
	}
	m46Mount(e, fixture.Order, m)
	return e, res.ID
}

func m46URL(id int64) string { return "/doc/" + fixture.Order + "/" + itoa(id) }

// (1) lineShell: an error on a hidden line field is a plain message in the
// line page's summary, with no link to an input that isn't there.
func TestHide46_LinePageSummary(t *testing.T) {
	e, id := m46Order(t, docscreen.Module{Hide: m01bHide("uom")})
	e.probe.ValidateErrors = []doctype.FieldError{{Field: "uom", Line: 1, Message: m46UnitMsg}}
	form := url.Values{
		"version": {m04Version(t, e, fixture.Order, id)}, "item_id": {itoa(e.fx.M.Item["CEMENT-OPC"])}, "qty": {"12"}, "rate": {"350"},
	}
	rec := e.post(t, m46URL(id)+"/lines/1", form)
	page := m04Page(t, rec, http.StatusUnprocessableEntity, "line post refused on the hidden unit")
	if got := norm(page.Find(".error-summary").Text()); !strings.Contains(got, m46UnitMsg) {
		t.Errorf("error summary = %q, want %q", got, m46UnitMsg)
	}
	if links := summaryLinks(page); slices.Contains(links, "#"+components.FieldID("uom")) {
		t.Errorf("error summary links %q, want no link to the hidden unit", links)
	}
	if n := m01bNamed(page, "uom"); n != 0 || m01bNamed(page, "qty") == 0 {
		t.Errorf("the line page renders %d unit inputs, want none beside qty", n)
	}
	wantNoDeadLinks(t, rec)
}

// (2) refused: a user holding read and submit but not write, whose submit
// DOC refuses on a hidden Required field, gets the document page with the
// error as a plain message (real AUTH; the rights-test rule).
func TestHide46_RefusedWithoutWrite(t *testing.T) {
	e, id := m01bNoSupplier(t)
	m01bMount(e, docscreentest.Hooked, docscreen.Module{Hide: m01bHide("supplier_id")})
	e.who = m04User(t, e, "m46-submitter@vardhan.test", "M46Submitter", map[string][]auth.Right{docscreentest.Hooked: {"read", "submit"}}, 0)
	rec := e.post(t, m01bURL(id)+"/submit", url.Values{"version": {m01bVersion(t, e, id)}})
	page := m04Page(t, rec, http.StatusUnprocessableEntity, "submit by a user without write")
	if page.Find("#wizard-form").Length() != 0 {
		t.Fatalf("a user without write got the step form, want the document page")
	}
	if got := norm(page.Find(".error-summary").Text()); !strings.Contains(got, "Supplier is required.") {
		t.Errorf("error summary = %q, want Supplier is required.", got)
	}
	if links := summaryLinks(page); slices.Contains(links, "#"+components.FieldID("supplier_id")) {
		t.Errorf("error summary links %q, want no link to the hidden supplier", links)
	}
	wantNoDeadLinks(t, rec)
}

// (3) the default line card leaves a hidden field out: with uom hidden the
// card is "{qty} × {rate}", on the Items step, the check page and the
// document page. Without the module it shows the unit (the control).
func TestHide46_DefaultCard(t *testing.T) {
	card := func(t *testing.T, e *hookedEnv, path string) string {
		t.Helper()
		page := m01bOK(t, e.get(t, path), "GET "+path)
		cards := page.Find(".lines-card")
		if cards.Length() != 1 {
			t.Fatalf("GET %s has %d line cards, want 1", path, cards.Length())
		}
		return norm(cards.Find(".lines-meta").Text())
	}
	e, id := m46Order(t, docscreen.Module{})
	if got := card(t, e, m46URL(id)+"/edit/items"); !strings.Contains(got, m46Unit) {
		t.Fatalf("with no module the card = %q, want the unit %s", got, m46Unit)
	}

	e, id = m46Order(t, docscreen.Module{Hide: m01bHide("uom")})
	v, err := e.d.View(t.Context(), e.open, fixture.Order, id)
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	want := v.Lines[0].Inputs["qty"] + " × " + v.Lines[0].Display["rate"]
	for _, path := range []string{"/edit/items", "/edit/check"} {
		if got := card(t, e, m46URL(id)+path); got != want {
			t.Errorf("GET %s card = %q, want %q", path, got, want)
		}
	}
	res, err := e.engine.Do(t.Context(), e.open, fixture.Order, id, doc.ActionSubmit, doc.ActionInput{Version: v.Version})
	if err != nil {
		t.Fatalf("submitting the Order: %v", err)
	}
	if got := card(t, e, m46URL(id)); strings.Contains(got, m46Unit) || !strings.HasPrefix(got, want) {
		t.Errorf("document page (%s) card = %q, want %q without %s", res.State, got, want, m46Unit)
	}
}

// reasonViewWith: a cancel refused with an error on a hidden line field (a
// module's ReverseEffects) shows it on the cancel page as a plain message.
func TestHide46_ReasonPage(t *testing.T) {
	e, id := m46Order(t, docscreen.Module{Hide: m01bHide("uom")})
	if _, err := e.engine.Do(t.Context(), e.open, fixture.Order, id, doc.ActionSubmit, doc.ActionInput{Version: e.fx.Record(t, fixture.Order, id).Version}); err != nil {
		t.Fatalf("submitting the Order: %v", err)
	}
	e.probe.Fail = map[string]error{"ReverseEffects": &doc.Error{
		Kind: doc.KindInvalid, Message: "Fix the lines.", Fields: []doctype.FieldError{{Field: "uom", Line: 1, Message: m46UnitMsg}},
	}}
	rec := e.post(t, m46URL(id)+"/cancel", url.Values{"version": {m04Version(t, e, fixture.Order, id)}, "reason": {"Ordered twice"}})
	page := m04Page(t, rec, http.StatusUnprocessableEntity, "cancel refused on the hidden unit")
	if got := norm(page.Find(".error-summary").Text()); !strings.Contains(got, m46UnitMsg) {
		t.Errorf("error summary = %q, want %q", got, m46UnitMsg)
	}
	if links := summaryLinks(page); slices.Contains(links, "#"+docscreen.LineFieldID(1, "uom")) || slices.Contains(links, "#"+components.FieldID("uom")) {
		t.Errorf("error summary links %q, want no link to the hidden unit", links)
	}
	wantNoDeadLinks(t, rec)
}

// gridRefused: a grid row save refused with an error on a hidden line field
// says so in the grid's alert; no cell can show it.
func TestHide46_GridAlert(t *testing.T) {
	// Hiding uom and description leaves 3 cells, which the grid lays out.
	e, id := m46Order(t, docscreen.Module{Hide: m01bHide("uom", "description")})
	e.probe.ValidateErrors = []doctype.FieldError{{Field: "uom", Line: 1, Message: m46UnitMsg}}
	form := url.Values{
		"version": {m04Version(t, e, fixture.Order, id)}, "item_id": {itoa(e.fx.M.Item["CEMENT-OPC"])}, "qty": {"12"}, "rate": {"350"},
	}
	rec := e.post(t, m46URL(id)+"/lines/1", form, "HX-Request", "true")
	page := m04Page(t, rec, http.StatusUnprocessableEntity, "grid row save refused on the hidden unit")
	if page.Find("#grid-row-1").Length() != 1 {
		t.Fatalf("the refusal is not the grid's row answer: %.400s", rec.Body)
	}
	if got := norm(page.Text()); !strings.Contains(got, m46UnitMsg) {
		t.Errorf("grid answer = %q, want %q", got, m46UnitMsg)
	}
}

// defaultCard: Hooked's first Link is the project, which the default card
// names as the item; with project_id hidden the card names no project, and
// hiding the second Link (item_id) leaves the project's name.
func TestHide46_DefaultCardItem(t *testing.T) {
	name := func(t *testing.T, e *hookedEnv, id int64) string {
		t.Helper()
		page := m01bOK(t, e.get(t, m01bURL(id)+"/edit/check"), "GET check")
		return norm(page.Find(".lines-card .lines-name").First().Text())
	}
	e, id := m01bHooked(t, docscreen.Module{})
	if got := name(t, e, id); !strings.Contains(got, "PRJ-METRO") {
		t.Fatalf("with no module line 1's card names %q, want the project PRJ-METRO", got)
	}
	e, id = m01bHooked(t, docscreen.Module{Hide: m01bHide("project_id")})
	if got := name(t, e, id); got != "" {
		t.Errorf("with project_id hidden line 1's card names %q, want nothing", got)
	}
	e, id = m01bHooked(t, docscreen.Module{Hide: m01bHide("item_id")})
	if got := name(t, e, id); !strings.Contains(got, "PRJ-METRO") {
		t.Errorf("with item_id hidden line 1's card names %q, want the project PRJ-METRO", got)
	}
}

// applyLineCards: Hide sees the line's stored inputs in Deps for the default
// card too, as on the line page and the grid (CD-256 (10)). This Hide hides
// uom only when Deps labels the line's stored item.
func TestHide46_DefaultCardLineDeps(t *testing.T) {
	order, _ := doctype.Lookup(fixture.Order)
	itemField, _ := order.Field("item_id")
	hide := func(c docscreen.Ctx, f doctype.Field) bool {
		if f.Name != "uom" || c.View == nil || len(c.View.Lines) == 0 {
			return false
		}
		var b strings.Builder
		if err := docscreen.FieldInput(itemField, "m46-probe", c.View.Lines[0].Inputs["item_id"], "", c.Deps).Render(c.Req.Context(), &b); err != nil {
			t.Errorf("FieldInput: %v", err)
		}
		return strings.Contains(b.String(), "CEMENT-OPC")
	}
	e, id := m46Order(t, docscreen.Module{Hide: hide})
	page := m01bOK(t, e.get(t, m46URL(id)+"/edit/check"), "GET check")
	if got := norm(page.Find(".lines-card .lines-meta").Text()); strings.Contains(got, m46Unit) {
		t.Errorf("check page card = %q, want no %s: Hide's Deps must carry the line's stored inputs", got, m46Unit)
	}
}

// m46Doc saves an Order draft by the open user (SHREE, CEMENT-OPC 1 at 350,
// amount amount) and, with submit, submits it.
func m46Doc(t *testing.T, e *hookedEnv, amount string, submit bool) int64 {
	t.Helper()
	fm := e.fx.M
	res, err := e.engine.Do(t.Context(), e.open, fixture.Order, 0, doc.ActionSave, doc.ActionInput{
		Values: map[string]string{"supplier_id": itoa(fm.Party["SHREE"]), "order_date": "2026-10-24", "amount": amount},
		Lines:  []map[string]string{{"item_id": itoa(fm.Item["CEMENT-OPC"]), "qty": "1", "rate": "350"}},
	})
	if err != nil {
		t.Fatalf("saving an Order draft: %v", err)
	}
	if submit {
		if res, err = e.engine.Do(t.Context(), e.open, fixture.Order, res.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version}); err != nil {
			t.Fatalf("submitting Order %d: %v", res.ID, err)
		}
	}
	return res.ID
}

// (4) Info.Party: with the party field (supplier_id) hidden, the document
// page and every reason and confirm page Screens lists leave the party out.
// Without the module each shows it (the control). Rejected, cancelled and
// closed reuse the approved Order: the party shows the same way there.
func TestHide46_PartyHidden(t *testing.T) {
	e, draft := m46Order(t, docscreen.Module{})
	pending, approved := m46Doc(t, e, "60000", true), m46Doc(t, e, "0", true)
	v, err := e.d.View(t.Context(), e.open, fixture.Order, approved)
	if err != nil || v.Info.Party == "" {
		t.Fatalf("View: party %q, err %v; want SHREE's party", v.Info.Party, err)
	}
	party := v.Info.Party
	if pv, _ := e.d.View(t.Context(), e.open, fixture.Order, pending); pv.Info.State != doctype.StatePending {
		t.Fatalf("the 60000 Order is %s, want pending", pv.Info.State)
	}
	ids := docscreen.SampleIDs{Draft: draft, Pending: pending, Approved: approved, Rejected: approved, Cancelled: approved, Closed: approved, Lines500: draft}
	var urls []string
	kinds := map[string]bool{}
	for _, s := range docscreen.Screens(fixture.Order, ids) {
		if s.Route == "/doc/{doctype}/{id}" || s.Route == "/doc/{doctype}/{id}/{action}" {
			urls = append(urls, s.URL)
			kinds[s.URL[strings.LastIndex(s.URL, "/")+1:]] = true
		}
	}
	for _, k := range []string{"reject", "cancel", "close", "delete"} {
		if !kinds[k] {
			t.Fatalf("Screens lists no %s page for fixture.Order: %q", k, urls)
		}
	}
	for _, c := range []struct {
		m     docscreen.Module
		shown bool
	}{{docscreen.Module{}, true}, {docscreen.Module{Hide: m01bHide("supplier_id")}, false}} {
		m46Mount(e, fixture.Order, c.m)
		for _, u := range urls {
			got := norm(m01bOK(t, e.get(t, u), "GET "+u).Find("#main").Text())
			if strings.Contains(got, party) != c.shown {
				t.Errorf("GET %s with the party shown %v: #main = %.300q, want %q %v", u, c.shown, got, party, c.shown)
			}
		}
	}
	// A master without a code: DOC's party is its name alone.
	e.fx.Exec(t, `UPDATE fixture_party SET code = '' WHERE id = ?`, e.fx.M.Party["SHREE"])
	if v, err = e.d.View(t.Context(), e.open, fixture.Order, approved); err != nil || v.Info.Party != "Shree Steel Traders" {
		t.Fatalf("View with SHREE's code blank: party %q, err %v; want the name alone", v.Info.Party, err)
	}
	for _, u := range []string{m46URL(approved), m46URL(approved) + "/cancel"} {
		if got := norm(m01bOK(t, e.get(t, u), "GET "+u).Find("#main").Text()); strings.Contains(got, v.Info.Party) {
			t.Errorf("GET %s with a code-less hidden party: #main = %.300q, want no %q", u, got, v.Info.Party)
		}
	}
}

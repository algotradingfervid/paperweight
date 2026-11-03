package docscreen_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// M0-47 (CD-319 (6)-(9)), on the real engine and the real AUTH service: the
// party is hidden by its field (kernel/doc PartyField), never by its value;
// the list and search pages leave it out too; a hidden PFigure Money field
// takes the grand total and its words off the document, check and reason
// pages; and Module.LineCard is handed the hidden-filtered default card.

// m47Two is a test DocType with two header Links to fixture.Party: bill_to_id
// first and optional, supplier_id Required, so supplier_id is DOC's party.
const (
	m47Two    = "docscreentest.M47Two"
	m47TwoRef = "m47-ref"
)

var m47TwoOnce sync.Once

// m47Lister is the app's Lister (internal/app uiadapters.go): the real
// list.Service, each row's Info from the engine.
type m47Lister struct {
	svc    *list.Service
	db     *db.DB
	engine doc.Engine
}

func (l m47Lister) List(ctx context.Context, p auth.Principal, dt *doctype.DocType, q docscreen.ListQuery) (types.Result[docscreen.ListRow], error) {
	res, err := l.svc.List(ctx, p, dt, list.Query{Search: q.Search, Page: max(q.Page, 1), PageSize: max(q.Size, 25)})
	if err != nil {
		return types.Result[docscreen.ListRow]{}, err
	}
	out := types.Result[docscreen.ListRow]{Total: res.Total}
	err = l.db.Read(ctx, func(tx *db.Tx) error {
		for _, r := range res.Rows {
			info, err := l.engine.Info(ctx, tx, dt.Name, r.ID)
			if err != nil {
				return err
			}
			out.Rows = append(out.Rows, docscreen.ListRow{Info: info, Values: map[string]string{}})
		}
		return nil
	})
	return out, err
}

// m47TwoEnv is the Hooked harness with the Hooker role given read, create,
// write, submit and delete on m47Two, the real list service as Lister, and
// two m47Two drafts by the open user with reference m47-ref: same (bill to
// and supplier both SHREE) and diff (bill to RAJKOT, supplier SHREE).
func m47TwoEnv(t *testing.T) (e *hookedEnv, same, diff int64) {
	t.Helper()
	m47TwoOnce.Do(func() {
		doctype.Register(doctype.DocType{
			Name: m47Two, Label: "Two-party Order", PluralLabel: "Two-party Orders", Table: "m47_two", LinesTable: "m47_two_line",
			Submittable: true, Hooks: scopeHooks{}, SearchFields: []string{"ref"},
			Fields: []doctype.Field{
				{Name: "bill_to_id", Label: "Bill to", Type: doctype.Link, Link: fixture.Party},
				{Name: "supplier_id", Label: "Supplier", Type: doctype.Link, Link: fixture.Party, Required: true},
				{Name: "ref", Label: "Reference", Type: doctype.Text, MaxLen: 40},
				{Name: "item_id", Label: "Item", Type: doctype.Link, Link: fixture.Item, Required: true, OnLine: true},
				{Name: "qty", Label: "Quantity", Type: doctype.Qty, Positive: true, OnLine: true},
			},
		})
	})
	e = newHookedEnv(t)
	e.fx.Exec(t, `CREATE TABLE m47_two (id INTEGER PRIMARY KEY AUTOINCREMENT, number TEXT UNIQUE, state TEXT NOT NULL DEFAULT 'draft',
		state_reason TEXT NOT NULL DEFAULT '', amended_from_id INTEGER, amendment_root_id INTEGER, amendment_no INTEGER NOT NULL DEFAULT 0,
		version INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL, created_by INTEGER NOT NULL, updated_at TEXT NOT NULL, updated_by INTEGER NOT NULL,
		bill_to_id INTEGER, supplier_id INTEGER, ref TEXT NOT NULL DEFAULT '')`)
	e.fx.Exec(t, `CREATE TABLE m47_two_line (id INTEGER PRIMARY KEY AUTOINCREMENT, parent_id INTEGER NOT NULL, line_no INTEGER NOT NULL,
		item_id INTEGER, qty INTEGER)`)
	e.fx.Naming.Prefix[m47Two] = "TP"
	rs := map[auth.Right]auth.Scope{"read": auth.ScopeAll, "create": auth.ScopeAll, "write": auth.ScopeAll, "submit": auth.ScopeAll, "delete": auth.ScopeAll}
	a := m04Auth(t, e)
	if err := a.SetRights(t.Context(), e.fx.Admin, "Hooker", m47Two, rs); err != nil {
		t.Fatalf("SetRights(Hooker, %s): %v", m47Two, err)
	}
	p, _, err := a.LoadPrincipal(t.Context(), e.open.ID)
	if err != nil {
		t.Fatalf("LoadPrincipal: %v", err)
	}
	e.open, e.who = p, p
	e.d.Lister = m47Lister{svc: &list.Service{DB: e.fx.DB, Auth: a}, db: e.fx.DB, engine: e.engine}
	fm := e.fx.M
	save := func(billTo string) int64 {
		res, err := e.engine.Do(t.Context(), p, m47Two, 0, doc.ActionSave, doc.ActionInput{
			Values: map[string]string{"bill_to_id": itoa(fm.Party[billTo]), "supplier_id": itoa(fm.Party["SHREE"]), "ref": m47TwoRef},
			Lines:  []map[string]string{{"item_id": itoa(fm.Item["CEMENT-OPC"]), "qty": "10"}},
		})
		if err != nil {
			t.Fatalf("saving an m47Two draft: %v", err)
		}
		return res.ID
	}
	return e, save("SHREE"), save("RAJKOT")
}

func m47TwoURL(id int64) string { return "/doc/" + m47Two + "/" + itoa(id) }

// m47Texts is the normalised text of each element sel finds in page.
func m47Texts(page *goquery.Document, sel string) []string {
	var out []string
	page.Find(sel).Each(func(_ int, s *goquery.Selection) { out = append(out, norm(s.Text())) })
	return out
}

func m47Has(texts []string, s string) bool {
	for _, x := range texts {
		if strings.Contains(x, s) {
			return true
		}
	}
	return false
}

// (6), (7): with the non-party bill_to_id hidden and holding the same record
// as the party field, the party still shows on the document page, the delete
// page's summary, the list page and the search page; matching the hidden
// field's value would leave it out. With the party field hidden it shows on
// none of them, though the shown bill_to_id holds another party. With no
// module it shows everywhere (the control).
func TestHide47_PartyByField(t *testing.T) {
	e, same, diff := m47TwoEnv(t)
	v, err := e.d.View(t.Context(), e.open, m47Two, same)
	if err != nil || v.Info.Party == "" || v.Display["bill_to_id"] != v.Info.Party {
		t.Fatalf("View: party %q, bill to %q, err %v; want SHREE's party in both", v.Info.Party, v.Display["bill_to_id"], err)
	}
	party := v.Info.Party
	reader := m04User(t, e, "m47-reader@vardhan.test", "M47Reader", map[string][]auth.Right{m47Two: {"read"}}, 0)
	// pages is where id's party shows: each page's party elements. A
	// draft's document page is the reader's: its writer goes to the check page.
	pages := func(t *testing.T, id int64) map[string][]string {
		t.Helper()
		e.who = reader
		out := map[string][]string{m47TwoURL(id): m47Texts(m01bOK(t, e.get(t, m47TwoURL(id)), "reader GET the draft"), ".doc-main > p")}
		e.who = e.open
		for u, sel := range map[string]string{
			m47TwoURL(id) + "/delete":            "#main p",
			"/doc/" + m47Two:                     ".datalist-party",
			"/search?q=" + m47TwoRef:             ".datalist-party",
			"/doc/" + m47Two + "?q=" + m47TwoRef: ".datalist-party",
		} {
			out[u] = m47Texts(m01bOK(t, e.get(t, u), "GET "+u), sel)
		}
		return out
	}
	for _, c := range []struct {
		name  string
		m     docscreen.Module
		id    int64
		shown bool
	}{
		{"no module", docscreen.Module{}, same, true},
		{"bill to hidden, same party", docscreen.Module{Hide: m01bHide("bill_to_id")}, same, true},
		{"supplier hidden", docscreen.Module{Hide: m01bHide("supplier_id")}, diff, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			m46Mount(e, m47Two, c.m)
			for u, texts := range pages(t, c.id) {
				if m47Has(texts, party) != c.shown {
					t.Errorf("GET %s: party elements %q, want %q shown %v", u, texts, party, c.shown)
				}
			}
			if !c.shown {
				u := m47TwoURL(c.id)
				e.who = reader
				defer func() { e.who = e.open }()
				if got := norm(m01bOK(t, e.get(t, u), "GET "+u).Find("#main").Text()); strings.Contains(got, party) {
					t.Errorf("GET %s with the party field hidden: #main = %.300q, want no %q", u, got, party)
				}
			}
		})
	}
}

// (8): with fixture.Order's PFigure Money field (amount) hidden, the grand
// total and its words are on none of the document page, the check page and
// the reason pages' summary; nothing replaces them. With no module, or
// other header fields hidden, each shows them (the controls): the total and
// words on the document and check pages, the total in a reason summary. The
// draft's document page is a reader's.
func TestHide47_GrandTotalHidden(t *testing.T) {
	e, _ := m46Order(t, docscreen.Module{})
	reader := m04User(t, e, "m47-order-reader@vardhan.test", "M47OrderReader", map[string][]auth.Right{fixture.Order: {"read"}}, 0)
	draft, pending := m46Doc(t, e, "60000", false), m46Doc(t, e, "60000", true)
	v, err := e.d.View(t.Context(), e.open, fixture.Order, pending)
	if err != nil || v.GrandTotal == "" || v.InWords == "" || v.Info.State != doctype.StatePending {
		t.Fatalf("View: total %q, words %q, state %s, err %v; want a pending Order with both", v.GrandTotal, v.InWords, v.Info.State, err)
	}
	both, total := []string{v.GrandTotal, v.InWords}, []string{v.GrandTotal}
	pages := []struct {
		url  string
		want []string
	}{
		{m46URL(draft), both}, {m46URL(draft) + "/edit/check", both}, {m46URL(pending), both},
		{m46URL(draft) + "/delete", total}, {m46URL(pending) + "/reject", total},
	}
	for _, c := range []struct {
		m     docscreen.Module
		shown bool
	}{
		{docscreen.Module{}, true},
		{docscreen.Module{Hide: m01bHide("supplier_id", "remarks")}, true}, // only the figure field takes them off
		{docscreen.Module{Hide: m01bHide("amount")}, false},
	} {
		m46Mount(e, fixture.Order, c.m)
		for i, p := range pages {
			u := p.url
			e.who = map[bool]auth.Principal{true: reader, false: e.open}[i == 0]
			got := norm(m01bOK(t, e.get(t, u), "GET "+u).Find("#main").Text())
			for _, s := range p.want {
				if strings.Contains(got, s) != c.shown {
					t.Errorf("GET %s with the amount shown %v: #main = %.400q, want %q %v", u, c.shown, got, s, c.shown)
				}
			}
		}
	}
}

// m47CardNote marks the note m47CardModule's LineCard writes.
const m47CardNote = "m47-card"

// m47CardModule hides fixture.Order's item and unit; its LineCard reads the
// card it is given into the note, and LineGrid draws the display grid, which
// shows that note.
func m47CardModule() docscreen.Module {
	return docscreen.Module{
		Hide: m01bHide("item_id", "uom"),
		LineCard: func(_ docscreen.Ctx, l docscreen.LineView) components.LineCard {
			c := l.Card
			c.Note = m47CardNote + " " + c.QtyRate + " | " + c.Item + " | " + c.Code
			return c
		},
		LineGrid: func(_ docscreen.Ctx, l docscreen.LineView) []components.GridCell {
			return []components.GridCell{
				{Label: "Line", Text: "m47-line"},
				{Label: "Quantity", Text: l.Display["qty"], Kind: "qty"},
				{Label: "Rate", Text: l.Display["rate"], Kind: "rate"},
			}
		},
	}
}

// (9): Module.LineCard is handed the default card without the hidden item
// and unit, so the cards and the display grid on the Items step, the check
// page and the document page, and the remove page's question, never name
// them.
func TestHide47_ModuleCardFiltered(t *testing.T) {
	e, id := m46Order(t, m47CardModule())
	v, err := e.d.View(t.Context(), e.open, fixture.Order, id)
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	item, code := v.Lines[0].Card.Item, v.Lines[0].Card.Code
	if item == "" || code == "" {
		t.Fatalf("View's line 1 card names item %q, code %q; want CEMENT-OPC's", item, code)
	}
	absent := []string{m46Unit, item, code}
	check := func(t *testing.T, page *goquery.Document, what string, grid bool) {
		t.Helper()
		cards := m47Texts(page, ".lines-card")
		if len(cards) != 1 || !strings.Contains(cards[0], m47CardNote) {
			t.Fatalf("%s: line cards %q, want one with the module's note", what, cards)
		}
		texts := map[string]string{"line card": cards[0]}
		if grid {
			g := page.Find(".line-editor")
			if g.Length() != 1 || !strings.Contains(norm(g.Text()), m47CardNote) {
				t.Fatalf("%s: %d display grids, want one with the module's note", what, g.Length())
			}
			texts["display grid"] = norm(g.Text())
		}
		for where, got := range texts {
			for _, s := range absent {
				if strings.Contains(got, s) {
					t.Errorf("%s: the %s %q names the hidden %q", what, where, got, s)
				}
			}
		}
	}
	for _, path := range []string{"/edit/items", "/edit/check"} {
		check(t, m01bOK(t, e.get(t, m46URL(id)+path), "GET "+path), path, true)
	}
	page := m01bOK(t, e.get(t, m46URL(id)+"/lines/1/remove"), "GET remove")
	if q := m47Texts(page, "#main p"); !m47Has(q, "Remove line 1?") || m47Has(q, item) {
		t.Errorf("remove page paragraphs %q, want the question Remove line 1? without %q", q, item)
	}
	if _, err := e.engine.Do(t.Context(), e.open, fixture.Order, id, doc.ActionSubmit, doc.ActionInput{Version: v.Version}); err != nil {
		t.Fatalf("submitting the Order: %v", err)
	}
	check(t, m01bOK(t, e.get(t, m46URL(id)), "GET the document"), "document page", true)
}

// CD-327 (1): Hide sees Ctx.Listing true on the list and search pages, with
// View nil, and false on /new, the steps, the document page and the reason
// pages.
func TestHide47_Listing(t *testing.T) {
	e, id, _ := m47TwoEnv(t)
	reader := m04User(t, e, "m47-listing-reader@vardhan.test", "M47ListingReader", map[string][]auth.Right{m47Two: {"read"}}, 0)
	var mu sync.Mutex
	var seen []bool // Listing on each Hide call of the current request
	m46Mount(e, m47Two, docscreen.Module{Hide: func(c docscreen.Ctx, _ doctype.Field) bool {
		mu.Lock()
		defer mu.Unlock()
		if c.Listing && c.View != nil {
			t.Errorf("%s: Hide with Listing true has a View", c.Req.URL)
		}
		seen = append(seen, c.Listing)
		return false
	}})
	for _, c := range []struct {
		url     string
		reader  bool
		listing bool
	}{
		{"/doc/" + m47Two, false, true},
		{"/doc/" + m47Two + "?q=" + m47TwoRef, false, true},
		{"/search?q=" + m47TwoRef, false, true},
		{"/doc/" + m47Two + "/new", false, false},
		{m47TwoURL(id) + "/edit/details", false, false},
		{m47TwoURL(id) + "/edit/items", false, false},
		{m47TwoURL(id) + "/edit/check", false, false},
		{m47TwoURL(id) + "/lines/1", false, false},
		{m47TwoURL(id) + "/delete", false, false},
		{m47TwoURL(id), true, false},
	} {
		mu.Lock()
		seen = nil
		mu.Unlock()
		e.who = map[bool]auth.Principal{true: reader, false: e.open}[c.reader]
		m01bOK(t, e.get(t, c.url), "GET "+c.url)
		mu.Lock()
		if len(seen) == 0 {
			t.Errorf("GET %s: Hide was never asked", c.url)
		}
		for _, l := range seen {
			if l != c.listing {
				t.Errorf("GET %s: Hide saw Listing %v, want %v", c.url, l, c.listing)
				break
			}
		}
		mu.Unlock()
	}
	e.who = e.open
}

// Count is List's Total.
func (l m47Lister) Count(ctx context.Context, p auth.Principal, dt *doctype.DocType, q docscreen.ListQuery) (int, error) {
	res, err := l.List(ctx, p, dt, q)
	return res.Total, err
}

package purchaseweb

import (
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/types"
)

// p15cOpen GETs ?from=requests on draft id as p and returns the page and the
// card titled card.
func (w *p15cWeb) open(t *testing.T, p auth.Principal, id int64, card string) (p15cPage, []p15cRow) {
	t.Helper()
	page := w.get(t, p, p15cFrom(id))
	if page.status != 200 {
		t.Fatalf("%s GET ?from=requests = %d: %s", p.Name, page.status, page.text())
	}
	c := p15cCard(page, card)
	if c.Length() == 0 {
		t.Fatalf("no card %s on Add from requests: %s", card, page.text())
	}
	return page, p15cRows(t, c)
}

// PUX-012's Add selected, done and undone (PO spec §20.4 item 2; UX §5.7;
// CD-272 do and undo; AD-13 brief): on Priya's SHREE draft (one line), she
// ticks CEMENT-OPC with 30 of its 40 and BOLT-M12's 2 BOX (not its stock
// unit, NOS), leaving WIRE-18G. Add selected saves with nav=stay, returning
// (303) to the bare Items URL. The draft keeps line 1 and gains, in order,
// CEMENT-OPC 30 BAG at SHREE's last price on PRJ-METRO (₹345, 5% off: the
// discount is posted blank so lineDefaults applies it) and BOLT-M12 2 BOX,
// each with its request_line_id. The link then reads (2 open); removing
// the BOLT-M12 line brings back (3 open).
func TestPUX012_AddSelectedAppends(t *testing.T) {
	w := newP15cWeb(t)
	env := w.env
	purchasetest.NewOrder(t, env, purchasetest.OrderLines(map[string]string{
		"item_id": w.id(t, "item", "CEMENT-OPC"), "uom_id": w.id(t, "uom", "BAG"), "hsn_sac": "2523",
		"qty": "10", "price": "345", "discount_pct": "5",
	})) // approved: SHREE's last CEMENT-OPC price on PRJ-METRO
	pr := w.prA(t, w.row(t, "BOLT-M12", "BOX", "2"))
	d := w.draft(t)
	before := w.lines(t, d.ID)

	page, rows := w.open(t, env.Priya, d.ID, pr.Number)
	cement, bolt := p15cFind(t, rows, "CEMENT-OPC"), p15cFind(t, rows, "BOLT-M12")
	action, form := p15cForm(t, page, p15cPrimary, []p15cRow{cement, bolt}, map[*goquery.Selection]string{cement.qty: "30"})
	a := w.do(t, env.Priya, "POST", action, form)
	if a.status != 303 || a.location != p15cItems(d.ID) {
		t.Fatalf("Add selected = %d to %q, want 303 to %s (nav=stay): %s", a.status, a.location, p15cItems(d.ID), a.text())
	}

	got := w.lines(t, d.ID)
	if len(got) != 3 || got[0] != before[0] {
		t.Fatalf("lines after Add selected = %+v, want line 1 unchanged plus two", got)
	}
	want := []struct {
		item, uom, qty string
		row            int64
	}{
		{"CEMENT-OPC", "BAG", "30", pr.Lines[0].ID},
		{"BOLT-M12", "BOX", "2", pr.Lines[2].ID},
	}
	for i, c := range want {
		l := got[i+1]
		if l.item != p15cInt(t, w.id(t, "item", c.item)) || l.uom != p15cInt(t, w.id(t, "uom", c.uom)) || l.qty != p15cQty(t, c.qty) ||
			!l.rqLineID.Valid || l.rqLineID.Int64 != c.row {
			t.Errorf("line %d = %+v, want %s %s %s on request line %d", l.no, l, c.qty, c.uom, c.item, c.row)
		}
	}
	if l := got[1]; l.price.Int64 != p15cRate(t, "345") || l.discount.Int64 != p15cPct(t, "5") {
		t.Errorf("CEMENT-OPC line price %v discount %v, want SHREE's last price 345 less 5%%", l.price, l.discount)
	}

	items := w.get(t, env.Priya, p15cItems(d.ID))
	if len(items.link(p15cLink(2))) != 1 {
		t.Errorf("Items after adding: %s, want %q (CEMENT-OPC 10 and WIRE-18G 25 still open)", items.text(), p15cLink(2))
	}
	rm := w.do(t, env.Priya, "POST", p15cItems(d.ID)[:len(p15cItems(d.ID))-len("/edit/items")]+"/lines/3/remove",
		url.Values{"version": {strconv.Itoa(w.version(t, d.ID))}})
	if rm.status != 303 {
		t.Fatalf("remove line 3 = %d: %s", rm.status, rm.text())
	}
	if items := w.get(t, env.Priya, p15cItems(d.ID)); len(items.link(p15cLink(3))) != 1 {
		t.Errorf("Items after removing BOLT-M12: %s, want %q", items.text(), p15cLink(3))
	}
}

// PUX-012's nothing ticked (UX §5.7): Add selected with no tick is 422
// Tick at least one line to add., the view redrawn from the post (a typed
// quantity kept), and nothing written. A generic Items post (Save and
// continue from the plain Items view) returns Save{}: it saves no new line,
// shows no such message and goes on to Check.
func TestPUX012_NothingTicked(t *testing.T) {
	w := newP15cWeb(t)
	env := w.env
	pr := w.prA(t)
	d := w.draft(t)
	before, n := w.lines(t, d.ID), w.lineCount(t)

	page, rows := w.open(t, env.Priya, d.ID, pr.Number)
	cement := p15cFind(t, rows, "CEMENT-OPC")
	action, form := p15cForm(t, page, p15cPrimary, nil, map[*goquery.Selection]string{cement.qty: "35"})
	a := w.do(t, env.Priya, "POST", action, form)
	if a.status != 422 {
		t.Fatalf("Add selected with nothing ticked = %d, want 422: %s", a.status, a.text())
	}
	if got := a.errors(); !slices.Equal(got, []string{p15cNoTick}) {
		t.Errorf("errors = %q, want %q", got, p15cNoTick)
	}
	if !slices.Contains(a.find("h2"), p15cHeading) {
		t.Errorf("422 redraw isn't the Add from requests view: %s", a.text())
	}
	if c := p15cCard(a, pr.Number); c.Length() == 0 {
		t.Errorf("422 redraw has no %s card", pr.Number)
	} else if r := p15cFind(t, p15cRows(t, c), "CEMENT-OPC"); r.value() != "35" {
		t.Errorf("422 redraw CEMENT-OPC quantity = %q, want the typed 35", r.value())
	}
	if got := w.lines(t, d.ID); !slices.Equal(got, before) || w.lineCount(t) != n {
		t.Errorf("lines after a refused Add selected = %+v, want unchanged", got)
	}

	items := w.get(t, env.Priya, p15cItems(d.ID))
	action, form = p15cForm(t, items, "Save and continue", nil, nil)
	g := w.do(t, env.Priya, "POST", action, form)
	if g.status != 303 || !strings.HasSuffix(g.location, "/edit/check") {
		t.Errorf("generic Items post = %d to %q, want 303 to the check step: %s", g.status, g.location, g.text())
	}
	if strings.Contains(g.text(), p15cNoTick) {
		t.Errorf("generic Items post shows %q", p15cNoTick)
	}
	if got := w.lines(t, d.ID); !slices.Equal(got, before) || w.lineCount(t) != n {
		t.Errorf("lines after a generic Items post = %+v, want unchanged", got)
	}
}

// PUX-012's stale page (CD-94, DOC-069; UX §5.7): Priya opens the view, the
// draft is saved elsewhere, and her Add selected is 409 with DOC's stale
// text; the redraw keeps her posted version and her tick, and nothing is
// written.
func TestPUX012_StaleVersion(t *testing.T) {
	w := newP15cWeb(t)
	env := w.env
	pr := w.prA(t)
	d := w.draft(t)
	page, rows := w.open(t, env.Priya, d.ID, pr.Number)
	posted := p15cDocVersion(page)
	terms := w.get(t, env.Priya, p15cItems(d.ID)[:len(p15cItems(d.ID))-len("items")]+"terms")
	taction, tform := p15cForm(t, terms, "Save and continue", nil, nil)
	tform.Set("remarks", "Changed in another tab")
	if a := w.do(t, env.Priya, "POST", taction, tform); a.status != 303 || w.version(t, d.ID) == p15cIntV(t, posted) {
		t.Fatalf("save elsewhere = %d, version still %s: %s", a.status, posted, a.text())
	}
	before, n := w.lines(t, d.ID), w.lineCount(t)

	cement := p15cFind(t, rows, "CEMENT-OPC")
	action, form := p15cForm(t, page, p15cPrimary, []p15cRow{cement}, nil)
	a := w.do(t, env.Priya, "POST", action, form)
	if a.status != 409 {
		t.Fatalf("stale Add selected = %d, want 409: %s", a.status, a.text())
	}
	if want := p15cStale(p15cDisplay(d)); !strings.Contains(a.text(), want) {
		t.Errorf("409 page = %q, want %q", a.text(), want)
	}
	if got := p15cDocVersion(a); got != posted {
		t.Errorf("409 redraw version = %q, want the posted %q (CD-94)", got, posted)
	}
	if c := p15cCard(a, pr.Number); c.Length() == 0 || !p15cFind(t, p15cRows(t, c), "CEMENT-OPC").ticked() {
		t.Errorf("409 redraw lost the CEMENT-OPC tick: %s", a.text())
	}
	if got := w.lines(t, d.ID); !slices.Equal(got, before) || w.lineCount(t) != n {
		t.Errorf("lines after a stale Add selected = %+v, want unchanged", got)
	}
}

// PUX-012 and PUX-013's refusal (PO spec §20.5; UX §5.7; AD-13 brief): Priya
// opens the view, then Arjun's draft takes all 40 BAG of CEMENT-OPC. Her Add
// selected is 422 with checkRequestRows' text naming his draft, as line 2
// (her draft has one line), the tick and quantity redrawn, nothing written.
func TestPUX012_RowTakenMeanwhile(t *testing.T) {
	w := newP15cWeb(t)
	env := w.env
	pr := w.prA(t)
	d := w.draft(t)
	page, rows := w.open(t, env.Priya, d.ID, pr.Number)

	other := w.saveAs(t, env.Arjun, map[string]string{
		"vendor_id": w.id(t, "party", "SHREE"), "site_id": w.id(t, "site", "METRO-SITE"),
		"project_id": w.id(t, "project", "PRJ-METRO"), "order_date": types.Today(env.Clock).ISO(),
	}, map[string]string{
		"item_id": w.id(t, "item", "CEMENT-OPC"), "uom_id": w.id(t, "uom", "BAG"), "qty": "40",
		"request_line_id": strconv.FormatInt(pr.Lines[0].ID, 10),
	})
	before, n := w.lines(t, d.ID), w.lineCount(t)

	cement := p15cFind(t, rows, "CEMENT-OPC")
	action, form := p15cForm(t, page, p15cPrimary, []p15cRow{cement}, nil)
	a := w.do(t, env.Priya, "POST", action, form)
	if a.status != 422 {
		t.Fatalf("Add selected on a taken row = %d, want 422: %s", a.status, a.text())
	}
	want := "Line 2: Request " + pr.Number + " line 1 is already on " + p15cDraftName(other) + "."
	if got := a.errors(); !slices.Contains(got, want) {
		t.Errorf("errors = %q, want %q", got, want)
	}
	if c := p15cCard(a, pr.Number); c.Length() == 0 {
		t.Errorf("422 redraw has no %s card: %s", pr.Number, a.text())
	} else if r := p15cFind(t, p15cRows(t, c), "CEMENT-OPC"); !r.ticked() || r.value() != "40" {
		t.Errorf("422 redraw CEMENT-OPC ticked %v quantity %q, want ticked 40", r.ticked(), r.value())
	}
	if got := w.lines(t, d.ID); !slices.Equal(got, before) || w.lineCount(t) != n {
		t.Errorf("lines after a refused Add selected = %+v, want unchanged", got)
	}
}

// PUX-012's states (UX §5.6; CD-263 G1): the link shows only on a draft's
// Items step for a user who may write it. Details and Terms of that draft
// have none. A pending, approved, rejected or cancelled PO's Items URL (and
// ?from=requests) goes to its document page, which has no link; so does
// Priya's draft for Arjun, who may read but not write it. A draft with no
// site or project has no link.
func TestPUX012_LinkOnlyOnDraftItems(t *testing.T) {
	w := newP15cWeb(t)
	env := w.env
	w.prA(t)
	d := w.draft(t)
	if got := w.get(t, env.Priya, p15cItems(d.ID)).link(p15cLink(2)); len(got) != 1 {
		t.Errorf("Priya's draft Items: no %q", p15cLink(2))
	}
	base := p15cItems(d.ID)[:len(p15cItems(d.ID))-len("items")]
	for _, step := range []string{"details", "terms", "check"} {
		if p := w.get(t, env.Priya, base+step); strings.Contains(p.text(), p15cHeading) {
			t.Errorf("draft %s step shows the link: %s", step, p.text())
		}
	}

	big := purchasetest.OrderLines(map[string]string{"qty": "1000"}) // ₹62,500: Purchase Manager approves
	pending := purchasetest.NewOrder(t, env, purchasetest.StopAtPending(), big)
	approved := purchasetest.NewOrder(t, env)
	rejected := purchasetest.NewOrder(t, env, purchasetest.StopAtPending(), big)
	w.act(t, env.Ravi, purchase.DocType, rejected.ID, doctype.ActionReject, "Rate too high")
	cancelled := purchasetest.NewOrder(t, env)
	w.act(t, env.Ravi, purchase.DocType, cancelled.ID, doctype.ActionCancel, "Vendor can't supply")
	for _, c := range []struct {
		name string
		p    auth.Principal
		id   int64
	}{
		{"pending", env.Priya, pending.ID},
		{"approved", env.Priya, approved.ID},
		{"rejected", env.Priya, rejected.ID},
		{"cancelled", env.Priya, cancelled.ID},
		{"draft for a reader", env.Arjun, d.ID},
	} {
		docPath := "/doc/" + purchase.DocType + "/" + strconv.FormatInt(c.id, 10)
		for _, path := range []string{p15cItems(c.id), p15cFrom(c.id)} {
			if p := w.get(t, c.p, path); p.status != 303 || p.location != docPath {
				t.Errorf("%s: GET %s = %d to %q, want 303 to %s", c.name, path, p.status, p.location, docPath)
			}
		}
		if p := w.get(t, c.p, docPath); strings.Contains(p.text(), p15cHeading) {
			t.Errorf("%s document page shows the link: %s", c.name, p.text())
		}
	}

	bare := w.saveAs(t, env.Arjun, map[string]string{"vendor_id": w.id(t, "party", "SHREE")})
	if p := w.get(t, env.Arjun, p15cItems(bare)); p.status != 200 || strings.Contains(p.text(), p15cHeading) {
		t.Errorf("draft with no site or project: Items = %d with link %v, want 200 and none", p.status, strings.Contains(p.text(), p15cHeading))
	}
}

func p15cIntV(t *testing.T, s string) int {
	t.Helper()
	return int(p15cInt(t, s))
}

package purchaseweb

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// UX §5.7 details beyond PUX-012 (P15c implementer): the intro names the
// site and project codes; a request with no needed-by date reads No
// needed-by date; a whole-number unit's quantity input is numeric (BAG) and
// a fractional one decimal (KGS).
func TestP15c_ViewDetails(t *testing.T) {
	w := newP15cWeb(t)
	env := w.env
	pr := purchasetest.NewRequest(t, env,
		purchasetest.RequestHeader(map[string]string{
			"site_id": w.id(t, "site", "METRO-SITE"), "project_id": w.id(t, "project", "PRJ-METRO"), "required_by": "",
		}),
		purchasetest.RequestLines(w.row(t, "CEMENT-OPC", "BAG", "40"), w.row(t, "WIRE-18G", "KGS", "25")))
	d := w.draft(t)
	page, rows := w.open(t, env.Priya, d.ID, pr.Number)
	text := page.text()
	if want := "Open request lines for METRO-SITE · PRJ-METRO."; !strings.Contains(text, want) {
		t.Errorf("view = %q, want the intro %q", text, want)
	}
	if want := "Suresh Patil · No needed-by date"; !strings.Contains(p15cCollapse(p15cCard(page, pr.Number).Text()), want) {
		t.Errorf("card has no %q", want)
	}
	if got, want := p15cFind(t, rows, "CEMENT-OPC").label(), pr.Lines[0].Values["description"].(string)+" CEMENT-OPC"; got != want {
		t.Errorf("CEMENT-OPC label = %q, want %q", got, want)
	}
	for code, mode := range map[string]string{"CEMENT-OPC": "numeric", "WIRE-18G": "decimal"} {
		if got, _ := p15cFind(t, rows, code).qty.Attr("inputmode"); got != mode {
			t.Errorf("%s quantity inputmode = %q, want %q", code, got, mode)
		}
	}
}

// UX §5.7's quantity that doesn't parse: 422 with NUM's text as the would-be
// line's error, linked from the summary to that row's quantity input and
// shown next to it, the tick kept, and nothing written. An untouched row
// keeps its pending default.
func TestP15c_QtyParseError(t *testing.T) {
	w := newP15cWeb(t)
	env := w.env
	pr := w.prA(t)
	d := w.draft(t)
	before, n := w.lines(t, d.ID), w.lineCount(t)
	page, rows := w.open(t, env.Priya, d.ID, pr.Number)
	cement := p15cFind(t, rows, "CEMENT-OPC")
	action, form := p15cForm(t, page, p15cPrimary, []p15cRow{cement}, map[*goquery.Selection]string{cement.qty: "4o"})
	a := w.do(t, env.Priya, "POST", action, form)
	if a.status != 422 {
		t.Fatalf("Add selected with quantity 4o = %d, want 422: %s", a.status, a.text())
	}
	_, perr := types.ParseQty("4o")
	want := "Line 2: " + perr.Error()
	if got := a.errors(); !slices.Equal(got, []string{want}) {
		t.Errorf("errors = %q, want %q", got, want)
	}
	id := docscreen.LineFieldID(2, "qty")
	if href, _ := a.doc.Find(".error-summary li a").Attr("href"); href != "#"+id {
		t.Errorf("summary link = %q, want #%s", href, id)
	}
	r := p15cFind(t, p15cRows(t, p15cCard(a, pr.Number)), "CEMENT-OPC")
	if got, _ := r.qty.Attr("id"); got != id || !r.ticked() || r.value() != "4o" {
		t.Errorf("redrawn CEMENT-OPC: id %q ticked %v quantity %q, want %s ticked 4o", got, r.ticked(), r.value(), id)
	}
	if msg := p15cCollapse(a.doc.Find("#" + id + "-msg").Text()); msg != want {
		t.Errorf("message next to the quantity = %q, want %q", msg, want)
	}
	if wire := p15cFind(t, p15cRows(t, p15cCard(a, pr.Number)), "WIRE-18G"); wire.value() != "25" || wire.ticked() {
		t.Errorf("untouched WIRE-18G: quantity %q ticked %v, want 25 unticked", wire.value(), wire.ticked())
	}
	if got := w.lines(t, d.ID); !slices.Equal(got, before) || w.lineCount(t) != n {
		t.Errorf("lines after a refused Add selected = %+v, want unchanged", got)
	}
}

// CD-280 D6 for a row held by two orders the viewer can't read: one On
// another order line, not one per order.
func TestP15c_HeldByTwoUnreadable(t *testing.T) {
	w := newP15cWeb(t)
	buyer := w.probe(t, "two")
	own := w.ownRequest(t, buyer)
	w.hold(t, purchasetest.StopAtDraft(), own.lines[1], "WIRE-18G", "KGS", "7217", "2")
	w.hold(t, purchasetest.StopAtDraft(), own.lines[1], "WIRE-18G", "KGS", "7217", "3")
	bd := w.saveAs(t, buyer, map[string]string{
		"site_id": w.id(t, "site", "METRO-SITE"), "project_id": w.id(t, "project", "PRJ-METRO"),
	})
	page := w.get(t, buyer, p15cFrom(bd))
	if got := strings.Count(p15cCollapse(p15cCard(page, own.display).Text()), p15cUnreadOne); got != 1 {
		t.Errorf("%q appears %d times, want once: %s", p15cUnreadOne, got, page.text())
	}
}

// The link and Add selected count a row with the least pending there is
// (0.001 KGS, one stored unit); once it is
// held, the view still lists the request but has no Add selected (nothing
// to tick).
func TestP15c_LeastPendingThenHeld(t *testing.T) {
	w := newP15cWeb(t)
	env := w.env
	pr := w.request(t, "METRO-SITE", "PRJ-METRO", w.row(t, "WIRE-18G", "KGS", "0.001"))
	d := w.draft(t)
	if got := w.get(t, env.Priya, p15cItems(d.ID)).link(p15cLink(1)); len(got) != 1 {
		t.Errorf("Items: no %q for a row with 0.001 pending", p15cLink(1))
	}
	page, rows := w.open(t, env.Priya, d.ID, pr.Number)
	if len(rows) != 1 {
		t.Errorf("row with 0.001 pending: %d tickable rows, want 1", len(rows))
	}
	if got := p15cPrimaryNav(page, p15cPrimary); got != "nav=stay" {
		t.Errorf("one row with 0.001 pending: %q posts %q, want nav=stay", p15cPrimary, got)
	}
	w.hold(t, purchasetest.StopAtDraft(), pr.Lines[0].ID, "WIRE-18G", "KGS", "7217", "0.001")
	page, rows = w.open(t, env.Priya, d.ID, pr.Number)
	if len(rows) != 0 {
		t.Errorf("held row is tickable")
	}
	if got := p15cPrimaryNav(page, p15cPrimary); got != "missing" {
		t.Errorf("all held: view has %q (%s), want none", p15cPrimary, got)
	}
}

// A draft with a project but no site, or a site (PUNE-WH, on no project)
// but no project, has no link, and ?from=requests shows the generic Items
// body, not the view.
func TestP15c_NoSiteOrProject(t *testing.T) {
	w := newP15cWeb(t)
	env := w.env
	w.prA(t)
	noSite := w.saveAs(t, env.Priya, map[string]string{
		"vendor_id": w.id(t, "party", "SHREE"), "project_id": w.id(t, "project", "PRJ-METRO"),
	})
	noProject := w.saveAs(t, env.Arjun, map[string]string{
		"vendor_id": w.id(t, "party", "SHREE"), "site_id": w.id(t, "site", "PUNE-WH"),
	})
	for _, path := range []string{p15cItems(noSite), p15cFrom(noSite), p15cItems(noProject), p15cFrom(noProject)} {
		p := env.Priya
		if strings.Contains(path, "/"+strconv.FormatInt(noProject, 10)+"/") {
			p = env.Arjun
		}
		page := w.get(t, p, path)
		if page.status != 200 || strings.Contains(page.text(), p15cHeading) || len(page.link(p15cBack)) != 0 {
			t.Errorf("GET %s = %d, shows %q or %q: %s", path, page.status, p15cHeading, p15cBack, page.text())
		}
	}
}

// A redraw keeps typed quantities and gives a row that appeared since the
// page was opened its pending default.
func TestP15c_RedrawNewRow(t *testing.T) {
	w := newP15cWeb(t)
	env := w.env
	pr := w.prA(t)
	d := w.draft(t)
	page, _ := w.open(t, env.Priya, d.ID, pr.Number)
	late := w.request(t, "METRO-SITE", "PRJ-METRO", w.row(t, "BOLT-M12", "BOX", "6"))
	action, form := p15cForm(t, page, p15cPrimary, nil, nil)
	a := w.do(t, env.Priya, "POST", action, form)
	if a.status != 422 {
		t.Fatalf("Add selected with nothing ticked = %d, want 422", a.status)
	}
	if r := p15cFind(t, p15cRows(t, p15cCard(a, late.Number)), "BOLT-M12"); r.value() != "6" {
		t.Errorf("new row's quantity = %q, want its pending 6", r.value())
	}
}

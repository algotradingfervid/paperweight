package purchaseweb

import (
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// P15c fix round 1 (review findings; UX §5.7; PO spec §20.5; CD-259).

// p15cGone is the refusal for a ticked row that is no longer open: named
// when the viewer can read its request (n the line it would have become, m
// the row's line number), unnamed otherwise and for an unknown id.
func p15cGone(n int, pr string, m int) string {
	return "Line " + strconv.Itoa(n) + ": " + pr + " line " + strconv.Itoa(m) + " is no longer open. Untick it or reload the page."
}

func p15cGoneUnread(n int) string {
	return "Line " + strconv.Itoa(n) + ": That request line is no longer open. Untick it or reload the page."
}

// p15cTicked reports whether the redraw's step form holds a ticked
// checkbox for request line id.
func p15cTicked(p p15cPage, id int64) bool {
	return p.doc.Find(`form#wizard-form input[type="checkbox"][name="rq_line"][value="`+strconv.FormatInt(id, 10)+`"][checked]`).Length() == 1
}

// A ticked row whose request was closed after the view was opened is
// refused, not dropped: 422 naming PR-A's line 1 as the draft's line 2, the
// tick kept and nothing written.
func TestP15c_ClosedMeanwhileRefused(t *testing.T) {
	w := newP15cWeb(t)
	env := w.env
	pr := w.prA(t)
	d := w.draft(t)
	page, rows := w.open(t, env.Priya, d.ID, pr.Number)
	w.act(t, env.Suresh, purchase.RequestDocType, pr.ID, doctype.ActionClose, "No longer needed")
	before, n := w.lines(t, d.ID), w.lineCount(t)

	cement := p15cFind(t, rows, "CEMENT-OPC")
	action, form := p15cForm(t, page, p15cPrimary, []p15cRow{cement}, nil)
	a := w.do(t, env.Priya, "POST", action, form)
	if a.status != 422 {
		t.Fatalf("Add selected on a closed request's row = %d to %q, want 422: %s", a.status, a.location, a.text())
	}
	if want := p15cGone(2, pr.Number, 1); !slices.Equal(a.errors(), []string{want}) {
		t.Errorf("errors = %q, want %q", a.errors(), want)
	}
	if !p15cTicked(a, pr.Lines[0].ID) {
		t.Errorf("422 redraw lost the CEMENT-OPC tick: %s", a.text())
	}
	if got := w.lines(t, d.ID); !slices.Equal(got, before) || w.lineCount(t) != n {
		t.Errorf("lines after a refused Add selected = %+v, want unchanged", got)
	}
}

// A forged tick for a row of a request the viewer can't read (PR-A, for the
// METRO-SITE buyer whose Request read is own), and a tick for an id no row
// has, are refused with the unnamed text as the would-be line 1 of her
// empty draft; neither names PR-A, and nothing is written.
func TestP15c_ForgedTickRefused(t *testing.T) {
	w := newP15cWeb(t)
	buyer := w.probe(t, "forger")
	prA := w.prA(t)
	own := w.ownRequest(t, buyer)
	bd := w.saveAs(t, buyer, map[string]string{
		"site_id": w.id(t, "site", "METRO-SITE"), "project_id": w.id(t, "project", "PRJ-METRO"),
	})
	page, _ := w.open(t, buyer, bd, own.display)
	n := w.lineCount(t)
	for name, id := range map[string]int64{"unreadable PR-A row": prA.Lines[0].ID, "unknown id": 999999} {
		action, form := p15cForm(t, page, p15cPrimary, nil, nil)
		s := strconv.FormatInt(id, 10)
		form.Add("rq_line", s)
		form.Set("rq_qty_"+s, "5")
		a := w.do(t, buyer, "POST", action, form)
		if a.status != 422 {
			t.Errorf("%s: Add selected = %d to %q, want 422: %s", name, a.status, a.location, a.text())
			continue
		}
		if want := p15cGoneUnread(1); !slices.Equal(a.errors(), []string{want}) {
			t.Errorf("%s: errors = %q, want %q", name, a.errors(), want)
		}
		if strings.Contains(a.text(), prA.Number) {
			t.Errorf("%s: refusal names %s: %s", name, prA.Number, a.text())
		}
		if len(w.lines(t, bd)) != 0 || w.lineCount(t) != n {
			t.Errorf("%s: lines written by a refused Add selected", name)
		}
	}
}

// CD-259: the version is checked before the ticks and quantities. A stale
// Add selected with nothing ticked, or with a quantity that doesn't parse,
// is 409 with DOC's stale text and the posted version kept.
func TestP15c_StaleBeforeTicks(t *testing.T) {
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

	for name, c := range map[string]struct {
		tick []p15cRow
		set  map[*goquery.Selection]string
	}{
		"nothing ticked": {nil, nil},
		"bad quantity":   {[]p15cRow{cement}, map[*goquery.Selection]string{cement.qty: "4o"}},
	} {
		action, form := p15cForm(t, page, p15cPrimary, c.tick, c.set)
		a := w.do(t, env.Priya, "POST", action, form)
		if a.status != 409 {
			t.Errorf("%s: stale Add selected = %d (errors %q), want 409", name, a.status, a.errors())
			continue
		}
		if want := p15cStale(p15cDisplay(d)); !strings.Contains(a.text(), want) {
			t.Errorf("%s: 409 page = %q, want %q", name, a.text(), want)
		}
		if got := p15cDocVersion(a); got != posted {
			t.Errorf("%s: 409 redraw version = %q, want the posted %q (CD-94)", name, got, posted)
		}
	}
	if got := w.lines(t, d.ID); !slices.Equal(got, before) || w.lineCount(t) != n {
		t.Errorf("lines after stale posts = %+v, want unchanged", got)
	}
}

// CD-259: the state is checked before the ticks. from=requests posted to
// Priya's pending order, with nothing ticked or with a bad quantity, gets
// the state refusal a generic Items post gets (409, DOC's waiting text),
// never the tick or quantity message.
func TestP15c_StateBeforeTicks(t *testing.T) {
	w := newP15cWeb(t)
	env := w.env
	pr := w.prA(t)
	big := purchasetest.OrderLines(map[string]string{"qty": "1000"}) // ₹62,500: Purchase Manager approves
	pending := purchasetest.NewOrder(t, env, purchasetest.StopAtPending(), big)
	v := strconv.Itoa(w.version(t, pending.ID))
	id := strconv.FormatInt(pr.Lines[0].ID, 10)
	want := p15cDisplay(pending) + " is waiting for approval and can't be changed. Withdraw it to make changes."
	n := w.lineCount(t)
	for name, form := range map[string]url.Values{
		"nothing ticked": {"version": {v}, "nav": {"stay"}, "from": {"requests"}},
		"bad quantity":   {"version": {v}, "nav": {"stay"}, "from": {"requests"}, "rq_line": {id}, "rq_qty_" + id: {"4o"}},
	} {
		a := w.do(t, env.Priya, "POST", p15cItems(pending.ID), form)
		if a.status != 409 || !strings.Contains(a.text(), want) {
			t.Errorf("%s: post to a pending order = %d (errors %q), want 409 with %q: %s", name, a.status, a.errors(), want, a.text())
		}
		if strings.Contains(a.text(), p15cNoTick) || strings.Contains(a.text(), "is not a valid number") {
			t.Errorf("%s: post to a pending order shows the tick or quantity message: %s", name, a.text())
		}
	}
	if w.lineCount(t) != n {
		t.Error("lines written by a post to a pending order")
	}
}

// A draft on PUNE-WH / PRJ-METRO (site id 1) with PR-C open there shows
// Add from requests (1 open) and lists PR-C.
func TestP15c_LinkOnFirstSite(t *testing.T) {
	w := newP15cWeb(t)
	env := w.env
	prC := w.request(t, "PUNE-WH", "PRJ-METRO", w.row(t, "CEMENT-OPC", "BAG", "9"))
	if site := w.id(t, "site", "PUNE-WH"); site != "1" {
		t.Fatalf("PUNE-WH id = %s, want 1 (the case this test is for)", site)
	}
	d := w.saveAs(t, env.Priya, map[string]string{
		"vendor_id": w.id(t, "party", "SHREE"), "site_id": w.id(t, "site", "PUNE-WH"),
		"project_id": w.id(t, "project", "PRJ-METRO"),
	})
	items := w.get(t, env.Priya, p15cItems(d))
	if got := items.link(p15cLink(1)); !slices.Equal(got, []string{p15cFrom(d)}) {
		t.Errorf("PUNE-WH draft Items: %q links %q, want one to %s: %s", p15cLink(1), got, p15cFrom(d), items.text())
	}
	w.open(t, env.Priya, d, prC.Number)
}

// A request row in HRS (uom id 10) is appended in HRS: the saved line's
// uom_id is that unit's.
func TestP15c_AppendKeepsUnit(t *testing.T) {
	w := newP15cWeb(t)
	env := w.env
	hrs := w.id(t, "uom", "HRS")
	if hrs != "10" {
		t.Fatalf("HRS id = %s, want 10 (a two-digit id)", hrs)
	}
	pr := w.request(t, "METRO-SITE", "PRJ-METRO", w.row(t, "SVC-CRANE", "HRS", "8"))
	d := w.draft(t)
	page, rows := w.open(t, env.Priya, d.ID, pr.Number)
	action, form := p15cForm(t, page, p15cPrimary, []p15cRow{p15cFind(t, rows, "SVC-CRANE")}, nil)
	a := w.do(t, env.Priya, "POST", action, form)
	if a.status != 303 {
		t.Fatalf("Add selected on an HRS row = %d (errors %q), want 303: %s", a.status, a.errors(), a.text())
	}
	got := w.lines(t, d.ID)
	if len(got) != 2 {
		t.Fatalf("lines = %+v, want the draft's one plus the HRS row", got)
	}
	if l := got[1]; l.uom != p15cInt(t, hrs) || l.item != p15cInt(t, w.id(t, "item", "SVC-CRANE")) || l.qty != p15cQty(t, "8") ||
		l.rqLineID.Int64 != pr.Lines[0].ID {
		t.Errorf("line 2 = %+v, want 8 HRS SVC-CRANE on request line %d", l, pr.Lines[0].ID)
	}
}

// UX §5.7: the refusal for a row taken meanwhile (§20.5) links from the
// summary to the would-be line's quantity input and shows next to it.
func TestP15c_TakenLinksToQty(t *testing.T) {
	w := newP15cWeb(t)
	env := w.env
	pr := w.prA(t)
	d := w.draft(t)
	page, rows := w.open(t, env.Priya, d.ID, pr.Number)
	other := w.hold(t, purchasetest.StopAtDraft(), pr.Lines[0].ID, "CEMENT-OPC", "BAG", "2523", "40")
	action, form := p15cForm(t, page, p15cPrimary, []p15cRow{p15cFind(t, rows, "CEMENT-OPC")}, nil)
	a := w.do(t, env.Priya, "POST", action, form)
	if a.status != 422 {
		t.Fatalf("Add selected on a taken row = %d, want 422: %s", a.status, a.text())
	}
	want := "Line 2: Request " + pr.Number + " line 1 is already on " + p15cDisplay(other) + "."
	if got := a.errors(); !slices.Equal(got, []string{want}) {
		t.Errorf("errors = %q, want %q", got, want)
	}
	id := docscreen.LineFieldID(2, "qty")
	if href, _ := a.doc.Find(".error-summary li a").Attr("href"); href != "#"+id {
		t.Errorf("summary link = %q, want #%s (the quantity input)", href, id)
	}
	if got, _ := p15cFind(t, p15cRows(t, p15cCard(a, pr.Number)), "CEMENT-OPC").qty.Attr("id"); got != id {
		t.Errorf("CEMENT-OPC quantity input id = %q, want %s", got, id)
	}
	if msg := p15cCollapse(a.doc.Find("#" + id + "-msg").Text()); msg != want {
		t.Errorf("message next to the quantity = %q, want %q", msg, want)
	}
}

// PO spec §20.5: more than pending is allowed. 41 of PR-A's 40 BAG saves
// (303) as 41 and the check page warns Line 2: ordering 1 more than request
// PR-A asked for.
func TestP15c_OverPendingAccepted(t *testing.T) {
	w := newP15cWeb(t)
	env := w.env
	pr := w.prA(t)
	d := w.draft(t)
	page, rows := w.open(t, env.Priya, d.ID, pr.Number)
	cement := p15cFind(t, rows, "CEMENT-OPC")
	action, form := p15cForm(t, page, p15cPrimary, []p15cRow{cement}, map[*goquery.Selection]string{cement.qty: "41"})
	a := w.do(t, env.Priya, "POST", action, form)
	if a.status != 303 || a.location != p15cItems(d.ID) {
		t.Fatalf("Add selected with 41 of 40 = %d to %q (errors %q), want 303 to %s", a.status, a.location, a.errors(), p15cItems(d.ID))
	}
	got := w.lines(t, d.ID)
	if len(got) != 2 || got[1].qty != p15cQty(t, "41") || got[1].rqLineID.Int64 != pr.Lines[0].ID {
		t.Fatalf("lines = %+v, want line 2 of 41 on request line %d", got, pr.Lines[0].ID)
	}
	chk := w.get(t, env.Priya, p15cItems(d.ID)[:len(p15cItems(d.ID))-len("items")]+"check")
	if want := "Line 2: ordering 1 more than request " + pr.Number + " asked for."; !strings.Contains(chk.text(), want) {
		t.Errorf("check page = %q, want the warning %q", chk.text(), want)
	}
}

// A request line id of two digits (10) is ticked and appended with that
// id: the checkbox value, the quantity name and the save's match all carry
// it in base 10.
func TestP15c_TwoDigitLineID(t *testing.T) {
	w := newP15cWeb(t)
	env := w.env
	for range 2 { // request lines 1 to 8, on another site and project
		w.request(t, "PUNE-WH", "PRJ-TECHPARK", w.row(t, "CEMENT-OPC", "BAG", "1"), w.row(t, "WIRE-18G", "KGS", "1"),
			w.row(t, "BOLT-M12", "NOS", "1"), w.row(t, "CABLE-4C", "MTR", "1"))
	}
	pr := w.prA(t)
	if pr.Lines[1].ID != 10 {
		t.Fatalf("PR-A's WIRE-18G line id = %d, want 10", pr.Lines[1].ID)
	}
	d := w.draft(t)
	page, rows := w.open(t, env.Priya, d.ID, pr.Number)
	wire := p15cFind(t, rows, "WIRE-18G")
	action, form := p15cForm(t, page, p15cPrimary, []p15cRow{wire}, map[*goquery.Selection]string{wire.qty: "20"})
	a := w.do(t, env.Priya, "POST", action, form)
	if a.status != 303 {
		t.Fatalf("Add selected on request line 10 = %d (errors %q), want 303", a.status, a.errors())
	}
	got := w.lines(t, d.ID)
	if len(got) != 2 || got[1].qty != p15cQty(t, "20") || got[1].rqLineID.Int64 != 10 {
		t.Errorf("lines = %+v, want line 2 of 20 KGS on request line 10", got)
	}
}

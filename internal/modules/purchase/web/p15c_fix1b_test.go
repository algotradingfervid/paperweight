package purchaseweb

import (
	"slices"
	"strconv"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// P15c fix round 1b: the no-longer-open rows a redraw draws.

// PR-A is closed after Priya opens the view; PR-B (BOLT-M12) stays open.
// She ticks PR-B's row, then PR-A's CEMENT-OPC and WIRE-18G: the would-be
// lines 2, 3 and 4 of her one-line draft. The 422 redraw draws each ticked
// id once (PR-B's row only in its card), each PR-A row as a "No longer
// open" checkbox with the id of its line (LineFieldID(n, "rq_line")), its
// own refusal next to it, and the summary links to it.
func TestP15c_GoneRowsAtTheirLines(t *testing.T) {
	w := newP15cWeb(t)
	env := w.env
	prA := w.prA(t)
	prB := w.request(t, "METRO-SITE", "PRJ-METRO", w.row(t, "BOLT-M12", "NOS", "6"))
	d := w.draft(t)
	page, bRows := w.open(t, env.Priya, d.ID, prB.Number)
	aRows := p15cRows(t, p15cCard(page, prA.Number))
	w.act(t, env.Suresh, purchase.RequestDocType, prA.ID, doctype.ActionClose, "No longer needed")

	tick := []p15cRow{p15cFind(t, bRows, "BOLT-M12"), p15cFind(t, aRows, "CEMENT-OPC"), p15cFind(t, aRows, "WIRE-18G")}
	action, form := p15cForm(t, page, p15cPrimary, tick, nil)
	form["rq_line"] = []string{ // in this order whatever the cards' order
		strconv.FormatInt(prB.Lines[0].ID, 10), strconv.FormatInt(prA.Lines[0].ID, 10), strconv.FormatInt(prA.Lines[1].ID, 10),
	}
	a := w.do(t, env.Priya, "POST", action, form)
	if a.status != 422 {
		t.Fatalf("Add selected with two closed rows = %d to %q, want 422: %s", a.status, a.location, a.text())
	}
	gone := []struct {
		line int
		id   int64
		want string
	}{
		{3, prA.Lines[0].ID, p15cGone(3, prA.Number, 1)},
		{4, prA.Lines[1].ID, p15cGone(4, prA.Number, 2)},
	}
	if want := []string{gone[0].want, gone[1].want}; !slices.Equal(a.errors(), want) {
		t.Errorf("errors = %q, want %q", a.errors(), want)
	}
	for _, id := range []int64{prB.Lines[0].ID, prA.Lines[0].ID, prA.Lines[1].ID} {
		if !p15cTicked(a, id) {
			t.Errorf("redraw draws request line %d's ticked checkbox %d times, want once: %s", id,
				a.doc.Find(`form#wizard-form input[name="rq_line"][value="`+strconv.FormatInt(id, 10)+`"]`).Length(), a.text())
		}
	}
	var hrefs []string
	a.doc.Find(".error-summary li a").Each(func(_ int, s *goquery.Selection) {
		h, _ := s.Attr("href")
		hrefs = append(hrefs, h)
	})
	var wantHrefs []string
	for _, g := range gone {
		id := docscreen.LineFieldID(g.line, "rq_line")
		wantHrefs = append(wantHrefs, "#"+id)
		box := a.doc.Find(`form#wizard-form input[type="checkbox"][name="rq_line"][value="` + strconv.FormatInt(g.id, 10) + `"]`)
		if got, _ := box.Attr("id"); got != id {
			t.Errorf("line %d's no-longer-open checkbox id = %q, want %s", g.line, got, id)
		}
		if msg := p15cCollapse(a.doc.Find("#" + id + "-msg").Text()); msg != g.want {
			t.Errorf("message next to line %d's checkbox = %q, want %q", g.line, msg, g.want)
		}
	}
	if !slices.Equal(hrefs, wantHrefs) {
		t.Errorf("summary links = %q, want %q", hrefs, wantHrefs)
	}
}

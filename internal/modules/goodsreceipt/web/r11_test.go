package grnweb_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// UI v2 R11 (GRN review H4, H5, M2; UX §3.5, §5.4, §5.6, §5.7; CD-387 (7)).

// r11Bar is the document page's action bar primary, by its text and link.
func (p s1Page) r11Bar() (label, href string, n int) {
	b := p.doc.Find(".doc-actions .actionbar .btn--primary")
	return s2Norm(b.Text()), b.AttrOr("href", ""), b.Length()
}

// H4, UX §3.5, §5.6: an approved GRN with no bill details has the bar
// primary Add bill details, jumping to the Bill details card, for a user who
// holds write, and only write beside read. Without write there is none, not
// on a read-only draft, and none once bill details exist. A cancelled receipt
// never has it, also for a writer who can't amend (the bar would be free).
func TestGRNWeb_AddBillPrimary(t *testing.T) {
	w := newS1World(t)
	po := w.s3bPO()
	writer := w.s3Role("Bill Writer", auth.RightRead, auth.RightWrite)
	reader := w.s3Role("Bill Reader", auth.RightRead, auth.RightCancel, auth.RightPrint, auth.RightSubmit)
	g := w.s3bGRN(w.Suresh, po, doctype.StateApproved)

	for name, p := range map[string]auth.Principal{"Suresh": w.Suresh, "write only": writer} {
		r := s3OK(t, w.get(p, s3Doc(g.ID)), name)
		if label, href, n := r.r11Bar(); n != 1 || label != "Add bill details" || href != "#bill" {
			t.Errorf("%s: bar primary %d %q to %q, want Add bill details to #bill", name, n, label, href)
		}
		if r.s3bCard().Length() != 1 {
			t.Errorf("%s: no #bill card for the primary to jump to", name)
		}
	}
	for name, p := range map[string]auth.Principal{"Priya": w.Priya, "no write": reader} {
		if label, _, n := s3OK(t, w.get(p, s3Doc(g.ID)), name).r11Bar(); n != 0 {
			t.Errorf("%s, approved without write: bar primary %q", name, label)
		}
	}

	d := w.s3bGRN(w.Suresh, po, doctype.StateDraft)
	if label, _, n := s3OK(t, w.get(w.Priya, s3Doc(d.ID)), "reader's draft").r11Bar(); n != 0 {
		t.Errorf("reader's draft: bar primary %q", label)
	}

	w.s3bMustSave(w.Suresh, g.ID, 0, s3bInvoice, "6,250.00")
	for name, p := range map[string]auth.Principal{"Suresh": w.Suresh, "write only": writer} {
		if label, _, n := s3OK(t, w.get(p, s3Doc(g.ID)), name+", saved").r11Bar(); n != 0 {
			t.Errorf("%s, with bill details: bar primary %q", name, label)
		}
	}

	c := w.s3bGRN(w.Suresh, po, doctype.StateApproved)
	w.Cancel(t, w.Ravi, c.ID, "Counted twice")
	if label, _, n := s3OK(t, w.get(writer, s3Doc(c.ID)), "cancelled, write only").r11Bar(); n != 0 {
		t.Errorf("cancelled, write only: bar primary %q", label)
	}
	if label, _, n := s3OK(t, w.get(w.Suresh, s3Doc(c.ID)), "cancelled, Suresh").r11Bar(); n != 1 || label != "Amend" {
		t.Errorf("cancelled, Suresh: bar primary %d %q, want Amend", n, label)
	}
}

// H4: the first bill save answers htmx with the card and drops the bar that
// held Add bill details (the browser test proves the swap); a refused save
// and a later change of stored details leave the page's bar alone.
func TestGRNWeb_BillSaveDropsPrimary(t *testing.T) {
	w := newS1World(t)
	g := w.s3bGRN(w.Suresh, w.s3bPO(), doctype.StateApproved)
	oob := `[hx-swap-oob^="delete:"]`

	r := w.s3bSave(w.Suresh, g.ID, s3bForm(0, "", "23-10-2026", "6,250.00"), true)
	if r.status != http.StatusUnprocessableEntity || r.doc.Find(oob).Length() != 0 {
		t.Errorf("refused save = %d with %d bar deletes, want 422 and none", r.status, r.doc.Find(oob).Length())
	}
	r = w.s3bSave(w.Suresh, g.ID, s3bForm(0, s3bInvoice, "23-10-2026", "6,250.00"), true)
	if r.status != http.StatusOK || r.doc.Find(oob).Length() != 1 {
		t.Errorf("first save = %d with %d bar deletes, want 200 and one", r.status, r.doc.Find(oob).Length())
	}
	if got := r.doc.Find(oob).AttrOr("hx-swap-oob", ""); !strings.Contains(got, ".doc-actions") {
		t.Errorf("the delete targets %q, want the document page's actions", got)
	}
	r = w.s3bSave(w.Suresh, g.ID, s3bForm(1, s3bInvoice, "23-10-2026", "7,000.00"), true)
	if r.status != http.StatusOK || r.doc.Find(oob).Length() != 0 {
		t.Errorf("later change = %d with %d bar deletes, want 200 and none", r.status, r.doc.Find(oob).Length())
	}
}

// r11Count is the Quantities footer's summary text.
func (p s1Page) r11Count() string {
	return s2Norm(p.doc.Find("#wizard .actionbar .actionbar-summary").Text())
}

// H5, CD-387 (7), CD-391, UX §5.4 Saving: the footer counts the pending
// cards that post a line (Received now above 0) of all pending cards, from
// the stored lines and from a redraw's post. A 0 or a blank isn't entered; a
// typed value that can't be read still is, as Save refuses it on its card
// (nonZero).
func TestGRNWeb_ItemsFooterCount(t *testing.T) {
	w := newS1World(t)
	po := w.s2RefPO()
	id := w.draft(po)
	r := w.s2Items(id)
	if got, want := r.r11Count(), "2 of 2 lines entered"; got != want {
		t.Errorf("prefilled: footer %q, want %q", got, want)
	}
	if n := r.doc.Find(`#wizard-form input[data-line-entry]`).Length(); n != 2 {
		t.Errorf("%d inputs the page's count reads, want 2", n)
	}
	if got := r.doc.Find("#wizard .actionbar [data-lines-entered]").AttrOr("aria-live", ""); got != "polite" {
		t.Errorf("the count's aria-live = %q, want polite: a screen reader hears it change while typing", got)
	}

	r = w.s2Press(id, r, "Clear all")
	if r.status == http.StatusSeeOther {
		r = w.s2Items(id)
	}
	if got, want := r.r11Count(), "0 of 2 lines entered"; got != want {
		t.Errorf("after Clear all: footer %q, want %q", got, want)
	}

	for _, c := range []struct {
		name, one, two, want string
	}{
		{"one over its line", "2,500", "", "1 of 2 lines entered"},
		{"a zero isn't entered", "2,500", "0", "1 of 2 lines entered"},
		{"both", "2,500", "abc", "2 of 2 lines entered"},
	} {
		p := w.s2Save(id, w.s2Items(id), s2Set{"received", 1, c.one}, s2Set{"received", 2, c.two})
		if p.status != http.StatusUnprocessableEntity {
			t.Fatalf("%s: save = %d, want a 422 redraw", c.name, p.status)
		}
		if got := p.r11Count(); got != c.want {
			t.Errorf("%s: footer %q, want %q", c.name, got, c.want)
		}
	}
}

// CD-391, GUX-010: a fully received line isn't one of the footer's lines,
// blank or typed into, and its input isn't one the page's script counts.
func TestGRNWeb_ItemsFooterCountsPendingOnly(t *testing.T) {
	w := newS1World(t)
	po := w.s2RefPO()
	w.s2Approve(po, map[int]string{2: "50"})
	id := w.draft(po)
	r := w.s2Items(id)
	if got, want := r.r11Count(), "1 of 1 lines entered"; got != want {
		t.Errorf("footer %q, want %q (card 2 is fully received)", got, want)
	}
	if n := r.doc.Find(`#wizard-form input[data-line-entry]`).Length(); n != 1 || !r.s2Input("received", 1).Is("[data-line-entry]") {
		t.Errorf("%d inputs the page's script counts, want only line 1's", n)
	}
	p := w.s2Save(id, r, s2Set{"received", 1, ""}, s2Set{"received", 2, "5"})
	if p.status == http.StatusSeeOther {
		p = w.s2Items(id)
	}
	if got, want := p.r11Count(), "0 of 1 lines entered"; got != want {
		t.Errorf("only the fully received line typed into: footer %q, want %q", got, want)
	}
}

// CD-391: a line with 0.001 KGS still pending (the smallest quantity) is a pending line, counted and marked
// for the page's script.
func TestGRNWeb_ItemsFooterCountsOnePending(t *testing.T) {
	w := newS1World(t)
	po := w.s2RefPO()
	w.s2Approve(po, map[int]string{2: "49.999"})
	r := w.s2Items(w.draft(po))
	if got, want := r.r11Count(), "2 of 2 lines entered"; got != want {
		t.Errorf("footer %q, want %q (card 2 has 0.001 pending)", got, want)
	}
	if !r.s2Input("received", 2).Is("[data-line-entry]") {
		t.Errorf("card 2, 0.001 pending, isn't counted by the page's script")
	}
}

// CD-391: with no line pending (a truck against a fully received PO) the
// footer has no count at all, not "0 of 0", even with a value typed in.
func TestGRNWeb_ItemsFooterNoCountWhenNothingPending(t *testing.T) {
	w := newS1World(t)
	po := w.s2RefPO()
	w.s2Approve(po, map[int]string{1: "1200", 2: "50"})
	id := w.draft(po)
	r := w.s2Items(id)
	if n := r.doc.Find("#wizard .actionbar .actionbar-summary").Length(); n != 0 {
		t.Errorf("nothing pending: footer summary %q, want none", r.r11Count())
	}
	if n := r.doc.Find(`#wizard-form input[data-line-entry]`).Length(); n != 0 {
		t.Errorf("nothing pending: %d inputs the page's script counts, want none", n)
	}
	p := w.s2Save(id, r, s2Set{"received", 1, "abc"})
	if p.status != http.StatusUnprocessableEntity || p.doc.Find("#wizard .actionbar .actionbar-summary").Length() != 0 {
		t.Errorf("nothing pending, 422 redraw = %d with footer %q, want 422 and none", p.status, p.r11Count())
	}
}

// CD-319: the count is read from received_qty, so a viewer it's hidden from
// gets no count; the other card fields leave it in place.
func TestGRNWeb_ItemsFooterCountHonoursHide(t *testing.T) {
	for name, want := range map[string]string{"received_qty": "", "rejected_qty": "2 of 2 lines entered"} {
		w := newS1World(t)
		id := w.draft(w.s2RefPO())
		w.hidden = map[string]bool{name: true}
		if got := w.s2Items(id).r11Count(); got != want {
			t.Errorf("%s hidden: footer %q, want %q", name, got, want)
		}
	}
}

// H5, CD-282 Q3: the hint moved up beside the code stays read with the input
// on every card, and goes with the input when Hide hides it.
func TestGRNWeb_ItemsHintBlock(t *testing.T) {
	w := newS1World(t)
	id := w.draft(w.s2RefPO())
	r := w.s2Items(id)
	for n := 1; n <= 2; n++ {
		in := r.s2Input("received", n)
		inID := in.AttrOr("id", "x")
		if got, want := in.AttrOr("aria-describedby", ""), inID+"-hint "+inID+"-msg"; got != want {
			t.Errorf("card %d aria-describedby %q, want %q", n, got, want)
		}
		block := r.s2Card(t, n).Find(`.qty-line__item [id="` + inID + `-hint"]`)
		if block.Length() != 1 {
			t.Errorf("card %d: the hint isn't in the item's block", n)
		}
		if r.s2Card(t, n).Find(".field .field__hint").First().AttrOr("id", "") == inID+"-hint" {
			t.Errorf("card %d: the hint is still in the field", n)
		}
	}
	w.hidden = map[string]bool{"received_qty": true}
	if got := w.s2Items(id).doc.Find(".qty-line__item").Text(); strings.Contains(got, "Ordered") {
		t.Errorf("Received now hidden, yet its hint shows: %q", got)
	}
}

// M2: the summary, the buttons and the cards are spaced as a column, and the
// cards in both places sit in the spaced column.
func TestGRNWeb_ItemsSpacing(t *testing.T) {
	w := newS1World(t)
	po := w.s2RefPO()
	w.s2Approve(po, map[int]string{2: "50"})
	r := w.s2Items(w.draft(po))
	if n := r.doc.Find("#wizard-form > .stack > .qty-lines > .qty-line").Length(); n != 1 {
		t.Errorf("%d pending cards in the column, want 1", n)
	}
	if n := r.doc.Find("#wizard-form details.disclosure .qty-lines > .qty-line").Length(); n != 1 {
		t.Errorf("%d fully received cards in a spaced column, want 1", n)
	}
	if n := r.doc.Find("#wizard-form > .stack > p").Length(); n != 1 {
		t.Errorf("%d summary lines in the column, want 1", n)
	}
}

// M11, CD-319 (9): with both quantities hidden the card has no quantity row
// at all, and never the default card's tax.
func TestGRNWeb_LineCardBothQuantitiesHidden(t *testing.T) {
	w := newS1World(t)
	id := w.s3cDraft(w.s3cPO())
	w.hidden = map[string]bool{"received_qty": true, "accepted_qty": true}
	got := s3OK(t, w.get(w.Suresh, stepPath(id, "check")), "check page").s3cCards()
	if len(got) == 0 || strings.Join(got[0].meta, " | ") != "Rejected 10 KGS: Bent pieces" {
		t.Errorf("cards %+v, want only the reject note on the first", got)
	}
}

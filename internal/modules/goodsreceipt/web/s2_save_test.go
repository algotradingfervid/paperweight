package grnweb_test

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/types"
)

// MOD-GRN S2, step 3's Save (UX doc §5.4 Saving, Receive all pending and
// Clear all; CD-256 (3), HOOK-016, CD-207; hooks spec §4.1). Suresh on the
// real auth.Service and engine (CD-213).

// s2P is s as a stored quantity's plain text.
func s2P(t *testing.T, s string) string {
	t.Helper()
	q, err := types.ParseQty(s)
	if err != nil {
		t.Fatal(err)
	}
	return q.Plain()
}

// s2Want is a stored line as s2Lines prints it.
func s2Want(t *testing.T, poLine int, received, rejected, reason string, id int64) string {
	t.Helper()
	return fmt.Sprintf("po%d=%s/%s/%s#%d", poLine, s2P(t, received), s2P(t, rejected), reason, id)
}

// s2WantStep fails unless r is the 303 a save answers, to the step whose
// URL ends in suffix.
func s2WantStep(t *testing.T, r s1Page, suffix, what string) {
	t.Helper()
	if r.status != http.StatusSeeOther || !strings.HasSuffix(r.location, suffix) {
		t.Fatalf("%s = %d %q %q, want 303 to …%s", what, r.status, r.location, r.errorSummary(), suffix)
	}
}

// HOOK-016, CD-256 (3), UX §5.4 Saving: Save posts one line per card with
// Received now above 0, each carrying the stored line's id when it has one,
// so an edited line keeps its id; a card at 0 or blank is not stored (its
// line is removed), and a card with no stored line becomes a new line.
func TestGRNWeb_ItemsSaveLines(t *testing.T) {
	w := newS1World(t)
	po := w.s2RefPO()
	id := w.draft(po)
	ids := w.s2LineIDs(id, po)
	if len(ids) != 2 {
		t.Fatalf("the draft from step 1 has lines %v, want PO lines 1 and 2", ids)
	}

	r := w.s2Save(id, w.s2Items(id), s2Set{"received", 1, "1000"}, s2Set{"received", 2, "0"})
	s2WantStep(t, r, "/edit/check", "save 1000 and 0")
	if got, want := w.s2Lines(id, po), []string{s2Want(t, 1, "1000", "0", "", ids[1])}; !slices.Equal(got, want) {
		t.Errorf("after 1000 and 0: lines %q, want %q (line 1 keeps id %d, line 2 removed)", got, want, ids[1])
	}

	r = w.s2Save(id, w.s2Items(id), s2Set{"received", 2, "7"})
	s2WantStep(t, r, "/edit/check", "save 7 on line 2")
	now := w.s2LineIDs(id, po)
	if now[1] != ids[1] || now[2] == 0 || now[2] == ids[2] {
		t.Errorf("after 7 on line 2: ids %v, want line 1 still %d and line 2 a new line (not the removed %d)", now, ids[1], ids[2])
	}
	if got, want := w.s2Lines(id, po), []string{s2Want(t, 1, "1000", "0", "", ids[1]), s2Want(t, 2, "7", "0", "", now[2])}; !slices.Equal(got, want) {
		t.Errorf("after 7 on line 2: lines %q, want %q", got, want)
	}

	r = w.s2Save(id, w.s2Items(id), s2Set{"received", 1, ""}, s2Set{"received", 2, "8"})
	s2WantStep(t, r, "/edit/check", "save blank and 8")
	if got, want := w.s2Lines(id, po), []string{s2Want(t, 2, "8", "0", "", now[2])}; !slices.Equal(got, want) {
		t.Errorf("after blank and 8: lines %q, want %q (line 2 keeps id %d, line 1 removed)", got, want, now[2])
	}
}

// UX §5.4 Some rejected: Rejected and Reason for rejecting save on the
// card's line; afterwards that card's disclosure is open with both values
// and the computed "Accepted {x} {uom}", while a card without rejects stays
// closed and shows no Accepted line.
func TestGRNWeb_ItemsRejected(t *testing.T) {
	w := newS1World(t)
	po := w.s2RefPO()
	id := w.draft(po)
	ids := w.s2LineIDs(id, po)

	r := w.s2Save(id, w.s2Items(id), s2Set{"rejected", 1, "10"}, s2Set{"reason", 1, "Bent pieces"})
	s2WantStep(t, r, "/edit/check", "save 10 rejected")
	want := []string{s2Want(t, 1, "1200", "10", "Bent pieces", ids[1]), s2Want(t, 2, "50", "0", "", ids[2])}
	if got := w.s2Lines(id, po); !slices.Equal(got, want) {
		t.Errorf("lines %q, want %q", got, want)
	}

	r = w.s2Items(id)
	one, two := r.s2Card(t, 1), r.s2Card(t, 2)
	if d := s2Disclosure(one, "Some rejected"); d.Length() != 1 || !s2Open(d) {
		t.Errorf("card 1's Some rejected isn't open with 10 rejected")
	}
	if got := r.s2Value("rejected", 1); got != "10" {
		t.Errorf("card 1 Rejected = %q, want 10", got)
	}
	if got := r.s2Value("reason", 1); got != "Bent pieces" {
		t.Errorf("card 1 Reason for rejecting = %q, want Bent pieces", got)
	}
	if !strings.Contains(s2Norm(one.Text()), "Accepted 1,190 KGS") {
		t.Errorf("card 1 has no Accepted 1,190 KGS: %q", s2Norm(one.Text()))
	}
	if d := s2Disclosure(two, "Some rejected"); d.Length() != 1 || s2Open(d) {
		t.Errorf("card 2's Some rejected is open with nothing rejected")
	}
	if strings.Contains(s2Norm(two.Text()), "Accepted ") {
		t.Errorf("card 2 shows an Accepted line with nothing rejected: %q", s2Norm(two.Text()))
	}
}

// GUX-011 (hooks spec §4.1, CD-263 G6, HOOK-017; UX §5.4): Receive all
// pending and Clear all are secondary submits in the step form posting
// nav=stay to a formaction with fill=all / fill=clear, busy labels
// Filling… and Clearing…. Without JavaScript each saves (version + 1) and
// answers 303 back to step 3. Receive all pending sets every card with
// pending above 0 to its pending, keeping the rejects typed on those lines,
// and leaves a fully received line alone; Clear all saves a non-nil empty
// line set, so the draft has no lines and every card shows blank.
func TestGRNWeb_ItemsFill(t *testing.T) {
	w := newS1World(t)
	po := w.s2PO(s2Line{"MS-ANGLE-50", "KGS", "1200"}, s2Line{"WIRE-18G", "KGS", "50"}, s2Line{"MS-ANGLE-50", "KGS", "9"})
	w.s2Approve(po, map[int]string{3: "9"})
	id := w.draft(po)
	ids := w.s2LineIDs(id, po)

	s2WantStep(t, w.s2Save(id, w.s2Items(id), s2Set{"received", 1, "600"}, s2Set{"rejected", 1, "10"},
		s2Set{"reason", 1, "Bent pieces"}, s2Set{"received", 2, "0"}), "/edit/check", "save 600 with 10 rejected")

	r := w.s2Items(id)
	for _, b := range []struct{ label, fill, busy string }{
		{"Receive all pending", "fill=all", "Filling…"},
		{"Clear all", "fill=clear", "Clearing…"},
	} {
		btn := r.s2Button(b.label)
		if btn.Length() != 1 {
			t.Fatalf("%d %q buttons in the step form, want 1", btn.Length(), b.label)
		}
		if typ := btn.AttrOr("type", "submit"); typ != "submit" || btn.AttrOr("name", "") != "nav" || btn.AttrOr("value", "") != "stay" ||
			!strings.Contains(btn.AttrOr("formaction", ""), b.fill) || btn.AttrOr("data-busy-label", "") != b.busy {
			t.Errorf("%q: type %q name %q value %q formaction %q busy %q; want a submit posting nav=stay to …%s, busy %s", b.label,
				typ, btn.AttrOr("name", ""), btn.AttrOr("value", ""), btn.AttrOr("formaction", ""), btn.AttrOr("data-busy-label", ""), b.fill, b.busy)
		}
	}

	v := w.s2Version(id)
	s2WantStep(t, w.s2Press(id, r, "Receive all pending"), "/edit/items", "Receive all pending")
	got, now := w.s2Lines(id, po), w.s2LineIDs(id, po)
	if want := []string{s2Want(t, 1, "1200", "10", "Bent pieces", ids[1]), s2Want(t, 2, "50", "0", "", now[2])}; !slices.Equal(got, want) {
		t.Errorf("after Receive all pending: lines %q, want %q", got, want)
	}
	if after := w.s2Version(id); after != v+1 {
		t.Errorf("Receive all pending: version %d, want %d", after, v+1)
	}

	v = w.s2Version(id)
	s2WantStep(t, w.s2Press(id, w.s2Items(id), "Clear all"), "/edit/items", "Clear all")
	if got := w.s2Lines(id, po); len(got) != 0 {
		t.Errorf("after Clear all: lines %q, want none", got)
	}
	if after := w.s2Version(id); after != v+1 {
		t.Errorf("Clear all: version %d, want %d", after, v+1)
	}
	r = w.s2Items(id)
	if n := len(r.s2Cards()); n != 3 {
		t.Errorf("after Clear all step 3 shows %d cards, want all 3", n)
	}
	for n := 1; n <= 3; n++ {
		if got := r.s2Value("received", n); got != "" || r.s2Input("received", n).Length() != 1 {
			t.Errorf("after Clear all card %d Received now = %q, want one blank input", n, got)
		}
	}

	s2WantStep(t, w.s2Press(id, r, "Receive all pending"), "/edit/items", "Receive all pending after Clear all")
	now = w.s2LineIDs(id, po)
	if got, want := w.s2Lines(id, po), []string{s2Want(t, 1, "1200", "0", "", now[1]), s2Want(t, 2, "50", "0", "", now[2])}; !slices.Equal(got, want) {
		t.Errorf("Receive all pending after Clear all: lines %q, want %q", got, want)
	}
}

// UX §5.4 Line numbers ("S2's test pins what a Received quantities save on
// a closed PO shows"): once Priya closes the PO, a step 3 save is refused
// (422, nothing written) with DOC-017's closed-source text, the PO named by
// number, and never moves on.
func TestGRNWeb_ItemsClosedPO(t *testing.T) {
	w := newS1World(t)
	po := w.s2RefPO()
	id := w.draft(po)
	r := w.s2Items(id)
	w.ClosePO(t, w.Priya, po.ID, "Vendor stopped supplying")
	before, v := w.RowCounts(t), w.s2Version(id)

	res := w.s2Save(id, r, s2Set{"received", 1, "5"})
	if res.status != http.StatusUnprocessableEntity {
		t.Fatalf("save on a closed PO = %d %q, want 422", res.status, res.location)
	}
	closed := po.Number + " is closed. Remove the line or pick another purchase order."
	msgs := strings.Split(res.errorSummary(), " | ")
	if len(msgs) == 0 || msgs[0] == "" {
		t.Errorf("save on a closed PO: no error in the summary")
	}
	for _, m := range msgs {
		if !strings.HasSuffix(m, closed) {
			t.Errorf("summary message %q, want DOC-017's %q", m, closed)
		}
	}
	if after := w.RowCounts(t); after != before || w.s2Version(id) != v {
		t.Errorf("save on a closed PO wrote: rows %+v → %+v, version %d → %d", before, after, v, w.s2Version(id))
	}
	if got := res.s2Value("received", 1); got != "5" {
		t.Errorf("the 422 redraw lost card 1's typed 5: %q", got)
	}
}

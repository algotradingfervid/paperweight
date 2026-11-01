package grnweb_test

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// MOD-GRN S2, step 3's errors and warnings (UX doc §5.4 Line numbers,
// Errors in place, Over-delivery warning, Hard stop; AD-12 P2, CD-264 (8),
// CD-277 (4), CD-282 R1, CD-286). Every line text names the PO line, so
// "Line 3:" sits next to card 3 even when only PO line 3 is posted. Suresh
// on the real auth.Service and engine (CD-213).

// s2NumText is NUM's parse text for s (types.ParseQty, as docscreen's
// ToPlain shows it), with "Line {n}: " (CD-264 (8)).
func s2NumText(t *testing.T, n int, s string) string {
	t.Helper()
	_, err := types.ParseQty(s)
	if err == nil {
		t.Fatalf("ParseQty(%q) reads it", s)
	}
	return "Line " + strconv.Itoa(n) + ": " + err.Error()
}

// GUX-016's second half and UX §5.4 Line numbers (CD-277 (4), AD-12 P2):
// with only PO line 3 (BOLT-M12, NOS, numeric keypad) given a quantity,
// step 3's Save refuses 1.5 with uom.CheckLineQty's whole-number text and
// a quantity or rejected NUM can't read with NUM's text, each starting
// "Line 3: " (never the posted position 1), shown under card 3's input and
// linked to it from the summary (R1); nothing is written and every typed
// value is redrawn. Two bad cards give one error each, in place.
func TestGRNWeb_ItemsParseErrors(t *testing.T) {
	w := newS1World(t)
	po := w.s2PO(s2Line{"MS-ANGLE-50", "KGS", "100"}, s2Line{"WIRE-18G", "KGS", "50"}, s2Line{"BOLT-M12", "NOS", "10"})
	id := w.draft(po)
	page := w.s2Items(id)
	if got := page.s2Input("received", 3).AttrOr("inputmode", ""); got != "numeric" {
		t.Errorf("card 3 (NOS) inputmode = %q, want numeric", got)
	}
	if got := s2Norm(page.s2Input("received", 3).Closest(".field__control--lg").Find(".field__suffix").Text()); got != "NOS" {
		t.Errorf("card 3 suffix = %q, want NOS", got)
	}
	before, v, lines := w.RowCounts(t), w.s2Version(id), w.s2Lines(id, po)

	type wantErr struct {
		msg, key, field string
		card            int
	}
	whole := "Line 3: Quantity 1.5 can't be a fraction for UoM NOS. Enter a whole number."
	for _, c := range []struct {
		name string
		sets []s2Set
		want []wantErr
	}{
		{"fraction for NOS", []s2Set{{"received", 1, ""}, {"received", 2, ""}, {"received", 3, "1.5"}},
			[]wantErr{{whole, "received", "received_qty", 3}}},
		{"received NUM can't read", []s2Set{{"received", 1, ""}, {"received", 2, ""}, {"received", 3, "abc"}},
			[]wantErr{{s2NumText(t, 3, "abc"), "received", "received_qty", 3}}},
		{"rejected NUM can't read", []s2Set{{"received", 1, ""}, {"received", 2, ""}, {"received", 3, "5"}, {"rejected", 3, "x"}},
			[]wantErr{{s2NumText(t, 3, "x"), "rejected", "rejected_qty", 3}}},
		{"two bad cards", []s2Set{{"received", 1, "abc"}, {"received", 2, ""}, {"received", 3, "1.5"}},
			[]wantErr{{s2NumText(t, 1, "abc"), "received", "received_qty", 1}, {whole, "received", "received_qty", 3}}},
	} {
		// One world for every case (each refusal writes nothing), so no
		// subtests: the helpers report on this test's t.
		func() {
			r := w.s2Save(id, page, c.sets...)
			if r.status != http.StatusUnprocessableEntity {
				t.Errorf("%s: save = %d %q, want 422", c.name, r.status, r.location)
				return
			}
			var msgs []string
			for _, e := range c.want {
				msgs = append(msgs, e.msg)
				s2WantLinkIn(t, r, e.msg, e.key, e.field, e.card)
			}
			if got := strings.Split(r.errorSummary(), " | "); !slices.Equal(got, msgs) {
				t.Errorf("%s: summary %q, want %q", c.name, got, msgs)
			}
			for _, s := range c.sets {
				if got := r.s2Value(s.key, s.line); got != s.val {
					t.Errorf("%s: redraw: card %d %s = %q, want the typed %q", c.name, s.line, s.key, got, s.val)
				}
			}
			if d := s2Disclosure(r.s2Card(t, 3), "Some rejected"); c.want[0].key == "rejected" && !s2Open(d) {
				t.Errorf("%s: card 3's Some rejected is closed while its Rejected has an error", c.name)
			}
			s2WantNoHidden(t, r, c.name+" 422 redraw")
			if after := w.RowCounts(t); after != before || w.s2Version(id) != v || !slices.Equal(w.s2Lines(id, po), lines) {
				t.Errorf("%s: a refused save wrote: rows %+v → %+v, version %d → %d", c.name, before, after, v, w.s2Version(id))
			}
		}()
	}
}

// GUX-014, GUX-016's first half and GUX-036 (R1, CD-286; GRN-008, GRN-043):
// the engine's line refusals on a step 3 save link to the card's own input
// by M0-26's per-line id of the posted position and show under it:
// rejected above received on line 1, and on PO line 3 when it is the only
// line posted ("Line 3:", in card 3, the id of position 1); accepted above
// twice the ordered quantity under card 1's Received now. Typed values on
// every card survive the 422 and nothing is written; exactly twice saves.
func TestGRNWeb_ItemsLineErrorLinks(t *testing.T) {
	t.Run("GUX-014 rejected above received", func(t *testing.T) {
		w := newS1World(t)
		po := w.s2RefPO()
		id := w.draft(po)
		lines := w.s2Lines(id, po)
		r := w.s2Save(id, w.s2Items(id), s2Set{"received", 1, "10"}, s2Set{"rejected", 1, "20"}, s2Set{"received", 2, "33"})
		msg := "Line 1: Rejected can't be more than received."
		if r.status != http.StatusUnprocessableEntity || r.errorSummary() != msg {
			t.Fatalf("save = %d %q, want 422 with %q", r.status, r.errorSummary(), msg)
		}
		if got := s2WantLinkIn(t, r, msg, "rejected", "rejected_qty", 1); got != docscreen.LineFieldID(1, "rejected_qty") {
			t.Errorf("link id %q, want %q", got, docscreen.LineFieldID(1, "rejected_qty"))
		}
		if !s2Open(s2Disclosure(r.s2Card(t, 1), "Some rejected")) {
			t.Errorf("card 1's Some rejected is closed with an error in it")
		}
		for _, s := range []s2Set{{"received", 1, "10"}, {"rejected", 1, "20"}, {"received", 2, "33"}} {
			if got := r.s2Value(s.key, s.line); got != s.val {
				t.Errorf("redraw: card %d %s = %q, want %q", s.line, s.key, got, s.val)
			}
		}
		if got := w.s2Lines(id, po); !slices.Equal(got, lines) {
			t.Errorf("the refused save wrote lines %q, want %q", got, lines)
		}
	})

	t.Run("GUX-016 only PO line 3 posted", func(t *testing.T) {
		w := newS1World(t)
		po := w.s2PO(s2Line{"MS-ANGLE-50", "KGS", "100"}, s2Line{"WIRE-18G", "KGS", "50"}, s2Line{"BOLT-M12", "NOS", "10"})
		id := w.draft(po)
		r := w.s2Save(id, w.s2Items(id), s2Set{"received", 1, ""}, s2Set{"received", 2, ""}, s2Set{"received", 3, "5"}, s2Set{"rejected", 3, "10"})
		msg := "Line 3: Rejected can't be more than received."
		if r.status != http.StatusUnprocessableEntity || r.errorSummary() != msg {
			t.Fatalf("save = %d %q, want 422 with %q", r.status, r.errorSummary(), msg)
		}
		if got := s2WantLinkIn(t, r, msg, "rejected", "rejected_qty", 3); got != docscreen.LineFieldID(1, "rejected_qty") {
			t.Errorf("link id %q, want %q: PO line 3 is posted line 1", got, docscreen.LineFieldID(1, "rejected_qty"))
		}
		if !s2Open(s2Disclosure(r.s2Card(t, 3), "Some rejected")) {
			t.Errorf("card 3's Some rejected is closed with an error in it")
		}
		s2WantUniqueIDs(t, r, "422 redraw")
	})

	t.Run("GUX-036 twice the ordered quantity", func(t *testing.T) {
		w := newS1World(t)
		po := w.s2RefPO()
		id := w.draft(po)
		lines := w.s2Lines(id, po)
		r := w.s2Save(id, w.s2Items(id), s2Set{"received", 1, "2,400.010"}, s2Set{"received", 2, "33"})
		msg := "Line 1: Accepted 2,400.01 KGS is more than twice the 1,200 KGS ordered. Check the quantity."
		if r.status != http.StatusUnprocessableEntity || r.errorSummary() != msg {
			t.Fatalf("save = %d %q, want 422 with %q", r.status, r.errorSummary(), msg)
		}
		s2WantLinkIn(t, r, msg, "received", "received_qty", 1)
		if got := r.s2Value("received", 2); got != "33" {
			t.Errorf("redraw: card 2 = %q, want the typed 33", got)
		}
		if got := w.s2Lines(id, po); !slices.Equal(got, lines) {
			t.Errorf("the refused save wrote lines %q, want %q", got, lines)
		}

		s2WantStep(t, w.s2Save(id, w.s2Items(id), s2Set{"received", 1, "2,400.000"}), "/edit/check", "save exactly twice")
		warn := "Line 1: Accepted 2,400 KGS is more than the 1,200 KGS pending on " + po.Number + "."
		if got := s2Norm(w.s2Items(id).s2Card(t, 1).Find(".banner--warning").Text()); !strings.Contains(got, warn) {
			t.Errorf("card 1 warning = %q, want %q", got, warn)
		}
	})
}

// GUX-013 (GRN-008, R1): 410 received with 10 rejected and no reason saves
// (Required is a submit rule, CD-178); the submit is refused (422) back on
// Received quantities with "Line 1: Reason for rejecting is required."
// linked from the summary to card 1's reason input, shown under it, and
// card 1's Some rejected open. The receipt stays a draft.
func TestGRNWeb_ItemsReasonOnSubmit(t *testing.T) {
	w := newS1World(t)
	po := w.s2RefPO()
	id := w.draft(po)
	w.s2Delivery(id)
	s2WantStep(t, w.s2Save(id, w.s2Items(id), s2Set{"received", 1, "410"}, s2Set{"rejected", 1, "10"}, s2Set{"reason", 1, ""}),
		"/edit/check", "save 410 with 10 rejected, no reason")

	r := w.post(w.Suresh, "/doc/"+goodsreceipt.DocType+"/"+strconv.FormatInt(id, 10)+"/submit",
		url.Values{"version": {strconv.Itoa(w.s2Version(id))}})
	msg := "Line 1: Reason for rejecting is required."
	if r.status != http.StatusUnprocessableEntity || s2Norm(r.doc.Find("h2#step-title").Text()) != "Received quantities" || r.errorSummary() != msg {
		t.Fatalf("submit = %d on %q with %q, want 422 on Received quantities with %q", r.status, s2Norm(r.doc.Find("h2#step-title").Text()), r.errorSummary(), msg)
	}
	if got := s2WantLinkIn(t, r, msg, "reason", "reject_reason", 1); got != docscreen.LineFieldID(1, "reject_reason") {
		t.Errorf("link id %q, want %q", got, docscreen.LineFieldID(1, "reject_reason"))
	}
	if !s2Open(s2Disclosure(r.s2Card(t, 1), "Some rejected")) {
		t.Errorf("card 1's Some rejected is closed while its reason has an error")
	}
	if g := w.Load(t, goodsreceipt.DocType, id); g.State != doctype.StateDraft {
		t.Errorf("state %s after the refused submit, want draft", g.State)
	}
}

// GUX-015, the step 3 part (GRN-013, CD-234, CD-238; UX §5.4 Over-delivery
// warning): with pending 400 on line 1, 450 typed into card 1 warns on card
// 1 with the PO line number and the PO's number, exactly once on the page,
// on a 422 redraw from the posted values (the stored 400 doesn't warn) and
// on every GET after it is saved; the save itself goes on. 450 with 50
// rejected doesn't warn anywhere.
func TestGRNWeb_ItemsWarnings(t *testing.T) {
	w := newS1World(t)
	po := w.s2RefPO()
	w.s2Approve(po, map[int]string{1: "800"})
	id := w.draft(po)
	warn := "Line 1: Accepted 450 KGS is more than the 400 KGS pending on " + po.Number + "."
	wantWarn := func(r s1Page, what string) {
		t.Helper()
		if got := s2Norm(r.s2Card(t, 1).Find(".banner--warning").Text()); !strings.Contains(got, warn) {
			t.Errorf("%s: card 1 warning = %q, want %q", what, got, warn)
		}
		if n := strings.Count(r.text(), warn); n != 1 {
			t.Errorf("%s: the warning shows %d times, want once", what, n)
		}
		if n := r.s2Card(t, 2).Find(".banner--warning").Length(); n != 0 {
			t.Errorf("%s: card 2 warns", what)
		}
	}
	if strings.Contains(w.s2Items(id).text(), "is more than the") {
		t.Errorf("the prefilled pending warns")
	}

	r := w.s2Save(id, w.s2Items(id), s2Set{"received", 1, "450"}, s2Set{"received", 2, "5"}, s2Set{"rejected", 2, "10"})
	if r.status != http.StatusUnprocessableEntity {
		t.Fatalf("save with line 2 rejected above received = %d, want 422", r.status)
	}
	wantWarn(r, "422 redraw")

	s2WantStep(t, w.s2Save(id, w.s2Items(id), s2Set{"received", 1, "450"}), "/edit/check", "save 450")
	wantWarn(w.s2Items(id), "GET after the save")

	s2WantStep(t, w.s2Save(id, w.s2Items(id), s2Set{"rejected", 1, "50"}, s2Set{"reason", 1, "Bent pieces"}), "/edit/check", "save 450 with 50 rejected")
	if r := w.s2Items(id); strings.Contains(r.text(), "is more than the") || r.doc.Find("#wizard .banner--warning").Length() != 0 {
		t.Errorf("450 with 50 rejected warns: %q", s2Norm(r.doc.Find("#wizard .banner--warning").Text()))
	}
}

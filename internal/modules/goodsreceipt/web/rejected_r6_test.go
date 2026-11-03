package grnweb_test

import (
	"strings"
	"testing"
)

// CD-387 (8), GRN review H1: Rejected shows its placeholder, never a 0, when
// nothing is rejected: on a fresh draft and after a save with Rejected blank
// or typed as 0. Blank saves as 0, as it always did. A real reject shows, and
// a typed value survives a redraw exactly as typed.
func TestItems_RejectedBlankWhenNone(t *testing.T) {
	w := newS1World(t)
	po := w.s2RefPO()
	id := w.draft(po)
	if got := w.s2Items(id).s2Value("rejected", 1); got != "" {
		t.Fatalf("fresh draft: Rejected = %q, want blank", got)
	}
	for _, typed := range []string{"", "0"} {
		w.s2Save(id, w.s2Items(id), s2Set{"received", 1, "10"}, s2Set{"rejected", 1, typed}, s2Set{"reason", 1, ""})
		if got := w.s2Lines(id, po); len(got) == 0 || !strings.HasPrefix(got[0], "po1=10/0/") {
			t.Errorf("rejected %q: stored %v, want 0 rejected on line 1", typed, got)
		}
		r := w.s2Items(id)
		if got := r.s2Value("rejected", 1); got != "" {
			t.Errorf("after a save with %q: Rejected shows %q, want blank", typed, got)
		}
		if got := r.s2Input("rejected", 1).AttrOr("placeholder", ""); got != "e.g. 10" {
			t.Errorf("after a save with %q: placeholder = %q", typed, got)
		}
	}
	w.s2Save(id, w.s2Items(id), s2Set{"received", 1, "10"}, s2Set{"rejected", 1, "2"}, s2Set{"reason", 1, "Bent"})
	if got := w.s2Items(id).s2Value("rejected", 1); got != "2" {
		t.Errorf("a real reject shows %q, want 2", got)
	}
	// A 422 keeps what was typed, a 0 included.
	r := w.s2Save(id, w.s2Items(id), s2Set{"received", 1, "x"}, s2Set{"rejected", 1, "0"})
	if r.status == 200 {
		t.Fatalf("status %d, want the 422 redraw", r.status)
	}
	if got := r.s2Value("rejected", 1); got != "0" {
		t.Errorf("422 redraw: Rejected = %q, want the typed 0", got)
	}
}

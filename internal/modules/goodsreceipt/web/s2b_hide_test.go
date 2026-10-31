package grnweb_test

import (
	"net/http"
	"slices"
	"testing"
)

// MOD-GRN S2b: CD-319 on step 3. A card field the Module's Hide hides (here
// another Hide wrapped round it, as the tooling redraw sweep's probe does)
// is neither drawn on any card nor read from the post: a crafted value
// leaves the stored one as it was. The other two fields stay.
func TestGRNWeb_ItemsHonourHide(t *testing.T) {
	keys := map[string]string{"received": "received_qty", "rejected": "rejected_qty", "reason": "reject_reason"}
	crafted := map[string]string{"received": "900", "rejected": "7", "reason": "Crafted"}
	for key, name := range keys {
		t.Run(name, func(t *testing.T) {
			w := newS1World(t)
			po := w.s2RefPO()
			id := w.draft(po)
			before := w.s2Items(id)
			field := before.s2Input(key, 1).AttrOr("name", "")
			r := w.s2Save(id, before, s2Set{"received", 1, "1000"}, s2Set{"rejected", 1, "5"}, s2Set{"reason", 1, "Bent"})
			s2WantStep(t, r, "/edit/check", "save before hiding")
			stored := w.s2Lines(id, po)

			w.hidden = map[string]bool{name: true}
			p := w.s2Items(id)
			for other := range keys {
				for n := 1; n <= 2; n++ {
					want := 1
					if other == key {
						want = 0
					}
					if got := p.s2Input(other, n).Length(); got != want {
						t.Errorf("card %d draws %d %s inputs with %s hidden, want %d", n, got, other, name, want)
					}
				}
			}
			f := p.s2Form()
			f.Set(field, crafted[key])
			if r := w.post(w.Suresh, stepPath(id, "items"), f); r.status != http.StatusSeeOther {
				t.Fatalf("save with a crafted hidden %s = %d %q, want 303", name, r.status, r.errorSummary())
			}
			if got := w.s2Lines(id, po); !slices.Equal(got, stored) {
				t.Errorf("save with %s hidden wrote lines %q, want %q kept", name, got, stored)
			}
		})
	}
}

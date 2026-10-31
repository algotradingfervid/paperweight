//go:build e2e

package e2e

import (
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// notYetSwept are module DocTypes registered before their module's Screens
// rows, by the head's ruling, each with who removes it. Each entry must name
// a registered, unswept DocType, so it can't outlive its reason.
var notYetSwept = map[string]string{
	"purchase.Order":   "AD-13 MOD-PO P16 sweep (CD-298)",
	"purchase.Request": "AD-13 MOD-PO P16 sweep (CD-298)",
}

// TestModule_EveryDocTypeSwept (hooks spec §6) is the gate screens.Uncovered
// can't be: that matches route patterns only, so a module DocType whose pages
// share fixture.Order's routes would pass unswept. Every document DocType
// registered in the app that isn't TestOnly (CD-295) must have a screen on
// each route docscreen.Screens gives it (docscreen.Unswept). TestOnly
// DocTypes may be partly swept: the fixture DocTypes share fixture.Order's
// screens, and fixture.Order, fully swept, is the floor. Test-only DocTypes
// of other packages (docscreentest.*, m04.*) aren't registered in this
// binary; docscreentest.Hooked is swept in the gallery
// (TestGallery_EveryDocTypeSwept).
func TestModule_EveryDocTypeSwept(t *testing.T) {
	h := Start(t)
	list := h.appScreens(h.prepareSweep())

	var docs []string
	for _, dt := range doctype.All() {
		if dt.IsDocument() {
			docs = append(docs, dt.Name)
		}
	}
	missing := docscreen.Unswept(list)
	if !slices.Contains(docs, fixture.Order) || slices.Contains(missing, fixture.Order) {
		t.Errorf("fixture.Order registered %v, unswept %v; want it registered and fully swept, the floor of this check",
			slices.Contains(docs, fixture.Order), slices.Contains(missing, fixture.Order))
	}
	for n, owner := range notYetSwept {
		switch {
		case !slices.Contains(docs, n):
			t.Errorf("notYetSwept names %s (%s), which isn't a registered document DocType: take it off", n, owner)
		case !slices.Contains(missing, n):
			t.Errorf("%s is swept now: take it off notYetSwept (%s)", n, owner)
		}
	}
	for _, n := range missing {
		dt, _ := doctype.Lookup(n)
		if _, later := notYetSwept[n]; later || dt.TestOnly {
			continue
		}
		t.Errorf("document DocType %s is registered but not swept: its module must add docscreen.Screens(%q, ids) to the sweep", n, n)
	}
	t.Logf("document DocTypes: %q; unswept: %q", docs, missing)
}

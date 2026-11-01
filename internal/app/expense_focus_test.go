package app_test

import (
	"net/http"
	"testing"
)

// CD-352 (4), N12: the Mark as paid and Undo paid pages have their own
// action footer, so they get no bottom nav; expense's report and report
// filter pages have none and keep it (as in TestBottomNav_FocusPages).
func TestExpFocus_PaidPagesHaveNoBottomNav(t *testing.T) {
	e := newExpWorld(t)
	unpaid := e.advance("8000.00")
	paid := e.advance("2000.00")
	e.paid(paid.ID)
	meena := e.signedIn(meenaEmail)
	for _, c := range []struct {
		path string
		nav  bool
	}{
		{"/reports/expense/advances", true},
		{"/reports/expense/advances/filter", true},
		{paidPath(unpaid.ID, "paid"), false},
		{paidPath(paid.ID, "unpaid"), false},
	} {
		r := meena.get(c.path)
		if r.status != http.StatusOK {
			t.Errorf("GET %s = %d", c.path, r.status)
			continue
		}
		if got := r.doc.Find("nav.bottomnav").Length() == 1; got != c.nav {
			t.Errorf("%s: bottom nav shown = %v, want %v", c.path, got, c.nav)
		}
	}
}

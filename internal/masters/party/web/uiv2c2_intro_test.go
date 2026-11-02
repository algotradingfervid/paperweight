package partyweb_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
)

// TestScreens_PartyWizardIntros (UI v2 §5.6): each party step has its intro
// under the stepper, from its FormStep; Check has none.
func TestScreens_PartyWizardIntros(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	intro := func(r webtest.Response) string { return strings.TrimSpace(r.Doc.Find("#wizard > p.step-intro").Text()) }

	first := b.Get(t, wizardMain)
	webtest.WantStatus(t, first, http.StatusOK)
	if got := intro(first); got != "A party is a company you buy from or sell to. Nothing is saved until the last step." {
		t.Errorf("details intro = %q", got)
	}
	addrs := b.PostHTMX(t, stepDetails, details("MEHTA"))
	if got := intro(addrs); got != "Add the addresses that go on orders and invoices." {
		t.Errorf("addresses intro = %q", got)
	}
	contacts := b.PostHTMX(t, stepAddresses, rows(mumbaiRow))
	if got := intro(contacts); got != "Add the people you deal with there, if any." {
		t.Errorf("contacts intro = %q", got)
	}
	check := b.PostHTMX(t, stepContacts, rows(sunilRow))
	if n := check.Doc.Find(".step-intro").Length(); n != 0 {
		t.Errorf("Check has %d intros, want none", n)
	}
}

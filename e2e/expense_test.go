//go:build e2e

package e2e

import (
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// MOD-EXP S5, M0-36 / CD-307 at 360 px: an unpaid advance's blank Paid on.
// Its page reads "Paid on  Not paid yet" (EUX-040), and its Open advances
// row, in a list with no state column, draws no Paid on cell and no stray
// separator: none doubled where the blank cell was, none at either end of
// the card. The paid advance's row shows its Paid on, so the unpaid row
// can't pass for want of the column. Both rows carry the longest employee
// name and the widest money; the page doesn't scroll sideways.
func TestE2E_ExpenseUnpaidAdvanceAt360(t *testing.T) {
	h := Start(t)
	long := testkit.SeedLongValues(t, h.App.DB(), h.Fx, testkit.LongValues{
		Name: uitest.LongName100, Devanagari: uitest.LongDevanagari, Code60: uitest.LongCode60, Email: uitest.LongEmail,
	})
	exp, unpaid := h.expenseSamples(long)
	paid := exp[expense.AdvanceDocType].Approved

	s := h.SignIn(meena, phone360)
	noSideScroll := func(where string) {
		t.Helper()
		if Eval[bool](s, `document.documentElement.scrollWidth > document.documentElement.clientWidth`) {
			t.Errorf("%s scrolls sideways at 360 px", where)
		}
	}

	s.Open(DocPath(expense.AdvanceDocType, unpaid))
	s.WaitText("Paid on Not paid yet")
	noSideScroll("the unpaid advance's page")

	s.Open("/reports/expense/advances")
	row := func(id int64) string {
		t.Helper()
		sel := strconv.Quote(`a[href="` + DocPath(expense.AdvanceDocType, id) + `"]`)
		s.Until("the row of advance "+strconv.FormatInt(id, 10), `document.querySelector(`+sel+`) !== null`)
		return strings.Join(strings.Fields(Eval[string](s, `document.querySelector(`+sel+`).innerText`)), " ")
	}
	if got := row(paid); !strings.Contains(got, "Paid on 24-10-2026") || !strings.Contains(got, uitest.LongName100) {
		t.Errorf("the paid advance's row = %q, want its Paid on 24-10-2026 and the long employee name", got)
	}
	got := row(unpaid)
	switch {
	case strings.Contains(got, "Paid on"):
		t.Errorf("the unpaid advance's row draws Paid on: %q", got)
	case strings.Contains(got, "· ·") || strings.Contains(got, "··"):
		t.Errorf("the unpaid advance's row has a doubled separator: %q", got)
	case strings.HasPrefix(got, "·") || strings.HasSuffix(got, "·"):
		t.Errorf("the unpaid advance's row starts or ends with a separator: %q", got)
	case !strings.Contains(got, uitest.LongName100):
		t.Errorf("the unpaid advance's row lacks the long employee name: %q", got)
	}
	noSideScroll("the Open advances report")
}

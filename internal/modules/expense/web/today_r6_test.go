package expenseweb_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// Expense UX §5.10: a new advance opens on today, as a new claim does.
func TestAdvanceNew_AdvanceDateDefaultsToToday(t *testing.T) {
	e := s2New(t)
	page := e.page(t, s2Deepak, "/doc/"+expense.AdvanceDocType+"/new")
	if got := page.Find("input#"+components.FieldID("advance_date")).AttrOr("value", ""); got != s2Today {
		t.Errorf("new advance's date = %q, want today %q", got, s2Today)
	}
}

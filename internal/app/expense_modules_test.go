package app

import (
	"testing"

	"github.com/vardhan/internaltools/internal/modules/expense"
)

// TestDocDeps_ExpenseModules: the generic screens get both Expense Modules
// (plan MOD-EXP S0), so S1 to S3's hooks reach the pages.
func TestDocDeps_ExpenseModules(t *testing.T) {
	mods := (&App{}).docDeps().Modules
	for _, dt := range []string{expense.ClaimDocType, expense.AdvanceDocType} {
		if _, ok := mods[dt]; !ok {
			t.Errorf("docDeps().Modules has no entry for %s", dt)
		}
	}
}

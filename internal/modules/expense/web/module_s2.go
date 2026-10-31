package expenseweb

import "github.com/vardhan/internaltools/internal/ui/docscreen"

// S2 (Expense line page) fills this in; nil keeps the generic line page.

// claimLine is the claim's custom line page.
func claimLine(_ Deps) *docscreen.Line { return nil }

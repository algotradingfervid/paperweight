package expenseweb

import (
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// S3 (Pages) fills these in; nil keeps the generic screen.

// claimPage is the claim's document page additions.
func claimPage(_ Deps) func(docscreen.Ctx) (docscreen.PageExtra, error) { return nil }

// claimReason is the claim's reason page additions.
func claimReason(_ Deps) func(docscreen.Ctx, doctype.Action) (docscreen.ReasonExtra, error) {
	return nil
}

// advancePage is the advance's document page additions.
func advancePage(_ Deps) func(docscreen.Ctx) (docscreen.PageExtra, error) { return nil }

// advanceReason is the advance's reason page additions.
func advanceReason(_ Deps) func(docscreen.Ctx, doctype.Action) (docscreen.ReasonExtra, error) {
	return nil
}

// advanceHide hides the advance's fields a viewer doesn't need.
func advanceHide(_ Deps) func(docscreen.Ctx, doctype.Field) bool { return nil }

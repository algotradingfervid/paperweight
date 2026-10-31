package expenseweb

import (
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// S1 (Claim wizard) fills these in; nil keeps the generic screen.

// claimStepLabels are the claim's step labels.
func claimStepLabels(_ Deps) map[string]string { return nil }

// claimSteps are the claim's custom steps.
func claimSteps(_ Deps) map[string]docscreen.Step { return nil }

// claimHide hides the employee from non-Administrators.
func claimHide(_ Deps) func(docscreen.Ctx, doctype.Field) bool { return nil }

// claimLineCard is an expense line's card.
func claimLineCard(_ Deps) func(docscreen.Ctx, docscreen.LineView) components.LineCard {
	return nil
}

// claimCheck is the claim's check page additions.
func claimCheck(_ Deps) func(docscreen.Ctx) (docscreen.CheckExtra, error) { return nil }

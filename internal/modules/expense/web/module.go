package expenseweb

import "github.com/vardhan/internaltools/internal/ui/docscreen"

// module.go doesn't change after S0 (plan MOD-EXP, File ownership): each
// field calls a function in its owner's module_<task>.go, and a nil result
// keeps docscreen's generic screen.

// ClaimModule is the claim's additions to the generic screens.
func ClaimModule(d Deps) docscreen.Module {
	return docscreen.Module{
		StepLabels: claimStepLabels(d), // S1
		Steps:      claimSteps(d),      // S1
		Hide:       claimHide(d),       // S1
		LineCard:   claimLineCard(d),   // S1
		Check:      claimCheck(d),      // S1
		LineNoun:   claimLineNoun(),    // S1
		AddTop:     true,               // S1: Add an expense above the cards (UX §5.4)
		Line:       claimLine(d),       // S2
		Page:       claimPage(d),       // S3
		Reason:     claimReason(d),     // S3
		// Cards at every width (EUX-025), until M0-40 and S2's Line.
		CardsOnly: true,
		// Check card tiles (UI v2 C4c).
		StepIcons: map[string]string{"details": "receipt-indian-rupee"},
		KeyFacts:  claimKeyFacts, // UI v2 §5.13 item 3
		// The line under the stepper on each step (UI v2 C2).
		StepIntros: map[string]string{
			"details": "What the spending was for, the claim date and any advance to settle.",
			"items":   "Add one expense for each bill, with a photo of the receipt if you have one.",
			"check":   "Check the claim, then submit it for approval.",
		},
	}
}

// AdvanceModule is the advance's additions to the generic screens.
func AdvanceModule(d Deps) docscreen.Module {
	return docscreen.Module{
		Page:   advancePage(d),   // S3
		Reason: advanceReason(d), // S3
		Hide:   advanceHide(d),   // S3
		// The type's name and the line's note (UX §5.10); authorised for S3
		// by AD-11 (S3 review round 1).
		LineCard: advanceLineCard(d), // S3
		// 5 editable line cells would give the grid (CD-315); cards at
		// every width (EUX-025).
		CardsOnly: true,
		// Check card tiles (UI v2 C4c).
		StepIcons: map[string]string{"details": "hand-coins"},
		KeyFacts:  advanceKeyFacts, // UI v2 §5.13 item 3
		Steps:     advanceSteps(),  // R12
		Line:      advanceLine(),   // R12
		// The line under the stepper (UI v2 §5.6).
		StepIntros: map[string]string{"details": introAdvance},
	}
}

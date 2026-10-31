package expense

// Advance settlement texts (MOD-EXP E4), copied exactly from spec §5 and
// CD-271 G1 and G7. {advance} is naming.Label of the advance.
const (
	msgChooseAdvance        = "Choose the advance to set this against."
	msgAdvanceOtherEmployee = "%s belongs to another employee."
	msgAdvanceZero          = "Enter the amount to set against %s, or remove the advance."
	msgLessAboveTotal       = "Less advance can't be more than the total."
	msgLessAboveLeft        = "Less advance can't be more than the ₹%s left on %s."
	msgAdvanceNotPaid       = "%s hasn't been marked as paid yet."
	// msgAdvanceLeft is the G7 warning: employee, money left, advance (or
	// anotherAdvance when the viewer can't read it; CD-240, CD-248).
	msgAdvanceLeft = "%s still has ₹%s left on %s."
	anotherAdvance = "another advance"

	// msgAdvanceClosed is DOC-017's closed-source text (doc/messages.go
	// msgSourceClosed with the advance's lower-cased label), which doc doesn't
	// export. The engine accepts a closed advance an amendment's original held
	// (CD-272 Gap 2), so the claim's own check gives the same text.
	msgAdvanceClosed = "%s is closed. Remove the line or pick another employee advance."
)

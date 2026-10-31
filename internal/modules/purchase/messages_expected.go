package purchase

// Expected on messages, copied from PO spec §27.1 (one condition each,
// CD-147), DOC's unreadable-value wording for the date and DOC's stale
// wording with the PO's display (plan P18; DOC exports no constructor).
const (
	msgExpectedNotApproved = "Only an approved order can have an expected date."
	msgExpectedBlank       = "Enter the expected date."
	msgExpectedBeforeOrder = "Expected on can't be before the order date."
	msgExpectedUnreadable  = "Expected on isn't a valid value. Check it and try again."
	// msgExpectedNoteLong is DOC's length wording for the note (AD-13 ruling).
	msgExpectedNoteLong = "Note can be at most 200 characters."
	noteMaxLen          = 200 // characters of the note (spec §27.1)
	// msgExpectedStale is "Someone else changed {display} after you opened it.
	// Reload it and make your changes again."
	msgExpectedStale = "Someone else changed %s after you opened it. Reload it and make your changes again."

	// The audit actions, in History's words (§27.1); the set takes dd-mm-yyyy.
	auditExpectedSet     = "Expected on set to %s"
	auditExpectedCleared = "Expected on cleared"
)

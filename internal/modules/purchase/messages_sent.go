package purchase

// Mark as sent messages, copied from PO spec §23 (one condition each,
// CD-147), DOC's unreadable-value and length wordings (AD-13 rulings) and
// DOC's stale wording with the PO's display (DOC exports no constructor).
const (
	msgSentNotApproved = "Only an approved order can be marked as sent."
	msgSentFuture      = "Sent on can't be in the future."
	msgSentBeforeOrder = "Sent on can't be before the order date."
	msgSentNoMethod    = "Choose how it was sent."
	msgSentUnreadable  = "Sent on isn't a valid value. Check it and try again."
	msgSentNoteLong    = "Note can be at most 200 characters."
	sentNoteMaxLen     = 200 // characters of the note (spec §23)
	// msgSentStale is "Someone else changed {display} after you opened it.
	// Reload it and make your changes again."
	msgSentStale = "Someone else changed %s after you opened it. Reload it and make your changes again."

	// The audit actions on the PO (spec §23).
	auditSentMarked   = "Marked as sent"
	auditSentUnmarked = "Marked as not sent"
)

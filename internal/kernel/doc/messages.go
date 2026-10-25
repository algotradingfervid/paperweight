package doc

// Every engine string (plan copy rules, §6). Verbs: %s is a field label or
// display, %d a line number or count.
const (
	// Field validation (Task 3). Line errors are prefixed with msgLinePrefix.
	msgLinePrefix   = "Line %d: "
	msgUnreadable   = "%s isn't a valid value. Check it and try again."
	msgReadOnly     = "%s is calculated and can't be entered." // CD-79
	msgRequired     = "%s is required."
	msgNonNegative  = "%s can't be negative."
	msgPositive     = "%s must be more than 0."
	msgBetween      = "%s must be between %s and %s."
	msgAtLeast      = "%s must be at least %s."
	msgAtMost       = "%s must be at most %s."
	msgMaxLen       = "%s can be at most %d characters."
	msgLinkNotFound = "%s not found. Pick one from the list."
	// label, display, lower-cased label (CD-38, CD-68)
	msgLinkDisabled = "%s %s is disabled. Pick an active %s, or ask an admin to enable it."
	msgNoLines      = "Add at least one line."
	msgTooManyLines = "A document can have at most 500 lines. Split it into two documents."
	msgLineID       = "Line id" // label used when a line's "id" key can't be read

	// Engine pipeline (Task 6). %s is the document's display name.
	msgGone             = "This document no longer exists. It may have been deleted."
	msgStale            = "Someone else changed %s after you opened it. Reload it and make your changes again."
	msgAlreadySubmitted = "%s has already been submitted."
	msgFixFields        = "Fix the highlighted fields and save again."
	msgInternal         = "doc: internal error in %s %s: %v" // doctype, action, recovered panic

	// Save (Task 7). %d is the line's 1-based input position.
	msgForeignLine = "Line %d doesn't belong to this document. Reload and try again."

	// Transition guard (Task 8). The first %s is the document's display name.
	msgUnknownAction     = "%s can't be changed that way."
	msgNotWaiting        = "%s is not waiting for approval. It is %s."
	msgNoLongerWaiting   = "%s is no longer waiting for approval. It was %s by %s." // display, approved|rejected|withdrawn, Name
	msgNotSubmitted      = "%s hasn't been submitted, so there is nothing to withdraw."
	msgOnlyRejected      = "Only rejected documents can be revised."
	msgOnlyApprovedCanc  = "Only approved documents can be cancelled. Delete the draft instead."
	msgOnlyApprovedClose = "Only approved documents can be closed."
	msgOnlyCancelled     = "Only cancelled documents can be amended."
	msgCancelFirst       = "Only cancelled documents can be amended. Cancel %s first."
	msgPendingLocked     = "%s is waiting for approval and can't be changed. Withdraw it to make changes."
	msgPendingCancel     = "%s is waiting for approval. Withdraw it first if it needs changes."
	msgPendingDelete     = "%s is waiting for approval and can't be deleted. Withdraw it first, then delete it."
	msgApprovedLocked    = "%s is approved and can't be edited. Cancel and amend it to make changes."
	msgApprovedWithdraw  = "%s has already been approved and can't be withdrawn. Cancel and amend it to make changes."
	msgApprovedDelete    = "Approved documents can't be deleted. Cancel it instead."
	msgRejectedLocked    = "%s was rejected. Revise it to move it back to draft."
	msgRejectedDelete    = "Rejected documents can't be deleted. Revise it to move it back to draft, then delete it."
	msgClosed            = "%s is closed and can't be changed."
	msgCancelledLocked   = "%s is cancelled and can't be changed. Amend it to make a corrected copy."
	msgCancelledDelete   = "Cancelled documents are kept for the record and can't be deleted."

	// Approval transitions (Task 9).
	msgReasonReject  = "Enter a reason for rejecting."
	msgReasonTooLong = "Keep the reason under 1,000 characters."

	// Close, cancel, amend (Task 10). msgAmended: the source's display name,
	// the amendment's (CD-27).
	msgCloseReason  = "Enter a reason for closing (at least 3 characters)."
	msgCancelReason = "Enter a reason for cancelling (at least 3 characters)."
	msgReasonLong   = "Keep the reason under 500 characters."
	msgAmended      = "%s has already been amended as %s. Open that document instead."
)

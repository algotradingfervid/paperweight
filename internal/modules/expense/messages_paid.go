package expense

// Mark as paid and Undo paid texts, copied exactly from spec §4 (CD-281).
const (
	msgPaidNotApproved = "Only an approved advance can be marked as paid."
	msgPaidFuture      = "Paid on can't be in the future."
	msgPaidBeforeDate  = "Paid on can't be before the advance date."
	msgPaidAlready     = "This advance is already marked as paid."
	msgPaidClaimUses   = "A claim already uses this advance, so it stays paid."
	msgPaidNotPaid     = "This advance isn't marked as paid."
	msgPaidCancel      = "This advance is marked as paid. Undo paid first, or close it with the balance noted."

	// msgPaidStale is DOC's stale text (DOC-069, doc/messages.go msgStale), which
	// doc doesn't export; {advance} or {claim} is naming.Label. The receipt
	// upload (receipt_upload.go) uses it too.
	msgPaidStale = "Someone else changed %s after you opened it. Reload it and make your changes again."
)

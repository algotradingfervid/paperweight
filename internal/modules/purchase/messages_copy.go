package purchase

// Copy's notes, copied from PO spec §24.1 (one condition each, CD-147). They
// are joined by a newline into the new draft's copy_notes.
const (
	// msgCopiedFrom is "Copied from {display}. Rates are as on that order;
	// check them before you submit."
	msgCopiedFrom = "Copied from %s. Rates are as on that order; check them before you submit."
	// msgCopyBlank is "{Label} {code} is disabled, so it was left blank."
	msgCopyBlank = "%s %s is disabled, so it was left blank."
	// msgCopyLeftOut is "Line {n} of {display} was left out: {label} {code}
	// is disabled.", the label lower-cased as DOC does (doc/validate.go).
	msgCopyLeftOut = "Line %d of %s was left out: %s %s is disabled."
)

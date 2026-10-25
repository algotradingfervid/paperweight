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
)

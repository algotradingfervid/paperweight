package naming

import (
	"errors"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// Error is a naming error with a user-facing message. It satisfies
// doctype.HTTPError (CD-44).
type Error struct {
	Kind  error  // one of the sentinels below, or auth.ErrForbidden
	Msg   string // user-facing text from the catalogue
	Field string // "template" or "last" when the error belongs to a form field
}

func (e *Error) Error() string { return e.Msg }

func (e *Error) Unwrap() error { return e.Kind }

// Status is 403 for forbidden, 409 for a duplicate number, 422 otherwise.
func (e *Error) Status() int {
	switch {
	case errors.Is(e.Kind, auth.ErrForbidden):
		return 403
	case errors.Is(e.Kind, ErrDuplicateNumber):
		return 409
	default:
		return 422
	}
}

// FieldErrors returns the message against Field, or nil when Field is empty.
func (e *Error) FieldErrors() []doctype.FieldError {
	if e.Field == "" {
		return nil
	}
	return []doctype.FieldError{{Field: e.Field, Message: e.Msg}}
}

// Error kinds. Forbidden errors use auth.ErrForbidden as Kind.
var (
	ErrInvalidTemplate    = errors.New("naming: invalid template")
	ErrInvalidDate        = errors.New("naming: invalid date")
	ErrNoSeries           = errors.New("naming: no series")
	ErrNeedsTx            = errors.New("naming: needs a write transaction")
	ErrExhausted          = errors.New("naming: series exhausted")
	ErrDuplicateNumber    = errors.New("naming: duplicate number")
	ErrNumberImmutable    = errors.New("naming: number is immutable")
	ErrClientNumber       = errors.New("naming: client-supplied number")
	ErrAmendmentLimit     = errors.New("naming: amendment limit reached")
	ErrAlreadyAmended     = errors.New("naming: already amended")
	ErrUnknownRoot        = errors.New("naming: unknown root number")
	ErrCounterRange       = errors.New("naming: counter out of range")
	ErrCounterBelowIssued = errors.New("naming: counter below issued")
	ErrDuplicatePrefix    = errors.New("naming: duplicate prefix")
	ErrPrefixMismatch     = errors.New("naming: prefix mismatch")
	ErrInvalidGSTNumber   = errors.New("naming: invalid GST document number")
)

// User-facing messages, copied from docs/tests/core/02-naming.md.
//
//nolint:unused // catalogue; each message is used by the task that builds its check
const (
	msgMissingNumber  = "Series must have a number part, for example {#####}."
	msgTwoNumbers     = "Series can have only one number part."
	msgNumberNotLast  = "The number part must be at the end of the series."
	msgPadding        = "The number part must have 3 to 6 digits."
	msgUnknownToken   = "Unknown part %s in series. Use {FY}, {YYYY}, {YY}, {MM} or a number part like {#####}." //nolint:gosec // G101 false positive on the name; %s = "{XYZ}"
	msgBadChars       = "Series can use only letters, digits, '-' and '/'."
	msgFirstChar      = "Series must start with a letter or digit."
	msgBraces         = "Series has an unmatched '{' or '}'."
	msgTooLong        = "Numbers from this series would be %d characters. Keep them to 14 or fewer (amendments add 2, and GST allows 16). Shorten the prefix or use fewer digits."
	msgGSTTooLong     = "Number must be 16 characters or fewer for GST."
	msgGSTFormat      = "Number must start with a letter or digit and use only letters, digits, '-' and '/' for GST."
	msgClientNumber   = "Document numbers are set automatically and can't be entered."
	msgImmutable      = "Document numbers can't be changed."
	msgDuplicate      = "Number %s is already used. Please try again."
	msgExhausted      = "Series %s has used all %d numbers. Ask an Administrator to add a digit to the series." // CD-66
	msgAlreadyAmended = "%s has already been amended as %s. Open that document instead."                        // DOC-065 wording (CD-27): root, existing amendment's Label
	msgAmendLimit     = "%s has been amended 9 times, which is the limit. Create a new %s instead."             // label lower-cased
	msgDupPrefix      = "Prefix %s is already used by %s. Use a different prefix."
	msgPrefixMismatch = "Prefix %s doesn't match series %s." // Review Focus 6
	msgBelowIssued    = "%s has already issued numbers up to %0*d. Set %d or more."
	msgCounterRange   = "Last number used must be a whole number from 0 to %d."
	msgForbidden      = "You don't have permission to change document numbering. Ask an Administrator." // CD-66
	msgNoSeries       = "No numbering series is set up for %s. Ask an Administrator."                   // Review Focus 2; CD-66
	msgNoDate         = "A document date is needed to give it a number."                                // Review Focus 1
	msgUnknownRoot    = "%s isn't a number issued for %s, so it can't be amended."
)

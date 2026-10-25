package naming

import (
	"regexp"
	"strconv"
)

// gstNumberRe is the GST document-number format.
var gstNumberRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9/-]*$`)

// ValidateGSTDocNumber checks s is a valid GST document number: 16
// characters or fewer, starting with a letter or digit and using only
// letters, digits, '-' and '/'.
func ValidateGSTDocNumber(s string) error {
	if len(s) > 16 {
		return &Error{Kind: ErrInvalidGSTNumber, Msg: msgGSTTooLong}
	}
	if !gstNumberRe.MatchString(s) {
		return &Error{Kind: ErrInvalidGSTNumber, Msg: msgGSTFormat}
	}
	return nil
}

// GuardNumberField rejects a client-supplied number on a document that has
// none, and a changed number on one that has. An equal echo is allowed.
func GuardNumberField(stored, supplied *string) error {
	switch {
	case supplied == nil:
		return nil
	case stored == nil:
		return &Error{Kind: ErrClientNumber, Msg: msgClientNumber}
	case *supplied != *stored:
		return &Error{Kind: ErrNumberImmutable, Msg: msgImmutable}
	}
	return nil
}

// amendmentNumber returns the k-th amendment number of root.
func amendmentNumber(root string, k int) string {
	return root + "-" + strconv.Itoa(k)
}

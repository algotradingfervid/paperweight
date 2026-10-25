package files

import (
	"errors"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// Kind says which rule an Error broke; 0 is not a files error.
type Kind int

const (
	KindTooLarge Kind = iota + 1
	KindEmpty
	KindType
	KindMismatch
	KindCSVEncoding
	KindLimit
	KindParentMissing
	KindForbidden
	KindCancelled
	KindRemoveLocked
	KindNotFound
	KindBlobMissing
	KindStorage
	KindDuplicate
	KindInUse
)

const (
	msgTooLarge    = "File is larger than 10 MB. Choose a smaller file or compress it."
	msgEmpty       = "File is empty. Choose another file."
	msgType        = "File type %s is not allowed. Use PDF, JPG, PNG, WEBP, HEIC, XLSX, DOCX or CSV."
	msgMismatch    = "File content does not match its %s extension. Upload the original file."
	msgCSVEncoding = "CSV files must be UTF-8. Save the file as CSV UTF-8 and try again."
)

// Error is a files error with the HTTP status and the message the user sees (CD-44).
type Error struct {
	Kind    Kind
	Code    int
	Message string
	Err     error
}

func (e *Error) Error() string { return e.Message }

func (e *Error) Unwrap() error { return e.Err }

func (e *Error) Status() int { return e.Code }

func (e *Error) FieldErrors() []doctype.FieldError { return nil }

var _ doctype.HTTPError = (*Error)(nil)

// KindOf returns the Kind of the *Error in err's chain, or 0.
func KindOf(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return 0
}

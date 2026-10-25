package doc

import (
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/errs"
)

// ErrorKind classifies an engine error; it decides the HTTP status (CD-44).
type ErrorKind string

const (
	KindInvalid  ErrorKind = "invalid"   // 422
	KindRefused  ErrorKind = "refused"   // 409
	KindStale    ErrorKind = "stale"     // 409
	KindNotFound ErrorKind = "not_found" // 404
	KindBusy     ErrorKind = "busy"      // 503
	KindBlocked  ErrorKind = "blocked"   // 409
)

// Error is every error the engine creates. It implements doctype.HTTPError.
type Error struct {
	Kind    ErrorKind
	Message string
	Fields  []FieldError
	Err     error
}

var _ doctype.HTTPError = (*Error)(nil)

func (e *Error) Error() string { return e.Message }

// Unwrap returns Err, so a not-found error matches doctype.ErrNotFound (CD-54).
func (e *Error) Unwrap() error { return e.Err }

// Status is the HTTP status of the error's Kind.
func (e *Error) Status() int {
	switch e.Kind {
	case KindInvalid:
		return 422
	case KindNotFound:
		return 404
	case KindBusy:
		return 503
	}
	return 409
}

// FieldErrors returns Fields.
func (e *Error) FieldErrors() []FieldError { return e.Fields }

// Refused is a KindRefused error with msg, for modules' BeforeSubmit hooks too.
func Refused(msg string) *Error { return &Error{Kind: KindRefused, Message: msg} }

// errNotImplemented is returned by the actions later tasks fill in.
var errNotImplemented = errs.ErrNotImplemented

func errGone() *Error {
	return &Error{Kind: KindNotFound, Message: msgGone, Err: doctype.ErrNotFound}
}

func invalid(fields []FieldError) *Error {
	return &Error{Kind: KindInvalid, Message: msgFixFields, Fields: fields}
}

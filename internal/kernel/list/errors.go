package list

import (
	"errors"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// ErrBadQuery matches every QueryError.
var ErrBadQuery = errors.New("bad list query")

// QueryError is a user-facing message for a list query that can't be run.
// It answers 422 (CD-44).
type QueryError struct{ Msg string }

func (e *QueryError) Error() string                     { return e.Msg }
func (e *QueryError) Is(target error) bool              { return target == ErrBadQuery }
func (e *QueryError) Status() int                       { return 422 }
func (e *QueryError) FieldErrors() []doctype.FieldError { return nil }

func badQuery(msg string) error { return &QueryError{Msg: msg} }

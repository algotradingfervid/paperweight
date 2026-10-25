package csvio

import (
	"net/http"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// FileError is a whole-file import failure; it answers 422 (CD-44).
type FileError struct{ Msg string }

func (e *FileError) Error() string                     { return e.Msg }
func (e *FileError) Status() int                       { return http.StatusUnprocessableEntity }
func (e *FileError) FieldErrors() []doctype.FieldError { return nil }

var _ doctype.HTTPError = (*FileError)(nil)

package mstkit

import (
	"net/http"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// ValidationError is a 422 with field errors (CD-44).
type ValidationError struct {
	Fields []doctype.FieldError
}

// Error joins the messages with "; ".
func (e *ValidationError) Error() string {
	msgs := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		msgs[i] = f.Message
	}
	return strings.Join(msgs, "; ")
}

// Status is 422.
func (e *ValidationError) Status() int { return http.StatusUnprocessableEntity }

// FieldErrors returns Fields.
func (e *ValidationError) FieldErrors() []doctype.FieldError { return e.Fields }

// DeleteRefusedError is the "Cannot delete …" refusal: 422, no field errors.
type DeleteRefusedError struct {
	Message string
}

func (e *DeleteRefusedError) Error() string { return e.Message }

// Status is 422.
func (e *DeleteRefusedError) Status() int { return http.StatusUnprocessableEntity }

// FieldErrors is nil: the refusal belongs to no field.
func (e *DeleteRefusedError) FieldErrors() []doctype.FieldError { return nil }

// Errors collects field errors; Err returns them as a *ValidationError.
type Errors struct {
	fields []doctype.FieldError
}

// Add records a header field error.
func (es *Errors) Add(field, msg string) {
	es.fields = append(es.fields, doctype.FieldError{Field: field, Message: msg})
}

// AddLine records a field error on line.
func (es *Errors) AddLine(line int, field, msg string) {
	es.fields = append(es.fields, doctype.FieldError{Field: field, Line: line, Message: msg})
}

// Err is nil when nothing was added, else a *ValidationError.
func (es *Errors) Err() error {
	if len(es.fields) == 0 {
		return nil
	}
	return &ValidationError{Fields: append([]doctype.FieldError(nil), es.fields...)}
}

// fieldError is a *ValidationError with one header field error.
func fieldError(field, msg string) error {
	return &ValidationError{Fields: []doctype.FieldError{{Field: field, Message: msg}}}
}

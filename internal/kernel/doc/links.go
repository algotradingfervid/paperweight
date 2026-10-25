package doc

import (
	"context"

	"github.com/vardhan/internaltools/internal/platform/db"
)

// Links between documents (Task 11).

// checkSources checks the source documents of d's LinkSpecs; nil is OK.
func (e *engine) checkSources(context.Context, *db.Tx, *DocType, Doc) *FieldError { return nil }

// checkBlockers refuses cancelling d while linked documents are live.
//
//nolint:unused // called by cancel from Task 10 on
func (e *engine) checkBlockers(context.Context, *db.Tx, *DocType, Doc, string) *Error { return nil }

// References lists the documents that link to the master row id of table (CD-22).
func (e *engine) References(context.Context, *db.Tx, string, int64, int) ([]Reference, error) {
	return nil, errNotImplemented
}

// IsReferenced reports the first document that links to the master row id of table.
func (e *engine) IsReferenced(context.Context, *db.Tx, string, int64) (string, bool, error) {
	return "", false, errNotImplemented
}

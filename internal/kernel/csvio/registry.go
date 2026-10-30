package csvio

import (
	"context"
	"errors"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// ErrNoImporter is returned by Preview and Import for a DocType without a
// RowFunc (a programming error; the UI offers import only for Importable
// DocTypes).
var ErrNoImporter = errors.New("csvio: no importer registered for this DocType")

var importers = map[string]RowFunc{}

// RegisterImporter registers fn as the row importer of doctype (CD-32).
// Call it at startup, before serving: importers has no lock. It panics on a
// duplicate name.
func RegisterImporter(doctype string, fn RowFunc) {
	if _, ok := importers[doctype]; ok {
		panic("csvio: importer registered twice: " + doctype)
	}
	importers[doctype] = fn
}

// LinkCheck reports whether p may link to row id of target, reading inside tx.
// auth.ErrNotFound or auth.ErrForbidden means p may not; any other error stops
// the import.
type LinkCheck func(ctx context.Context, tx *db.Tx, a auth.Service, p auth.Principal, target *doctype.DocType, id int64) error

var linkCheck LinkCheck

// SetLinkCheck makes fn the check of every link cell and every suggestion
// (CD-232). mstkit sets it in init, because the check needs each master's
// created_by rule and csvio can't import mstkit. Without it nothing is hidden,
// which is safe: every production master registers through mstkit, so a binary
// with masters always has it, and only a binary or test that registers its own
// tables runs without. Call it at startup: linkCheck has no lock.
func SetLinkCheck(fn LinkCheck) { linkCheck = fn }

type importKey struct{}

// IsImport reports whether ctx is the one csvio passes to a RowFunc. MST's
// create code uses it to skip its own create audit entry (CD-60).
func IsImport(ctx context.Context) bool {
	v, _ := ctx.Value(importKey{}).(bool)
	return v
}

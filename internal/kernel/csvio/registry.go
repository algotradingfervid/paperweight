package csvio

import (
	"context"
	"errors"
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

type importKey struct{}

// IsImport reports whether ctx is the one csvio passes to a RowFunc. MST's
// create code uses it to skip its own create audit entry (CD-60).
func IsImport(ctx context.Context) bool {
	v, _ := ctx.Value(importKey{}).(bool)
	return v
}

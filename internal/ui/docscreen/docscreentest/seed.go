package docscreentest

import (
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doc/docfake"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// Seed stores a document in f: info for Info and rec, with that info, for
// Load.
func Seed(f *docfake.Fake, info doctype.Info, rec doc.Record) {
	rec.Info = info
	f.SetInfo(info)
	f.SetRecord(rec)
}

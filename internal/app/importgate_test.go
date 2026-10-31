package app_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// CD-318: csvio's import resolves a link cell by the target's code column
// (resolveLink, targetRows, added), so no importable Link may target a master
// without a code field. The app registers every DocType.
func TestApp_ImportableLinksTargetCodedMasters(t *testing.T) {
	for _, dt := range doctype.All() {
		for _, f := range dt.ImportFields() {
			if f.Type != doctype.Link {
				continue
			}
			target, ok := doctype.Lookup(f.Link)
			if !ok {
				t.Errorf("%s.%s links to unregistered %q", dt.Name, f.Name, f.Link)
				continue
			}
			if _, hasCode := target.Field("code"); target.IsMaster() && !hasCode {
				t.Errorf("%s.%s is importable but links to %s, which has no code: import would fail on its code column", dt.Name, f.Name, target.Name)
			}
		}
	}
}

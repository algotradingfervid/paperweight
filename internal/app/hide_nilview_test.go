package app_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// TestDocDeps_HideNilView (M0-47, CD-319 (7), CD-327 (1)): the list and
// search pages ask each Module's Hide with no View and Listing set. For every Module in a
// built app's docDeps, Hide answers for every header and line field of its
// DocType with View nil, without a panic.
func TestDocDeps_HideNilView(t *testing.T) {
	deps := newWorld(t).app.DocDeps()
	checked := 0
	for name, m := range deps.Modules {
		if m.Hide == nil {
			continue
		}
		dt, ok := doctype.Lookup(name)
		if !ok {
			t.Errorf("docDeps().Modules has %s, which is not a registered DocType", name)
			continue
		}
		c := docscreen.Ctx{Req: httptest.NewRequest(http.MethodGet, "/doc/"+name, nil), DT: dt, Deps: deps, Listing: true}
		for _, f := range dt.Fields {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("%s Hide(%s) with View nil panics: %v", name, f.Name, r)
					}
				}()
				m.Hide(c, f)
			}()
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no Module in docDeps has a Hide: the test checks nothing")
	}
}

package docscreen_test

import (
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
)

// importOnlyType is a document with one ImportOnly field. The field's label
// and name appear nowhere else on its pages.
const (
	importOnlyType  = "docscreentest.Imported"
	importOnlyLabel = "Legacy ledger ref"
	importOnlyName  = "legacy_ledger_ref"
	storedLabel     = "Consignee"
)

var registerImportOnlyOnce sync.Once

func registerImportOnly() {
	registerImportOnlyOnce.Do(func() {
		doctype.Register(doctype.DocType{
			Name: importOnlyType, Label: "Imported Note", PluralLabel: "Imported Notes", Table: "docscreentest_imported", Submittable: true,
			Fields: []doctype.Field{
				{Name: importOnlyName, Label: importOnlyLabel, Type: doctype.Text, Importable: true, ImportOnly: true},
				{Name: "consignee", Label: storedLabel, Type: doctype.Text, ListPriority: 2},
			},
		})
	})
}

// wantNoImportOnly checks one page rendered, shows storedLabel when show is
// set, and has neither the ImportOnly label nor its name.
func wantNoImportOnly(t *testing.T, what string, code int, body string, show bool) {
	t.Helper()
	if code != http.StatusOK {
		t.Fatalf("%s: status = %d, body %.300s", what, code, body)
	}
	if show && !strings.Contains(body, storedLabel) {
		t.Errorf("%s: page lacks the stored field's label %q", what, storedLabel)
	}
	for _, s := range []string{importOnlyLabel, importOnlyName} {
		if strings.Contains(body, s) {
			t.Errorf("%s: page contains %q", what, s)
		}
	}
}

// CD-210: an ImportOnly field never reaches a screen. The record carries a
// value for it, so a page walking the fields would show it.
func TestDocScreen_ImportOnlyNotRendered(t *testing.T) {
	registerImportOnly()
	base := "/doc/" + importOnlyType

	e := newDocEnv(t, allRights...)
	for _, r := range append([]auth.Right{"create"}, allRights...) {
		e.az.Grant("Buyer", importOnlyType, r, auth.ScopeAll)
	}
	docscreentest.Seed(e.docs, draftInfo(importOnlyType, "Imported Note", 50), doc.Record{
		Version: 1, Values: map[string]string{importOnlyName: "L-77", "consignee": "Pune Metro"},
	})
	// A draft's page redirects to its check step, so the view is of an approved one.
	approved := draftInfo(importOnlyType, "Imported Note", 51)
	approved.State, approved.Number, approved.Display = doctype.StateApproved, "IN/26-27/00001", "IN/26-27/00001"
	docscreentest.Seed(e.docs, approved, doc.Record{
		Version: 2, Values: map[string]string{importOnlyName: "L-77", "consignee": "Pune Metro"},
	})
	for _, c := range []struct{ what, url string }{
		{"new form", base + "/new"},
		{"edit form", base + "/50/edit/details"},
		{"check page", base + "/50/edit/check"},
		{"view page", base + "/51"},
	} {
		rec := e.get(t, c.url)
		wantNoImportOnly(t, c.what, rec.Code, rec.Body.String(), true)
	}

	l := newEnv(t, "read")
	l.az.Grant("Buyer", importOnlyType, "read", auth.ScopeAll)
	l.lister.Set(importOnlyType, types.Result[docscreen.ListRow]{Total: 1, Rows: []docscreen.ListRow{{
		Info:   draftInfo(importOnlyType, "Imported Note", 50),
		Values: map[string]string{importOnlyName: "L-77", "consignee": "Pune Metro"},
	}}})
	rec := l.get(t, base)
	wantNoImportOnly(t, "list page", rec.Code, rec.Body.String(), false)
	if !strings.Contains(rec.Body.String(), "Pune Metro") {
		t.Error("list page lacks the stored field's value")
	}
}

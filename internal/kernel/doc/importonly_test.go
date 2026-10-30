package doc_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/doc"
)

// ioDocType is a test-only document with two ImportOnly fields. Its table,
// made by the test, has no column for either.
const ioDocType = "io.Imported"

var _ = func() bool {
	doc.Register(doc.DocType{
		Name: ioDocType, Label: "Imported document", PluralLabel: "Imported documents",
		Table: "io_imported", LinesTable: "io_imported_line", Hooks: p2lHooks{},
		Fields: []doc.Field{
			{Name: "contact_email", Label: "Contact email", Type: doc.Text, Importable: true, ImportOnly: true},
			{Name: "note", Label: "Note", Type: doc.Text},
			{Name: "opening_count", Label: "Opening count", Type: doc.Int, Importable: true, ImportOnly: true},
		},
	})
	return true
}()

// wantNoImportOnly loads id through the public paths: neither may return an
// ImportOnly field, and note and the version must be the stored ones.
func wantNoImportOnly(t *testing.T, h *fixturetest.H, what string, id int64, note string, version int) {
	t.Helper()
	d := h.Load(t, ioDocType, id)
	rec := h.Record(t, ioDocType, id)
	for _, name := range []string{"contact_email", "opening_count"} {
		if v, ok := d.Header[name]; ok {
			t.Errorf("%s: Doc.Header[%s] = %v, want no such key", what, name, v)
		}
		if v, ok := rec.Values[name]; ok {
			t.Errorf("%s: Record.Values[%s] = %q, want no such key", what, name, v)
		}
	}
	if d.Header["note"] != any(note) || rec.Values["note"] != note {
		t.Errorf("%s: note = %v and %q, want %q", what, d.Header["note"], rec.Values["note"], note)
	}
	if d.Version != version {
		t.Errorf("%s: Version = %d, want %d", what, d.Version, version)
	}
}

func TestEngine_IgnoresImportOnly(t *testing.T) {
	h := fixturetest.New(t)
	h.Exec(t, `CREATE TABLE io_imported (
		id INTEGER PRIMARY KEY, number TEXT UNIQUE, state TEXT NOT NULL DEFAULT 'draft',
		state_reason TEXT NOT NULL DEFAULT '', amended_from_id INTEGER, amendment_root_id INTEGER,
		amendment_no INTEGER NOT NULL DEFAULT 0, version INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL, created_by INTEGER NOT NULL, updated_at TEXT NOT NULL,
		updated_by INTEGER NOT NULL, note TEXT NOT NULL DEFAULT '')`)
	h.Exec(t, `CREATE TABLE io_imported_line (
		id INTEGER PRIMARY KEY, parent_id INTEGER NOT NULL, line_no INTEGER NOT NULL)`)

	// opening_count would fail to parse if the engine read it.
	res := h.MustDo(t, t.Context(), h.Priya, ioDocType, 0, doc.ActionSave, doc.ActionInput{Values: map[string]string{
		"note": "first", "contact_email": "accounts@shreesteel.test", "opening_count": "not a number",
	}})
	wantNoImportOnly(t, h, "after create", res.ID, "first", 1)

	in := h.In(h.Load(t, ioDocType, res.ID))
	in.Values["note"] = "second"
	in.Values["contact_email"] = "sales@shreesteel.test"
	in.Values["opening_count"] = "not a number"
	h.MustDo(t, t.Context(), h.Priya, ioDocType, res.ID, doc.ActionSave, in)
	wantNoImportOnly(t, h, "after save", res.ID, "second", 2)

	// A posted value is ignored: changing only these is no change (DOC-029).
	in = h.In(h.Load(t, ioDocType, res.ID))
	in.Values["contact_email"] = "other@shreesteel.test"
	h.MustDo(t, t.Context(), h.Priya, ioDocType, res.ID, doc.ActionSave, in)
	wantNoImportOnly(t, h, "after a save changing only an ImportOnly value", res.ID, "second", 2)
}

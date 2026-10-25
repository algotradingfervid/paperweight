package doctype

import (
	"strings"
	"testing"
)

// CD-144: a header Field may name a convention column when it maps to that
// column itself; the engine then owns it (System, never taken from input).
func TestRegistry_ConventionColumnAsDisplayField(t *testing.T) {
	for _, f := range []Field{
		{Name: "number", Label: "Number", Type: Text, Sortable: true, Filterable: true},
		{Name: "number", Label: "Number", Column: "number", Type: Text, ReadOnly: true},
		{Name: "created_at", Label: "Created", Type: Timestamp},
	} {
		dt := testOrder()
		dt.Fields = append(dt.Fields, f)
		r := newRegistry()
		mustNotPanic(t, f.Name, func() { r.Register(dt) })
		got, ok := r.Lookup(dt.Name)
		if !ok {
			continue
		}
		g, _ := got.Field(f.Name)
		if g.Column != f.Name || !g.System {
			t.Errorf("%s: registered as Column %q System %v, want Column %q System true", f.Name, g.Column, g.System, f.Name)
		}
	}

	for _, c := range []struct {
		name  string
		field Field
	}{
		{"line parent_id", Field{Name: "parent_id", Label: "Parent", Type: Int, OnLine: true}},
		{"EditableAfterSubmit on a convention column", Field{Name: "number", Label: "Number", Type: Text, EditableAfterSubmit: true}},
	} {
		dt := testOrder()
		dt.Fields = append(dt.Fields, c.field)
		got := mustPanic(t, c.name, func() { newRegistry().Register(dt) })
		if got != "" && !strings.Contains(got, "convention column") {
			t.Errorf("%s: panic = %q, want a convention column error", c.name, got)
		}
	}
}

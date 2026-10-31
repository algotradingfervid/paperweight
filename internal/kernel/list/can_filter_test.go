package list_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
)

// M0-41: CanFilter reports exactly what Build accepts. Each case also runs
// through Build with no values (in and is empty need none), so a drift
// between the two fails here.
func TestCanFilter_AgreesWithBuild(t *testing.T) {
	fields := []doctype.Field{
		{Name: "state", Label: "State", Type: doctype.Select, Filterable: true},
		{Name: "secret", Label: "Secret", Type: doctype.Text},
		{Name: "due", Label: "Due", Type: doctype.Date, Filterable: true},
		{Name: "qty", Label: "Qty", Type: doctype.Qty, Filterable: true, OnLine: true},
	}
	withLines := &doctype.DocType{Name: "cf.Lines", PluralLabel: "CF lines", Table: "cf_lines", LinesTable: "cf_lines_line", Fields: fields}
	noLines := &doctype.DocType{Name: "cf.NoLines", PluralLabel: "CF no lines", Table: "cf_no_lines", Fields: fields}
	cases := []struct {
		name  string
		dt    *doctype.DocType
		field string
		op    list.Op
		want  bool
	}{
		{"allowed", withLines, "state", list.OpIn, true},
		{"unknown field", withLines, "nope", list.OpIn, false},
		{"not filterable", withLines, "secret", list.OpIn, false},
		{"on-line field without prefix", withLines, "qty", list.OpIn, false},
		{"header field with line prefix", withLines, "line.state", list.OpIn, false},
		{"on-line field with prefix", withLines, "line.qty", list.OpIn, true},
		{"line prefix without lines table", noLines, "line.qty", list.OpIn, false},
		{"op not allowed for kind", withLines, "due", list.OpIn, false},
		{"op allowed for kind", withLines, "due", list.OpIsEmpty, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := list.CanFilter(c.dt, c.field, c.op); got != c.want {
				t.Errorf("CanFilter(%s, %q, %q) = %v, want %v", c.dt.Name, c.field, c.op, got, c.want)
			}
			_, _, err := list.Build(c.dt, query(filter(c.field, c.op)), noScope)
			if (err == nil) != c.want {
				t.Errorf("Build with filter %q %q: err = %v, want accepted = %v", c.field, c.op, err, c.want)
			}
		})
	}
}

package doctype

import (
	"strings"
	"testing"
)

// Supporting tests for WP1-DOC Task 1 review minors (no DOC case ID): roll-up
// and link field levels in CheckRegistry, column clashes in Register.

func lineTarget() DocType {
	return DocType{
		Name: "t.Order", Label: "Purchase Order", Table: "t_order", LinesTable: "t_order_lines",
		Fields: []Field{
			{Name: "qty", Label: "Qty", Type: Qty, OnLine: true},
			{Name: "received_qty", Label: "Received qty", Type: Qty, OnLine: true, RollUp: true, EditableAfterSubmit: true},
			{Name: "head_qty", Label: "Head qty", Type: Qty, RollUp: true, EditableAfterSubmit: true},
		},
		Hooks: nopHooks{},
	}
}

func lineSource(links []LinkSpec, rollups []RollUp) DocType {
	return DocType{
		Name: "t.Receipt", Label: "Goods Receipt", Table: "t_receipt", LinesTable: "t_receipt_lines",
		Fields: []Field{
			{Name: "order_line_id", Label: "Order line", Type: Int, OnLine: true},
			{Name: "qty", Label: "Qty", Type: Qty, OnLine: true},
			{Name: "order_id", Label: "Order", Type: Int},
			{Name: "total_qty", Label: "Total qty", Type: Qty},
		},
		Links: links, RollUps: rollups, Hooks: nopHooks{},
	}
}

func TestCheckRegistry_FieldLevels(t *testing.T) {
	good := RollUp{TargetDocType: "t.Order", TargetField: "received_qty", SourceField: "qty", JoinField: "order_line_id"}
	cases := []struct {
		name string
		src  DocType
		want []string // the error must name each
	}{
		{"RollUp SourceField on the header",
			lineSource(nil, []RollUp{{TargetDocType: "t.Order", TargetField: "received_qty", SourceField: "total_qty", JoinField: "order_line_id"}}),
			[]string{"t.Order.received_qty", "total_qty"}},
		{"RollUp JoinField on the header",
			lineSource(nil, []RollUp{{TargetDocType: "t.Order", TargetField: "received_qty", SourceField: "qty", JoinField: "order_id"}}),
			[]string{"t.Order.received_qty", "order_id"}},
		{"RollUp TargetField on the header",
			lineSource(nil, []RollUp{{TargetDocType: "t.Order", TargetField: "head_qty", SourceField: "qty", JoinField: "order_line_id"}}),
			[]string{"t.Order.head_qty", "head_qty"}},
		{"LinkSpec OnLine naming a header field",
			lineSource([]LinkSpec{{Field: "order_id", Target: "t.Order", OnLine: true}}, nil),
			[]string{"order_id"}},
		{"LinkSpec header naming a line field",
			lineSource([]LinkSpec{{Field: "order_line_id", Target: "t.Order"}}, nil),
			[]string{"order_line_id"}},
	}
	for _, c := range cases {
		r := newRegistry()
		r.Register(lineTarget())
		r.Register(c.src)
		err := r.Check()
		if err == nil {
			t.Errorf("%s: Check() = nil, want an error", c.name)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: Check() = %q, want it to name %q", c.name, err, w)
			}
		}
	}

	r := newRegistry()
	r.Register(lineTarget())
	r.Register(lineSource([]LinkSpec{
		{Field: "order_line_id", Target: "t.Order", OnLine: true},
		{Field: "order_id", Target: "t.Order"},
	}, []RollUp{good}))
	if err := r.Check(); err != nil {
		t.Errorf("valid levels: Check() = %v, want nil", err)
	}
}

func TestRegistry_RejectsColumnClashes(t *testing.T) {
	cases := []struct {
		name  string
		field Field
		want  []string // the panic must name each
	}{
		{"same Column on the header", Field{Name: "days", Label: "Days", Type: Int, Column: "credit_days"}, []string{"days", "credit_days"}},
		{"same Column on the lines", Field{Name: "qty2", Label: "Qty 2", Type: Qty, OnLine: true, Column: "qty"}, []string{"qty2", "qty"}},
		{"header convention column", Field{Name: "version", Label: "Version", Type: Int}, []string{"version"}},
		{"header convention column via Column", Field{Name: "maker", Label: "Maker", Type: Int, Column: "created_by"}, []string{"maker", "created_by"}},
		{"line convention column", Field{Name: "parent", Label: "Parent", Type: Int, OnLine: true, Column: "parent_id"}, []string{"parent", "parent_id"}},
		{"line id", Field{Name: "id", Label: "Id", Type: Int, OnLine: true}, []string{"id"}},
	}
	for _, c := range cases {
		dt := testOrder()
		dt.Fields = append(dt.Fields, c.field)
		got := mustPanic(t, c.name, func() { newRegistry().Register(dt) })
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: panic = %q, want it to name %q", c.name, got, w)
			}
		}
	}

	// The same column at different levels is fine (header and line tables differ).
	dt := testOrder()
	dt.Fields = append(dt.Fields, Field{Name: "head_qty", Label: "Head qty", Type: Qty, Column: "qty"})
	mustNotPanic(t, "same Column on header and line", func() { newRegistry().Register(dt) })
}

package grnweb

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// MOD-GRN S1e (CD-341 (2), CD-350): declared copies Required and Tip from
// the field's declaration, as docscreen's FieldInput does; no GRN field declares a Tip
// yet, so the screens can't show it.
func TestDeclared_RequiredAndTip(t *testing.T) {
	dt := &doctype.DocType{Fields: []doctype.Field{
		{Name: "a", Required: true, Tip: "How to fill a."},
		{Name: "b"},
	}}
	if p := declared(dt, "a", components.FieldProps{Name: "x"}); !p.Required || p.Tip != "How to fill a." || p.Name != "x" {
		t.Errorf("declared a = %+v, want Required, its Tip and the given Name", p)
	}
	if p := declared(dt, "b", components.FieldProps{Required: true, Tip: "stale"}); p.Required || p.Tip != "" {
		t.Errorf("declared b = %+v, want neither Required nor Tip", p)
	}
}

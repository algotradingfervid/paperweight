package purchaseweb

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// MOD-PO P15-0 (CD-256): Modules has one Module for Order and one for
// Request, and until each screen task builds its part every field keeps
// docscreen's generic behaviour: generic steps, no hidden field, the line's
// own card and no extras. internal/app's TestApp_PurchaseModulesUnchanged
// renders the pages. P15d built the Request's (p15d_*_test.go), P15b the
// Order's Page and Reason (p15b_*_test.go), P15c its items step
// (p15c_*_test.go).
func TestModules_StubsKeepGeneric(t *testing.T) {
	mods := Modules(Deps{})
	if len(mods) != 2 {
		t.Fatalf("Modules has %d entries, want Order and Request", len(mods))
	}
	card := components.LineCard{Item: "MS Angle 50"}
	line := docscreen.LineView{No: 1, Card: card}
	field := doctype.Field{Name: "remarks"}
	if _, ok := mods[purchase.RequestDocType]; !ok {
		t.Fatalf("Modules has no %s", purchase.RequestDocType)
	}
	for _, dt := range []string{purchase.DocType} {
		m, ok := mods[dt]
		if !ok {
			t.Fatalf("Modules has no %s", dt)
		}
		for slug, s := range m.Steps {
			if slug == "items" {
				continue // P15c built it (p15c_*_test.go)
			}
			if s.Body != nil || s.Save != nil || s.Bar != nil || s.Top != nil {
				t.Errorf("%s step %q isn't the generic step", dt, slug)
			}
		}
		if m.CardsOnly || m.NoLinePages || m.AddTop || m.Line != nil || len(m.StepLabels) != 0 {
			t.Errorf("%s Module sets a flag, a line page or labels", dt)
		}
		if m.Hide(docscreen.Ctx{}, field) {
			t.Errorf("%s Hide hides %s", dt, field.Name)
		}
		if ce, err := m.Check(docscreen.Ctx{}); err != nil || ce.Notes != nil || ce.Warnings != nil || ce.Totals != nil || ce.Cards != nil {
			t.Errorf("%s Check = %+v, %v; want nothing", dt, ce, err)
		}
		if got := m.LineCard(docscreen.Ctx{}, line); got.Item != card.Item {
			t.Errorf("%s LineCard = %+v, want the line's own card", dt, got)
		}
	}
	if s := mods[purchase.DocType].Steps; len(s) != 2 {
		t.Errorf("Order Steps = %d, want terms (P15a) and items (P15c)", len(s))
	}
}

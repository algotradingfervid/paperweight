package purchaseweb

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// MOD-PO P15-0 (CD-256): Modules has one Module for Order and one for
// Request, and until each screen task builds its part every field keeps
// docscreen's generic behaviour: generic steps, no hidden field, the line's
// own card and no extras. internal/app's TestApp_PurchaseModulesUnchanged
// renders the pages. P15d built the Request's (p15d_*_test.go), and P15a the
// Order's terms step, Hide, Check, LineCard and LineGrid (p15a_*_test.go).
func TestModules_StubsKeepGeneric(t *testing.T) {
	mods := Modules(Deps{})
	if len(mods) != 2 {
		t.Fatalf("Modules has %d entries, want Order and Request", len(mods))
	}
	if _, ok := mods[purchase.RequestDocType]; !ok {
		t.Fatalf("Modules has no %s", purchase.RequestDocType)
	}
	for _, dt := range []string{purchase.DocType} {
		m, ok := mods[dt]
		if !ok {
			t.Fatalf("Modules has no %s", dt)
		}
		for slug, s := range m.Steps {
			if slug == "terms" {
				continue // P15a's
			}
			if s.Body != nil || s.Save != nil || s.Bar != nil || s.Top != nil {
				t.Errorf("%s step %q isn't the generic step", dt, slug)
			}
		}
		if m.CardsOnly || m.NoLinePages || m.AddTop || m.Line != nil || len(m.StepLabels) != 0 {
			t.Errorf("%s Module sets a flag, a line page or labels", dt)
		}
		if pe, err := m.Page(docscreen.Ctx{}); err != nil || pe.Warnings != nil || pe.LineNotes != nil || pe.Pairs != nil || pe.Cards != nil || pe.Notice != nil || pe.More != nil || pe.Primary != nil || pe.Files != nil {
			t.Errorf("%s Page = %+v, %v; want nothing", dt, pe, err)
		}
		if re, err := m.Reason(docscreen.Ctx{}, doctype.ActionClose); err != nil || re != (docscreen.ReasonExtra{}) {
			t.Errorf("%s Reason = %+v, %v; want nothing", dt, re, err)
		}
	}
	if s := mods[purchase.DocType].Steps; len(s) != 2 {
		t.Errorf("Order Steps = %d, want terms (P15a) and items (P15c)", len(s))
	}
}

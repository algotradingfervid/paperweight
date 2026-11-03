package purchaseweb

import (
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// r7Starred reports whether the field whose control id starts with f-<name>
// marks it required (the label's hidden " (required)").
func r7Starred(t *testing.T, p p15dPage, name string) bool {
	t.Helper()
	field := p.doc.Find(`[id^="f-` + name + `"]`).First().Closest(".field")
	if field.Length() != 1 {
		t.Fatalf("no .field for %s: %s", name, p.text())
	}
	return strings.Contains(field.Find("label, legend").First().Text(), "(required)")
}

// CD-387 (2): fields that resolve themselves when left blank (Project, UoM,
// Tax code) have no star; the item and the quantity keep theirs.
func TestR7_OrderSelfFillNoStar(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t, purchasetest.WithNoRules()), nil)
	env := w.env
	details := w.get(t, env.Priya, "/doc/"+purchase.DocType+"/new")
	if r7Starred(t, details, "project_id") {
		t.Error("PO Project starred")
	}
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	line := w.get(t, env.Priya, p15dDoc(purchase.DocType, po.ID)+"/lines/new")
	for name, want := range map[string]bool{"item_id": true, "qty": true, "uom_id": false, "tax_code_id": false} {
		if got := r7Starred(t, line, name); got != want {
			t.Errorf("PO line %s starred = %v, want %v", name, got, want)
		}
	}
}

func TestR7_RequestSelfFillNoStar(t *testing.T) {
	w := newP15dWeb(t)
	env := w.env
	if r7Starred(t, w.get(t, env.Suresh, "/doc/"+purchase.RequestDocType+"/new"), "project_id") {
		t.Error("request Project starred")
	}
	pr := purchasetest.NewRequest(t, env, purchasetest.StopAtDraft())
	line := w.get(t, env.Suresh, p15dDoc(purchase.RequestDocType, pr.ID)+"/lines/new")
	for name, want := range map[string]bool{"item_id": true, "uom_id": false} {
		if got := r7Starred(t, line, name); got != want {
			t.Errorf("request line %s starred = %v, want %v", name, got, want)
		}
	}
}

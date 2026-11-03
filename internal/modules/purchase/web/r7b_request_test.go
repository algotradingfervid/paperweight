package purchaseweb

import (
	"slices"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// R7b review: a request line whose unit didn't resolve is what Submit
// refuses (requestBlanks), so its Check marks Items Needs attention and
// lists DOC's line text there, never as a top warning; a resolved request's
// Items is Complete.
func TestR7b_RequestCheckNamesMissing(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t), nil)
	env := w.env
	ok := purchasetest.NewRequest(t, env, purchasetest.StopAtDraft())
	path := p15dDoc(purchase.RequestDocType, ok.ID) + "/edit/check"
	if got := p15dCollapse(r7bSection(w.get(t, env.Suresh, path).doc, "Items").Find(".check-status").Text()); got != "Complete" {
		t.Errorf("resolved request's Items = %q, want Complete", got)
	}
	if _, err := env.DB.SQL().ExecContext(t.Context(), `UPDATE purchase_request_line SET uom_id = NULL WHERE parent_id = ?`, ok.ID); err != nil {
		t.Fatal(err)
	}
	check := w.get(t, env.Suresh, path)
	items := r7bSection(check.doc, "Items")
	if got := p15dCollapse(items.Find(".check-status").Text()); got != "Needs attention" {
		t.Errorf("Items = %q, want Needs attention", got)
	}
	if got := items.Find("ul > li").Map(func(_ int, s *goquery.Selection) string { return p15dCollapse(s.Text()) }); !slices.Equal(got, []string{"Line 1: Unit is required."}) {
		t.Errorf("Items lists %q, want the unit", got)
	}
	if got := check.find("main .banner--warning"); r13Has(got, "is required.") {
		t.Errorf("check warnings = %q, want the unit in its section only", got)
	}
}

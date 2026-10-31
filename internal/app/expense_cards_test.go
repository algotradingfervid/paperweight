package app_test

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense"
)

// CD-315, EUX-025: the Claim's and the Advance's Items steps show line cards
// at every width, desktop included, never the editable grid. The Advance's
// line row has exactly 5 editable cells, which would give it the grid, and
// the Claim got it until M0-40; each Module's CardsOnly keeps the cards.
func TestApp_ExpenseItemsCardsOnly(t *testing.T) {
	w := newWorld(t)
	admin, _, _ := w.canonical()
	travel := w.text(`SELECT CAST(id AS TEXT) FROM expense_type WHERE code = 'TRAVEL'`)
	b := w.signedIn(adminEmail)
	for _, c := range []struct {
		dt     string
		header map[string]string
		line   map[string]string
	}{
		{expense.AdvanceDocType, map[string]string{"advance_date": "2026-10-24", "purpose": "Pune Metro site visit"},
			map[string]string{"expense_type_id": travel, "line_amount": "5000.00"}},
		{expense.ClaimDocType, map[string]string{"claim_date": "2026-10-24", "purpose": "Pune Metro site visit"},
			map[string]string{"expense_type_id": travel, "expense_date": "2026-10-24", "amount": "1000.00"}},
	} {
		t.Run(c.dt, func(t *testing.T) {
			res, err := w.app.Engine().Do(context.Background(), admin, c.dt, 0, doctype.ActionSave, doc.ActionInput{
				Values: c.header, Lines: []map[string]string{c.line, c.line},
			})
			if err != nil {
				t.Fatalf("save a %s as admin: %v", c.dt, err)
			}
			base := "/doc/" + c.dt + "/" + strconv.FormatInt(res.ID, 10)
			r := b.get(base + "/edit/items")
			if r.status != http.StatusOK {
				t.Fatalf("GET %s/edit/items = %d, want 200", base, r.status)
			}
			for _, sel := range []string{"#line-editor", ".line-editor", ".linegrid", ".linelist--grid", "form.linegrid-form", `[id^="grid-row-"]`} {
				if n := r.doc.Find(sel).Length(); n != 0 {
					t.Errorf("the Items step has %d %s, want no grid", n, sel)
				}
			}
			if n := r.doc.Find(".linelist .lines-card").Length(); n != 2 {
				t.Errorf("the Items step has %d line cards, want 2", n)
			}
		})
	}
}

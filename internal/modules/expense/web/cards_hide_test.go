package expenseweb

import (
	"fmt"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// cardText is everything a card shows, its receipt included.
func cardText(cards ...components.LineCard) string {
	var b strings.Builder
	for _, c := range cards {
		fmt.Fprintf(&b, "%+v\n", c)
		if c.Receipt != nil {
			fmt.Fprintf(&b, "%+v\n", *c.Receipt)
		}
	}
	return b.String()
}

// CD-319 (9): a claim line's card shows nothing of a field the registered
// Hide hides, with the module's own Hide or-ed in as the redraw sweep
// registers it. Each field is hidden in turn; the fields that don't depend
// on it stay shown, so every check bites. The type and amount come from
// docscreen's card, already filtered; the card never adds them back.
func TestClaimLineCard_HonoursHide(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, "deepak@vardhan.test")
	d := Deps{Service: e.Service, Engine: e.Engine, Auth: e.Auth, Files: e.Files}
	dt, _ := doctype.Lookup(expense.ClaimDocType)
	claim := e.DraftClaim(t, deepak, nil, expensetest.Values{
		"company_bill": "true", "supplier_gstin": "27AABCS1429B1ZU", "invoice_no": "INV-778", "gst_amount": "18.00", "billed_to_id": strconv.FormatInt(e.E8Branch(t, "BLR"), 10),
	})
	rec := e.Load(t, expense.ClaimDocType, claim.ID)
	bill := e.MustAttach(t, deepak, expense.ClaimDocType, claim.ID, "bill.pdf", []byte("%PDF-1.4 card bill"))
	site := e.F.Sites[0]
	// Line 1 is stored (its Billed-to snapshot is read by id); line 2 is only
	// a view: a site, why it has no receipt, and GST but no company bill.
	lines := []docscreen.LineView{{
		ID: rec.Lines[0]["id"], Card: components.LineCard{Item: "Travel", Amount: "₹1,000.00"},
		Display: map[string]string{"expense_date": "ZqDate", "gst_amount": "ZqGST", "description": "ZqNote", "expense_type_id": "ZqType"},
		Inputs: map[string]string{
			"company_bill": "1", "project_id": strconv.FormatInt(e.E8Project(t, "PRJ-METRO"), 10),
			"receipt_sha256": bill.SHA256, "billed_to_id": rec.Lines[0]["billed_to_id"],
		},
	}, {
		Card:    components.LineCard{Item: "Travel", Amount: "₹1,000.00"},
		Display: map[string]string{"expense_date": "ZqDate2", "gst_amount": "ZqNoBill"}, // not a company bill: no GST
		Inputs:  map[string]string{"site_id": strconv.FormatInt(site.ID, 10), "no_receipt_reason": "ZqWhy"},
	}}
	gstin := ""
	for _, b := range e.F.Branches {
		if b.Code == "BLR" {
			gstin = b.GSTIN
		}
	}
	shows := map[string]string{ // what each field puts on the cards
		"expense_date": "ZqDate", "project_id": "PRJ-METRO", "site_id": site.Code,
		"company_bill": "GST ", "gst_amount": "ZqGST", "billed_to_id": billedTo + gstin,
		"description": "ZqNote", "receipt_sha256": "/files/" + strconv.FormatInt(bill.ID, 10),
		"no_receipt_reason": "ZqWhy", "expense_type_id": "ZqType",
	}
	gone := map[string][]string{ // fields drawn only with the hidden one
		"company_bill":   {"gst_amount", "billed_to_id"},
		"gst_amount":     {"company_bill", "billed_to_id"},
		"receipt_sha256": {"no_receipt_reason"},
	}
	cards := func(hidden string) string {
		own := claimHide(d)
		hide := func(c docscreen.Ctx, f doctype.Field) bool { return f.Name == hidden || own(c, f) }
		c := docscreen.Ctx{
			Req: httptest.NewRequest("GET", "/", nil), Principal: deepak, DT: dt,
			View: &docscreen.DocView{Info: rec.Info, Lines: lines},
			Deps: docscreen.Deps{DB: e.DB, Modules: map[string]docscreen.Module{expense.ClaimDocType: {Hide: hide}}},
		}
		card := claimLineCard(d)
		return cardText(card(c, lines[0]), card(c, lines[1]))
	}
	if all := cards(""); !strings.Contains(all, "ZqDate · PRJ-METRO") || !strings.Contains(all, "ZqDate2 · "+site.Code) {
		t.Errorf("nothing hidden: %s, want the dates with their project and site codes", all)
	}
	all := cards("")
	for f, s := range shows {
		if f != "expense_type_id" && !strings.Contains(all, s) {
			t.Errorf("nothing hidden: %q missing from the cards (%s), want %s shown", s, all, f)
		}
	}
	for hidden := range shows {
		got := cards(hidden)
		if strings.Contains(got, "ZqNoBill") {
			t.Errorf("hide %s: GST shown on a line that isn't a company bill: %s", hidden, got)
		}
		if strings.Contains(got, shows[hidden]) {
			t.Errorf("hide %s: the cards show %q: %s", hidden, shows[hidden], got)
		}
		for f, s := range shows {
			if f != hidden && f != "expense_type_id" && !slices.Contains(gone[hidden], f) && !strings.Contains(got, s) {
				t.Errorf("hide %s: %q missing (%s), want %s still shown", hidden, s, got, f)
			}
		}
		if hidden == "no_receipt_reason" || hidden == "receipt_sha256" {
			if strings.Contains(got, noReceiptYet) || strings.Contains(got, noReceipt) {
				t.Errorf("hide %s: the cards say there is no receipt: %s", hidden, got)
			}
		}
	}
}

// CD-319 (9): an advance line's card shows its note unless the registered
// Hide hides description.
func TestAdvanceLineCard_HonoursHide(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, "deepak@vardhan.test")
	d := Deps{Service: e.Service, Engine: e.Engine, Auth: e.Auth, Files: e.Files}
	dt, _ := doctype.Lookup(expense.AdvanceDocType)
	l := docscreen.LineView{Card: components.LineCard{Item: "Travel", Code: "TRAVEL"}, Display: map[string]string{"description": "ZqNote"}}
	for _, hidden := range []string{"", "description"} {
		own := advanceHide(d)
		hide := func(c docscreen.Ctx, f doctype.Field) bool { return f.Name == hidden || own(c, f) }
		c := docscreen.Ctx{
			Req: httptest.NewRequest("GET", "/", nil), Principal: deepak, DT: dt,
			Deps: docscreen.Deps{DB: e.DB, Modules: map[string]docscreen.Module{expense.AdvanceDocType: {Hide: hide}}},
		}
		card := advanceLineCard(d)(c, l)
		if want := map[string]string{"": "ZqNote"}[hidden]; card.Note != want || card.Code != "" || card.Item != "Travel" {
			t.Errorf("hide %q: card %+v, want note %q, the type's name and no code", hidden, card, want)
		}
	}
}

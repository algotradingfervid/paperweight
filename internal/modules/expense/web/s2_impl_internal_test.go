package expenseweb

import (
	"bytes"
	"context"
	"net/http/httptest"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// An error on any field of Bill in company GSTIN opens it, even on an
// unticked line with no value, including the tax code and HSN/SAC whose
// values alone don't (AD ruling, review 1). Validate gives no tax code or
// HSN/SAC error a page can post, so this draws the disclosure directly.
func TestGSTDisclosure_ErrorOpens(t *testing.T) {
	dt, ok := doctype.Lookup(expense.ClaimDocType)
	if !ok {
		t.Fatal("expense.Claim isn't registered")
	}
	for _, name := range []string{"", "company_bill", "billed_to_id", "supplier_gstin", "invoice_no", "tax_code_id", "hsn_sac", "gst_amount"} {
		s := docscreen.StepState{}
		if name != "" {
			s.Errors = []doctype.FieldError{{Field: name, Message: "Line 1: wrong."}}
		}
		c := docscreen.Ctx{Req: httptest.NewRequest("GET", "/", nil), DT: dt}
		var buf bytes.Buffer
		if err := gstDisclosure(newLineState(c, 0, s), expense.LineForm{BranchGSTINs: 2}).Render(context.Background(), &buf); err != nil {
			t.Fatal(err)
		}
		d, err := goquery.NewDocumentFromReader(&buf)
		if err != nil {
			t.Fatal(err)
		}
		_, open := d.Find("details").Attr("open")
		if open != (name != "") {
			t.Errorf("error on %q: open = %v, want %v", name, open, name != "")
		}
	}
}

package print_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/kernel/print/printtest"
	"github.com/vardhan/internaltools/internal/platform/brand"
	"github.com/vardhan/internaltools/internal/platform/db"
)

var approvedPO = doctype.Info{DocType: testPO, ID: 1, State: doctype.StateApproved, Number: "PO/26-27/00001", Display: "PO/26-27/00001"}

// renderTrade renders d as test.PurchaseOrder through the page shell. With
// fromLetterhead the view takes the letterhead from LetterheadFor(PUNE), as a
// module view does; otherwise it prints d.Letterhead as given.
func renderTrade(t *testing.T, d print.TradeDoc, fromLetterhead bool) string {
	t.Helper()
	reg := print.NewRegistry()
	reg.Register(testPO, func(ctx context.Context, tx *db.Tx, _ int64) (templ.Component, error) {
		if fromLetterhead {
			lh, err := print.LetterheadFor(ctx, tx, printtest.PuneID)
			if err != nil {
				return nil, err
			}
			d.Letterhead = lh
		}
		return print.TradeDocument(d), nil
	})
	var sb strings.Builder
	ctx := print.WithLetterhead(t.Context(), printtest.Letterhead)
	if err := reg.Render(ctx, nil, &sb, approvedPO); err != nil {
		t.Fatalf("Render: %v", err)
	}
	return sb.String()
}

func mustContain(t *testing.T, html string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(html, w) {
			t.Errorf("print missing %q", w)
		}
	}
	if t.Failed() {
		t.Logf("html:\n%s", html)
	}
}

// FILE-051
func TestPrint_PurchaseOrderGolden(t *testing.T) {
	d := printtest.ReferencePO()
	d.Letterhead = print.LetterheadInfo{}
	html := renderTrade(t, d, true)
	printtest.Golden(t, "po_approved.golden.html", []byte(html))
	mustContain(t, html,
		`src="/files/1"`, "Vardhan Infra Projects Pvt Ltd", "Pune Works", "Pune, Maharashtra", "GSTIN: 27AAFCV2449G1Z7",
		"Purchase Order", "PO/26-27/00001", "Date: 24-10-2026", "Shree Steel Traders", "Mumbai, Maharashtra",
		"MS-ANGLE-50 MS Angle 50x50x5", "GSTIN: 27AABCS1429B1ZU",
		"<th>Sr</th>", "<th>Item</th>", "<th>HSN</th>", "<th>Qty</th>", "<th>UoM</th>", "<th>Rate</th>", "<th>Amount</th>",
		"Maharashtra (27)", "7216", "7217", "₹75,000.00", "₹4,800.00", "CGST 9%", "SGST 9%", "₹7,182.00",
		"Grand Total ₹94,164.00", "Rupees Ninety Four Thousand One Hundred Sixty Four Only", "Delivery within 7 days",
		"For Vardhan Infra Projects Pvt Ltd", "Authorised Signatory",
	)
	head, end := strings.Index(html, "<thead>"), strings.Index(html, "</thead>")
	if head < 0 || end < head {
		t.Fatalf("column headers are not inside <thead>: no <thead>...</thead> pair in output")
	}
	if !strings.Contains(html[head:end], "<th>Amount</th>") {
		t.Errorf("column headers are not inside <thead>")
	}
}

// FILE-059
func TestPrint_EscapesUserContent(t *testing.T) {
	d := printtest.ReferencePO()
	d.Party.Name = "<script>alert(1)</script>"
	d.Lines[0].Description = `"><img src=x onerror=alert(1)>`
	d.Terms = "{{.Secret}}"
	html := renderTrade(t, d, false)
	mustContain(t, html, "&lt;script&gt;alert(1)&lt;/script&gt;", "{{.Secret}}")
	if strings.Contains(html, "<img src=x") {
		t.Errorf("description rendered as markup:\n%s", html)
	}
	if strings.Contains(html, "<script") {
		t.Errorf("vendor name rendered as markup:\n%s", html)
	}
}

// FILE-064
func TestPrint_OptionalPartsOmitted(t *testing.T) {
	d := printtest.ReferencePO()
	d.Letterhead.LogoURL = ""
	d.Letterhead.GSTIN = ""
	d.Party = print.Party{Label: "Vendor", Name: "Ganesh Hardware", Address: []string{"Pune, Maharashtra"}}
	d.Terms = ""
	html := renderTrade(t, d, false)
	mustContain(t, html, `<img class="logo" src="`+brand.DocumentLogoURL)
	if n, m := strings.Count(html, "GSTIN:"), strings.Count(html, "GSTIN: Unregistered"); n != 1 || m != 1 {
		t.Errorf("GSTIN: occurs %d times (Unregistered %d), want only the vendor's GSTIN: Unregistered:\n%s", n, m, html)
	}
	if strings.Contains(html, "Terms") {
		t.Errorf("Terms heading rendered with no terms:\n%s", html)
	}
}

// FILE-065
func TestPrint_UsesNUMFormatting(t *testing.T) {
	d := printtest.ReferencePO()
	d.Lines = []print.TradeLine{{Sr: 1, Item: "MS-ANGLE-50 MS Angle 50x50x5", HSN: "7216", Qty: 1_200_000, UoM: "kg", Rate: 625_000, Amount: 123456789}}
	d.ApprovedBy = "Ravi Kulkarni"
	d.ApprovedAt = time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC)
	html := renderTrade(t, d, false)
	mustContain(t, html, "₹12,34,567.89", `<td class="qty">1,200</td><td class="uom">kg</td>`, "Date: 24-10-2026", "Ravi Kulkarni", "24-10-2026 10:30")
}

// FILE-082
func TestPrint_WordsUsePayableTotal(t *testing.T) {
	d := printtest.ReferencePO()
	d.GrandTotal, d.RoundedTotal, d.HasRoundedTotal = 1_411_280, 1_411_300, true
	html := renderTrade(t, d, false)
	mustContain(t, html, "Rounded Total ₹14,113.00", "Rupees Fourteen Thousand One Hundred Thirteen Only")
	if got, err := d.AmountInWords(); err != nil || got != "Rupees Fourteen Thousand One Hundred Thirteen Only" {
		t.Errorf("AmountInWords() = %q, %v", got, err)
	}

	d.RoundedTotal, d.HasRoundedTotal = d.GrandTotal, false
	html = renderTrade(t, d, false)
	mustContain(t, html, "Rupees Fourteen Thousand One Hundred Twelve and Paise Eighty Only")
	if strings.Contains(html, "Rounded Total") {
		t.Errorf("Rounded Total shown with rounding off:\n%s", html)
	}
	if d.PayableTotal() != 1_411_280 {
		t.Errorf("PayableTotal() = %d, want 1411280", d.PayableTotal())
	}
}

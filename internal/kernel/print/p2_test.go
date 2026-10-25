package print_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/kernel/print/printtest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

const (
	testGRN     = "test.GRN"
	testExpense = "test.Expense"
)

var approvedAt = time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC)

// renderView renders c as info's print page on PUNE's letterhead.
func renderView(t *testing.T, info doctype.Info, c templ.Component) string {
	t.Helper()
	reg := print.NewRegistry()
	reg.Register(info.DocType, func(context.Context, *db.Tx, int64) (templ.Component, error) { return c, nil })
	return printtest.RenderString(t, reg, info)
}

func referenceGRN() print.GRNDoc {
	return print.GRNDoc{
		Letterhead:  printtest.Letterhead[printtest.PuneID],
		Date:        types.Date{Year: 2026, Month: 10, Day: 24},
		Party:       print.Party{Label: "Vendor", Name: "Shree Steel Traders", Address: []string{"Mumbai, Maharashtra"}, GSTIN: "27AABCS1429B1ZU"},
		PONumber:    "PO/26-27/00001",
		PODate:      types.Date{Year: 2026, Month: 10, Day: 24},
		ChallanNo:   "SST/1182",
		ChallanDate: types.Date{Year: 2026, Month: 10, Day: 23},
		Lines: []print.GRNLine{
			{Sr: 1, Item: "MS-ANGLE-50 MS Angle 50x50x5", HSN: "7216", UoM: "kg", Received: 1_200_000, Accepted: 1_180_000, Rejected: 20_000},
		},
	}
}

// FILE-067
func TestPrint_GRNGolden(t *testing.T) {
	info := doctype.Info{DocType: testGRN, ID: 1, State: doctype.StateApproved, Number: "GR/26-27/00001", Display: "GR/26-27/00001"}
	html := renderView(t, info, print.GRNDocument(referenceGRN()))
	printtest.Golden(t, "grn_approved.golden.html", []byte(html))
	mustContain(t, html,
		"Goods Receipt Note", "GR/26-27/00001", "Against PO: PO/26-27/00001 dated 24-10-2026",
		"Vendor Challan No.: SST/1182", "Challan Date: 23-10-2026", "Shree Steel Traders",
		"<th>Received</th><th>Accepted</th><th>Rejected</th>",
		`<td class="qty">1,200</td><td class="qty">1,180</td><td class="qty">20</td>`,
		"MS-ANGLE-50 MS Angle 50x50x5", "kg",
	)
}

// FILE-068
func TestPrint_ExpenseGolden(t *testing.T) {
	d := print.ExpenseDoc{
		Letterhead: printtest.Letterhead[printtest.PuneID],
		Employee:   "Ravi Kulkarni (EMP-003)",
		PeriodFrom: types.Date{Year: 2026, Month: 10, Day: 20},
		PeriodTo:   types.Date{Year: 2026, Month: 10, Day: 23},
		Lines: []print.ExpenseLine{
			{Sr: 1, Date: types.Date{Year: 2026, Month: 10, Day: 20}, Type: "TRAVEL", Description: "Taxi to site", Amount: 125_000, HasReceipt: true},
			{Sr: 2, Date: types.Date{Year: 2026, Month: 10, Day: 21}, Type: "LODGING", Description: "Hotel, one night", Amount: 350_000, HasReceipt: true},
			{Sr: 3, Date: types.Date{Year: 2026, Month: 10, Day: 22}, Type: "FOOD", Description: "Lunch", Amount: 48_050},
		},
		GrandTotal:      523_050,
		RoundedTotal:    523_100,
		HasRoundedTotal: true,
		Attachments:     []string{"taxi.jpg", "hotel.pdf"},
		ApprovedBy:      "Ravi Kulkarni",
		ApprovedAt:      approvedAt,
	}
	info := doctype.Info{DocType: testExpense, ID: 1, State: doctype.StateApproved, Number: "EX/26-27/00001", Display: "EX/26-27/00001"}
	html := renderView(t, info, print.ExpenseDocument(d))
	printtest.Golden(t, "expense_approved.golden.html", []byte(html))
	mustContain(t, html,
		"Expense Claim", "EX/26-27/00001", "Employee: Ravi Kulkarni (EMP-003)", "Period: 20-10-2026 to 23-10-2026",
		"TRAVEL", "Taxi to site", "20-10-2026", "₹1,250.00", "₹480.50",
		"Grand Total ₹5,230.50", "Rounded Total ₹5,231.00", "Rupees Five Thousand Two Hundred Thirty One Only",
		"Attachments: taxi.jpg, hotel.pdf", "Approved by Ravi Kulkarni on 24-10-2026 10:30",
	)
	if n, m := strings.Count(html, "Receipt: Yes"), strings.Count(html, "Receipt: No"); n != 2 || m != 1 {
		t.Errorf("Receipt: Yes %d times, Receipt: No %d times; want 2 and 1:\n%s", n, m, html)
	}
}

// FILE-069
func TestPrint_ApprovalAndAmendLines(t *testing.T) {
	d := printtest.ReferencePO()
	d.ApprovedBy, d.ApprovedAt = "Ravi Kulkarni", approvedAt
	html := renderTrade(t, d, false)
	mustContain(t, html, "Approved by Ravi Kulkarni on 24-10-2026 10:30")
	if strings.Contains(html, "Amends") {
		t.Errorf("unamended PO shows an Amends line:\n%s", html)
	}

	d = printtest.ReferencePO()
	d.AmendsNumber = "PO/26-27/00001"
	info := doctype.Info{DocType: testPO, ID: 2, State: doctype.StateApproved, Number: "PO/26-27/00001-1", Display: "PO/26-27/00001-1"}
	html = renderView(t, info, print.TradeDocument(d))
	mustContain(t, html, "PO/26-27/00001-1", "Amends PO/26-27/00001")
	if strings.Contains(html, "Approved by") {
		t.Errorf("print with no approver shows an Approved by line:\n%s", html)
	}
}

// FILE-071
func TestPrint_EmptyDraft(t *testing.T) {
	d := printtest.ReferencePO()
	d.Lines, d.Taxes = nil, nil
	d.Taxable, d.GrandTotal, d.RoundedTotal = 0, 0, 0
	info := doctype.Info{DocType: testPO, ID: 42, State: doctype.StateDraft, Display: "Draft 42"}
	html := renderView(t, info, print.TradeDocument(d))
	mustContain(t, html,
		`<p class="doc-number">Draft 42</p>`, `<tr><td colspan="7">No items</td></tr>`,
		"Grand Total ₹0.00", "Rupees Zero Only", `<div class="watermark" data-status="draft">DRAFT</div>`,
	)
	if n := strings.Count(html, "<tr>"); n != 2 {
		t.Errorf("lines table has %d rows, want the header and one No items row:\n%s", n, html)
	}
}

// FILE-083
func TestPrintModel_WordsDerived(t *testing.T) {
	po := printtest.ReferencePO()
	got, err := po.AmountInWords()
	want, werr := print.InWordsINR(po.PayableTotal())
	if err != nil || werr != nil || got != want {
		t.Errorf("AmountInWords() = %q, %v; InWordsINR(PayableTotal()) = %q, %v", got, err, want, werr)
	}
	paths, err := filepath.Glob(filepath.Join("..", "..", "..", "migrations", "005*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(paths, filepath.Join("..", "..", "..", "migrations", "00500_attachment.sql")) {
		t.Fatalf("migrations/00500_attachment.sql not found in %v", paths)
	}
	for _, p := range paths {
		b, err := os.ReadFile(p) //nolint:gosec // G304: p is a migrations/005*.sql glob match
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.ToLower(string(b)), "in_words") {
			t.Errorf("%s stores in_words; amount in words is derived at render time", p)
		}
	}
}

// Supporting test (no case ID; feeds WP3's FILE-058 and FILE-070).
func TestPrint_ToolbarAndFooterServed(t *testing.T) {
	html := renderPO(t, doctype.StateApproved, 1, "PO/26-27/00001", "PO/26-27/00001")
	mustContain(t, html,
		`<div class="toolbar"><button id="print-btn" type="button">Print / Save as PDF</button><button id="close-btn" type="button">Close</button></div>`,
		`<script src="/print/assets/print.js">`,
		`<link rel="stylesheet" href="/print/test.PurchaseOrder/1/page.css">`,
	)
	if !strings.Contains(print.CSS, "@media print { .toolbar { display: none; } }") {
		t.Errorf("print.CSS does not hide the toolbar in @media print")
	}
	for _, want := range []string{"getElementById('print-btn')", "window.print()", "getElementById('close-btn')", "window.close()"} {
		if !strings.Contains(print.JS, want) {
			t.Errorf("print.JS missing %q", want)
		}
	}
}

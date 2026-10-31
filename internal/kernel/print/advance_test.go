package print_test

import (
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/kernel/print/printtest"
	"github.com/vardhan/internaltools/internal/types"
)

const testAdvance = "test.Advance"

// MOD-EXP X3: the claim print with every additive field set; the words
// follow NetPayable, not the total (Expense spec §10).
func TestPrint_ExpenseAdvanceFields(t *testing.T) {
	d := print.ExpenseDoc{
		Letterhead: printtest.Letterhead[printtest.PuneID],
		Employee:   "Deepak Nair (EMP-007)",
		PeriodFrom: types.Date{Year: 2026, Month: 10, Day: 20},
		PeriodTo:   types.Date{Year: 2026, Month: 10, Day: 23},
		Purpose:    "Site visit, Pune Metro Depot",
		Lines: []print.ExpenseLine{
			{Sr: 1, Date: types.Date{Year: 2026, Month: 10, Day: 20}, Type: "TRAVEL", Description: "Taxi to site", Project: "PRJ-METRO", Amount: 125_000, HasReceipt: true},
			{Sr: 2, Date: types.Date{Year: 2026, Month: 10, Day: 21}, Type: "LODGING", Description: "Hotel, one night", Project: "PRJ-METRO", Amount: 350_000, HasReceipt: true},
			{Sr: 3, Date: types.Date{Year: 2026, Month: 10, Day: 22}, Type: "FOOD", Description: "Lunch", Amount: 25_000},
		},
		GrandTotal:    500_000,
		LessAdvance:   300_000,
		AdvanceNumber: "EA/26-27/00001",
		NetPayable:    200_000,
		HasNetPayable: true,
		Attachments:   []string{"taxi.jpg", "hotel.pdf"},
		ApprovedBy:    "Priya Shah",
		ApprovedAt:    approvedAt,
	}
	info := doctype.Info{DocType: testExpense, ID: 1, State: doctype.StateApproved, Number: "EX/26-27/00001", Display: "EX/26-27/00001"}
	html := renderView(t, info, print.ExpenseDocument(d))
	printtest.Golden(t, "expense_advance_fields.golden.html", []byte(html))
	mustContain(t, html,
		"Expense Claim", "EX/26-27/00001", "Employee: Deepak Nair (EMP-007)", "Purpose: Site visit, Pune Metro Depot",
		"Grand Total ₹5,000.00", "Advance: EA/26-27/00001", "Less Advance ₹3,000.00", "To Pay ₹2,000.00",
		`<p class="words">Rupees Two Thousand Only</p>`, "Approved by Priya Shah on 24-10-2026 10:30",
	)
	if n := strings.Count(html, "Project: PRJ-METRO"); n != 2 {
		t.Errorf("Project: PRJ-METRO %d times, want 2 (the line without a project shows none):\n%s", n, html)
	}
	if strings.Contains(html, "Rupees Five Thousand") {
		t.Errorf("words follow the total, want NetPayable:\n%s", html)
	}

	// A net payable of zero is still set: the words say zero.
	d.LessAdvance, d.NetPayable = 500_000, 0
	html = renderView(t, info, print.ExpenseDocument(d))
	mustContain(t, html, "To Pay ₹0.00", `<p class="words">Rupees Zero Only</p>`)
}

// MOD-EXP X3: the Employee Advance print (Expense spec §10).
func TestPrint_AdvanceGolden(t *testing.T) {
	d := print.AdvanceDoc{
		Letterhead: printtest.Letterhead[printtest.PuneID],
		Employee:   "Deepak Nair (EMP-007)",
		Date:       types.Date{Year: 2026, Month: 10, Day: 24},
		Purpose:    "Site visit, Pune Metro Depot",
		Lines: []print.AdvanceLine{
			{Sr: 1, Type: "TRAVEL", Description: "Train and taxi", Project: "PRJ-METRO", Amount: 200_000},
			{Sr: 2, Type: "LODGING", Amount: 100_000},
		},
		Total:      300_000,
		ApprovedBy: "Priya Shah",
		ApprovedAt: approvedAt,
	}
	info := doctype.Info{DocType: testAdvance, ID: 1, State: doctype.StateApproved, Number: "EA/26-27/00001", Display: "EA/26-27/00001"}
	html := renderView(t, info, print.AdvanceDocument(d))
	printtest.Golden(t, "advance_approved.golden.html", []byte(html))
	mustContain(t, html,
		`<h1 class="doc-title">Employee Advance`, "EA/26-27/00001", "Employee: Deepak Nair (EMP-007)", "Date: 24-10-2026",
		"Purpose: Site visit, Pune Metro Depot", "TRAVEL", "Train and taxi", "Project: PRJ-METRO", "LODGING",
		"₹2,000.00", "₹1,000.00", "Total ₹3,000.00", `<p class="words">Rupees Three Thousand Only</p>`,
		"Approved by Priya Shah on 24-10-2026 10:30", "For Vardhan Infra Projects Pvt Ltd", "Authorised Signatory",
	)
	if n := strings.Count(html, "Project: "); n != 1 {
		t.Errorf("Project: %d times, want 1 (the line without a project shows none):\n%s", n, html)
	}

	// A total the words can't be written for prints the figures only.
	d.Total = -1
	html = renderView(t, info, print.AdvanceDocument(d))
	if strings.Contains(html, `<p class="words">`) {
		t.Errorf("negative total renders a words line:\n%s", html)
	}
}

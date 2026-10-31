package expense_test

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/types"
)

// e8LineKey identifies a report row: its claim, date and note.
func e8LineKey(r expense.LineRow) string {
	return fmt.Sprintf("%s %s %s", r.Claim, r.Date.ISO(), r.Note)
}

// EXP-035: one row per line of an approved or closed claim, every spec §13
// column read back by value (labels from the snapshots), the report's total
// to the paisa, and the month, type, project and employee filters each
// narrowing the rows. Scope on the real auth.Service: Priya sees her team
// only. The world is expensetest.E8LinesScenario.
func TestExp_LinesReport(t *testing.T) {
	ctx := context.Background()
	e := expensetest.New(t)
	e.E8LinesScenario(t)

	hotel := expense.LineRow{
		Claim: "EX/26-27/00001", Employee: "Deepak Nair", Date: e8Date(t, "2026-09-30"), Type: "Lodging",
		Project: "PRJ-METRO", Site: "METRO-SITE", Note: "Hotel Pune 2 nights", Amount: 472000, GST: 72000,
		SupplierGSTIN: "27AABCS1429B1ZU", InvoiceNo: "INV/778", BilledToGSTIN: "29AAFCV2449G1Z3",
		Receipt: true, State: doc.StateApproved,
	}
	lunch := expense.LineRow{
		Claim: "EX/26-27/00001", Employee: "Deepak Nair", Date: e8Date(t, "2026-10-22"), Type: "Food",
		Project: "PRJ-TECHPARK", Site: "TECH-SITE", Note: "Team lunch", Amount: 83055, GST: 0,
		SupplierGSTIN: "", InvoiceNo: "", BilledToGSTIN: "",
		Receipt: false, State: doc.StateApproved,
	}
	fuel := expense.LineRow{
		Claim: "EX/26-27/00002", Employee: "Kavya Rao", Date: e8Date(t, "2026-10-23"), Type: "Fuel",
		Project: "", Site: "", Note: "Diesel for site jeep", Amount: 5, GST: 0,
		SupplierGSTIN: "", InvoiceNo: "", BilledToGSTIN: "",
		Receipt: false, State: doc.StateClosed,
	}

	for _, tc := range []struct {
		name, email string
		filter      expense.LinesFilter
		want        []expense.LineRow
		total       types.Money
	}{
		{"all", "meena@vardhan.test", expense.LinesFilter{}, []expense.LineRow{hotel, lunch, fuel}, 555060},
		{"month 2026-09", "meena@vardhan.test", expense.LinesFilter{Month: "2026-09"}, []expense.LineRow{hotel}, 472000},
		{"month 2026-10", "meena@vardhan.test", expense.LinesFilter{Month: "2026-10"}, []expense.LineRow{lunch, fuel}, 83060},
		{"type FOOD", "meena@vardhan.test", expense.LinesFilter{TypeID: e.E8Type(t, "FOOD")}, []expense.LineRow{lunch}, 83055},
		{"project PRJ-METRO", "meena@vardhan.test", expense.LinesFilter{ProjectID: e.E8Project(t, "PRJ-METRO")}, []expense.LineRow{hotel}, 472000},
		{"employee Kavya", "meena@vardhan.test", expense.LinesFilter{EmployeeID: e.E8Employee(t, "EMP-006")}, []expense.LineRow{fuel}, 5},
		{"priya's team", "priya@vardhan.test", expense.LinesFilter{}, []expense.LineRow{hotel, lunch}, 555055},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rep, err := e.Service.ExpenseLinesReport(ctx, e.P(t, tc.email), tc.filter)
			if err != nil {
				t.Fatalf("ExpenseLinesReport: %v", err)
			}
			wantLines(t, rep.Rows, tc.want)
			if rep.Total != tc.total {
				t.Errorf("total = %s, want %s", rep.Total.Plain(), tc.total.Plain())
			}
		})
	}
}

// EXP-052: filtered to PRJ-METRO, the lines report lists only the approved
// and closed claims' lines and their total (CD-271 G6, CD-281 (e)). A
// cancel takes the approved claim's lines out.
func TestExp_LinesReportStates(t *testing.T) {
	ctx := context.Background()
	e := expensetest.New(t)
	deepak, priya, meena := e.P(t, "deepak@vardhan.test"), e.P(t, "priya@vardhan.test"), e.P(t, "meena@vardhan.test")
	metro := e8itoa(e.E8Project(t, "PRJ-METRO"))
	line := func(note, amount string) expensetest.Values {
		return expensetest.Values{"description": note, "amount": amount, "project_id": metro}
	}

	e.DraftClaim(t, deepak, nil, line("draft", "100.00"))
	pending := e.DraftClaim(t, deepak, nil, line("pending", "200.00"))
	e.E8Step(t, deepak, expense.ClaimDocType, pending.ID, doc.ActionSubmit, "")
	rejected := e.DraftClaim(t, deepak, nil, line("rejected", "400.00"))
	e.E8Step(t, deepak, expense.ClaimDocType, rejected.ID, doc.ActionSubmit, "")
	e.E8Step(t, priya, expense.ClaimDocType, rejected.ID, doc.ActionReject, "Wrong amount.")
	approved := e.ApprovedClaim(t, deepak, nil, line("approved", "1234.56"),
		expensetest.Values{"description": "other project", "amount": "50.00", "project_id": e8itoa(e.E8Project(t, "PRJ-TECHPARK"))})
	closed := e.ApprovedClaim(t, deepak, nil, line("closed", "0.44"))
	e.E8Step(t, meena, expense.ClaimDocType, closed.ID, doc.ActionClose, "Paid by NEFT 1234.")
	cancelled := e.ApprovedClaim(t, deepak, nil, line("cancelled", "800.00"))
	e.E8Step(t, meena, expense.ClaimDocType, cancelled.ID, doc.ActionCancel, "Entered twice.")
	e.E8Fill(t)

	read := func(t *testing.T) ([]string, types.Money) {
		t.Helper()
		rep, err := e.Service.ExpenseLinesReport(ctx, meena, expense.LinesFilter{ProjectID: e.E8Project(t, "PRJ-METRO")})
		if err != nil {
			t.Fatalf("ExpenseLinesReport: %v", err)
		}
		var notes []string
		for _, r := range rep.Rows {
			notes = append(notes, r.Note)
		}
		slices.Sort(notes)
		return notes, rep.Total
	}

	notes, total := read(t)
	if want := []string{"approved", "closed"}; !slices.Equal(notes, want) {
		t.Errorf("PRJ-METRO lines = %q, want %q (approved and closed claims only)", notes, want)
	}
	if want := types.Money(123500); total != want {
		t.Errorf("PRJ-METRO total = %s, want %s", total.Plain(), want.Plain())
	}

	e.E8Step(t, meena, expense.ClaimDocType, approved.ID, doc.ActionCancel, "Entered twice.")
	e.E8Fill(t)
	notes, total = read(t)
	if want := []string{"closed"}; !slices.Equal(notes, want) {
		t.Errorf("after the cancel, PRJ-METRO lines = %q, want %q", notes, want)
	}
	if want := types.Money(44); total != want {
		t.Errorf("after the cancel, PRJ-METRO total = %s, want %s", total.Plain(), want.Plain())
	}
}

func e8itoa(n int64) string { return fmt.Sprint(n) }

// wantLines checks got holds exactly want's lines, in any order, one
// assertion per column.
func wantLines(t *testing.T, got, want []expense.LineRow) {
	t.Helper()
	var gotKeys, wantKeys []string
	for _, r := range got {
		gotKeys = append(gotKeys, e8LineKey(r))
	}
	for _, r := range want {
		wantKeys = append(wantKeys, e8LineKey(r))
	}
	slices.Sort(gotKeys)
	slices.Sort(wantKeys)
	if !slices.Equal(gotKeys, wantKeys) {
		t.Errorf("rows = %q, want %q", gotKeys, wantKeys)
	}
	for _, w := range want {
		k := e8LineKey(w)
		i := slices.IndexFunc(got, func(r expense.LineRow) bool { return e8LineKey(r) == k })
		if i < 0 {
			continue
		}
		g := got[i]
		if g.Claim != w.Claim {
			t.Errorf("%s: claim = %q, want %q", k, g.Claim, w.Claim)
		}
		if g.Employee != w.Employee {
			t.Errorf("%s: employee = %q, want %q", k, g.Employee, w.Employee)
		}
		if g.Date != w.Date {
			t.Errorf("%s: date = %s, want %s", k, g.Date.ISO(), w.Date.ISO())
		}
		if g.Type != w.Type {
			t.Errorf("%s: type = %q, want %q", k, g.Type, w.Type)
		}
		if g.Project != w.Project {
			t.Errorf("%s: project = %q, want %q", k, g.Project, w.Project)
		}
		if g.Site != w.Site {
			t.Errorf("%s: site = %q, want %q", k, g.Site, w.Site)
		}
		if g.Note != w.Note {
			t.Errorf("%s: note = %q, want %q", k, g.Note, w.Note)
		}
		if g.Amount != w.Amount {
			t.Errorf("%s: amount = %s, want %s", k, g.Amount.Plain(), w.Amount.Plain())
		}
		if g.GST != w.GST {
			t.Errorf("%s: GST = %s, want %s", k, g.GST.Plain(), w.GST.Plain())
		}
		if g.SupplierGSTIN != w.SupplierGSTIN {
			t.Errorf("%s: supplier GSTIN = %q, want %q", k, g.SupplierGSTIN, w.SupplierGSTIN)
		}
		if g.InvoiceNo != w.InvoiceNo {
			t.Errorf("%s: invoice no. = %q, want %q", k, g.InvoiceNo, w.InvoiceNo)
		}
		if g.BilledToGSTIN != w.BilledToGSTIN {
			t.Errorf("%s: billed-to GSTIN = %q, want %q", k, g.BilledToGSTIN, w.BilledToGSTIN)
		}
		if g.Receipt != w.Receipt {
			t.Errorf("%s: receipt = %t, want %t", k, g.Receipt, w.Receipt)
		}
		if g.State != w.State {
			t.Errorf("%s: state = %q, want %q", k, g.State, w.State)
		}
	}
}

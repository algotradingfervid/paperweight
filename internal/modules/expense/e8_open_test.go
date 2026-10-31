package expense_test

import (
	"context"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/types"
)

// e8Date is the ISO date s.
func e8Date(t *testing.T, s string) types.Date {
	t.Helper()
	d, err := types.DateFromISO(s)
	if err != nil {
		t.Fatalf("date %q: %v", s, err)
	}
	return d
}

// EXP-034: the open claims report lists approved, not closed claims with
// every spec §13 column, read through Scope for read on the real
// auth.Service: Priya (Employee, read team) sees Deepak's rows only, Meena
// (Director, read all) every row. Closed, cancelled and pending claims are
// left out. The world is expensetest.E8OpenScenario.
func TestExp_OpenReports(t *testing.T) {
	ctx := context.Background()
	e := expensetest.New(t)
	e.E8OpenScenario(t)

	// d1: approved by Priya at 2026-10-27 01:00 IST, which is 2026-10-26 in
	// UTC: approved on is the IST business date.
	d1 := expense.OpenClaimRow{
		Claim: "EX/26-27/00001", Employee: "Deepak Nair", Purpose: "Pune Metro site visit",
		ApprovedOn: e8Date(t, "2026-10-27"), ApprovedBy: "Priya Shah",
		Total: 208055, LessAdvance: 100000, ToPay: 108055, NoReceipt: 1,
	}
	k1 := expense.OpenClaimRow{
		Claim: "EX/26-27/00002", Employee: "Kavya Rao", Purpose: "Whitefield client meeting",
		ApprovedOn: e8Date(t, "2026-10-28"), ApprovedBy: "Meena Iyer",
		Total: 200001, LessAdvance: 0, ToPay: 200001, NoReceipt: 2,
	}

	t.Run("claims", func(t *testing.T) {
		for _, tc := range []struct {
			email string
			want  []expense.OpenClaimRow
		}{
			{"meena@vardhan.test", []expense.OpenClaimRow{d1, k1}},
			{"priya@vardhan.test", []expense.OpenClaimRow{d1}},
		} {
			t.Run(tc.email, func(t *testing.T) {
				rows, err := e.Service.OpenClaimsReport(ctx, e.P(t, tc.email))
				if err != nil {
					t.Fatalf("OpenClaimsReport: %v", err)
				}
				wantOpenClaims(t, rows, tc.want)
			})
		}
	})

	// adv1: paid on 2026-10-25, ₹1,000.00 used by EX/26-27/00001. The clock
	// is 2026-11-03 00:30 IST (2026-11-02 in UTC), so the age in days counts
	// IST days.
	adv1 := expense.AdvanceRow{
		Advance: "EA/26-27/00001", Employee: "Deepak Nair", Date: e8Date(t, "2026-10-24"), Purpose: "Pune Metro site visit",
		Amount: 500000, PaidOn: new(e8Date(t, "2026-10-25")), Used: 100000, Left: 400000, AgeDays: 10,
	}
	adv2 := expense.AdvanceRow{
		Advance: "EA/26-27/00002", Employee: "Kavya Rao", Date: e8Date(t, "2026-10-20"), Purpose: "Bengaluru client visit",
		Amount: 300050, PaidOn: nil, Used: 0, Left: 300050, AgeDays: 14,
	}
	deepak, kavya := e.E8Employee(t, "EMP-007"), e.E8Employee(t, "EMP-006")

	t.Run("advances", func(t *testing.T) {
		for _, tc := range []struct {
			name, email string
			filter      expense.AdvancesFilter
			want        []expense.AdvanceRow
		}{
			{"meena all", "meena@vardhan.test", expense.AdvancesFilter{}, []expense.AdvanceRow{adv1, adv2}},
			{"meena Kavya", "meena@vardhan.test", expense.AdvancesFilter{EmployeeID: kavya}, []expense.AdvanceRow{adv2}},
			{"meena Deepak", "meena@vardhan.test", expense.AdvancesFilter{EmployeeID: deepak}, []expense.AdvanceRow{adv1}},
			{"priya all", "priya@vardhan.test", expense.AdvancesFilter{}, []expense.AdvanceRow{adv1}},
			{"priya Kavya", "priya@vardhan.test", expense.AdvancesFilter{EmployeeID: kavya}, nil},
		} {
			t.Run(tc.name, func(t *testing.T) {
				rows, err := e.Service.AdvancesReport(ctx, e.P(t, tc.email), tc.filter)
				if err != nil {
					t.Fatalf("AdvancesReport: %v", err)
				}
				wantAdvances(t, rows, tc.want)
			})
		}
	})
}

// wantOpenClaims checks got holds exactly want's claims, in any order, one
// assertion per column.
func wantOpenClaims(t *testing.T, got, want []expense.OpenClaimRow) {
	t.Helper()
	var gotClaims, wantClaims []string
	for _, r := range got {
		gotClaims = append(gotClaims, r.Claim)
	}
	for _, r := range want {
		wantClaims = append(wantClaims, r.Claim)
	}
	slices.Sort(gotClaims)
	slices.Sort(wantClaims)
	if !slices.Equal(gotClaims, wantClaims) {
		t.Errorf("claims = %q, want %q", gotClaims, wantClaims)
	}
	for _, w := range want {
		i := slices.IndexFunc(got, func(r expense.OpenClaimRow) bool { return r.Claim == w.Claim })
		if i < 0 {
			continue
		}
		g := got[i]
		if g.Employee != w.Employee {
			t.Errorf("%s: employee = %q, want %q", w.Claim, g.Employee, w.Employee)
		}
		if g.Purpose != w.Purpose {
			t.Errorf("%s: purpose = %q, want %q", w.Claim, g.Purpose, w.Purpose)
		}
		if g.ApprovedOn != w.ApprovedOn {
			t.Errorf("%s: approved on = %s, want %s", w.Claim, g.ApprovedOn.ISO(), w.ApprovedOn.ISO())
		}
		if g.ApprovedBy != w.ApprovedBy {
			t.Errorf("%s: approved by = %q, want %q", w.Claim, g.ApprovedBy, w.ApprovedBy)
		}
		if g.Total != w.Total {
			t.Errorf("%s: total = %s, want %s", w.Claim, g.Total.Plain(), w.Total.Plain())
		}
		if g.LessAdvance != w.LessAdvance {
			t.Errorf("%s: less advance = %s, want %s", w.Claim, g.LessAdvance.Plain(), w.LessAdvance.Plain())
		}
		if g.ToPay != w.ToPay {
			t.Errorf("%s: to pay = %s, want %s", w.Claim, g.ToPay.Plain(), w.ToPay.Plain())
		}
		if g.NoReceipt != w.NoReceipt {
			t.Errorf("%s: lines without a receipt = %d, want %d", w.Claim, g.NoReceipt, w.NoReceipt)
		}
	}
}

// wantAdvances checks got holds exactly want's advances, in any order, one
// assertion per column.
func wantAdvances(t *testing.T, got, want []expense.AdvanceRow) {
	t.Helper()
	var gotAdv, wantAdv []string
	for _, r := range got {
		gotAdv = append(gotAdv, r.Advance)
	}
	for _, r := range want {
		wantAdv = append(wantAdv, r.Advance)
	}
	slices.Sort(gotAdv)
	slices.Sort(wantAdv)
	if !slices.Equal(gotAdv, wantAdv) {
		t.Errorf("advances = %q, want %q", gotAdv, wantAdv)
	}
	for _, w := range want {
		i := slices.IndexFunc(got, func(r expense.AdvanceRow) bool { return r.Advance == w.Advance })
		if i < 0 {
			continue
		}
		g := got[i]
		if g.Employee != w.Employee {
			t.Errorf("%s: employee = %q, want %q", w.Advance, g.Employee, w.Employee)
		}
		if g.Date != w.Date {
			t.Errorf("%s: date = %s, want %s", w.Advance, g.Date.ISO(), w.Date.ISO())
		}
		if g.Purpose != w.Purpose {
			t.Errorf("%s: purpose = %q, want %q", w.Advance, g.Purpose, w.Purpose)
		}
		if g.Amount != w.Amount {
			t.Errorf("%s: amount = %s, want %s", w.Advance, g.Amount.Plain(), w.Amount.Plain())
		}
		switch {
		case w.PaidOn == nil && g.PaidOn != nil:
			t.Errorf("%s: paid on = %s, want blank (unpaid)", w.Advance, g.PaidOn.ISO())
		case w.PaidOn != nil && g.PaidOn == nil:
			t.Errorf("%s: paid on blank, want %s", w.Advance, w.PaidOn.ISO())
		case w.PaidOn != nil && *g.PaidOn != *w.PaidOn:
			t.Errorf("%s: paid on = %s, want %s", w.Advance, g.PaidOn.ISO(), w.PaidOn.ISO())
		}
		if g.Used != w.Used {
			t.Errorf("%s: used = %s, want %s", w.Advance, g.Used.Plain(), w.Used.Plain())
		}
		if g.Left != w.Left {
			t.Errorf("%s: left = %s, want %s", w.Advance, g.Left.Plain(), w.Left.Plain())
		}
		if g.AgeDays != w.AgeDays {
			t.Errorf("%s: age in days = %d, want %d", w.Advance, g.AgeDays, w.AgeDays)
		}
	}
}

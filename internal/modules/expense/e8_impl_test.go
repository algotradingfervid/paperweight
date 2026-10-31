package expense_test

import (
	"context"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// e8ImplExec runs fixture SQL q in one write.
func e8ImplExec(t *testing.T, e *expensetest.Env, q string, args ...any) {
	t.Helper()
	err := e.DB.Write(context.Background(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(context.Background(), q, args...)
		return err
	})
	if err != nil {
		t.Fatalf("fixture SQL: %v", err)
	}
}

// e8ImplOpen is meena's open claims row for claim number, failing when it
// isn't listed.
func e8ImplOpen(t *testing.T, e *expensetest.Env, number string) expense.OpenClaimRow {
	t.Helper()
	rows, err := e.Service.OpenClaimsReport(context.Background(), e.P(t, "meena@vardhan.test"))
	if err != nil {
		t.Fatalf("OpenClaimsReport: %v", err)
	}
	i := slices.IndexFunc(rows, func(r expense.OpenClaimRow) bool { return r.Claim == number })
	if i < 0 {
		t.Fatalf("open claims = %+v, want %s listed", rows, number)
	}
	return rows[i]
}

// A claim approved on submit with no enabled rule (APR's approved_no_rule
// history row) shows that row's date and user as approved on and by.
func TestE8Impl_OpenClaimApprovedNoRule(t *testing.T) {
	ctx := context.Background()
	e := expensetest.New(t)
	admin, deepak := e.P(t, "admin@vardhan.test"), e.P(t, "deepak@vardhan.test")
	rules, err := e.Approvals.Rules(ctx, admin)
	if err != nil {
		t.Fatalf("Rules: %v", err)
	}
	for _, r := range rules {
		if r.DocType == expense.ClaimDocType {
			if err := e.Approvals.SetRuleEnabled(ctx, admin, r.ID, false); err != nil {
				t.Fatalf("SetRuleEnabled: %v", err)
			}
		}
	}
	c := e.DraftClaim(t, deepak, nil)
	if res := e.E8Step(t, deepak, expense.ClaimDocType, c.ID, doc.ActionSubmit, ""); res.State != doc.StateApproved {
		t.Fatalf("state after submit = %q, want approved (no rule)", res.State)
	}
	e.E8Fill(t)

	got := e8ImplOpen(t, e, "EX/26-27/00001")
	if want := e8Date(t, "2026-10-24"); got.ApprovedOn != want {
		t.Errorf("approved on = %s, want %s", got.ApprovedOn.ISO(), want.ISO())
	}
	if got.ApprovedBy != "Deepak Nair" {
		t.Errorf("approved by = %q, want the submitter %q", got.ApprovedBy, "Deepak Nair")
	}
}

// An approved claim with no approval history row is still listed, with
// approved on and by blank.
func TestE8Impl_OpenClaimNoHistory(t *testing.T) {
	e := expensetest.New(t)
	c := e.DraftClaim(t, e.P(t, "deepak@vardhan.test"), nil)
	e8ImplExec(t, e, `UPDATE expense_claim SET state = 'approved', number = 'EX/26-27/00042' WHERE id = ?`, c.ID)
	e.E8Fill(t)

	got := e8ImplOpen(t, e, "EX/26-27/00042")
	if got.ApprovedOn != (types.Date{}) {
		t.Errorf("approved on = %s, want blank", got.ApprovedOn.ISO())
	}
	if got.ApprovedBy != "" {
		t.Errorf("approved by = %q, want blank", got.ApprovedBy)
	}
	if got.Total != 100000 {
		t.Errorf("total = %s, want 1000.00", got.Total.Plain())
	}
}

// Scope is read on each report's own table: a throwaway draft claim of
// Kavya's makes claim ids differ from advance ids, so Priya's open claims
// and advances are her team's only when each report scopes its own table.
func TestE8Impl_ScopeOwnTable(t *testing.T) {
	ctx := context.Background()
	e := expensetest.New(t)
	e.DraftClaim(t, e.P(t, "kavya@vardhan.test"), nil)
	e.E8OpenScenario(t)
	priya := e.P(t, "priya@vardhan.test")

	claims, err := e.Service.OpenClaimsReport(ctx, priya)
	if err != nil {
		t.Fatalf("OpenClaimsReport: %v", err)
	}
	var got []string
	for _, r := range claims {
		got = append(got, r.Claim)
	}
	if want := []string{"EX/26-27/00001"}; !slices.Equal(got, want) {
		t.Errorf("priya's open claims = %q, want %q", got, want)
	}

	advances, err := e.Service.AdvancesReport(ctx, priya, expense.AdvancesFilter{})
	if err != nil {
		t.Fatalf("AdvancesReport: %v", err)
	}
	got = nil
	for _, r := range advances {
		got = append(got, r.Advance)
	}
	if want := []string{"EA/26-27/00001"}; !slices.Equal(got, want) {
		t.Errorf("priya's open advances = %q, want %q", got, want)
	}

	lines, err := e.Service.ExpenseLinesReport(ctx, priya, expense.LinesFilter{})
	if err != nil {
		t.Fatalf("ExpenseLinesReport: %v", err)
	}
	got = nil
	for _, r := range lines.Rows {
		got = append(got, r.Claim)
	}
	slices.Sort(got)
	got = slices.Compact(got)
	if want := []string{"EX/26-27/00001", "EX/26-27/00003"}; !slices.Equal(got, want) {
		t.Errorf("priya's expense lines claims = %q, want %q", got, want)
	}
}

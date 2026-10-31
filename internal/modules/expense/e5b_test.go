package expense_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/types"
)

// E5b: E5 follow-ups through the public path, now that E2's Validate and
// E4's settlement compute the claim's figures.

// e5bBOM starts every CSV export (csvio).
const e5bBOM = "\ufeff"

// A claim set against a paid advance and submitted: the approval request's
// amount is the claim's total, not its net payable (spec §3.1 Hooks.Amount).
func TestE5b_RequestAmountIsTotal(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, e5Deepak)
	adv := e.PaidAdvance(t, deepak, types.Money(1_000_000))
	res := e.DraftClaim(t, deepak, expensetest.Values{"advance_id": e4ID(adv.ID), "less_advance": "1500.00"},
		expensetest.Values{"amount": "2500.00"})
	res = e5MustSubmit(t, e, deepak, expense.ClaimDocType, res)

	if c := e2ReadClaim(t, e, res.ID); e2Int(c.total) != 250000 || e2Int(c.net) != 100000 {
		t.Fatalf("claim total %d, net_payable %d; want 250000, 100000", e2Int(c.total), e2Int(c.net))
	}
	if r := e5Request(t, e, expense.ClaimDocType, res.ID); r.Amount != types.Money(250000) {
		t.Errorf("request amount = %s, want the total ₹2,500.00 (net payable is ₹1,000.00)", types.FormatINR(r.Amount))
	}
}

// The Director's claim CSV: the header holds the claim's field labels, and
// each of the three claims is a row starting with its number.
func TestE5b_DirectorCSV(t *testing.T) {
	e := expensetest.New(t)
	deepak, kavya, priya, meena := e.P(t, e5Deepak), e.P(t, e5Kavya), e.P(t, e5Priya), e.P(t, e5Meena)
	claims := []doc.Result{e.ApprovedClaim(t, deepak, nil), e.ApprovedClaim(t, kavya, nil), e.ApprovedClaim(t, priya, nil)}

	n, csv, err := e5Export(t, e, meena, expense.ClaimDocType)
	if err != nil || n != 3 {
		t.Fatalf("the Director's export = %d rows, %v; want 3 rows", n, err)
	}
	rows := strings.Split(strings.TrimSuffix(csv, "\r\n"), "\r\n")
	const header = "Claim number,Employee,Claim date,Purpose,Advance,Less advance,Total,To pay,Period from,Period to,State,Remarks"
	if len(rows) == 0 || rows[0] != e5bBOM+header {
		t.Fatalf("CSV header = %q, want %q", rows[0], e5bBOM+header)
	}
	var got, want []string
	for _, r := range rows[1:] {
		got = append(got, strings.SplitN(r, ",", 2)[0])
	}
	for _, c := range claims {
		want = append(want, c.Number)
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) || want[0] != "EX/26-27/00001" {
		t.Errorf("CSV claim numbers = %q, want %q", got, want)
	}
}

// An Administrator closes an approved claim (Administrator holds every
// right through is_admin, so CloseRight's cancel too).
func TestE5b_AdminCloses(t *testing.T) {
	e := expensetest.New(t)
	deepak, admin := e.P(t, e5Deepak), e.P(t, e5Admin)
	claim := e.ApprovedClaim(t, deepak, nil)

	res := e5Must(t, e, admin, expense.ClaimDocType, claim.ID, doc.ActionClose,
		doc.ActionInput{Version: claim.Version, Reason: "Paid by NEFT 1234 on 25-10-2026"})
	if res.State != doc.StateClosed {
		t.Errorf("the Administrator's close: state %s, want closed", res.State)
	}
	e5WantState(t, e, expense.ClaimDocType, claim.ID, doctype.StateClosed, claim.Number)
}

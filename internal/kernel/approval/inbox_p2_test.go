package approval_test

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	at "github.com/vardhan/internaltools/internal/kernel/approval/approvaltest"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/types"
)

// stuck is Anil's "Stuck approvals" list; any error fails the test.
func stuck(t *testing.T, e *at.Env) []approval.StuckItem {
	t.Helper()
	items, err := e.A.Stuck(t.Context(), e.Anil)
	at.MustOK(t, err)
	return items
}

// inboxTotal is the Total of p's inbox (what the count badge shows).
func inboxTotal(t *testing.T, e *at.Env, p auth.Principal) int {
	t.Helper()
	res, err := e.A.Inbox(t.Context(), p, types.Page{Number: 1, Size: 25})
	at.MustOK(t, err)
	return res.Total
}

func TestApprovalStuck_ListedForAdmin(t *testing.T) { // APR-043
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	if e.State(t, d) != doctype.StatePending {
		t.Fatalf("Draft 42 is %s, want pending_approval", e.State(t, d))
	}
	// Ravi and Meena can still approve it, so nothing is stuck yet.
	if got := stuck(t, e); len(got) != 0 {
		t.Errorf("Stuck before anyone is disabled = %+v, want empty", got)
	}

	e.Disable(t, e.Ravi.ID)
	e.Disable(t, e.Meena.ID)
	got := stuck(t, e)
	if len(got) != 1 {
		t.Fatalf("Stuck with every Purchase Manager and Director disabled = %+v, want Draft 42 only", got)
	}
	const msg = "No active approver: needs Purchase Manager or Director."
	if it := got[0]; it.Title != "Draft 42" || it.DocType != at.Order || it.DocID != 42 || it.Message != msg {
		t.Errorf("stuck item %+v, want Draft 42 (%s 42) with %q", it, at.Order, msg)
	}

	// Only an Administrator may see the list (ErrNotAdmin, 403).
	items, err := e.A.Stuck(t.Context(), e.Priya)
	if !errors.Is(err, approval.ErrNotAdmin) {
		t.Errorf("Priya's Stuck error = %v, want ErrNotAdmin", err)
	}
	at.WantStatus(t, err, 403)
	if len(items) != 0 {
		t.Errorf("Priya's refused Stuck returned %+v, want nothing", items)
	}

	// An eligible approver exists again: Draft 42 drops off.
	e.Auth.AddRole(e.Kavya.ID, "Purchase Manager")
	if got := stuck(t, e); len(got) != 0 {
		t.Errorf("Stuck after Kavya becomes a Purchase Manager = %+v, want empty", got)
	}
}

func TestApprovalHistory_KeepsRuleAndAmount(t *testing.T) { // APR-084
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	at.MustOK(t, e.Reject(e.Ravi, d, "Too high"))
	at.MustOK(t, e.Revise(e.Priya, d))
	e.SetAmount(t, d, at.Rs(5_50_000))
	at.MustOK(t, e.Submit(e.Priya, d))
	_, err := e.A.UpdateRule(t.Context(), e.Anil, e.R1, 1, approval.RuleInput{DocType: at.Order, FromAmount: at.Rs(75000),
		ApproverType: approval.ApproverRole, Role: "Purchase Manager"})
	at.MustOK(t, err)

	type sub struct {
		amount types.Money
		rule   int64
	}
	var got []sub
	for _, r := range e.History(t, d) {
		if r.Action == approval.HistSubmitted {
			got = append(got, sub{r.Amount, r.RuleID})
		}
	}
	want := []sub{{6000000, e.R1}, {55000000, e.R2}}
	if !slices.Equal(got, want) {
		t.Errorf("submitted rows (amount, rule) = %+v, want %+v (R1 = %d, R2 = %d)", got, want, e.R1, e.R2)
	}
}

// Carry-over for APR-093: Ravi's inbox Total (his count) goes 1 → 0 when
// Priya withdraws.
func TestApproval_Withdraw_InboxCountDrops(t *testing.T) { // APR-093
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	if n := inboxTotal(t, e, e.Ravi); n != 1 {
		t.Fatalf("Ravi's inbox Total before withdraw = %d, want 1", n)
	}
	at.MustOK(t, e.Withdraw(e.Priya, d))
	if n := inboxTotal(t, e, e.Ravi); n != 0 {
		t.Errorf("Ravi's inbox Total after withdraw = %d, want 0", n)
	}
	if n := inboxTotal(t, e, e.Meena); n != 0 {
		t.Errorf("Meena's inbox Total after withdraw = %d, want 0", n)
	}
}

// Carry-over for APR-072: every losing approver gets the full CD-26 message
// naming the winner.
func TestApprovalConcurrency_LoserNamesWinner(t *testing.T) { // APR-072
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	var wg sync.WaitGroup
	errs := make([]error, 10)
	for i := range 10 {
		p := e.Ravi
		if i%2 == 1 {
			p = e.Meena
		}
		wg.Go(func() { errs[i] = e.Approve(p, d) })
	}
	wg.Wait()

	var winner string
	for _, r := range e.History(t, d) {
		if r.Action == approval.HistApproved {
			winner = r.ByName
		}
	}
	if winner != "Ravi Kulkarni" && winner != "Meena Iyer" {
		t.Fatalf("approved history row by %q, want Ravi Kulkarni or Meena Iyer", winner)
	}
	want := "PO/26-27/00001 is no longer waiting for approval. It was approved by " + winner + "."
	ok := 0
	for _, err := range errs {
		if err == nil {
			ok++
			continue
		}
		at.WantErr(t, err, approval.ErrNotPending, want)
		at.WantStatus(t, err, 409)
	}
	if ok != 1 {
		t.Errorf("%d approvals succeeded, want exactly 1", ok)
	}
}

// Carry-over for APR-083: history times are UTC, whatever zone the clock
// reports in.
func TestApprovalHistory_TimesAreUTC(t *testing.T) { // APR-083
	e := at.New(t)
	ist := time.FixedZone("IST", 5*3600+1800)
	at10 := time.Date(2026, 10, 24, 10, 30, 0, 0, ist)
	e.Clock.Set(at10)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	at.MustOK(t, e.Reject(e.Ravi, d, "Too high"))
	at.MustOK(t, e.Revise(e.Priya, d))
	at.MustOK(t, e.Submit(e.Priya, d))
	at.MustOK(t, e.Approve(e.Ravi, d))
	h := e.History(t, d)
	if len(h) != 5 {
		t.Fatalf("history %d rows, want 5", len(h))
	}
	for i, r := range h {
		if r.At.Location() != time.UTC || !r.At.Equal(at10) {
			t.Errorf("row %d (%s) At = %v (%v), want %v in UTC", i, r.Action, r.At, r.At.Location(), at10.UTC())
		}
	}
}

// Carry-over for APR-087: Draft 42 is really approved (then cancelled) before
// the amended Draft 61 is submitted, so "the original approval does not carry
// over" is checked against an approval that exists.
func TestApprovalAmend_OriginalApprovalDoesNotCarryOver(t *testing.T) { // APR-087
	e := at.New(t)
	orig := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, orig))
	at.MustOK(t, e.Approve(e.Ravi, orig))
	if e.State(t, orig) != doctype.StateApproved || e.Number(t, orig) == "" {
		t.Fatalf("Draft 42 is %s with number %q, want approved with a number", e.State(t, orig), e.Number(t, orig))
	}
	at.MustOK(t, e.Cancel(e.Ravi, orig, "Wrong supplier"))
	if e.State(t, orig) != doctype.StateCancelled {
		t.Fatalf("original is %s, want cancelled", e.State(t, orig))
	}

	amended := e.NewDoc(t, at.Order, 61, at.Rs(60000), e.Priya)
	if h := e.History(t, amended); len(h) != 0 {
		t.Errorf("amended Draft 61 starts with history %v, want none", actions(h))
	}
	at.MustOK(t, e.Submit(e.Priya, amended))
	r, ok := e.Latest(t, amended)
	if e.State(t, amended) != doctype.StatePending || !ok || r.RuleID != e.R1 || r.Outcome != "" || r.DocID != 61 {
		t.Errorf("Draft 61 state %s request %+v, want pending with an open request under R1 (%d)", e.State(t, amended), r, e.R1)
	}
	if e.Title(t, amended) != "Draft 61" || e.Number(t, amended) != "" {
		t.Errorf("Draft 61 title %q number %q, want Draft 61 with no number", e.Title(t, amended), e.Number(t, amended))
	}
	if got, want := actions(e.History(t, amended)), []string{approval.HistSubmitted}; !slices.Equal(got, want) {
		t.Errorf("Draft 61 history %v, want %v (no approval carried over)", got, want)
	}
	if got := e.InboxTitles(t, e.Ravi); !slices.Equal(got, []string{"Draft 61"}) {
		t.Errorf("Ravi's inbox %q, want [Draft 61] waiting for fresh approval", got)
	}
}

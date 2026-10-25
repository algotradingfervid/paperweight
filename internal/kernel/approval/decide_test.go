package approval_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	at "github.com/vardhan/internaltools/internal/kernel/approval/approvaltest"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// pending42 is Priya's Draft 42 (₹60,000.00, PRJ-METRO) submitted and pending under R1.
func pending42(t *testing.T) (*at.Env, at.Doc) {
	t.Helper()
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	return e, d
}

// lastHist is the document's newest history row; the test stops when there is none.
func lastHist(t *testing.T, e *at.Env, d at.Doc) approval.HistoryRow {
	t.Helper()
	h := e.History(t, d)
	if len(h) == 0 {
		t.Fatalf("%s %d: no history rows", d.Type, d.ID)
	}
	return h[len(h)-1]
}

// checkApprover calls APR's CheckApprover directly, as a caller other than DOC would.
func checkApprover(t *testing.T, e *at.Env, p auth.Principal, d at.Doc) error {
	t.Helper()
	return e.DB.Write(t.Context(), func(tx *db.Tx) error {
		return e.A.CheckApprover(t.Context(), tx, p, d.Type, d.ID)
	})
}

// record calls APR's Record directly inside one db.Write.
func record(t *testing.T, e *at.Env, d at.Doc, action doctype.Action, p auth.Principal, reason string) error {
	t.Helper()
	return e.DB.Write(t.Context(), func(tx *db.Tx) error {
		return e.A.Record(t.Context(), tx, d.Type, d.ID, action, p, reason)
	})
}

var fixedNow = time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC)

const ownPO = "You can't approve your own Purchase Order. Ask another Purchase Manager or Director to approve it."

func TestApprovalApprove_RoleHolderCanApprove(t *testing.T) { // APR-029
	e, d := pending42(t)
	at.MustOK(t, e.Approve(e.Ravi, d))
	last := lastHist(t, e, d)
	if e.State(t, d) != doctype.StateApproved || e.Number(t, d) != "PO/26-27/00001" {
		t.Errorf("state %s number %q, want approved PO/26-27/00001", e.State(t, d), e.Number(t, d))
	}
	if last.Action != approval.HistApproved || last.By != e.Ravi.ID {
		t.Errorf("last history %+v, want approved by Ravi (%d)", last, e.Ravi.ID)
	}
}

func TestApprovalApprove_NonApproverRefused(t *testing.T) { // APR-030
	e := at.New(t)
	e.Auth.AddRole(e.Ravi.ID, "Purchase User")
	d := e.NewDoc(t, at.Order, 43, at.Rs(60000), e.Ravi, at.Project(at.ProjMetro))
	at.MustOK(t, e.Submit(e.Ravi, d))
	// Priya can read it (Purchase User, PRJ-METRO) but holds no approve right: APR's text, not AUTH's 403 (CD-52).
	err := e.Approve(e.Priya, d)
	at.WantErr(t, err, approval.ErrNotEligible, "Only a user with the Purchase Manager or Director role can approve Draft 43 (₹60,000.00).")
	at.WantStatus(t, err, 403)
	if e.State(t, d) != doctype.StatePending {
		t.Errorf("state %s, want pending_approval", e.State(t, d))
	}
}

func TestApprovalApprove_HigherTierCanApprove(t *testing.T) { // APR-032
	e, d := pending42(t)
	at.MustOK(t, e.Approve(e.Meena, d))
	if e.State(t, d) != doctype.StateApproved {
		t.Errorf("state %s, want approved", e.State(t, d))
	}
}

func TestApprovalApprove_LowerTierRefused(t *testing.T) { // APR-033
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 50, at.Rs(600000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	at.WantErr(t, e.Approve(e.Ravi, d), approval.ErrNotEligible, "Only a user with the Director role can approve Draft 50 (₹6,00,000.00).")
}

func TestApprovalApprove_NeedsReadAccess(t *testing.T) { // APR-034
	e, d := pending42(t)
	e.Auth.Restrict(e.Ravi.ID, []int64{at.ProjTechPark}, nil)
	at.WantNotFound(t, e.Approve(e.Ravi, d)) // CD-52: 404 before APR is called
	at.WantNotFound(t, e.Reject(e.Ravi, d, "No"))
	at.WantNotFound(t, checkApprover(t, e, e.Ravi, d)) // Task 7 step 1: APR repeats the visibility check for direct callers
	if e.State(t, d) != doctype.StatePending {
		t.Errorf("state %s, want pending_approval", e.State(t, d))
	}
	if slices.Contains(e.InboxTitles(t, e.Ravi), "Draft 42") { // passes after Task 8
		t.Error("Draft 42 must not be in Ravi's inbox")
	}
}

func TestApprovalSelf_RequesterBlocked(t *testing.T) { // APR-035
	e := at.New(t)
	e.Auth.AddRole(e.Ravi.ID, "Purchase User")
	d := e.NewDoc(t, at.Order, 43, at.Rs(70000), e.Ravi)
	at.MustOK(t, e.Submit(e.Ravi, d))
	at.WantErr(t, e.Approve(e.Ravi, d), approval.ErrSelfApproval, ownPO)
}

func TestApprovalSelf_CreatorOrSubmitter(t *testing.T) { // APR-036
	e := at.New(t)
	e.Auth.AddRole(e.Ravi.ID, "Purchase User")
	d44 := e.NewDoc(t, at.Order, 44, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Ravi, d44))
	d45 := e.NewDoc(t, at.Order, 45, at.Rs(60000), e.Ravi)
	at.MustOK(t, e.Submit(e.Priya, d45))
	for _, d := range []at.Doc{d44, d45} {
		at.WantErr(t, e.Approve(e.Ravi, d), approval.ErrSelfApproval, ownPO)
		at.MustOK(t, e.Approve(e.Meena, d))
	}
}

func TestApprovalSelf_ClaimantBlocked(t *testing.T) { // APR-037
	e := at.New(t)
	e.Auth.AddRole(e.Ravi.ID, "Director")
	d := e.NewDoc(t, at.Claim, 12, at.Rs(3450), e.Anil, at.ForEmployee("EMP-003"))
	at.MustOK(t, e.Submit(e.Anil, d))
	at.WantErr(t, e.Approve(e.Ravi, d), approval.ErrSelfApproval, "You can't approve your own Expense Claim. Ask another Director to approve it.")
}

func TestApprovalSelf_AdminNotExempt(t *testing.T) { // APR-039
	e := at.New(t)
	e.Auth.AddRole(e.Anil.ID, "Purchase Manager")
	d := e.NewDoc(t, at.Order, 46, at.Rs(60000), e.Anil)
	at.MustOK(t, e.Submit(e.Anil, d))
	at.WantErr(t, e.Approve(e.Anil, d), approval.ErrSelfApproval, ownPO)
}

func TestApprovalEligible_LiveRoleCheck(t *testing.T) { // APR-042
	e, d := pending42(t)
	e.Auth.AddRole(e.Kavya.ID, "Purchase Manager")
	at.MustOK(t, e.Approve(e.Kavya, d))

	e2, d2 := pending42(t)
	e2.Auth.AddRole(e2.Ravi.ID, "Stores") // Stores reads every fixture.Order, so DOC's visibility check lets him through
	e2.RemoveRole(t, e2.Ravi.ID, "Purchase Manager")
	at.WantErr(t, e2.Approve(e2.Ravi, d2), approval.ErrNotEligible, "Only a user with the Purchase Manager or Director role can approve Draft 42 (₹60,000.00).")
	if e2.State(t, d2) != doctype.StatePending {
		t.Errorf("state %s, want pending_approval", e2.State(t, d2))
	}
}

func TestApprovalExpense_OtherManagerRefused(t *testing.T) { // APR-045 (CD-11 team scope, CD-52 order)
	e := at.New(t)
	d := e.NewDoc(t, at.Claim, 10, at.Rs(3450), e.Deepak)
	at.MustOK(t, e.Submit(e.Deepak, d))
	at.WantNotFound(t, e.Approve(e.Ravi, d)) // Ravi's team is Priya and Suresh, not Deepak: he can't see Draft 10 → 404
	// Anil sees every claim and holds approve, but is neither Priya nor a Director.
	at.WantErr(t, e.Approve(e.Anil, d), approval.ErrNotEligible,
		"Only Priya Shah or a user with the Director role can approve Draft 10 (₹3,450.00).")
	if e.State(t, d) != doctype.StatePending {
		t.Errorf("state %s, want pending_approval", e.State(t, d))
	}
	if slices.Contains(e.InboxTitles(t, e.Ravi), "Draft 10") { // passes after Task 8
		t.Error("Draft 10 must not be in Ravi's inbox")
	}
}

func TestApprovalExpense_ManagerDisabledWhilePending(t *testing.T) { // APR-049
	setup := func(t *testing.T) (*at.Env, at.Doc) {
		e := at.New(t)
		d := e.NewDoc(t, at.Claim, 10, at.Rs(3450), e.Deepak)
		at.MustOK(t, e.Submit(e.Deepak, d))
		e.Disable(t, e.Priya.ID)
		return e, d
	}
	e, d := setup(t)
	at.MustOK(t, e.Approve(e.Meena, d))
	if e.State(t, d) != doctype.StateApproved {
		t.Errorf("state %s, want approved", e.State(t, d))
	}

	e2, _ := setup(t) // the inbox move (passes after Task 8)
	if slices.Contains(e2.InboxTitles(t, e2.Priya), "Draft 10") || !slices.Contains(e2.InboxTitles(t, e2.Meena), "Draft 10") {
		t.Error("the claim must move from Priya's list to the Director list")
	}
}

func TestApprovalApprove_OnlyFromPending(t *testing.T) { // APR-055
	e := at.New(t)
	cases := []struct {
		id     int64
		state  doctype.State
		number string
		msg    string
	}{
		{60, doctype.StateDraft, "", "Draft 60 is not waiting for approval. It is Draft."},
		{61, doctype.StateApproved, "PO/26-27/00001", "PO/26-27/00001 is not waiting for approval. It is Approved."},
		{62, doctype.StateRejected, "", "Draft 62 is not waiting for approval. It is Rejected."},
		{63, doctype.StateCancelled, "PO/26-27/00002", "PO/26-27/00002 is not waiting for approval. It is Cancelled."},
		{64, doctype.StateClosed, "PO/26-27/00003", "PO/26-27/00003 is not waiting for approval. It is Closed."},
	}
	for _, c := range cases {
		d := e.NewDoc(t, at.Order, c.id, at.Rs(60000), e.Priya, at.InState(c.state, c.number))
		at.WantErr(t, e.Approve(e.Ravi, d), approval.ErrNotPending, c.msg)
	}
}

func TestApprovalReject_NotAfterApprove(t *testing.T) { // APR-057
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 61, at.Rs(60000), e.Priya, at.InState(doctype.StateApproved, "PO/26-27/00001"))
	at.WantErr(t, e.Reject(e.Meena, d, "Too late"), approval.ErrNotPending, "PO/26-27/00001 is not waiting for approval. It is Approved.")
}

func TestApprovalReject_ReasonRequired(t *testing.T) { // APR-058
	e, d := pending42(t)
	for _, r := range []string{"", "   "} {
		at.WantErr(t, e.Reject(e.Ravi, d, r), approval.ErrReasonRequired, "Enter a reason for rejecting.")
	}
	if e.State(t, d) != doctype.StatePending {
		t.Errorf("state %s, want pending_approval", e.State(t, d))
	}
}

func TestApprovalReject_StoresReason(t *testing.T) { // APR-060
	e, d := pending42(t)
	reason := "Rate is 12% above last PO. Renegotiate with vendor."
	at.MustOK(t, e.Reject(e.Ravi, d, reason))
	if e.State(t, d) != doctype.StateRejected || e.Effects(t, d) != 0 || e.Title(t, d) != "Draft 42" {
		t.Errorf("state %s, effects %d, title %q; want rejected, 0, Draft 42", e.State(t, d), e.Effects(t, d), e.Title(t, d))
	}
	last := lastHist(t, e, d)
	if last.Action != approval.HistRejected || last.Reason != reason || last.By != e.Ravi.ID || !last.At.Equal(fixedNow) {
		t.Errorf("last history %+v, want rejected by Ravi (%d) at %s with the reason", last, e.Ravi.ID, fixedNow)
	}
	r, ok := e.Latest(t, d)
	if !ok || r.Outcome != approval.OutcomeRejected || r.Reason != reason || r.DecidedBy != e.Ravi.ID {
		t.Errorf("request %v %+v, want outcome rejected by Ravi with the reason", ok, r)
	}
}

func TestApprovalReject_SameEligibility(t *testing.T) { // APR-061
	e, d := pending42(t)
	// Priya reads it but holds no approve right: APR's text (CD-52).
	at.WantErr(t, e.Reject(e.Priya, d, "No"), approval.ErrNotEligible, "Only a user with the Purchase Manager or Director role can approve Draft 42 (₹60,000.00).")
	e.Auth.AddRole(e.Ravi.ID, "Purchase User")
	d43 := e.NewDoc(t, at.Order, 43, at.Rs(60000), e.Ravi)
	at.MustOK(t, e.Submit(e.Ravi, d43))
	at.WantErr(t, e.Reject(e.Ravi, d43, "No"), approval.ErrSelfApproval, ownPO)
}

func TestApprovalConcurrency_FirstActionWins(t *testing.T) { // APR-071
	e, d := pending42(t)
	at.MustOK(t, e.Approve(e.Ravi, d))
	at.WantErr(t, e.Reject(e.Meena, d, "Too costly"), approval.ErrNotPending, "PO/26-27/00001 is no longer waiting for approval. It was approved by Ravi Kulkarni.")
	if n := len(e.History(t, d)); n != 2 {
		t.Errorf("history rows %d, want 2 (submitted, approved)", n)
	}
	if r, ok := e.Latest(t, d); !ok || r.Outcome != approval.OutcomeApproved || r.DecidedBy != e.Ravi.ID {
		t.Errorf("request %v %+v, want still approved by Ravi (%d)", ok, r, e.Ravi.ID)
	}
	if e.State(t, d) != doctype.StateApproved || e.Number(t, d) != "PO/26-27/00001" {
		t.Errorf("state %s number %q, want approved PO/26-27/00001 unchanged", e.State(t, d), e.Number(t, d))
	}
}

func TestApprovalReject_ReasonCountsCharactersNotBytes(t *testing.T) { // Review Focus 1
	e, d := pending42(t)
	for _, r := range []string{" \n\t ", "  　"} {
		at.WantErr(t, e.Reject(e.Ravi, d, r), approval.ErrReasonRequired, "Enter a reason for rejecting.")
	}
	body := strings.Repeat("दर", 500) // 1,000 characters, 3,000 bytes
	at.MustOK(t, e.Reject(e.Ravi, d, "\n"+body+"\t"))
	if last := lastHist(t, e, d); last.Action != approval.HistRejected || last.Reason != body {
		t.Errorf("last history %s with reason of %d bytes, want rejected with the trimmed 3,000-byte reason", last.Action, len(last.Reason))
	}
}

func TestRecord_WithdrawAndRevise(t *testing.T) {
	e, d := pending42(t)
	at.MustOK(t, record(t, e, d, doctype.ActionWithdraw, e.Priya, ""))
	r, ok := e.Latest(t, d)
	if !ok || r.Outcome != approval.OutcomeWithdrawn || r.DecidedBy != e.Priya.ID {
		t.Errorf("request %v %+v, want outcome withdrawn decided by Priya (%d)", ok, r, e.Priya.ID)
	}
	if last := lastHist(t, e, d); last.Action != approval.HistWithdrawn || last.By != e.Priya.ID || last.Amount != at.Rs(60000) || last.RuleID != e.R1 {
		t.Errorf("last history %+v, want withdrawn by Priya with amount 6000000 under R1 (%d)", last, e.R1)
	}
	if err := record(t, e, d, doctype.ActionWithdraw, e.Priya, ""); !errors.Is(err, approval.ErrNotPending) {
		t.Errorf("second withdraw: error %v, want ErrNotPending", err)
	}

	d43 := e.NewDoc(t, at.Order, 43, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d43))
	at.MustOK(t, e.Reject(e.Ravi, d43, "Wrong vendor"))
	at.MustOK(t, record(t, e, d43, doctype.ActionRevise, e.Priya, ""))
	if last := lastHist(t, e, d43); last.Action != approval.HistRevised || last.Amount != at.Rs(60000) || last.RuleID != e.R1 {
		t.Errorf("last history %+v, want revised with amount 6000000 under R1 (%d)", last, e.R1)
	}

	if err := record(t, e, d43, doctype.ActionClose, e.Priya, ""); !errors.Is(err, approval.ErrUnknownAction) {
		t.Errorf("Record(close): error %v, want ErrUnknownAction", err)
	}
}

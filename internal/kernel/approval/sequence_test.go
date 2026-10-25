package approval_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	at "github.com/vardhan/internaltools/internal/kernel/approval/approvaltest"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// actions are the history rows' actions, oldest first.
func actions(h []approval.HistoryRow) []string {
	var acts []string
	for _, r := range h {
		acts = append(acts, r.Action)
	}
	return acts
}

// rejectedAndRevised is Priya's Draft 42 (₹60,000.00) submitted, rejected by Ravi and revised.
func rejectedAndRevised(t *testing.T) (*at.Env, at.Doc) {
	t.Helper()
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	at.MustOK(t, e.Reject(e.Ravi, d, "Too high"))
	at.MustOK(t, e.Revise(e.Priya, d))
	return e, d
}

// grnPending is Suresh's GRN Draft 7 (₹1,20,000.00) pending under a GRN rule
// from ₹0.00 → Purchase Manager that Ravi may read and approve (APR-052/053).
func grnPending(t *testing.T) (*at.Env, at.Doc) {
	t.Helper()
	e := at.New(t)
	e.Auth.Grant("Purchase Manager", at.Receipt, "read", "all")
	e.Auth.Grant("Purchase Manager", at.Receipt, "approve", "all")
	e.AddRule(t, approval.RuleInput{DocType: at.Receipt, FromAmount: 0, ApproverType: approval.ApproverRole, Role: "Purchase Manager"})
	d := e.NewDoc(t, at.Receipt, 7, at.Rs(120000), e.Suresh)
	at.MustOK(t, e.Submit(e.Suresh, d))
	if e.State(t, d) != doctype.StatePending {
		t.Fatalf("GRN Draft 7 is %s, want pending_approval", e.State(t, d))
	}
	return e, d
}

func TestApprovalSubmit_NoRuleApprovesDirectly(t *testing.T) { // APR-015
	e := at.New(t)
	d := e.NewDoc(t, at.Receipt, 43, at.Rs(120000), e.Suresh)
	at.MustOK(t, e.Submit(e.Suresh, d))
	if e.State(t, d) != doctype.StateApproved || e.Number(t, d) != "GR/26-27/00001" || e.Effects(t, d) != 1 {
		t.Errorf("state %s number %q effects %d, want approved GR/26-27/00001 1", e.State(t, d), e.Number(t, d), e.Effects(t, d))
	}
	if r, ok := e.Latest(t, d); ok {
		t.Errorf("no request expected, got %+v", r)
	}
	h := e.History(t, d)
	if len(h) != 2 || h[0].Action != approval.HistSubmitted || h[0].By != e.Suresh.ID ||
		h[1].Action != approval.HistAutoApproved || h[1].By != e.Suresh.ID {
		t.Errorf("history %+v, want submitted and approved_no_rule by Suresh", h)
	}
}

func TestApprovalApprove_AppliesEffectsAtomically(t *testing.T) { // APR-052 (APR half: Record and effects share the tx)
	e, d := grnPending(t)
	at.MustOK(t, e.Approve(e.Ravi, d))
	last := e.History(t, d)
	if len(last) == 0 {
		t.Fatal("no history")
	}
	l := last[len(last)-1]
	if e.State(t, d) != doctype.StateApproved || e.Number(t, d) != "GR/26-27/00001" || e.Effects(t, d) != 1 {
		t.Errorf("state %s number %q effects %d", e.State(t, d), e.Number(t, d), e.Effects(t, d))
	}
	if l.Action != approval.HistApproved || l.By != e.Ravi.ID || !l.At.Equal(e.Clock.Now()) || l.At.Location().String() != "UTC" || l.Amount != 12000000 {
		t.Errorf("last history %+v, want approved by Ravi at %v for 12000000 paise", l, e.Clock.Now())
	}
}

func TestApprovalApprove_RevalidatesAndRollsBack(t *testing.T) { // APR-053
	e, d := grnPending(t)
	overReceipt := errors.New("Line 1: received quantity is more than the quantity still pending on the order.")
	if err := e.Approve(e.Ravi, d, at.FailAfterRecord(overReceipt)); !errors.Is(err, overReceipt) {
		t.Fatalf("error = %v, want DOC's over-receipt error", err)
	}
	r, ok := e.Latest(t, d)
	h := e.History(t, d)
	if e.State(t, d) != doctype.StatePending || !ok || r.Outcome != "" || r.DecidedBy != 0 {
		t.Errorf("state %s request %+v, want pending with an open request", e.State(t, d), r)
	}
	if e.Number(t, d) != "" || e.Counter(t, "GR/26-27/") != 0 || e.Effects(t, d) != 0 {
		t.Errorf("number %q counter %d effects %d, want nothing used", e.Number(t, d), e.Counter(t, "GR/26-27/"), e.Effects(t, d))
	}
	if !slices.Equal(actions(h), []string{approval.HistSubmitted}) {
		t.Errorf("history %v, want only submitted", actions(h))
	}
	// The request is still open: Ravi can approve it now.
	at.MustOK(t, e.Approve(e.Ravi, d))
	if e.Number(t, d) != "GR/26-27/00001" {
		t.Errorf("number %q after the retry, want GR/26-27/00001", e.Number(t, d))
	}
}

func TestApprovalResubmit_LowerAmountSkipsApproval(t *testing.T) { // APR-065
	e, d := rejectedAndRevised(t)
	e.SetAmount(t, d, at.Rs(45000))
	at.MustOK(t, e.Submit(e.Priya, d))
	want := []string{"submitted", "rejected", "revised", "submitted", "approved_no_rule"}
	if got := actions(e.History(t, d)); !slices.Equal(got, want) {
		t.Errorf("history %v, want %v", got, want)
	}
	if e.State(t, d) != doctype.StateApproved || e.Number(t, d) != "PO/26-27/00001" {
		t.Errorf("state %s number %q, want approved PO/26-27/00001", e.State(t, d), e.Number(t, d))
	}
}

func TestApprovalResubmit_HigherAmountHigherRule(t *testing.T) { // APR-066
	e, d := rejectedAndRevised(t)
	first := e.History(t, d)[0]
	e.SetAmount(t, d, at.Rs(550000))
	at.MustOK(t, e.Submit(e.Priya, d))
	r, ok := e.Latest(t, d)
	if !ok || r.RuleID != e.R2 || r.Outcome != "" || first.RuleID != e.R1 || e.State(t, d) != doctype.StatePending {
		t.Errorf("request %+v, want a new open request under R2 (%d)", r, e.R2)
	}
	at.WantErr(t, e.Approve(e.Ravi, d), approval.ErrNotEligible, "Only a user with the Director role can approve Draft 42 (₹5,50,000.00).")
}

func TestApprovalResubmit_FreshRequest(t *testing.T) { // APR-067
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	first, ok := e.Latest(t, d)
	if !ok {
		t.Fatal("no request after the first submit")
	}
	at.MustOK(t, e.Reject(e.Ravi, d, "Too high"))
	at.MustOK(t, e.Revise(e.Priya, d))
	at.MustOK(t, e.Submit(e.Priya, d))
	now, _ := e.Latest(t, d)
	old := e.RequestByID(t, first.ID)
	if now.ID == first.ID || now.Outcome != "" || old.Outcome != approval.OutcomeRejected || old.DecidedBy != e.Ravi.ID {
		t.Errorf("new %+v old %+v, want a fresh open request and the old one rejected by Ravi", now, old)
	}
	if e.State(t, d) != doctype.StatePending || e.Title(t, d) != "Draft 42" || e.Number(t, d) != "" {
		t.Errorf("state %s title %q, want pending Draft 42", e.State(t, d), e.Title(t, d))
	}
}

func TestApprovalConcurrency_ParallelApproveOnce(t *testing.T) { // APR-072
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
	ok := 0
	for _, err := range errs {
		if err == nil {
			ok++
			continue
		}
		if !errors.Is(err, approval.ErrNotPending) || !strings.HasPrefix(err.Error(), "PO/26-27/00001 is no longer waiting for approval. It was approved by ") {
			t.Error(err)
		}
	}
	approved := 0
	for _, r := range e.History(t, d) {
		if r.Action == approval.HistApproved {
			approved++
		}
	}
	if ok != 1 || approved != 1 || e.Effects(t, d) != 1 || e.Number(t, d) != "PO/26-27/00001" || e.Counter(t, "PO/26-27/") != 1 {
		t.Errorf("ok=%d approved=%d effects=%d number=%q counter=%d, want 1 1 1 PO/26-27/00001 1",
			ok, approved, e.Effects(t, d), e.Number(t, d), e.Counter(t, "PO/26-27/"))
	}
}

func TestApprovalHistory_AppendOnly(t *testing.T) { // APR-083
	e, d := rejectedAndRevised(t)
	at.MustOK(t, e.Submit(e.Priya, d))
	at.MustOK(t, e.Approve(e.Ravi, d))
	h := e.History(t, d)
	want := []string{"submitted", "rejected", "revised", "submitted", "approved"}
	wantBy := []types.UserID{e.Priya.ID, e.Ravi.ID, e.Priya.ID, e.Priya.ID, e.Ravi.ID}
	if len(h) != len(want) {
		t.Fatalf("history %v, want %v", actions(h), want)
	}
	for i, r := range h {
		if r.Action != want[i] || r.By != wantBy[i] || r.Amount != 6000000 || r.RuleID != e.R1 || !r.At.Equal(e.Clock.Now()) {
			t.Errorf("row %d %+v, want %s by %v, 6000000 paise, rule %d", i, r, want[i], wantBy[i], e.R1)
		}
	}
	for _, q := range []string{`UPDATE approval_history SET reason = 'changed'`, `DELETE FROM approval_history`} {
		err := e.DB.Write(t.Context(), func(tx *db.Tx) error {
			_, err := tx.SQL().ExecContext(context.Background(), q)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Errorf("%s allowed: %v", q, err)
		}
	}
	if got := actions(e.History(t, d)); !slices.Equal(got, want) {
		t.Errorf("history after the refused changes %v, want %v", got, want)
	}
}

func TestApprovalAmend_NeedsFreshApproval(t *testing.T) { // APR-087 (APR half: a new id has no history and needs fresh approval)
	e := at.New(t)
	e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya, at.InState(doctype.StateCancelled, "PO/26-27/00042"))
	amended := e.NewDoc(t, at.Order, 61, at.Rs(60000), e.Priya)
	if len(e.History(t, amended)) != 0 {
		t.Error("amended copy starts with empty APR history")
	}
	at.MustOK(t, e.Submit(e.Priya, amended))
	r, ok := e.Latest(t, amended)
	if e.State(t, amended) != doctype.StatePending || !ok || r.RuleID != e.R1 || r.Outcome != "" || e.Title(t, amended) != "Draft 61" {
		t.Errorf("state %s request %+v title %q, want pending under R1 as Draft 61", e.State(t, amended), r, e.Title(t, amended))
	}
}

func TestApprovalCopy_ClearsApprovalFields(t *testing.T) { // APR-088 (APR state is keyed by document id, so a copy has none)
	e := at.New(t)
	orig := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, orig))
	at.MustOK(t, e.Approve(e.Ravi, orig))
	if e.Number(t, orig) != "PO/26-27/00001" {
		t.Fatalf("original number %q, want PO/26-27/00001", e.Number(t, orig))
	}
	cp := e.NewDoc(t, at.Order, 70, at.Rs(60000), e.Priya)
	var s approval.Status
	at.MustOK(t, e.DB.Read(t.Context(), func(tx *db.Tx) (err error) { s, err = e.A.Status(t.Context(), tx, at.Order, 70); return }))
	if r, ok := e.Latest(t, cp); ok || s.Line != "" || s.Pending || len(e.History(t, cp)) != 0 || e.Title(t, cp) != "Draft 70" {
		t.Errorf("copy carries approval state: request %+v status %+v title %q", r, s, e.Title(t, cp))
	}
}

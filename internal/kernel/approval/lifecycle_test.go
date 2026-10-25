package approval_test

// DOC-coupled P1 cases (Task 12a). They need the real doc.Engine and NAM, so
// they are WP3 entry and exit criteria (CD-40) and skip under the reference
// driver via RequireEngine. DOC owns every refusal message asserted here.

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	at "github.com/vardhan/internaltools/internal/kernel/approval/approvaltest"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

const (
	pendingEdit   = "Draft 42 is waiting for approval and can't be changed. Withdraw it to make changes."
	approvedNoWdr = "PO/26-27/00001 has already been approved and can't be withdrawn. Cancel and amend it to make changes."
)

// docAudit is the document's audit entries, oldest first. Under the engine,
// 12b must route the engine's audit writes into e.Audit (or docAudit must read
// audit.ForDoc instead); otherwise every audit check here is vacuous.
func docAudit(e *at.Env, d at.Doc) []audit.Entry {
	var out []audit.Entry
	for _, en := range e.Audit.Entries() {
		if en.DocType == d.Type && en.DocID == d.ID {
			out = append(out, en)
		}
	}
	return out
}

// snapshot is what "nothing changes" compares: state, number, version,
// history and audit counts, and the latest request's outcome.
type snapshot struct {
	State          doctype.State
	Number         string
	Version        int64
	History, Audit int
	Outcome        string
}

func snap(t *testing.T, e *at.Env, d at.Doc) snapshot {
	t.Helper()
	r, _ := e.Latest(t, d)
	return snapshot{e.State(t, d), e.Number(t, d), e.Version(t, d), len(e.History(t, d)), len(docAudit(e, d)), r.Outcome}
}

func wantUnchanged(t *testing.T, e *at.Env, d at.Doc, before snapshot) {
	t.Helper()
	if got := snap(t, e, d); got != before {
		t.Errorf("document changed: %+v, want %+v", got, before)
	}
}

// wantMsg checks the refusal's exact text and HTTP status (CD-44).
func wantMsg(t *testing.T, err error, msg string, status int) {
	t.Helper()
	if err == nil || err.Error() != msg {
		t.Errorf("error = %v\nwant %q", err, msg)
	}
	at.WantStatus(t, err, status)
}

func hasWithdrawn(h []approval.HistoryRow) bool {
	return slices.ContainsFunc(h, func(r approval.HistoryRow) bool { return r.Action == approval.HistWithdrawn })
}

func TestApprovalMatch_OnlyAtSubmit(t *testing.T) { // APR-024
	e := at.New(t)
	e.RequireEngine(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(80000), e.Priya)
	for _, remarks := range []string{"First save", "Second save", "Third save"} {
		at.MustOK(t, e.Save(e.Priya, d, map[string]string{"remarks": remarks}))
	}
	if _, ok := e.Latest(t, d); ok {
		t.Error("a save opened an approval request")
	}
	if h := e.History(t, d); len(h) != 0 {
		t.Errorf("history %+v, want none before submit", h)
	}
	if e.State(t, d) != doctype.StateDraft || e.Number(t, d) != "" || e.Title(t, d) != "Draft 42" {
		t.Errorf("state %s number %q title %q, want draft, no number, Draft 42", e.State(t, d), e.Number(t, d), e.Title(t, d))
	}
}

func TestApprovalSubmit_LocksWithoutNumber(t *testing.T) { // APR-027
	e := at.New(t)
	e.RequireEngine(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	if e.State(t, d) != doctype.StatePending || e.Number(t, d) != "" || e.Title(t, d) != "Draft 42" {
		t.Errorf("state %s number %q title %q, want pending_approval, no number, Draft 42", e.State(t, d), e.Number(t, d), e.Title(t, d))
	}
	before := snap(t, e, d)
	wantMsg(t, e.Save(e.Priya, d, map[string]string{"remarks": "Changed after submit"}), pendingEdit, 409)
	wantUnchanged(t, e, d, before)
}

func TestApprovalSubmit_ValidationBeforeRules(t *testing.T) { // APR-028
	e := at.New(t)
	e.RequireEngine(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	// A save refuses quantity 0, so the bad line is written directly, as
	// DOC's own tests do with Exec (fixture.Order's lines table, DOC Task 2).
	at.MustOK(t, e.DB.Write(t.Context(), func(tx *db.Tx) error {
		res, err := tx.SQL().ExecContext(t.Context(), `UPDATE fixture_order_line SET qty = 0 WHERE parent_id = ?`, d.ID)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil || n != 1 {
			return fmt.Errorf("qty=0 update: %d rows, err %v; want 1 row", n, err)
		}
		return nil
	}))
	before := snap(t, e, d)
	err := e.Submit(e.Priya, d)
	// DOC prefixes the line ("Line 1: …") and owns the full text.
	if err == nil || !strings.Contains(err.Error(), "Quantity must be more than 0.") {
		t.Errorf("error = %v, want DOC's \"Quantity must be more than 0.\"", err)
	}
	if _, ok := e.Latest(t, d); ok {
		t.Error("an approval request was opened for an invalid document")
	}
	if h := e.History(t, d); len(h) != 0 {
		t.Errorf("history %+v, want none", h)
	}
	if e.State(t, d) != doctype.StateDraft {
		t.Errorf("state %s, want draft", e.State(t, d))
	}
	wantUnchanged(t, e, d, before)
}

func TestApprovalRevise_RejectedIsLocked(t *testing.T) { // APR-062
	e := at.New(t)
	e.RequireEngine(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	at.MustOK(t, e.Reject(e.Ravi, d, "Rate is too high."))
	before := snap(t, e, d)
	wantMsg(t, e.Save(e.Priya, d, map[string]string{"lines.1.qty": "10.000"}), "Draft 42 was rejected. Click Revise to edit it.", 409)
	wantUnchanged(t, e, d, before)
}

func TestApprovalPending_EditRefused(t *testing.T) { // APR-073
	e := at.New(t)
	e.RequireEngine(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	before := snap(t, e, d)
	wantMsg(t, e.Save(e.Priya, d, map[string]string{"amount": "40000.00"}), pendingEdit, 409)
	wantUnchanged(t, e, d, before)
	if r, _ := e.Latest(t, d); r.Amount != 6000000 {
		t.Errorf("request amount %d, want 6000000", r.Amount)
	}
}

func TestApprovalPending_ApproverCannotEdit(t *testing.T) { // APR-074
	e := at.New(t)
	e.RequireEngine(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	before := snap(t, e, d)
	if got, want := e.Offered(e.Ravi, d), []doctype.Action{doctype.ActionApprove, doctype.ActionReject}; !slices.Equal(got, want) {
		t.Errorf("offered to Ravi %v, want %v", got, want)
	}
	// DOC checks rights before the state guard, so seeded Ravi (Purchase
	// Manager, no write right on Purchase Order) gets AUTH's 403. Write is
	// granted next to check the pending message.
	err := e.Save(e.Ravi, d, map[string]string{"lines.1.rate": "70.0000"})
	wantMsg(t, err, "You don't have permission to write Purchase Order. Ask your administrator for a role that allows it.", 403)
	if !errors.Is(err, auth.ErrForbidden) {
		t.Errorf("error = %v, want auth.ErrForbidden", err)
	}
	wantUnchanged(t, e, d, before)
	// With write granted, the pending guard refuses with the same message as the requester gets.
	e.Auth.Grant("Purchase Manager", at.Order, "write", "all")
	wantMsg(t, e.Save(e.Ravi, d, map[string]string{"lines.1.rate": "70.0000"}), pendingEdit, 409)
	wantUnchanged(t, e, d, before)
}

func TestApprovalPending_NoDeleteNoCancel(t *testing.T) { // APR-075
	e := at.New(t)
	e.RequireEngine(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	before := snap(t, e, d)
	wantMsg(t, e.Delete(e.Priya, d), "Draft 42 is waiting for approval and can't be deleted. Withdraw it first, then delete it.", 409)
	wantMsg(t, e.Cancel(e.Ravi, d, "Supplier can't deliver."), "Draft 42 is waiting for approval. Withdraw it first if it needs changes.", 409)
	wantUnchanged(t, e, d, before)
}

func TestApprovalTransition_NoDirectStateWrite(t *testing.T) { // APR-076
	e := at.New(t)
	e.RequireEngine(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	before := snap(t, e, d)
	err := e.SetState(e.Priya, d, doctype.StateApproved)
	if err == nil || err.Error() != "State can only change through Submit, Withdraw, Approve, Reject or Revise." {
		t.Errorf("error = %v, want the state-field refusal", err)
	}
	wantUnchanged(t, e, d, before)
	if _, ok := e.Latest(t, d); ok {
		t.Error("an approval request was opened")
	}
}

func TestApproval_Withdraw_SubmitterBackToDraft(t *testing.T) { // APR-091
	e := at.New(t)
	e.RequireEngine(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Save(e.Priya, d, map[string]string{"remarks": "Deliver to Pune site"}))
	at.MustOK(t, e.Submit(e.Priya, d))
	if v := e.Version(t, d); v != 3 {
		t.Fatalf("version %d before withdraw, want 3", v)
	}
	at.MustOK(t, e.Withdraw(e.Priya, d))
	if e.State(t, d) != doctype.StateDraft || e.Version(t, d) != 4 {
		t.Errorf("state %s version %d, want draft 4", e.State(t, d), e.Version(t, d))
	}
	if e.Number(t, d) != "" || e.Title(t, d) != "Draft 42" || e.Effects(t, d) != 0 {
		t.Errorf("number %q title %q effects %d, want no number, Draft 42, no effects", e.Number(t, d), e.Title(t, d), e.Effects(t, d))
	}
	if r, ok := e.Latest(t, d); !ok || r.Outcome != approval.OutcomeWithdrawn || r.DecidedBy != e.Priya.ID {
		t.Errorf("latest request %+v, want withdrawn by Priya", r)
	}
	if last := lastHist(t, e, d); last.Action != approval.HistWithdrawn || last.By != e.Priya.ID || !last.At.Equal(fixedNow) || last.At.Location() != fixedNow.Location() {
		t.Errorf("last history %+v, want withdrawn by Priya at %s UTC", last, fixedNow)
	}
	var withdraws []audit.Entry
	for _, en := range docAudit(e, d) {
		if en.Action == string(doctype.ActionWithdraw) {
			withdraws = append(withdraws, en)
		}
	}
	if len(withdraws) != 1 || withdraws[0].From != string(doctype.StatePending) || withdraws[0].To != string(doctype.StateDraft) || withdraws[0].By != e.Priya.ID {
		t.Errorf("withdraw audit %+v, want one pending_approval→draft by Priya", withdraws)
	}
	// Editable again.
	at.MustOK(t, e.Save(e.Priya, d, map[string]string{"remarks": "Changed after withdraw"}))
}

func TestApproval_Withdraw_NeedsSubmitRight(t *testing.T) { // APR-092
	e := at.New(t)
	e.RequireEngine(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	at.MustOK(t, e.Withdraw(e.Anil, d))
	if last := lastHist(t, e, d); last.Action != approval.HistWithdrawn || last.By != e.Anil.ID || last.ByName != "Anil Kapoor" {
		t.Errorf("last history %+v, want withdrawn by Anil Kapoor", last)
	}

	d2 := e.NewDoc(t, at.Order, 43, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d2))
	before := snap(t, e, d2)
	e.Auth.AddRole(e.Deepak.ID, "Order Reader")
	e.Auth.Grant("Order Reader", at.Order, "read", "all")
	wantMsg(t, e.Withdraw(e.Deepak, d2), "You don't have permission to submit Purchase Order. Ask your administrator for a role that allows it.", 403)
	wantUnchanged(t, e, d2, before)

	e.Auth.AddRole(e.Kavya.ID, "Purchase User")
	e.Auth.Restrict(e.Kavya.ID, []int64{at.ProjTechPark}, nil)
	at.WantNotFound(t, e.Withdraw(e.Kavya, d2))
	wantUnchanged(t, e, d2, before)
}

func TestApproval_Withdraw_OnlyFromPending(t *testing.T) { // APR-094
	at.New(t).RequireEngine(t)
	cases := []struct {
		state  doctype.State
		number string
		msg    string
	}{
		{doctype.StateDraft, "", "Draft 42 hasn't been submitted, so there is nothing to withdraw."},
		{doctype.StateRejected, "", "Draft 42 was rejected. Revise it to move it back to draft."},
		{doctype.StateApproved, "PO/26-27/00001", approvedNoWdr},
		{doctype.StateClosed, "PO/26-27/00001", "PO/26-27/00001 is closed and can't be changed."},
		{doctype.StateCancelled, "PO/26-27/00001", "PO/26-27/00001 is cancelled and can't be changed. Amend it to make a corrected copy."},
	}
	for _, c := range cases {
		t.Run(string(c.state), func(t *testing.T) {
			e := at.New(t)
			d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya, at.InState(c.state, c.number))
			before := snap(t, e, d)
			wantMsg(t, e.Withdraw(e.Priya, d), c.msg, 409)
			wantUnchanged(t, e, d, before)
		})
	}
}

func TestApproval_Withdraw_StaleAfterApprove(t *testing.T) { // APR-095
	at.New(t).RequireEngine(t)
	t.Run("sequential", func(t *testing.T) {
		e := at.New(t)
		d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
		at.MustOK(t, e.Submit(e.Priya, d))
		at.MustOK(t, e.Approve(e.Ravi, d))
		before := snap(t, e, d)
		wantMsg(t, e.Withdraw(e.Priya, d), approvedNoWdr, 409)
		wantUnchanged(t, e, d, before)
		if e.State(t, d) != doctype.StateApproved || e.Number(t, d) != "PO/26-27/00001" || e.Effects(t, d) != 1 {
			t.Errorf("state %s number %q effects %d, want approved PO/26-27/00001 1", e.State(t, d), e.Number(t, d), e.Effects(t, d))
		}
		if hasWithdrawn(e.History(t, d)) {
			t.Error("a withdrawn history row was written")
		}
	})
	t.Run("race", func(t *testing.T) {
		for range 10 {
			e := at.New(t)
			d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
			at.MustOK(t, e.Submit(e.Priya, d))
			var approveErr, withdrawErr error
			var wg sync.WaitGroup
			wg.Go(func() { approveErr = e.Approve(e.Ravi, d) })
			wg.Go(func() { withdrawErr = e.Withdraw(e.Priya, d) })
			wg.Wait()
			if (approveErr == nil) == (withdrawErr == nil) {
				t.Fatalf("approve %v, withdraw %v: want exactly one to succeed", approveErr, withdrawErr)
			}
			h := e.History(t, d)
			if approveErr == nil {
				wantMsg(t, withdrawErr, approvedNoWdr, 409)
				if e.State(t, d) != doctype.StateApproved || e.Number(t, d) != "PO/26-27/00001" || e.Effects(t, d) != 1 || hasWithdrawn(h) {
					t.Errorf("approve won: state %s number %q effects %d history %+v", e.State(t, d), e.Number(t, d), e.Effects(t, d), h)
				}
			} else {
				wantMsg(t, approveErr, "Draft 42 is no longer waiting for approval. It was withdrawn by Priya Shah.", 409)
				if e.State(t, d) != doctype.StateDraft || e.Number(t, d) != "" || e.Effects(t, d) != 0 || !hasWithdrawn(h) {
					t.Errorf("withdraw won: state %s number %q effects %d history %+v", e.State(t, d), e.Number(t, d), e.Effects(t, d), h)
				}
			}
		}
	})
}

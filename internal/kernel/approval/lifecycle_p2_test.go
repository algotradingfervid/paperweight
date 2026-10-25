package approval_test

// DOC-coupled P2 cases (Task 18). Like Task 12a they need the real doc.Engine,
// so each starts with RequireEngine and skips under the reference driver.

import (
	"errors"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	at "github.com/vardhan/internaltools/internal/kernel/approval/approvaltest"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// trail is DOC's audit trail of the document read through audit's public
// read path (audit.ForDoc), oldest first.
func trail(t *testing.T, e *at.Env, d at.Doc) []audit.Entry {
	t.Helper()
	var out []audit.Entry
	at.MustOK(t, e.DB.Read(t.Context(), func(tx *db.Tx) (err error) {
		out, err = audit.ForDoc(t.Context(), tx, d.Type, d.ID)
		return err
	}))
	return out
}

// rejected42 is Priya's Draft 42 (₹60,000.00, R1) rejected by Ravi.
func rejected42(t *testing.T, e *at.Env) at.Doc {
	t.Helper()
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	at.MustOK(t, e.Reject(e.Ravi, d, "Rate is too high."))
	return d
}

func TestApprovalRevise_NeedsWriteRight(t *testing.T) { // APR-064
	e := at.New(t)
	e.RequireEngine(t)
	d := rejected42(t, e)
	// Suresh (Stores) reads Purchase Order through the seed; the grant makes
	// the read right explicit. He has no write right on it.
	e.Auth.Grant("Stores", at.Order, "read", "all")
	before := snap(t, e, d)
	// AUTH's verb for the write right is "change" (auth.DeniedError), as in the case text.
	err := e.Revise(e.Suresh, d)
	wantMsg(t, err, "You don't have permission to change Purchase Order. Ask your administrator for a role that allows it.", 403)
	if !errors.Is(err, auth.ErrForbidden) {
		t.Errorf("error = %v, want auth.ErrForbidden", err)
	}
	wantUnchanged(t, e, d, before)
	if e.State(t, d) != doctype.StateRejected {
		t.Errorf("state %s, want rejected", e.State(t, d))
	}
	if slices.ContainsFunc(e.History(t, d), func(r approval.HistoryRow) bool { return r.Action == approval.HistRevised }) {
		t.Error("a revised history row was written")
	}
}

func TestApprovalRevise_DraftDeletable(t *testing.T) { // APR-069
	e := at.New(t)
	e.RequireEngine(t)
	d := rejected42(t, e)
	at.MustOK(t, e.Revise(e.Priya, d))
	if e.State(t, d) != doctype.StateDraft {
		t.Fatalf("state %s after revise, want draft", e.State(t, d))
	}
	at.MustOK(t, e.Delete(e.Priya, d))

	// Deleted per DOC draft rules: the record is gone.
	at.WantNotFound(t, e.DB.Read(t.Context(), func(tx *db.Tx) error {
		_, err := e.Docs.Info(t.Context(), tx, d.Type, d.ID)
		return err
	}))

	// The approval history is append-only and still readable.
	var actions []string
	for _, r := range e.History(t, d) {
		actions = append(actions, r.Action)
	}
	if want := []string{approval.HistSubmitted, approval.HistRejected, approval.HistRevised}; !slices.Equal(actions, want) {
		t.Errorf("history after delete %v, want %v", actions, want)
	}

	// No request is open: the last one stays closed as rejected, never withdrawn.
	r, ok := e.Latest(t, d)
	if !ok || r.Outcome != approval.OutcomeRejected || r.DecidedBy != e.Ravi.ID {
		t.Errorf("latest request %+v (found %v), want closed as rejected by Ravi", r, ok)
	}
	if hasWithdrawn(e.History(t, d)) {
		t.Error("a withdrawn history row was written")
	}

	// The audit trail keeps the transitions and records the delete.
	var got []string
	for _, en := range trail(t, e, d) {
		got = append(got, en.Action)
	}
	want := []string{string(doctype.ActionSubmit), string(doctype.ActionReject), string(doctype.ActionRevise), string(doctype.ActionDelete)}
	for _, a := range want {
		if !slices.Contains(got, a) {
			t.Errorf("audit actions %v: missing %q", got, a)
		}
	}
	if slices.Contains(got, string(doctype.ActionWithdraw)) {
		t.Errorf("audit actions %v: want no withdraw", got)
	}
}

func TestApprovalReject_NotExportable(t *testing.T) { // APR-070
	e := at.New(t)
	e.RequireEngine(t)
	// Draft 10 is rejected; Draft 11 is approved, so the list is shown to
	// return approved claims and Draft 10's absence means something.
	rej := e.NewDoc(t, at.Claim, 10, at.Rs(3450), e.Deepak)
	at.MustOK(t, e.Submit(e.Deepak, rej))
	at.MustOK(t, e.Reject(e.Priya, rej, "Bill is missing."))
	ok := e.NewDoc(t, at.Claim, 11, at.Rs(2000), e.Deepak)
	at.MustOK(t, e.Submit(e.Deepak, ok))
	at.MustOK(t, e.Approve(e.Priya, ok))
	if e.State(t, rej) != doctype.StateRejected || e.State(t, ok) != doctype.StateApproved {
		t.Fatalf("states %s / %s, want rejected / approved", e.State(t, rej), e.State(t, ok))
	}

	// The core stand-in for "approved, not yet exported": a list query on
	// fixture.Claim filtered state = approved, run as Meena (Director, read all).
	svc := &list.Service{DB: e.DB, Auth: e.Auth}
	res, err := svc.List(t.Context(), e.Meena, claimWithState(t), list.Query{
		Filters: []list.Filter{{Field: "state", Op: list.OpEq, Values: []string{string(doctype.StateApproved)}}},
	})
	at.MustOK(t, err)
	var ids []int64
	for _, r := range res.Rows {
		ids = append(ids, r.ID)
	}
	if !slices.Equal(ids, []int64{ok.ID}) {
		t.Errorf("approved claims %v, want only Draft 11 (id %d); Draft 10 (id %d) must be absent", ids, ok.ID, rej.ID)
	}
}

// claimWithState is the registered fixture.Claim descriptor with a filterable
// header field on its state convention column (CD-144): the registered
// descriptor declares no state field, so list refuses a state filter on it.
func claimWithState(t *testing.T) *doctype.DocType {
	t.Helper()
	reg, ok := doctype.Lookup(at.Claim)
	if !ok {
		t.Fatalf("%s is not registered: the engine driver must register internal/fixture", at.Claim)
	}
	dt := *reg
	dt.Fields = append(slices.Clone(reg.Fields), doctype.Field{Name: "state", Column: "state", Label: "State", Type: doctype.Select, Filterable: true, System: true})
	return &dt
}

func TestApprovalHistory_InDocAudit(t *testing.T) { // APR-085
	e := at.New(t)
	e.RequireEngine(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	at.MustOK(t, e.Approve(e.Ravi, d))

	var approves []audit.Entry
	for _, en := range trail(t, e, d) {
		if en.From == string(doctype.StatePending) && en.To == string(doctype.StateApproved) {
			approves = append(approves, en)
		}
	}
	if len(approves) != 1 {
		t.Fatalf("audit entries pending_approval → approved: %+v, want exactly one", approves)
	}
	en := approves[0]
	if en.Action != string(doctype.ActionApprove) || en.By != e.Ravi.ID {
		t.Errorf("entry action %q by %d, want approve by Ravi (%d)", en.Action, en.By, e.Ravi.ID)
	}
	if !en.At.Equal(fixedNow) || en.At.Location() != fixedNow.Location() {
		t.Errorf("entry at %v, want %s UTC", en.At, fixedNow)
	}
	if !slices.ContainsFunc(en.Changes, func(c audit.Change) bool {
		return c.Field == "number" && c.Before == "" && c.After == "PO/26-27/00001" && c.LineNo == 0
	}) {
		t.Errorf("entry changes %+v, want number \"\" → \"PO/26-27/00001\"", en.Changes)
	}
}

func TestApprovalCancel_NoApprovalNeeded(t *testing.T) { // APR-086
	e := at.New(t)
	e.RequireEngine(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	at.MustOK(t, e.Approve(e.Ravi, d))
	if e.State(t, d) != doctype.StateApproved || e.Number(t, d) != "PO/26-27/00001" {
		t.Fatalf("state %s number %q, want approved PO/26-27/00001", e.State(t, d), e.Number(t, d))
	}
	before, _ := e.Latest(t, d)
	hist := len(e.History(t, d))

	e.Auth.Grant("Director", at.Order, "cancel", "all") // not in the seeded matrix
	at.MustOK(t, e.Cancel(e.Meena, d, "Supplier can't deliver."))

	if e.State(t, d) != doctype.StateCancelled || e.Number(t, d) != "PO/26-27/00001" {
		t.Errorf("state %s number %q, want cancelled PO/26-27/00001", e.State(t, d), e.Number(t, d))
	}
	if r, _ := e.Latest(t, d); r.ID != before.ID || r.Outcome != approval.OutcomeApproved {
		t.Errorf("latest request %+v, want the approved request %d (no new request)", r, before.ID)
	}
	if n := len(e.History(t, d)); n != hist {
		t.Errorf("history has %d rows after cancel, want %d (no approval rows)", n, hist)
	}
	for _, p := range []auth.Principal{e.Ravi, e.Meena} {
		if titles := e.InboxTitles(t, p); len(titles) != 0 {
			t.Errorf("%s's inbox %v, want empty", p.Name, titles)
		}
	}
}

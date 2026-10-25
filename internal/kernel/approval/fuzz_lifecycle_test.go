package approval_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	at "github.com/vardhan/internaltools/internal/kernel/approval/approvaltest"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Fuzz step operations.
const (
	opSubmit = iota
	opWithdraw
	opApprove
	opReject
	opRevise
	opEdit
	opCancel
	numOps
)

var (
	fuzzAmounts       = []types.Money{at.Rs(1500), 4999999, at.Rs(60000), at.Rs(600000)}
	fuzzRejectReasons = []string{"", " \n\t ", "Price too high", "दाम ज़्यादा है 🙏"}
	fuzzCancelReasons = []string{"", "ab", "Wrong vendor", "Order placed twice"}
)

// step encodes one fuzz step as the two bytes FuzzApproval_StateInvariants decodes.
func step(op, doc, user, param int) []byte {
	return []byte{byte(op + numOps*(doc+2*user)), byte(param)}
}

func steps(s ...[]byte) []byte { return slices.Concat(s...) }

// Fuzz step users, as indexes into the decoder's user list.
const fzPriya, fzRavi, fzMeena, fzKavya = 0, 1, 2, 3

// fuzzSeeds are FuzzApproval_StateInvariants' seed corpus;
// TestApproval_FuzzSeedsReachStates checks where seeds 1–3 end.
var fuzzSeeds = [][]byte{
	{},
	steps(step(opSubmit, 0, fzPriya, 0), step(opCancel, 0, fzRavi, 2), step(opSubmit, 0, fzPriya, 0)),
	steps(step(opEdit, 0, fzPriya, 2), step(opSubmit, 0, fzPriya, 0), step(opApprove, 0, fzKavya, 0),
		step(opApprove, 0, fzPriya, 0), step(opApprove, 0, fzRavi, 0), step(opCancel, 0, fzRavi, 3)),
	steps(step(opSubmit, 1, fzRavi, 0), step(opApprove, 1, fzRavi, 0), step(opReject, 1, fzMeena, 1),
		step(opReject, 1, fzMeena, 3), step(opRevise, 1, fzRavi, 0), step(opEdit, 1, fzRavi, 3),
		step(opSubmit, 1, fzPriya, 0), step(opApprove, 1, fzRavi, 0), step(opApprove, 1, fzMeena, 0)),
	steps(step(opEdit, 0, fzPriya, 3), step(opSubmit, 0, fzPriya, 0), step(opWithdraw, 0, fzPriya, 0),
		step(opEdit, 0, fzPriya, 1), step(opSubmit, 0, fzPriya, 0), step(opWithdraw, 0, fzPriya, 0),
		step(opReject, 0, fzMeena, 2), step(opRevise, 0, fzPriya, 0)),
}

// FuzzApproval_StateInvariants (APR-089): random sequences of submit,
// withdraw, approve, reject, revise, edit amount and cancel by Priya, Ravi,
// Meena or Kavya over two Purchase Orders (one Priya's, one Ravi's) keep the
// state machine's invariants after every step. Actions may fail; they must
// not panic. Rules are the seed rules R1 (₹50,000.00, Purchase Manager) and
// R2 (₹5,00,000.00, Director).
func FuzzApproval_StateInvariants(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data []byte) { runSteps(t, data) })
}

// runSteps decodes data into steps on a fresh Env with Priya's PO (docs[0],
// ₹1,500.00) and Ravi's PO (docs[1], ₹60,000.00), checking the invariants
// after every step.
func runSteps(t *testing.T, data []byte) (*at.Env, []at.Doc) {
	t.Helper()
	e := at.New(t)
	e.Auth.AddRole(e.Ravi.ID, "Purchase User") // so Ravi can create and submit his own PO
	users := []auth.Principal{e.Priya, e.Ravi, e.Meena, e.Kavya}
	roles := map[types.UserID][]string{e.Ravi.ID: {"Purchase Manager"}, e.Meena.ID: {"Director"}}
	docs := []at.Doc{e.NewDoc(t, at.Order, 1, at.Rs(1500), e.Priya), e.NewDoc(t, at.Order, 2, at.Rs(60000), e.Ravi)}
	creator := []types.UserID{e.Priya.ID, e.Ravi.ID}
	amount := []types.Money{at.Rs(1500), at.Rs(60000)}
	var submitter [2]types.UserID
	var submitted [2]types.Money
	for i := 0; i+1 < len(data) && i < 60; i += 2 {
		op, doc, u, param := int(data[i])%numOps, int(data[i])/numOps%2, users[int(data[i])/(2*numOps)%4], int(data[i+1])%4
		d := docs[doc]
		var err error
		switch op {
		case opSubmit:
			if err = e.Submit(u, d); err == nil {
				submitter[doc], submitted[doc] = u.ID, amount[doc]
			}
		case opWithdraw:
			err = e.Withdraw(u, d)
		case opApprove:
			err = e.Approve(u, d)
		case opReject:
			err = e.Reject(u, d, fuzzRejectReasons[param])
		case opRevise:
			err = e.Revise(u, d)
		case opEdit: // DOC only saves a draft; the stand-in save has no rights check
			if e.State(t, d) == doctype.StateDraft {
				amount[doc] = fuzzAmounts[param]
				e.SetAmount(t, d, amount[doc])
			}
		case opCancel:
			err = e.Cancel(u, d, fuzzCancelReasons[param])
		}
		for k, d := range docs {
			own := []types.UserID{creator[k], submitter[k]}
			checkInvariants(t, e, d, submitted[k], own, roles, i/2, op, u.Name, err)
		}
	}
	return e, docs
}

// TestApproval_FuzzSeedsReachStates pins where fuzz seeds 1–3 end, so the
// seeds keep reaching the cancelled and approved branches the invariants
// cover.
func TestApproval_FuzzSeedsReachStates(t *testing.T) {
	lastApproved := func(t *testing.T, e *at.Env, d at.Doc) (approval.HistoryRow, bool) {
		t.Helper()
		var last approval.HistoryRow
		var ok bool
		for _, h := range e.History(t, d) {
			if h.Action == approval.HistApproved {
				last, ok = h, true
			}
		}
		return last, ok
	}
	t.Run("seed 1: Priya's PO auto-approved then cancelled", func(t *testing.T) {
		e, docs := runSteps(t, fuzzSeeds[1])
		d := docs[0]
		if got := e.State(t, d); got != doctype.StateCancelled {
			t.Errorf("state %s, want %s", got, doctype.StateCancelled)
		}
		if e.Number(t, d) == "" {
			t.Error("cancelled PO lost its number")
		}
		if n := e.Effects(t, d); n != 0 {
			t.Errorf("effects %d after cancel, want 0", n)
		}
		if !slices.ContainsFunc(e.History(t, d), func(h approval.HistoryRow) bool { return h.Action == approval.HistAutoApproved }) {
			t.Error("no auto-approved row in the history")
		}
	})
	t.Run("seed 2: Priya's PO approved by Ravi then cancelled", func(t *testing.T) {
		e, docs := runSteps(t, fuzzSeeds[2])
		d := docs[0]
		if got := e.State(t, d); got != doctype.StateCancelled {
			t.Errorf("state %s, want %s", got, doctype.StateCancelled)
		}
		if h, ok := lastApproved(t, e, d); !ok || h.By != e.Ravi.ID || h.RuleID != e.R1 {
			t.Errorf("last approval %+v (found %v), want Ravi's under R1", h, ok)
		}
	})
	t.Run("seed 3: Ravi's PO approved by Meena under R2", func(t *testing.T) {
		e, docs := runSteps(t, fuzzSeeds[3])
		d := docs[1]
		if got := e.State(t, d); got != doctype.StateApproved {
			t.Errorf("state %s, want %s", got, doctype.StateApproved)
		}
		if h, ok := lastApproved(t, e, d); !ok || h.By != e.Meena.ID || h.RuleID != e.R2 {
			t.Errorf("last approval %+v (found %v), want Meena's under R2 (Director)", h, ok)
		}
	})
}

// checkInvariants checks APR-089's invariants for d after one step. own are
// its creator and last submitter, submitted the amount at its last submit.
func checkInvariants(t *testing.T, e *at.Env, d at.Doc, submitted types.Money, own []types.UserID,
	roles map[types.UserID][]string, n, op int, by string, stepErr error) {
	t.Helper()
	state, number, effects, open := e.State(t, d), e.Number(t, d), e.Effects(t, d), openRequests(t, e, d)
	report := func(msg string, args ...any) {
		t.Helper()
		t.Fatalf("step %d (op %d by %s, err %v) on %s: "+msg, append([]any{n, op, by, stepErr, e.Title(t, d)}, args...)...)
	}
	if state == doctype.StateApproved {
		var last approval.HistoryRow
		for _, h := range e.History(t, d) {
			if h.Action == approval.HistApproved || h.Action == approval.HistAutoApproved {
				last = h
			}
		}
		switch last.Action {
		case approval.HistAutoApproved:
			if submitted >= at.Rs(50000) {
				report("approved with no rule but its last submit (%d) matched a rule", submitted)
			}
		case approval.HistApproved:
			need := []string{"Purchase Manager", "Director"}
			if submitted >= at.Rs(500000) {
				need = need[1:]
			}
			eligible := slices.ContainsFunc(roles[last.By], func(r string) bool { return slices.Contains(need, r) })
			if !eligible || slices.Contains(own, last.By) {
				report("approved by user %d (roles %q, own %v) for amount %d", last.By, roles[last.By], own, submitted)
			}
		default:
			report("approved with no approval in its history")
		}
	}
	if (state == doctype.StatePending) != (open == 1) {
		report("state %s with %d open requests", state, open)
	}
	if state == doctype.StateDraft && open != 0 {
		report("draft with %d open requests", open)
	}
	if state == doctype.StateRejected {
		if r, ok := e.Latest(t, d); !ok || strings.TrimSpace(r.Reason) == "" {
			report("rejected with no reason (request found %v)", ok)
		}
	}
	if number != "" && !slices.Contains([]doctype.State{doctype.StateApproved, doctype.StateClosed, doctype.StateCancelled}, state) {
		report("number %q in state %s", number, state)
	}
	if effects > 0 && state != doctype.StateApproved && state != doctype.StateClosed {
		report("effects %d in state %s", effects, state)
	}
}

// openRequests counts the document's open approval requests.
func openRequests(t *testing.T, e *at.Env, d at.Doc) int {
	t.Helper()
	var n int
	at.MustOK(t, e.DB.Read(context.Background(), func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM approval_requests WHERE doctype = ? AND doc_id = ? AND outcome IS NULL`, d.Type, d.ID).Scan(&n)
	}))
	return n
}

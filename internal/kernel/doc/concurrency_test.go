package doc_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/types"
)

// ccOverfillMsg is DOC-072's refusal for the second receipt of 6 on an order
// line of 10.
const ccOverfillMsg = "Line 1: receiving 6 would take PO/26-27/00001 line 1 to 12 of 10. Reduce the quantity."

// ccRuns is how many times DOC-073 repeats the cancel-versus-approve race.
const ccRuns = 50

// ccCall is one concurrent Engine.Do.
type ccCall struct {
	ctx context.Context
	p   auth.Principal
	dt  string
	id  int64
	a   doc.Action
	in  doc.ActionInput
}

// ccOut is one concurrent Engine.Do's result.
type ccOut struct {
	res doc.Result
	err error
}

// ccRace runs every call in its own goroutine, all released at once, and
// returns their results in call order.
func ccRace(h *fixturetest.H, calls []ccCall) []ccOut {
	out := make([]ccOut, len(calls))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, c := range calls {
		wg.Go(func() {
			<-start
			res, err := h.Engine.Do(c.ctx, c.p, c.dt, c.id, c.a, c.in)
			out[i] = ccOut{res: res, err: err}
		})
	}
	close(start)
	wg.Wait()
	return out
}

// DOC-071
func TestDoc_Concurrency_ParallelSaves(t *testing.T) {
	h := fixturetest.New(t)
	d := trRefPOAt(t, h, 5)
	before := len(h.Audit(t, fixture.Order, d.ID))
	count := h.AuditCount(t)

	const n = 20
	calls := make([]ccCall, n)
	for i := range calls {
		in := h.In(d)
		if in.Version != 5 {
			t.Fatalf("setup: input Version = %d, want 5", in.Version)
		}
		in.Values["remarks"] = fmt.Sprintf("parallel save %d", i)
		calls[i] = ccCall{ctx: t.Context(), p: h.Priya, dt: fixture.Order, id: d.ID, a: doc.ActionSave, in: in}
	}
	out := ccRace(h, calls)

	stale := fmt.Sprintf(msgStaleFmt, trDraft(d.ID))
	winner, wins := -1, 0
	for i, o := range out {
		if o.err == nil {
			winner = i
			wins++
			if o.res.Version != 6 {
				t.Errorf("save %d: Result.Version = %d, want 6", i, o.res.Version)
			}
			continue
		}
		fixturetest.ErrIs(t, o.err, doc.KindStale, stale)
	}
	if wins != 1 {
		t.Fatalf("successful saves = %d, want exactly 1 (the other %d stale)", wins, n-1)
	}

	after := h.Load(t, fixture.Order, d.ID)
	if after.Version != 6 {
		t.Errorf("Version = %d, want 6", after.Version)
	}
	if want := fmt.Sprintf("parallel save %d", winner); after.Header["remarks"] != any(want) {
		t.Errorf("remarks = %v, want the winner's %q", after.Header["remarks"], want)
	}
	entries := h.Audit(t, fixture.Order, d.ID)
	if len(entries) != before+1 {
		t.Fatalf("ForDoc rows = %d, want %d (exactly one new)", len(entries), before+1)
	}
	if e := entries[len(entries)-1]; e.Action != "save" {
		t.Errorf("new audit row Action = %q, want save", e.Action)
	}
	if got := h.AuditCount(t); got != count+1 {
		t.Errorf("AuditCount = %d, want %d", got, count+1)
	}
}

// DOC-072
func TestDoc_Concurrency_NoOverfill(t *testing.T) {
	h := fixturetest.New(t)
	o, ol := ruOrder(t, h, "10")
	if o.Number != "PO/26-27/00001" {
		t.Fatalf("setup: order Number = %q, want PO/26-27/00001", o.Number)
	}
	ruReceiptsNeedApproval(h)
	rs := []doc.Doc{
		ruSubmit(t, h, fixturetest.RL{OrderLineID: ol[0], Qty: "6"}),
		ruSubmit(t, h, fixturetest.RL{OrderLineID: ol[0], Qty: "6"}),
	}
	calls := make([]ccCall, len(rs))
	for i, r := range rs {
		if r.State != doc.StatePending {
			t.Fatalf("setup: R%d State = %q, want pending_approval", i+1, r.State)
		}
		ctx := fixture.WithProbe(t.Context(), &fixture.Probe{ForbidOverReceipt: true})
		calls[i] = ccCall{ctx: ctx, p: h.Ravi, dt: fixture.Receipt, id: r.ID, a: doc.ActionApprove, in: trIn(r.Version)}
	}
	ruWantQty(t, h, ol[0], 0, "setup: both receipts pending")
	ov := h.Load(t, fixture.Order, o.ID).Version

	out := ccRace(h, calls)

	var won, lost []int
	for i, x := range out {
		if x.err == nil {
			won = append(won, i)
		} else {
			lost = append(lost, i)
		}
	}
	if len(won) != 1 || len(lost) != 1 {
		t.Fatalf("approvals: R%v succeeded, R%v failed (errs %v, %v); want exactly one of each",
			won, lost, out[0].err, out[1].err)
	}
	w, l := rs[won[0]], rs[lost[0]]
	fixturetest.ErrIs(t, out[lost[0]].err, doc.KindRefused, ccOverfillMsg)
	if res := out[won[0]].res; res.State != doc.StateApproved || res.Number != "GR/26-27/00001" {
		t.Errorf("winner Result State %q Number %q, want approved GR/26-27/00001", res.State, res.Number)
	}

	ruWantQty(t, h, ol[0], 6000, "after both approvals")
	trState(t, h, fixture.Receipt, w.ID, doc.StateApproved, w.Version+1)
	if d := trState(t, h, fixture.Receipt, l.ID, doc.StatePending, l.Version); d.Number != "" {
		t.Errorf("refused receipt Number = %q, want none", d.Number)
	}
	trState(t, h, fixture.Order, o.ID, doc.StateApproved, ov+1)
	if effects := ruEffects(t, h, o.ID); len(effects) != 1 {
		t.Errorf("order effect rows = %d, want 1 (the winner's roll-up): %+v", len(effects), effects)
	}
}

// DOC-073
func TestDoc_Concurrency_CancelVsApprove(t *testing.T) {
	// The test case's literal scenario: R is pending and "approve R" runs
	// alongside "cancel O". A pending R always blocks the cancel, so this
	// checks the outcome (approve succeeds, cancel refused), not a race.
	t.Run("approve pending receipt", func(t *testing.T) {
		h := fixturetest.New(t)
		ruReceiptsNeedApproval(h)
		ccCancelVsApprove(t, h, doc.StatePending, doc.ActionApprove)
	})
	// The race coverage: a draft R whose submit auto-approves it, where only
	// the checks inside the write transaction keep both from succeeding.
	t.Run("submit draft receipt without rule", func(t *testing.T) {
		h := fixturetest.New(t)
		h.Approvals.Needed = func(string, types.Money) bool { return false }
		ccCancelVsApprove(t, h, doc.StateDraft, doc.ActionSubmit)
	})
}

// ccCancelVsApprove runs ccRuns times: an approved order O with a receipt R
// in rState on it, then "cancel O" and R's action a at the same time. Exactly
// one may succeed, and never O cancelled with R approved.
func ccCancelVsApprove(t *testing.T, h *fixturetest.H, rState doc.State, a doc.Action) {
	t.Helper()
	for run := range ccRuns {
		o, ol := ruOrder(t, h, "10.000")
		r := h.Load(t, fixture.Receipt, h.NewReceipt(t, h.Suresh, fixturetest.RL{OrderLineID: ol[0], Qty: "4"}).ID)
		actor := h.Suresh
		if rState == doc.StatePending {
			h.MustDo(t, t.Context(), h.Suresh, fixture.Receipt, r.ID, doc.ActionSubmit, trIn(r.Version))
			r = h.Load(t, fixture.Receipt, r.ID)
			actor = h.Ravi
		}
		if r.State != rState {
			t.Fatalf("run %d setup: receipt State = %q, want %q", run, r.State, rState)
		}

		out := ccRace(h, []ccCall{
			{ctx: t.Context(), p: h.Ravi, dt: fixture.Order, id: o.ID, a: doc.ActionCancel, in: trIn(o.Version, ruOrderCancelReason)},
			{ctx: t.Context(), p: actor, dt: fixture.Receipt, id: r.ID, a: a, in: trIn(r.Version)},
		})
		cancelErr, actErr := out[0].err, out[1].err

		oNow, rNow := h.Load(t, fixture.Order, o.ID), h.Load(t, fixture.Receipt, r.ID)
		if oNow.State == doc.StateCancelled && rNow.State == doc.StateApproved {
			t.Fatalf("run %d: order cancelled and receipt approved (cancel err %v, %s err %v)", run, cancelErr, a, actErr)
		}
		switch {
		case cancelErr == nil && actErr != nil:
			if oNow.State != doc.StateCancelled || rNow.State != rState {
				t.Errorf("run %d: cancel won: order %q, receipt %q; want cancelled, %q", run, oNow.State, rNow.State, rState)
			}
			lkSourceRefused(t, actErr, fmt.Sprintf(lkMsgSourceCancelled, o.Number))
			ruWantQty(t, h, ol[0], 0, fmt.Sprintf("run %d: cancel won", run))
		case cancelErr != nil && actErr == nil:
			if oNow.State != doc.StateApproved || rNow.State != doc.StateApproved {
				t.Errorf("run %d: %s won: order %q, receipt %q; want both approved", run, a, oNow.State, rNow.State)
			}
			// Cancel first sees a pending R as a blocker (a draft doesn't
			// block); after R's roll-up has bumped O's version it is stale.
			var de *doc.Error
			if rState == doc.StatePending && errors.As(cancelErr, &de) && de.Kind == doc.KindBlocked {
				b := trDraft(r.ID)
				fixturetest.ErrIs(t, cancelErr, doc.KindBlocked, fmt.Sprintf(lkMsgBlockedOne, o.Number, b, "waiting for approval", b))
			} else {
				fixturetest.ErrIs(t, cancelErr, doc.KindStale, fmt.Sprintf(msgStaleFmt, o.Number))
			}
			ruWantQty(t, h, ol[0], 4000, fmt.Sprintf("run %d: %s won", run, a))
		default:
			t.Fatalf("run %d: cancel err %v, %s err %v; want exactly one to succeed", run, cancelErr, a, actErr)
		}
	}
}

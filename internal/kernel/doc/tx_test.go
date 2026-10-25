package doc_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// errRuFault is the error the probe injects into a hook or APR call.
var errRuFault = errors.New("ru: injected fault")

// ruDo runs the action and reports a panic that escaped Do as an error, so a
// broken recovery fails the test instead of crashing the binary.
func ruDo(t *testing.T, h *fixturetest.H, p *fixture.Probe, who auth.Principal, dt string, id int64, a doc.Action, in doc.ActionInput) (res doc.Result, err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Do(%s %d %s) panicked: %v", dt, id, a, r)
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return h.Do(t, fixture.WithProbe(t.Context(), p), who, dt, id, a, in)
}

// ruUnchanged fails unless the stored document still has state s, version v
// and no number.
func ruUnchanged(t *testing.T, h *fixturetest.H, dt string, id int64, s doc.State, v int) {
	t.Helper()
	d := trState(t, h, dt, id, s, v)
	if d.Number != "" {
		t.Errorf("Number = %q, want \"\"", d.Number)
	}
	if got := h.Display(t, dt, id); got != trDraft(id) {
		t.Errorf("Display = %q, want %q", got, trDraft(id))
	}
}

// DOC-076
func TestDoc_Tx_ApplyEffectsFailureRollsBack(t *testing.T) {
	t.Run("approve", func(t *testing.T) {
		h := fixturetest.New(t)
		_, ol := ruOrder(t, h, "10.000", "5.000")
		ruReceiptsNeedApproval(h)
		r := ruSubmit(t, h, fixturetest.RL{OrderLineID: ol[0], Qty: "4"}, fixturetest.RL{OrderLineID: ol[1], Qty: "2"})
		if r.State != doc.StatePending {
			t.Fatalf("setup: receipt State = %q, want pending_approval", r.State)
		}
		count := h.AuditCount(t)

		probe := &fixture.Probe{FailAfterWrite: map[string]error{"ApplyEffects": errRuFault}}
		_, err := ruDo(t, h, probe, h.Ravi, fixture.Receipt, r.ID, doc.ActionApprove, trIn(r.Version))
		if !errors.Is(err, errRuFault) {
			t.Fatalf("approve with failing ApplyEffects: err = %v, want the injected fault", err)
		}
		ruUnchanged(t, h, fixture.Receipt, r.ID, doc.StatePending, r.Version)
		ruWantQty(t, h, ol[0], 0, "L1 after the failed approve")
		ruWantQty(t, h, ol[1], 0, "L2 after the failed approve")
		if got := h.AuditCount(t); got != count {
			t.Errorf("AuditCount = %d, want %d (unchanged)", got, count)
		}

		next := ruApproved(t, h, h.Ravi, fixturetest.RL{OrderLineID: ol[0], Qty: "1"})
		if next.Number != "GR/26-27/00001" {
			t.Errorf("next receipt Number = %q, want GR/26-27/00001 (no gap)", next.Number)
		}
		ruWantQty(t, h, ol[0], 1000, "L1 after the next receipt is approved")
		ruWantQty(t, h, ol[1], 0, "L2 after the next receipt is approved")
	})
	// CD-122: DOC owns the no-rule GR path; a failed submit stays a draft and
	// leaves no number gap.
	t.Run("submit without rule", func(t *testing.T) {
		h := fixturetest.New(t)
		_, ol := ruOrder(t, h, "10.000", "5.000")
		res := h.NewReceipt(t, h.Suresh, fixturetest.RL{OrderLineID: ol[0], Qty: "4"}, fixturetest.RL{OrderLineID: ol[1], Qty: "2"})
		count := h.AuditCount(t)

		probe := &fixture.Probe{FailAfterWrite: map[string]error{"ApplyEffects": errRuFault}}
		_, err := ruDo(t, h, probe, h.Suresh, fixture.Receipt, res.ID, doc.ActionSubmit, trIn(res.Version))
		if !errors.Is(err, errRuFault) {
			t.Fatalf("submit with failing ApplyEffects: err = %v, want the injected fault", err)
		}
		ruUnchanged(t, h, fixture.Receipt, res.ID, doc.StateDraft, res.Version)
		ruWantQty(t, h, ol[0], 0, "L1 after the failed submit")
		ruWantQty(t, h, ol[1], 0, "L2 after the failed submit")
		if got := h.AuditCount(t); got != count {
			t.Errorf("AuditCount = %d, want %d (unchanged)", got, count)
		}

		ok := h.MustDo(t, t.Context(), h.Suresh, fixture.Receipt, res.ID, doc.ActionSubmit, trIn(res.Version))
		if ok.State != doc.StateApproved || ok.Number != "GR/26-27/00001" {
			t.Errorf("second submit: State %q Number %q, want approved GR/26-27/00001", ok.State, ok.Number)
		}
		ruWantQty(t, h, ol[0], 4000, "L1 after the second submit")
		ruWantQty(t, h, ol[1], 2000, "L2 after the second submit")
	})
}

// DOC-077
func TestDoc_Tx_ReverseEffectsFailureRollsBack(t *testing.T) {
	h := fixturetest.New(t)
	_, ol := ruOrder(t, h, "10.000")
	r := ruApproved(t, h, h.Ravi, fixturetest.RL{OrderLineID: ol[0], Qty: "4"})
	ruWantQty(t, h, ol[0], 4000, "receipt approved")
	count := h.AuditCount(t)

	probe := &fixture.Probe{FailAfterWrite: map[string]error{"ReverseEffects": errRuFault}}
	_, err := ruDo(t, h, probe, h.Admin, fixture.Receipt, r.ID, doc.ActionCancel, trIn(r.Version, ruCancelReason))
	if !errors.Is(err, errRuFault) {
		t.Fatalf("cancel with failing ReverseEffects: err = %v, want the injected fault", err)
	}
	if !slices.Contains(probe.Hooks(), "ReverseEffects") {
		t.Errorf("hooks = %q, want ReverseEffects called", probe.Hooks())
	}
	if d := trState(t, h, fixture.Receipt, r.ID, doc.StateApproved, r.Version); d.Number != "GR/26-27/00001" {
		t.Errorf("Number = %q, want GR/26-27/00001", d.Number)
	}
	ruWantQty(t, h, ol[0], 4000, "after the failed cancel (hook's write rolled back)")
	if c := trActions(h.Audit(t, fixture.Receipt, r.ID), "cancel"); len(c) != 0 {
		t.Errorf("cancel audit rows = %+v, want none", c)
	}
	if got := h.AuditCount(t); got != count {
		t.Errorf("AuditCount = %d, want %d (unchanged)", got, count)
	}
}

// DOC-086
func TestAudit_SameTransaction(t *testing.T) {
	h := fixturetest.New(t)
	o, ol := ruOrder(t, h, "10.000")
	ruReceiptsNeedApproval(h)
	r := ruSubmit(t, h, fixturetest.RL{OrderLineID: ol[0], Qty: "4"})
	if r.State != doc.StatePending {
		t.Fatalf("setup: receipt State = %q, want pending_approval", r.State)
	}
	count := h.AuditCount(t)
	approve := func(t *testing.T, p *fixture.Probe, v int) error {
		t.Helper()
		_, err := ruDo(t, h, p, h.Ravi, fixture.Receipt, r.ID, doc.ActionApprove, trIn(v))
		return err
	}
	wantNoRows := func(t *testing.T) {
		t.Helper()
		if got := h.AuditCount(t); got != count {
			t.Errorf("AuditCount = %d, want %d (unchanged)", got, count)
		}
		trState(t, h, fixture.Receipt, r.ID, doc.StatePending, r.Version)
		ruWantQty(t, h, ol[0], 0, "after the failed approve")
	}

	t.Run("failure after the effect row", func(t *testing.T) {
		// Record:approve runs after the order's effect row and before the
		// receipt's approve row.
		h.Approvals.Fail = map[string]error{"Record:approve": errRuFault}
		defer func() { h.Approvals.Fail = nil }()
		if err := approve(t, &fixture.Probe{}, r.Version); !errors.Is(err, errRuFault) {
			t.Fatalf("approve: err = %v, want the injected fault", err)
		}
		wantNoRows(t)
	})
	t.Run("refused validation", func(t *testing.T) {
		p := &fixture.Probe{ValidateErrors: []doctype.FieldError{{Field: "remarks", Message: "Remarks are wrong."}}}
		if err := approve(t, p, r.Version); err == nil {
			t.Fatal("approve with a Validate error: err = nil, want an error")
		}
		wantNoRows(t)
	})
	t.Run("permission", func(t *testing.T) {
		h.Auth.Deny = func(auth.Principal, string, auth.Right) bool { return true }
		defer func() { h.Auth.Deny = nil }()
		if err := approve(t, &fixture.Probe{}, r.Version); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("approve denied: err = %v, want auth.ErrForbidden", err)
		}
		wantNoRows(t)
	})
	t.Run("stale version", func(t *testing.T) {
		err := approve(t, &fixture.Probe{}, r.Version-1)
		fixturetest.ErrIs(t, err, doc.KindStale, "")
		wantNoRows(t)
	})
	t.Run("the same approve without a fault writes both rows", func(t *testing.T) {
		if err := approve(t, &fixture.Probe{}, r.Version); err != nil {
			t.Fatalf("approve: %v", err)
		}
		if got := h.AuditCount(t); got != count+2 {
			t.Errorf("AuditCount = %d, want %d (the receipt's approve row and the order's effect row)", got, count+2)
		}
		if n := len(ruEffects(t, h, o.ID)); n != 1 {
			t.Errorf("order effect rows = %d, want 1", n)
		}
		ruWantQty(t, h, ol[0], 4000, "after the approve")
	})
}

// Review Focus 3
func TestDoc_HookPanic_RollsBack(t *testing.T) {
	for _, hook := range []string{"BeforeSubmit", "ApplyEffects"} {
		t.Run(hook, func(t *testing.T) {
			h := fixturetest.New(t)
			_, ol := ruOrder(t, h, "10.000")
			res := h.NewReceipt(t, h.Suresh, fixturetest.RL{OrderLineID: ol[0], Qty: "4"})
			count := h.AuditCount(t)

			probe := &fixture.Probe{Panic: map[string]bool{hook: true}}
			if _, err := ruDo(t, h, probe, h.Suresh, fixture.Receipt, res.ID, doc.ActionSubmit, trIn(res.Version)); err == nil {
				t.Fatalf("submit with %s panicking: err = nil, want an error", hook)
			}
			ruUnchanged(t, h, fixture.Receipt, res.ID, doc.StateDraft, res.Version)
			ruWantQty(t, h, ol[0], 0, "after the panic")
			if got := h.AuditCount(t); got != count {
				t.Errorf("AuditCount = %d, want %d (unchanged)", got, count)
			}

			// The engine keeps working after the panic.
			ok := h.MustDo(t, t.Context(), h.Suresh, fixture.Receipt, res.ID, doc.ActionSubmit, trIn(res.Version))
			if ok.State != doc.StateApproved || ok.Number != "GR/26-27/00001" {
				t.Errorf("submit after the panic: State %q Number %q, want approved GR/26-27/00001", ok.State, ok.Number)
			}
			ruWantQty(t, h, ol[0], 4000, "submit after the panic")
		})
	}
}

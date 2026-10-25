package doc_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/doc"
)

// DOC-006: the order's effect row after a no-rule (auto-approved) submit is
// caused by the receipt's submit.
func TestDoc_Submit_NoRule_EffectCausedBySubmit(t *testing.T) {
	h := fixturetest.New(t)
	o, ol := ruOrder(t, h, "10.000")
	r := ruSubmit(t, h, fixturetest.RL{OrderLineID: ol[0], Qty: "4"})
	if r.State != doc.StateApproved {
		t.Fatalf("setup: receipt State = %q, want approved", r.State)
	}
	effects := ruEffects(t, h, o.ID)
	if len(effects) != 1 {
		t.Fatalf("order effect rows = %+v, want exactly one", effects)
	}
	c := effects[0].CausedBy
	if c == nil {
		t.Fatal("effect CausedBy = nil, want the receipt's submit")
	}
	if c.Action != "submit" || c.DocType != fixture.Receipt || c.DocID != r.ID {
		t.Errorf("effect CausedBy = %+v, want %s %d action submit", *c, fixture.Receipt, r.ID)
	}
}

package fixture_test

import (
	"context"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// Supporting test for CD-162 (no DOC case ID): fixture.Claim declares its
// state convention column as a read-only field (CD-144), so lists can filter
// by it and the engine never takes it from input.
func TestFixture_ClaimStateField(t *testing.T) {
	f, ok := mustLookup(t, fixture.Claim).Field("state")
	if !ok {
		t.Fatal("fixture.Claim has no field \"state\"")
	}
	if f.Column != "state" || !f.System || !f.ReadOnly || !f.Filterable || f.OnLine {
		t.Errorf("fixture.Claim state field = %+v; want Column state, System, ReadOnly, Filterable, header", f)
	}

	h := fixturetest.New(t)
	ctx := context.Background()
	res := h.NewClaim(t, h.Deepak, "EMP-007", fixturetest.CL{Type: "TRAVEL", Amount: "100"})
	fixturetest.Eq(t, h.Record(t, fixture.Claim, res.ID).Values["state"], string(doctype.StateDraft))

	// A posted state on create is ignored.
	in := h.In(h.Load(t, fixture.Claim, res.ID))
	in = doc.ActionInput{
		Values: map[string]string{"employee_id": in.Values["employee_id"], "claim_date": in.Values["claim_date"], "state": string(doctype.StateApproved)},
		Lines:  []map[string]string{{"expense_type": "FOOD", "amount": "50"}},
	}
	created := h.MustDo(t, ctx, h.Deepak, fixture.Claim, 0, doc.ActionSave, in)
	fixturetest.Eq(t, h.Load(t, fixture.Claim, created.ID).State, doctype.StateDraft)

	// A posted state on save is ignored.
	in = h.In(h.Load(t, fixture.Claim, res.ID))
	in.Values["state"] = string(doctype.StateApproved)
	in.Values["remarks"] = "edited"
	h.MustDo(t, ctx, h.Deepak, fixture.Claim, res.ID, doc.ActionSave, in)
	got := h.Load(t, fixture.Claim, res.ID)
	fixturetest.Eq(t, got.State, doctype.StateDraft)
	fixturetest.Eq(t, h.Record(t, fixture.Claim, res.ID).Values["state"], string(doctype.StateDraft))
	if got.Header["remarks"] != "edited" {
		t.Errorf("remarks after save = %v, want edited (the save ran)", got.Header["remarks"])
	}
}

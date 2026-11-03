package expenseweb

import (
	"maps"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// R9 (EXP): a paid advance's timeline has Paid, with its day: the step it
// stands at while approved. An unpaid one has no mark (a later step), and
// neither does one whose paid_on is hidden from the viewer, which would
// otherwise reveal it (CD-319).
func TestS3_AdvancePaidStep(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, "deepak@vardhan.test")
	d := Deps{Service: e.Service, Engine: e.Engine, Auth: e.Auth, Files: e.Files}
	paid := e.PaidAdvance(t, deepak, 1000000)
	unpaid := e.ApprovedAdvance(t, deepak, 1000000)
	on := e.Load(t, expense.AdvanceDocType, paid.ID).Values["paid_on"]
	want := map[string]components.LifeStep{lifePaid: {Mark: components.LifeCurrent, Date: on[len("2026-10-"):] + "-" + on[len("2026-"):len("2026-10")]}}
	for _, c := range []struct {
		name string
		id   int64
		hide []string
		want map[string]components.LifeStep
	}{
		{"paid", paid.ID, nil, want},
		{"not paid", unpaid.ID, nil, nil},
		{"paid, paid_on hidden", paid.ID, []string{"paid_on"}, nil},
	} {
		x, err := advanceExtra(s3HideCtx(t, e, deepak, expense.AdvanceDocType, c.id, advanceHide(d), c.hide...), d)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !maps.Equal(x.Life, c.want) {
			t.Errorf("%s: Life = %+v, want %+v", c.name, x.Life, c.want)
		}
	}
}

// Closed, the Paid step is done with its day, and the closed step is the
// state; marks for other states are never current, so A-26's one step holds.
func TestPaidLife_States(t *testing.T) {
	for _, c := range []struct {
		state doctype.State
		mark  components.LifeMark
	}{
		{doctype.StateApproved, components.LifeCurrent},
		{doctype.StateClosed, components.LifeDone},
	} {
		got := paidLife(c.state, "05-11-2026", true)
		if w := (components.LifeStep{Mark: c.mark, Date: "05-11"}); got[lifePaid] != w {
			t.Errorf("%s: %+v, want %+v", c.state, got[lifePaid], w)
		}
	}
	if got := paidLife(doctype.StateApproved, "Not paid yet", false); got != nil {
		t.Errorf("not paid: %+v, want none", got)
	}
}

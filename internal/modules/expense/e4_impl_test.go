package expense_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/types"
)

// Implementer tests for E4's code beyond the committed cases.

// A blank less advance on an advance with nothing left is refused at submit
// with its own text on advance_id (CD-305 (2)), nothing written.
func TestE4Impl_BlankOnFullyUsedIsZero(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, e4Deepak)
	adv := e.PaidAdvance(t, deepak, e4Rs(1000))
	e.ApprovedClaim(t, deepak, e4On(adv.ID, ""), expensetest.Values{"amount": "1000.00"})
	e4WantUsed(t, e, adv.ID, "fully used", e4Rs(1000), 0)

	c := e4Claim(t, e, deepak, "500.00", e4On(adv.ID, ""))
	want := fmt.Sprintf("%s has nothing left to set against. Remove it from this claim.", e4EA1)
	var err error
	e.WantRefused(t, want, func() error {
		_, err = e6Try(t, e, deepak, expense.ClaimDocType, c.ID, doc.ActionSubmit, "")
		return err
	})
	var he doctype.HTTPError
	if !errors.As(err, &he) || !slices.Contains(he.FieldErrors(), doctype.FieldError{Field: "advance_id", Message: want}) {
		t.Errorf("submit: %v, want %q on advance_id", err, want)
	}
}

// The effect entry records used_amount's change, and the advance's
// updated_by is the claim's updater.
func TestE4Impl_EffectChange(t *testing.T) {
	e := expensetest.New(t)
	deepak, priya := e.P(t, e4Deepak), e.P(t, e4Priya)
	adv := e.PaidAdvance(t, deepak, e4Rs(10000))
	e.ApprovedClaim(t, deepak, e4On(adv.ID, "6000.00"), expensetest.Values{"amount": "6000.00"})

	effects := e4Effects(t, e, adv.ID)
	if len(effects) != 1 {
		t.Fatalf("effect entries = %+v, want 1", effects)
	}
	want := []audit.Change{{Field: "used_amount", Before: "0", After: "600000", Op: audit.OpSet}}
	if !slices.Equal(effects[0].Changes, want) {
		t.Errorf("effect changes = %+v, want %+v", effects[0].Changes, want)
	}
	var by int64
	e2Scan(t, e, `SELECT updated_by FROM employee_advance WHERE id = ?`, []any{adv.ID}, &by)
	if types.UserID(by) != priya.ID {
		t.Errorf("advance updated_by = %d, want the approver priya %d", by, priya.ID)
	}
}

// LeftOn and AdvanceWarnings: an advance p can't read, or one that doesn't
// exist, is not found (CD-245).
func TestE4Impl_HiddenAdvanceIsGone(t *testing.T) {
	e := expensetest.New(t)
	deepak, kavya := e.P(t, e4Deepak), e.P(t, e4Kavya)
	adv := e.PaidAdvance(t, deepak, e4Rs(1000))
	for _, c := range []struct {
		name string
		id   int64
	}{{"hidden", adv.ID}, {"missing", adv.ID + 100}} {
		if _, err := e.Service.LeftOn(t.Context(), kavya, c.id); !errors.Is(err, auth.ErrNotFound) {
			t.Errorf("LeftOn %s: %v, want not found", c.name, err)
		}
		if _, err := e.Service.AdvanceWarnings(t.Context(), kavya, c.id); !errors.Is(err, auth.ErrNotFound) {
			t.Errorf("AdvanceWarnings %s: %v, want not found", c.name, err)
		}
	}
}

// A paid advance with money left doesn't warn about itself.
func TestE4Impl_NoWarningAboutItself(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, e4Deepak)
	adv := e.PaidAdvance(t, deepak, e4Rs(1000))
	if ws := e4Warnings(t, e, deepak, adv.ID); len(ws) != 0 {
		t.Errorf("warnings on the only advance = %q, want none", ws)
	}
}

// AdvanceChoices fills every field, oldest advance date first; a user with no
// read grant on advances gets none.
func TestE4Impl_Choices(t *testing.T) {
	e := expensetest.New(t)
	deepak, priya := e.P(t, e4Deepak), e.P(t, e4Priya)
	emp := e4Employee(t, e, "EMP-007")
	later := e.PaidAdvance(t, deepak, e4Rs(1000))
	early := e.DraftAdvance(t, deepak, expensetest.Values{"advance_date": "2026-10-20", "purpose": "Site visit"},
		expensetest.Values{"line_amount": "2500.50"})
	e6Do(t, e, deepak, expense.AdvanceDocType, early.ID, doc.ActionSubmit, "")
	e6Do(t, e, priya, expense.AdvanceDocType, early.ID, doc.ActionApprove, "")
	e4Paid(t, e, early.ID)
	e.ApprovedClaim(t, deepak, e4On(early.ID, "500.00"), expensetest.Values{"amount": "500.00"})

	got, err := e.Service.AdvanceChoices(t.Context(), deepak, emp)
	if err != nil {
		t.Fatal(err)
	}
	want := []expense.AdvanceChoice{
		{ID: early.ID, Label: e4Number(t, e, expense.AdvanceDocType, early.ID), Date: types.Date{Year: 2026, Month: 10, Day: 20},
			Purpose: "Site visit", Amount: types.Money(250050), Left: types.Money(200050)},
		{ID: later.ID, Label: e4EA1, Date: types.Date{Year: 2026, Month: 10, Day: 24},
			Purpose: e4Purpose(t, e, later.ID), Amount: e4Rs(1000), Left: e4Rs(1000)},
	}
	if !slices.Equal(got, want) {
		t.Errorf("choices =\n  %+v\nwant\n  %+v", got, want)
	}

	none := auth.Principal{ID: deepak.ID, Name: deepak.Name}
	if got, err := e.Service.AdvanceChoices(t.Context(), none, emp); err != nil || len(got) != 0 {
		t.Errorf("choices for a user with no roles = %+v, %v; want none, nil", got, err)
	}
}

// e4Purpose is the advance's stored purpose.
func e4Purpose(t *testing.T, e *expensetest.Env, id int64) string {
	t.Helper()
	var s string
	e2Scan(t, e, `SELECT purpose FROM employee_advance WHERE id = ?`, []any{id}, &s)
	return s
}

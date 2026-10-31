package expense_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// The date rules name paid_on, so the page shows them at the field (UX
// §5.18); the other refusals name no field.
func TestAdv_MarkAsPaidFields(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := expensetest.New(t)
	adv := e9Advance(t, e, doc.StateApproved)
	v := e9Read(t, e, adv.ID).version
	meena := e.P(t, e9Meena)
	for _, c := range []struct {
		on    string
		field string
	}{{"2026-10-25", "paid_on"}, {"2026-10-19", "paid_on"}} {
		err := e.Service.MarkPaid(ctx, meena, adv.ID, v, e9Date(t, c.on))
		var de *doc.Error
		if !errors.As(err, &de) || len(de.Fields) != 1 || de.Fields[0].Field != c.field || de.Fields[0].Message != err.Error() {
			t.Errorf("MarkPaid on %s: err %#v, want one field error on %s", c.on, err, c.field)
		}
	}
	err := e.Service.UndoPaid(ctx, meena, adv.ID, v)
	var de *doc.Error
	if !errors.As(err, &de) || len(de.Fields) != 0 || err.Error() != e9MsgNotPaid {
		t.Errorf("UndoPaid unpaid: err %#v, want %q with no field", err, e9MsgNotPaid)
	}
}

// e9SetRow runs an UPDATE on the advance as a fixture step.
func e9SetRow(t *testing.T, e *expensetest.Env, q string, args ...any) {
	t.Helper()
	err := e.DB.Write(context.Background(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(context.Background(), q, args...)
		return err
	})
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

// MarkPaid and UndoPaid stamp the advance with the acting user and the
// clock's time. The row is first stamped with another user and time, so a
// write that skips either column fails.
func TestAdv_MarkAsPaidStamps(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := expensetest.New(t)
	adv := e9Advance(t, e, doc.StateApproved)
	meena := e.P(t, e9Meena)
	wantAt := types.TimestampOf(e.Clock.Now()).String()
	for _, step := range []struct {
		name string
		do   func(v int) error
	}{
		{"MarkPaid", func(v int) error { return e.Service.MarkPaid(ctx, meena, adv.ID, v, e9Date(t, e9PaidOn)) }},
		{"UndoPaid", func(v int) error { return e.Service.UndoPaid(ctx, meena, adv.ID, v) }},
	} {
		e9SetRow(t, e, `UPDATE employee_advance SET updated_by = ?, updated_at = ? WHERE id = ?`,
			int64(e.P(t, e9Deepak).ID), "2000-01-01T00:00:00.000Z", adv.ID)
		if err := step.do(e9Read(t, e, adv.ID).version); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		var by int64
		var at string
		err := e.DB.Read(ctx, func(tx *db.Tx) error {
			return tx.SQL().QueryRowContext(ctx, `SELECT updated_by, updated_at FROM employee_advance WHERE id = ?`, adv.ID).Scan(&by, &at)
		})
		if err != nil {
			t.Fatalf("read the stamps: %v", err)
		}
		if by != int64(meena.ID) || at != wantAt {
			t.Errorf("after %s: updated_by %d, updated_at %q; want meena %d, %q", step.name, by, at, meena.ID, wantAt)
		}
	}
}

// Undo paid needs an approved advance too: a closed or cancelled one marked
// as paid is refused with the not-approved text and nothing is written.
func TestAdv_UndoPaidNotApproved(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		to   func(t *testing.T, e *expensetest.Env) int64
	}{
		{"closed", func(t *testing.T, e *expensetest.Env) int64 {
			a := e.PaidAdvance(t, e.P(t, e9Deepak), types.Money(500000))
			v := e9Read(t, e, a.ID).version
			return e9Do(t, e, e9Meena, expense.AdvanceDocType, a.ID, doc.ActionClose, doc.ActionInput{Version: v, Reason: "Trip called off."}).ID
		}},
		{"cancelled", func(t *testing.T, e *expensetest.Env) int64 {
			// A paid advance can't be cancelled, so paid_on is set after the
			// cancel with a fixture query.
			a := e9Advance(t, e, doc.StateApproved)
			id := e9Do(t, e, e9Meena, expense.AdvanceDocType, a.ID, doc.ActionCancel, doc.ActionInput{Version: a.Version, Reason: "Trip called off."}).ID
			e9SetRow(t, e, `UPDATE employee_advance SET paid_on = advance_date WHERE id = ?`, id)
			return id
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := expensetest.New(t)
			id := tc.to(t, e)
			s := e9Read(t, e, id)
			if s.state != tc.name || !s.paidOn.Valid {
				t.Fatalf("advance is %q, paid_on %v; want %s and paid", s.state, s.paidOn, tc.name)
			}
			e9Refused(t, e, e9MsgNotApproved, 422, func() error {
				return e.Service.UndoPaid(ctx, e.P(t, e9Meena), id, s.version)
			})
		})
	}
}

package modkit_test

import (
	"context"
	"testing"

	"github.com/vardhan/internaltools/internal/modules/modkit"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

// PO spec §14.2: a document dated on or before locked_up_to is refused with
// "{Label} {display} is dated {date}, which is locked. The period is locked up
// to {lock date}." (422, dates dd-mm-yyyy); a blank lock or a later date
// passes. The lock is set through SaveSettings and read through the caller's
// transaction, on one connection (CD-213).
func TestCheckNotLocked(t *testing.T) {
	e := newEnv(t)
	var (
		jan01 = types.Date{Year: 2020, Month: 1, Day: 1}
		feb15 = types.Date{Year: 2026, Month: 2, Day: 15}
		apr01 = types.Date{Year: 2026, Month: 4, Day: 1}
	)

	type check struct {
		name           string
		label, display string
		date           types.Date
		want           string // "" = allowed
	}
	run := func(t *testing.T, cases []check) {
		t.Helper()
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				err := testkit.NoSecondConn(t, e.db, func(ctx context.Context) error {
					return e.db.Write(ctx, func(tx *db.Tx) error {
						return modkit.CheckNotLocked(ctx, tx, c.label, c.display, c.date)
					})
				})
				call := "CheckNotLocked(" + c.label + ", " + c.display + ", " + types.FormatDate(c.date) + ")"
				if c.want == "" {
					if err != nil {
						t.Errorf("%s = %v, want nil", call, err)
					}
					return
				}
				wantRefusal(t, call, err, 422, c.want)
			})
		}
	}

	t.Run("blank_lock", func(t *testing.T) {
		run(t, []check{
			{name: "old_date", label: "GRN", display: "Draft 42", date: jan01},
			{name: "lock_date_to_be", label: "GRN", display: "Draft 42", date: mar31},
		})
	})

	e.save(t, 1, modkit.SettingsInput{LockedUpTo: lock(mar31)})
	t.Run("locked_up_to_31_03_2026", func(t *testing.T) {
		run(t, []check{
			{name: "on_the_lock", label: "GRN", display: "Draft 42", date: mar31,
				want: "GRN Draft 42 is dated 31-03-2026, which is locked. The period is locked up to 31-03-2026."},
			{name: "before_the_lock", label: "Expense Claim", display: "EC/26-27/00007", date: feb15,
				want: "Expense Claim EC/26-27/00007 is dated 15-02-2026, which is locked. The period is locked up to 31-03-2026."},
			{name: "after_the_lock", label: "GRN", display: "Draft 43", date: apr01},
		})
	})

	// Unlocking (clearing the date) lets the same document through again.
	e.save(t, 2, modkit.SettingsInput{})
	t.Run("unlocked", func(t *testing.T) {
		run(t, []check{
			{name: "on_the_old_lock", label: "GRN", display: "Draft 42", date: mar31},
		})
	})
}

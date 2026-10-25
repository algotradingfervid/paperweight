package fixturetest_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

// Supporting tests for WP1-DOC Task 4 (no DOC case ID).

var _ clock.Clock = (*fixturetest.StepClock)(nil)

// newFixtureDB returns a testkit database with the fixture migrations applied.
func newFixtureDB(t *testing.T) *db.DB {
	t.Helper()
	d := testkit.NewDB(t)
	if err := db.Migrate(t.Context(), d, fixture.MigrationsFS, "goose_fixture"); err != nil {
		t.Fatalf("db.Migrate(fixture.MigrationsFS, goose_fixture): %v", err)
	}
	return d
}

// insertOrder inserts a bare fixture.Order header row and returns its id.
func insertOrder(t *testing.T, tx *db.Tx) int64 {
	t.Helper()
	const at = "2026-10-24T05:00:00.000Z"
	res, err := tx.SQL().ExecContext(t.Context(),
		`INSERT INTO fixture_order (created_at, created_by, updated_at, updated_by) VALUES (?, 4, ?, 4)`, at, at)
	if err != nil {
		t.Fatalf("insert fixture_order: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}
	return id
}

// setNumber stores number on the fixture.Order row id, as the engine does on first approval.
func setNumber(t *testing.T, tx *db.Tx, id int64, number string) {
	t.Helper()
	if _, err := tx.SQL().ExecContext(t.Context(),
		`UPDATE fixture_order SET number = ?, state = 'approved' WHERE id = ?`, number, id); err != nil {
		t.Fatalf("set number %q on fixture_order %d: %v", number, id, err)
	}
}

func TestNaming_AssignIsTransactional(t *testing.T) {
	d := newFixtureDB(t)
	probe := &fixture.Probe{}
	ctx := fixture.WithProbe(t.Context(), probe)
	n := &fixturetest.Naming{}
	date := types.Date{Year: 2026, Month: time.October, Day: 24}
	errRollback := errors.New("roll back")

	var rolledBack string
	err := d.Write(ctx, func(tx *db.Tx) error {
		id := insertOrder(t, tx)
		num, err := n.Assign(ctx, tx, fixture.Order, id, date)
		if err != nil {
			return err
		}
		rolledBack = num
		setNumber(t, tx, id, num)
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("first Write = %v, want the rollback error %v", err, errRollback)
	}
	if rolledBack != "PO/26-27/00001" {
		t.Errorf("Assign in the rolled-back tx = %q, want %q", rolledBack, "PO/26-27/00001")
	}

	var committed, next string
	err = d.Write(ctx, func(tx *db.Tx) error {
		id := insertOrder(t, tx)
		num, err := n.Assign(ctx, tx, fixture.Order, id, date)
		if err != nil {
			return err
		}
		committed = num
		setNumber(t, tx, id, num)
		// The count is computed in the given tx, so it sees the row just numbered.
		id2 := insertOrder(t, tx)
		num2, err := n.Assign(ctx, tx, fixture.Order, id2, date)
		if err != nil {
			return err
		}
		next = num2
		return nil
	})
	if err != nil {
		t.Fatalf("second Write: %v", err)
	}
	if committed != "PO/26-27/00001" {
		t.Errorf("Assign after the rollback = %q, want %q (the rolled-back number must not count)", committed, "PO/26-27/00001")
	}
	if next != "PO/26-27/00002" {
		t.Errorf("Assign after numbering a row in the same tx = %q, want %q", next, "PO/26-27/00002")
	}
	want := []string{"NAM.Assign", "NAM.Assign", "NAM.Assign"}
	if !reflect.DeepEqual(probe.Calls, want) {
		t.Errorf("probe.Calls = %q, want %q", probe.Calls, want)
	}
}

func TestStepClock_Set(t *testing.T) {
	c := fixturetest.NewStepClock("2026-10-24T05:00:00Z")
	if got, want := c.Now(), time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("NewStepClock(2026-10-24T05:00:00Z).Now() = %v, want %v", got, want)
	}

	c.Set("2026-10-25T09:30:00Z")
	if got, want := c.Now(), time.Date(2026, 10, 25, 9, 30, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("after Set(2026-10-25T09:30:00Z) Now() = %v, want %v", got, want)
	}

	at := time.Date(2026, 11, 1, 12, 15, 0, 0, time.UTC)
	c.SetTime(at)
	if got := c.Now(); !got.Equal(at) {
		t.Errorf("after SetTime(%v) Now() = %v, want %v", at, got, at)
	}
}

func TestApprovals_RecordsProtocolActions(t *testing.T) {
	d := newFixtureDB(t)
	probe := &fixture.Probe{}
	ctx := fixture.WithProbe(t.Context(), probe)
	ravi := testkit.Principal(t, "ravi@vardhan.test")

	a := &fixturetest.Approvals{}
	err := d.Write(ctx, func(tx *db.Tx) error {
		return a.Record(ctx, tx, fixture.Order, 1, doctype.ActionApprove, ravi, "")
	})
	if err != nil {
		t.Fatalf("Record(approve) = %v, want nil", err)
	}
	if want := []string{"APR.Record:approve"}; !reflect.DeepEqual(a.Calls, want) {
		t.Errorf("Calls after Record(approve) = %q, want %q", a.Calls, want)
	}
	if want := []string{"APR.Record:approve"}; !reflect.DeepEqual(probe.Calls, want) {
		t.Errorf("probe.Calls after Record(approve) = %q, want %q", probe.Calls, want)
	}

	errWithdraw := errors.New("withdraw failed")
	a.Fail = map[string]error{"Record:withdraw": errWithdraw}
	err = d.Write(ctx, func(tx *db.Tx) error {
		return a.Record(ctx, tx, fixture.Order, 1, doctype.ActionWithdraw, ravi, "")
	})
	if !errors.Is(err, errWithdraw) {
		t.Errorf("Record(withdraw) with Fail[Record:withdraw] = %v, want %v", err, errWithdraw)
	}
	if want := []string{"APR.Record:approve", "APR.Record:withdraw"}; !reflect.DeepEqual(a.Calls, want) {
		t.Errorf("Calls after Record(withdraw) = %q, want %q", a.Calls, want)
	}
}

func TestAuth_HideAndRestrict(t *testing.T) {
	probe := &fixture.Probe{}
	ctx := fixture.WithProbe(context.Background(), probe)
	priya := testkit.Principal(t, "priya@vardhan.test")
	errValues := errors.New("project not allowed")
	errLink := errors.New("linked order not visible")

	a := fixturetest.NewAuth()
	a.Hide = func(_ auth.Principal, dt string, id int64) bool { return dt == fixture.Order && id == 5 }
	a.DenyValues = func(string, map[string][]int64) error { return errValues }
	a.DenyLink = func(string, int64) error { return errLink }

	err := a.Can(ctx, priya, fixture.Order, auth.Right("read"), &auth.RecordRef{DocType: fixture.Order, ID: 5, CreatedBy: priya.ID})
	if !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("Can(read) on a hidden record = %v, want auth.ErrNotFound", err)
	}
	if !errors.Is(err, doctype.ErrNotFound) {
		t.Errorf("Can(read) on a hidden record = %v, want errors.Is(err, doctype.ErrNotFound)", err)
	}

	if err := a.CheckValues(ctx, priya, fixture.Order, map[string][]int64{"project_id": {2}}); !errors.Is(err, errValues) {
		t.Errorf("CheckValues with DenyValues set = %v, want %v", err, errValues)
	}
	if err := a.CheckLink(ctx, priya, fixture.Order, 5); !errors.Is(err, errLink) {
		t.Errorf("CheckLink with DenyLink set = %v, want %v", err, errLink)
	}

	want := []string{"AUTH.Can:read", "AUTH.CheckValues:project_id=2", "AUTH.CheckLink:fixture.Order:5"}
	if !reflect.DeepEqual(a.Calls, want) {
		t.Errorf("Calls = %q, want %q", a.Calls, want)
	}
	if !reflect.DeepEqual(probe.Calls, want) {
		t.Errorf("probe.Calls = %q, want %q", probe.Calls, want)
	}
}

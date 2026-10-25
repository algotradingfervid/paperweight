package naming

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// must fails the test at once on a non-nil error.
func must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// TestCounter_UpsertIncrement covers NAM-030.
func TestCounter_UpsertIncrement(t *testing.T) {
	ctx := context.Background()
	ts := "2026-10-24T05:00:00.000Z"
	d := testkit.NewDB(t)

	var got []int64
	for i := 0; i < 2; i++ {
		must(t, d.Write(ctx, func(tx *db.Tx) error {
			n, err := next(ctx, tx, "fixture.Order", "PO/26-27/", ts)
			got = append(got, n)
			return err
		}))
	}
	if want := []int64{1, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("next twice = %v, want %v", got, want)
	}

	tpl := mustParse(t, "PO/{FY}/{#####}")
	dt := day(2026, time.October, 24)
	if tpl.prefix(dt) != "PO/26-27/" {
		t.Fatalf("prefix = %q, want %q", tpl.prefix(dt), "PO/26-27/")
	}
	var rendered []string
	for _, n := range got {
		rendered = append(rendered, tpl.render(dt, n))
	}
	if want := []string{"PO/26-27/00001", "PO/26-27/00002"}; !reflect.DeepEqual(rendered, want) {
		t.Errorf("rendered = %v, want %v", rendered, want)
	}

	var order, receipt int64
	must(t, d.Read(ctx, func(tx *db.Tx) error {
		var err error
		if order, err = readLast(ctx, tx.SQL(), "fixture.Order", "PO/26-27/"); err != nil {
			return err
		}
		receipt, err = readLast(ctx, tx.SQL(), "fixture.Receipt", "PO/26-27/")
		return err
	}))
	if order != 2 {
		t.Errorf("readLast(fixture.Order, PO/26-27/) = %d, want 2", order)
	}
	if receipt != 0 {
		t.Errorf("readLast(fixture.Receipt, PO/26-27/) = %d, want 0 (no row)", receipt)
	}

	// The stored row itself: one row for the key, last = 2.
	var rows int
	var last int64
	must(t, d.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx,
			`SELECT COUNT(*), COALESCE(MAX(last), -1) FROM naming_counters WHERE doctype = ? AND prefix = ?`,
			"fixture.Order", "PO/26-27/").Scan(&rows, &last)
	}))
	if rows != 1 || last != 2 {
		t.Errorf("naming_counters(fixture.Order, PO/26-27/): rows = %d, last = %d; want 1 row with last 2", rows, last)
	}
}

package audit_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/errs"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

// WP1-DOC Task 2 acceptance tests: the append-only audit store.

var ist = time.FixedZone("IST", 5*3600+30*60)

// roundTripEntry is the plan's Task 2 entry: a header change, a line change
// and an added line, caused by an approved receipt.
func roundTripEntry() audit.Entry {
	return audit.Entry{
		DocType: "fixture.Order",
		DocID:   1,
		Action:  "save",
		Changes: []audit.Change{
			{Field: "remarks", Before: `""`, After: `"x"`, Op: "set"},
			{Field: "qty", LineNo: 2, LineID: 502, Before: "10000", After: "12000", Op: "set"},
			{LineNo: 3, LineID: 503, Op: "add", After: `{"qty":3000}`},
		},
		By:       types.UserID(4),
		At:       time.Date(2026, 10, 24, 10, 30, 0, 0, ist),
		CausedBy: &audit.Ref{DocType: "fixture.Receipt", DocID: 2, Number: "GRN/26-27/00001", Action: "approve"},
	}
}

// validEntry is a complete minimal entry that Store.Append must accept.
func validEntry() audit.Entry {
	return audit.Entry{
		DocType: "fixture.Order",
		DocID:   1,
		Action:  "save",
		Changes: []audit.Change{{Field: "remarks", Before: `""`, After: `"x"`, Op: audit.OpSet}},
		By:      types.UserID(4),
		At:      testkit.Clock().Now(),
	}
}

func appendEntry(t *testing.T, d *db.DB, w audit.Writer, e audit.Entry) {
	t.Helper()
	err := d.Write(context.Background(), func(tx *db.Tx) error {
		return w.Append(context.Background(), tx, e)
	})
	if err != nil {
		t.Fatalf("Append(%+v) = %v, want nil", e, err)
	}
}

func forDoc(t *testing.T, d *db.DB, doctype string, id int64) []audit.Entry {
	t.Helper()
	var got []audit.Entry
	err := d.Read(context.Background(), func(tx *db.Tx) error {
		var err error
		got, err = audit.ForDoc(context.Background(), tx, doctype, id)
		return err
	})
	if err != nil {
		t.Fatalf("ForDoc(%q, %d) = %v, want nil", doctype, id, err)
	}
	return got
}

func count(t *testing.T, d *db.DB) int {
	t.Helper()
	var n int
	err := d.Read(context.Background(), func(tx *db.Tx) error {
		var err error
		n, err = audit.Count(context.Background(), tx)
		return err
	})
	if err != nil {
		t.Fatalf("Count = %v, want nil", err)
	}
	return n
}

// equalIgnoringAt compares two entries field by field, with At compared by
// instant (time.Time locations differ after a UTC round trip).
func equalIgnoringAt(got, want audit.Entry) bool {
	g, w := got, want
	g.At, w.At = time.Time{}, time.Time{}
	return reflect.DeepEqual(g, w) && got.At.Equal(want.At)
}

func TestAudit_AppendRoundTrip(t *testing.T) {
	d := testkit.NewDB(t)
	ctx := context.Background()
	want := roundTripEntry()

	appendEntry(t, d, audit.Store{}, want)

	// A later entry on the same document and one on another document:
	// ForDoc returns only this document's entries, oldest first.
	later := audit.Entry{
		DocType: "fixture.Order", DocID: 1, Action: "submit",
		From: "draft", To: "pending_approval", Reason: "ready",
		Changes: []audit.Change{}, By: types.UserID(4),
		At: time.Date(2026, 10, 24, 5, 1, 0, 0, time.UTC),
	}
	appendEntry(t, d, audit.Store{}, later)
	other := audit.Entry{
		DocType: "fixture.Order", DocID: 2, Action: "save",
		Changes: []audit.Change{}, By: types.UserID(4), At: testkit.Clock().Now(),
	}
	appendEntry(t, d, audit.Store{}, other)
	receipt := audit.Entry{
		DocType: "fixture.Receipt", DocID: 1, Action: "save",
		Changes: []audit.Change{}, By: types.UserID(5), At: testkit.Clock().Now(),
	}
	appendEntry(t, d, audit.Store{}, receipt)

	got := forDoc(t, d, "fixture.Order", 1)
	if len(got) != 2 {
		t.Fatalf("ForDoc(fixture.Order, 1) returned %d entries, want 2: %+v", len(got), got)
	}
	if !equalIgnoringAt(got[0], want) {
		t.Errorf("ForDoc[0] =\n  %+v\nwant\n  %+v", got[0], want)
	}
	if got[0].CausedBy == nil || *got[0].CausedBy != *want.CausedBy {
		t.Errorf("ForDoc[0].CausedBy = %+v, want %+v", got[0].CausedBy, want.CausedBy)
	}
	wantAt := time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC)
	if !got[0].At.Equal(wantAt) {
		t.Errorf("ForDoc[0].At = %v, want %v", got[0].At, wantAt)
	}
	if !equalIgnoringAt(got[1], later) {
		t.Errorf("ForDoc[1] =\n  %+v\nwant\n  %+v", got[1], later)
	}
	if got[1].CausedBy != nil {
		t.Errorf("ForDoc[1].CausedBy = %+v, want nil", got[1].CausedBy)
	}

	// At is stored as UTC in the timestamp layout.
	var storedAt string
	err := d.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx,
			"SELECT at FROM audit_log WHERE doc_type = ? AND doc_id = ? AND action = ?",
			"fixture.Order", 1, "save").Scan(&storedAt)
	})
	if err != nil {
		t.Fatalf("read stored at: %v", err)
	}
	if storedAt != "2026-10-24T05:00:00.000Z" {
		t.Errorf("stored at = %q, want %q", storedAt, "2026-10-24T05:00:00.000Z")
	}
}

func TestAudit_AppendRejectsIncomplete(t *testing.T) {
	d := testkit.NewDB(t)
	ctx := context.Background()

	// A complete entry is accepted, so the refusals below are about the
	// missing parts and not a store that refuses everything.
	appendEntry(t, d, audit.Store{}, validEntry())
	before := count(t, d)
	if before != 1 {
		t.Fatalf("Count after one Append = %d, want 1", before)
	}

	cases := []struct {
		name   string
		mutate func(e *audit.Entry)
	}{
		{"empty DocType", func(e *audit.Entry) { e.DocType = "" }},
		{"empty Action", func(e *audit.Entry) { e.Action = "" }},
		{"zero At", func(e *audit.Entry) { e.At = time.Time{} }},
		{"Op move", func(e *audit.Entry) {
			e.Changes = []audit.Change{{Field: "qty", LineNo: 2, LineID: 502, Before: "10000", After: "12000", Op: "move"}}
		}},
		{"Op add with LineID 0", func(e *audit.Entry) {
			e.Changes = []audit.Change{{LineNo: 3, LineID: 0, Op: audit.OpAdd, After: `{"qty":3000}`}}
		}},
		{"Op remove with LineID 0", func(e *audit.Entry) {
			e.Changes = []audit.Change{{LineNo: 3, LineID: 0, Op: audit.OpRemove, Before: `{"qty":3000}`}}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := validEntry()
			c.mutate(&e)
			err := d.Write(ctx, func(tx *db.Tx) error {
				return audit.Store{}.Append(ctx, tx, e)
			})
			if err == nil {
				t.Fatalf("Append(%+v) = nil, want an error", e)
			}
			if errors.Is(err, errs.ErrNotImplemented) {
				t.Fatalf("Append(%+v) = %v, want a refusal, not ErrNotImplemented", e, err)
			}
			if n := count(t, d); n != before {
				t.Errorf("Count after refused Append = %d, want %d", n, before)
			}
		})
	}
}

func TestAudit_DefaultIsStore(t *testing.T) {
	var _ audit.Writer = audit.Store{}
	if _, ok := audit.Default.(audit.Store); !ok {
		t.Fatalf("audit.Default is %T, want audit.Store", audit.Default)
	}

	d := testkit.NewDB(t)
	ctx := context.Background()
	before := count(t, d)
	err := d.Write(ctx, func(tx *db.Tx) error {
		return audit.Append(ctx, tx, validEntry())
	})
	if err != nil {
		t.Fatalf("audit.Append = %v, want nil", err)
	}
	if n := count(t, d); n != before+1 {
		t.Errorf("Count after audit.Append = %d, want %d", n, before+1)
	}
}

// DOC-085
func TestAudit_AppendOnlyTriggers(t *testing.T) {
	d := testkit.NewDB(t)
	ctx := context.Background()
	appendEntry(t, d, audit.Store{}, roundTripEntry())
	before := forDoc(t, d, "fixture.Order", 1)
	if len(before) != 1 {
		t.Fatalf("ForDoc after one Append returned %d entries, want 1", len(before))
	}

	var id int64
	err := d.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, "SELECT id FROM audit_log WHERE doc_type = ? AND doc_id = ?", "fixture.Order", 1).Scan(&id)
	})
	if err != nil {
		t.Fatalf("read audit row id: %v", err)
	}

	for _, stmt := range []string{
		"UPDATE audit_log SET user_id = 1 WHERE id = ?",
		"DELETE FROM audit_log WHERE id = ?",
	} {
		err := d.Write(ctx, func(tx *db.Tx) error {
			_, err := tx.SQL().ExecContext(ctx, stmt, id)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), "audit log is append-only") {
			t.Errorf("%s: err = %v, want it to contain %q", stmt, err, "audit log is append-only")
		}
	}

	after := forDoc(t, d, "fixture.Order", 1)
	if len(after) != len(before) {
		t.Fatalf("ForDoc after UPDATE/DELETE returned %d entries, want %d", len(after), len(before))
	}
	for i := range before {
		if !equalIgnoringAt(after[i], before[i]) {
			t.Errorf("ForDoc[%d] changed:\n  got  %+v\n  want %+v", i, after[i], before[i])
		}
	}
	if n := count(t, d); n != 1 {
		t.Errorf("Count after UPDATE/DELETE = %d, want 1", n)
	}
}

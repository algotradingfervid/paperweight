package naming

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// approveNew creates a document dated d, submits it for approval, approves it
// and returns its number.
func approveNew(t *testing.T, h *harness, dt string, d types.Date) string {
	t.Helper()
	id := h.create(dt, d)
	must(t, h.submit(id, true))
	must(t, h.approve(id))
	got := h.doc(id)
	if got.State != string(doctype.StateApproved) || !got.Number.Valid {
		t.Fatalf("%s dated %s after approve: state %q, number %v; want approved with a number",
			dt, dateText(d), got.State, got.Number)
	}
	return got.Number.String
}

// TestAssign_DraftHasNoNumber covers NAM-022.
func TestAssign_DraftHasNoNumber(t *testing.T) {
	h := newHarness(t)
	id := h.create(dtPO, day(2026, 10, 24))

	got := h.doc(id)
	if got.Number.Valid {
		t.Errorf("draft number = %q, want NULL", got.Number.String)
	}
	if c := h.counter(dtPO, "PO/26-27/"); c != 0 {
		t.Errorf("counter(PO/26-27/) = %d, want 0", c)
	}
	if n := h.count(`SELECT COUNT(*) FROM naming_counters`); n != 0 {
		t.Errorf("naming_counters has %d rows, want 0", n)
	}
	if l, want := Label(got.Number.String, id), fmt.Sprintf("Draft %d", id); l != want {
		t.Errorf("Label = %q, want %q", l, want)
	}
}

// TestAssign_OnApproval covers NAM-023.
func TestAssign_OnApproval(t *testing.T) {
	h := newHarness(t)
	id := h.create(dtPO, day(2026, 10, 24))

	must(t, h.submit(id, true))
	got := h.doc(id)
	if got.State != string(doctype.StatePending) {
		t.Errorf("state after submit = %q, want %q", got.State, doctype.StatePending)
	}
	if got.Number.Valid {
		t.Errorf("number after submit = %q, want NULL", got.Number.String)
	}
	if l, want := Label(got.Number.String, id), fmt.Sprintf("Draft %d", id); l != want {
		t.Errorf("Label after submit = %q, want %q", l, want)
	}

	must(t, h.approve(id))
	got = h.doc(id)
	if got.State != string(doctype.StateApproved) {
		t.Errorf("state after approve = %q, want %q", got.State, doctype.StateApproved)
	}
	if !got.Number.Valid || got.Number.String != "PO/26-27/00001" {
		t.Errorf("number after approve = %v, want PO/26-27/00001", got.Number)
	}
}

// TestAssign_DirectApprove covers NAM-024 (expense claim, CD-25; EX series, CD-115).
func TestAssign_DirectApprove(t *testing.T) {
	h := newHarness(t)
	id := h.create(dtEXP, day(2026, 10, 24))

	must(t, h.submit(id, false))
	got := h.doc(id)
	if got.State != string(doctype.StateApproved) {
		t.Errorf("state after submit with no rule = %q, want %q", got.State, doctype.StateApproved)
	}
	if !got.Number.Valid || got.Number.String != "EX/26-27/00001" {
		t.Errorf("number = %v, want EX/26-27/00001", got.Number)
	}
}

// TestAssign_UsesDocumentDateNotClock covers NAM-004.
func TestAssign_UsesDocumentDateNotClock(t *testing.T) {
	h := newHarness(t)

	// 2026-03-31T19:00Z is 01-04-2026 00:30 IST: the clock is in FY 26-27,
	// the document date in 25-26.
	h.nm = New(h.db, fixedClock(time.Date(2026, time.March, 31, 19, 0, 0, 0, time.UTC)), h.roles, h.audit)
	if got := approveNew(t, h, dtPO, day(2026, 3, 31)); got != "PO/25-26/00001" {
		t.Errorf("PO dated 2026-03-31 approved at 2026-03-31T19:00Z = %q, want PO/25-26/00001", got)
	}

	h.nm = New(h.db, fixedClock(time.Date(2026, time.April, 10, 5, 0, 0, 0, time.UTC)), h.roles, h.audit)
	if got := approveNew(t, h, dtPO, day(2026, 4, 2)); got != "PO/26-27/00001" {
		t.Errorf("PO dated 2026-04-02 approved at 2026-04-10T05:00Z = %q, want PO/26-27/00001", got)
	}
}

// TestAssign_FYReset covers NAM-005.
func TestAssign_FYReset(t *testing.T) {
	h := newHarness(t)
	h.preset(dtPO, "PO/25-26/", 143)

	if got := approveNew(t, h, dtPO, day(2026, 4, 1)); got != "PO/26-27/00001" {
		t.Errorf("PO dated 2026-04-01 = %q, want PO/26-27/00001", got)
	}
	if c := h.counter(dtPO, "PO/25-26/"); c != 143 {
		t.Errorf("counter(PO/25-26/) = %d, want 143", c)
	}
}

// TestAssign_BackdatedUsesOwnFY covers NAM-006.
func TestAssign_BackdatedUsesOwnFY(t *testing.T) {
	h := newHarness(t)
	h.nm = New(h.db, fixedClock(time.Date(2026, time.April, 12, 5, 0, 0, 0, time.UTC)), h.roles, h.audit)
	h.preset(dtPO, "PO/25-26/", 143)
	h.preset(dtPO, "PO/26-27/", 4)

	if got := approveNew(t, h, dtPO, day(2026, 3, 28)); got != "PO/25-26/00144" {
		t.Errorf("PO dated 2026-03-28 = %q, want PO/25-26/00144", got)
	}
	if got := approveNew(t, h, dtPO, day(2026, 4, 12)); got != "PO/26-27/00005" {
		t.Errorf("PO dated 2026-04-12 = %q, want PO/26-27/00005", got)
	}
}

// TestAssign_PerDocTypeCounters covers NAM-028 (GR series, CD-113).
func TestAssign_PerDocTypeCounters(t *testing.T) {
	h := newHarness(t)
	d := day(2026, 10, 24)

	got := []string{
		approveNew(t, h, dtPO, d),
		approveNew(t, h, dtGRN, d),
		approveNew(t, h, dtPO, d),
	}
	want := []string{"PO/26-27/00001", "GR/26-27/00001", "PO/26-27/00002"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("numbers = %q, want %q", got, want)
			break
		}
	}
}

// TestAssign_RejectsZeroDate covers Review Focus 1.
func TestAssign_RejectsZeroDate(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	err := h.db.Write(ctx, func(tx *db.Tx) error {
		_, err := h.nm.Assign(ctx, tx, dtPO, 1, types.Date{})
		return err
	})
	if !errors.Is(err, ErrInvalidDate) {
		t.Fatalf("Assign with zero date: err = %v, want ErrInvalidDate", err)
	}
	if err.Error() != "A document date is needed to give it a number." {
		t.Errorf("message = %q, want %q", err.Error(), "A document date is needed to give it a number.")
	}
	if n := h.count(`SELECT COUNT(*) FROM naming_counters`); n != 0 {
		t.Errorf("naming_counters has %d rows, want 0", n)
	}
	if n := h.count(`SELECT COUNT(*) FROM naming_issued`); n != 0 {
		t.Errorf("naming_issued has %d rows, want 0", n)
	}
}

// TestAssign_UnknownDocType covers Review Focus 2 (CD-66).
func TestAssign_UnknownDocType(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	err := h.db.Write(ctx, func(tx *db.Tx) error {
		_, err := h.nm.Assign(ctx, tx, "fixture.Unknown", 1, day(2026, 10, 24))
		return err
	})
	if !errors.Is(err, ErrNoSeries) {
		t.Fatalf("Assign for fixture.Unknown: err = %v, want ErrNoSeries", err)
	}
	if want := "No numbering series is set up for fixture.Unknown. Ask an Administrator."; err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
	if n := h.count(`SELECT COUNT(*) FROM naming_counters`); n != 0 {
		t.Errorf("naming_counters has %d rows, want 0", n)
	}
	if n := h.count(`SELECT COUNT(*) FROM naming_issued`); n != 0 {
		t.Errorf("naming_issued has %d rows, want 0", n)
	}
}

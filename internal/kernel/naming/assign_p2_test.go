package naming

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// TestAssign_NoDateTokenNeverResets covers NAM-007.
func TestAssign_NoDateTokenNeverResets(t *testing.T) {
	h := newHarness(t)
	must(t, h.nm.EnsureSeries(context.Background(), "nam.GRN2", "GRN", "GRN{######}"))
	// GRN000143, dated 2026-03-31, is implied by the preset.
	h.preset("nam.GRN2", "GRN", 143)

	if got := approveNew(t, h, "nam.GRN2", day(2026, 4, 1)); got != "GRN000144" {
		t.Errorf("GRN dated 2026-04-01 = %q, want GRN000144", got)
	}
	wantCounter(t, h, "nam.GRN2", "GRN", 144)
}

// TestAssign_MonthlyPrefixResets covers NAM-008.
func TestAssign_MonthlyPrefixResets(t *testing.T) {
	h := newHarness(t)
	must(t, h.nm.EnsureSeries(context.Background(), "nam.Exp2", "Expense Claim", "EXP/{YY}{MM}/{###}"))

	got := []string{
		approveNew(t, h, "nam.Exp2", day(2026, 4, 30)),
		approveNew(t, h, "nam.Exp2", day(2026, 4, 30)),
		approveNew(t, h, "nam.Exp2", day(2026, 5, 1)),
	}
	want := []string{"EXP/2604/001", "EXP/2604/002", "EXP/2605/001"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("numbers = %q, want %q", got, want)
			break
		}
	}
}

// TestAssign_UniqueIndexBackstop covers NAM-035.
func TestAssign_UniqueIndexBackstop(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	d := day(2026, 10, 24)

	if got := approveNew(t, h, dtPO, d); got != "PO/26-27/00001" {
		t.Fatalf("first PO = %q, want PO/26-27/00001", got)
	}
	b := h.create(dtPO, d)
	must(t, h.submit(b, true))
	// Force the counter back so the next number collides.
	must(t, h.db.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, `UPDATE naming_counters SET last = 0`)
		return err
	}))

	err := h.approve(b)
	wantNamingError(t, "approve second PO", err, ErrDuplicateNumber,
		"Number PO/26-27/00001 is already used. Please try again.")
	wantDoc(t, h, b, doctype.StatePending, "")
	wantCounter(t, h, dtPO, "PO/26-27/", 0)
	if n := h.count(`SELECT COUNT(*) FROM naming_issued`); n != 1 {
		t.Errorf("naming_issued has %d rows, want 1", n)
	}
}

// TestAssign_PaddingExhausted covers NAM-063 (GR series, CD-113; CD-66).
func TestAssign_PaddingExhausted(t *testing.T) {
	h := newHarness(t)
	must(t, h.nm.SetTemplate(context.Background(), admin, dtGRN, "GR/{FY}/{###}"))
	h.preset(dtGRN, "GR/26-27/", 999)
	g := h.create(dtGRN, day(2026, 10, 24))

	// No approval rule, so submit is its approval.
	err := h.submit(g, false)
	wantNamingError(t, "submit GRN", err, ErrExhausted,
		"Series GR/26-27/ has used all 999 numbers. Ask an Administrator to add a digit to the series.")
	wantDoc(t, h, g, doctype.StateDraft, "")
	wantCounter(t, h, dtGRN, "GR/26-27/", 999)
	if n := h.count(`SELECT COUNT(*) FROM naming_issued`); n != 0 {
		t.Errorf("naming_issued has %d rows, want 0", n)
	}
}

// TestSettings_PaddingChangeContinues covers NAM-053.
func TestSettings_PaddingChangeContinues(t *testing.T) {
	h := newHarness(t)
	h.preset(dtPO, "PO/26-27/", 143)
	must(t, h.nm.SetTemplate(context.Background(), admin, dtPO, "PO/{FY}/{####}"))

	if got := approveNew(t, h, dtPO, day(2026, 10, 24)); got != "PO/26-27/0144" {
		t.Errorf("PO after padding change = %q, want PO/26-27/0144", got)
	}
	wantCounter(t, h, dtPO, "PO/26-27/", 144)
}

// TestAssign_BusyTimeout covers NAM-034 (CD-08): another connection holds the
// write lock past a 200 ms busy timeout.
func TestAssign_BusyTimeout(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	p := h.create(dtPO, day(2026, 10, 24))
	must(t, h.submit(p, true))

	d2, err := db.Open(testkit.DBPath(t), db.WithBusyTimeout(200*time.Millisecond))
	if err != nil {
		t.Fatalf("db.Open second handle: %v", err)
	}
	t.Cleanup(func() { _ = d2.Close() })
	h2 := newHarnessOn(t, d2)

	held := make(chan struct{})
	release := make(chan struct{})
	writerDone := make(chan error, 1)
	go func() {
		writerDone <- h.db.Write(ctx, func(*db.Tx) error {
			close(held)
			<-release
			return nil
		})
	}()
	released := false
	releaseWriter := func() {
		if !released {
			released = true
			close(release)
			if err := <-writerDone; err != nil {
				t.Errorf("holding writer: %v", err)
			}
		}
	}
	defer releaseWriter()
	<-held

	start := time.Now()
	err = h2.approve(p)
	waited := time.Since(start)
	releaseWriter()

	if !errors.Is(err, db.ErrBusy) {
		t.Fatalf("approve while another connection holds the lock: err = %v, want db.ErrBusy", err)
	}
	if want := "The system is busy. Please try again in a moment."; err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
	// The wait is the 200 ms timeout, not the default 5 s.
	if waited > 3*time.Second {
		t.Errorf("approve waited %v, want about 200ms (WithBusyTimeout not applied)", waited)
	}
	wantDoc(t, h, p, doctype.StatePending, "")
	wantCounter(t, h, dtPO, "PO/26-27/", 0)
	if n := h.count(`SELECT COUNT(*) FROM naming_issued`); n != 0 {
		t.Errorf("naming_issued has %d rows, want 0", n)
	}
}

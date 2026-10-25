package naming

import (
	"context"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// TestPreview_DoesNotConsume covers NAM-036: previewing the next number
// neither uses it nor takes the write lock.
func TestPreview_DoesNotConsume(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	d := day(2026, 10, 24)
	h.preset(dtPO, "PO/26-27/", 2)

	// Previewed twice (form header, settings page): same number, counter untouched.
	for i := 1; i <= 2; i++ {
		got, err := h.nm.Preview(ctx, dtPO, d)
		if err != nil {
			t.Fatalf("Preview #%d: %v", i, err)
		}
		if got != "PO/26-27/00003" {
			t.Errorf("Preview #%d = %q, want %q", i, got, "PO/26-27/00003")
		}
	}
	wantCounter(t, h, dtPO, "PO/26-27/", 2)
	if n := h.count(`SELECT COUNT(*) FROM naming_issued`); n != 0 {
		t.Errorf("naming_issued has %d rows after Preview, want 0", n)
	}

	// No write lock: Preview returns while another writer holds db.Write open.
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

	type result struct {
		num string
		err error
	}
	res := make(chan result, 1)
	go func() {
		num, err := h.nm.Preview(ctx, dtPO, d)
		res <- result{num, err}
	}()
	select {
	case r := <-res:
		if r.err != nil {
			t.Errorf("Preview while a writer holds the lock: %v", r.err)
		} else if r.num != "PO/26-27/00003" {
			t.Errorf("Preview while a writer holds the lock = %q, want %q", r.num, "PO/26-27/00003")
		}
	case <-time.After(time.Second):
		t.Errorf("Preview did not return within 1s while a writer held the lock (it takes the write lock)")
	}
	releaseWriter()

	// The previewed number is the one the next approval gets.
	id := h.create(dtPO, d)
	must(t, h.submit(id, true))
	must(t, h.approve(id))
	wantDoc(t, h, id, doctype.StateApproved, "PO/26-27/00003")
	wantCounter(t, h, dtPO, "PO/26-27/", 3)
}

// TestPreview_Exhausted: a preview never shows a number that can't be issued
// (AD decision). At 99999 with {#####} it gives Assign's ErrExhausted text
// instead of rendering 100000.
func TestPreview_Exhausted(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	d := day(2026, 10, 24)
	h.preset(dtPO, "PO/26-27/", 99999)
	const msg = "Series PO/26-27/ has used all 99999 numbers. Ask an Administrator to add a digit to the series."

	got, err := h.nm.Preview(ctx, dtPO, d)
	wantNamingError(t, "Preview at 99999", err, ErrExhausted, msg)
	if got != "" {
		t.Errorf("Preview at 99999 = %q, want \"\"", got)
	}
	wantCounter(t, h, dtPO, "PO/26-27/", 99999)

	// Assign at the same counter gives the same message.
	id := h.create(dtPO, d)
	must(t, h.submit(id, true))
	wantNamingError(t, "approve at 99999", h.approve(id), ErrExhausted, msg)
}

// TestPreview_RejectsZeroDate covers Review Focus 1 for Preview.
func TestPreview_RejectsZeroDate(t *testing.T) {
	h := newHarness(t)
	got, err := h.nm.Preview(context.Background(), dtPO, types.Date{})
	wantNamingError(t, "Preview with zero date", err, ErrInvalidDate, "A document date is needed to give it a number.")
	if got != "" {
		t.Errorf("Preview with zero date = %q, want \"\"", got)
	}
	if n := h.count(`SELECT COUNT(*) FROM naming_counters`); n != 0 {
		t.Errorf("naming_counters has %d rows, want 0", n)
	}
}

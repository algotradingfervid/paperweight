package naming

import (
	"context"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
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

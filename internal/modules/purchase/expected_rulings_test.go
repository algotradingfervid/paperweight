package purchase_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// msgExpectedNoteLong is DOC's generic length wording for the 200-character
// note (AD-13 ruling (1) on P18; spec §27.1 note max 200).
const msgExpectedNoteLong = "Note can be at most 200 characters."

// PO-080, AD-13 ruling (1) on P18 (spec §27.1; CD-147): a note over 200
// characters is refused with DOC's length text (422) on insert and on update,
// with the row and the PO's audit entries unchanged; exactly 200 characters
// saves, counted as characters, not bytes.
func TestPO_ExpectedOnNoteLength(t *testing.T) {
	env := purchasetest.New(t)
	ctx := t.Context()
	svc := env.Service
	po := purchasetest.NewOrder(t, env)
	nov5 := expDate(2026, time.November, 5)

	// Insert (posted 0, no row): 201 characters refused.
	before := expCountsOf(t, env, po.ID)
	expWantRefused(t, "SetExpected with a 201-character note, version 0",
		svc.SetExpected(ctx, env.Arjun, po.ID, 0, nov5, strings.Repeat("a", 201)), http.StatusUnprocessableEntity, msgExpectedNoteLong)
	if after := expCountsOf(t, env, po.ID); after != before {
		t.Errorf("row counts after the refused insert = %+v, want %+v", after, before)
	}
	if r := expectedRow(t, env, po.ID); r.ok {
		t.Errorf("row after the refused insert = %+v, want none", r)
	}

	// Exactly 200 saves.
	note200 := strings.Repeat("a", 200)
	if err := svc.SetExpected(ctx, env.Arjun, po.ID, 0, nov5, note200); err != nil {
		t.Fatalf("SetExpected with a 200-character note: %v", err)
	}
	if r := expectedRow(t, env, po.ID); !r.ok || r.note != note200 || r.version != 1 {
		t.Errorf("row after the 200-character note = version %d, note length %d, ok %v; want version 1 with the note", r.version, len(r.note), r.ok)
	}

	// Update (posted current): 201 refused, the row as it was.
	before, row := expCountsOf(t, env, po.ID), expectedRow(t, env, po.ID)
	expWantRefused(t, "SetExpected with a 201-character note, version 1",
		svc.SetExpected(ctx, env.Ravi, po.ID, 1, nov5, strings.Repeat("b", 201)), http.StatusUnprocessableEntity, msgExpectedNoteLong)
	if after := expCountsOf(t, env, po.ID); after != before {
		t.Errorf("row counts after the refused update = %+v, want %+v", after, before)
	}
	if after := expectedRow(t, env, po.ID); after != row {
		t.Errorf("row after the refused update = %+v, want %+v", after, row)
	}

	// 200 characters of a multi-byte rune is still 200 characters.
	rupees := strings.Repeat("₹", 200)
	if err := svc.SetExpected(ctx, env.Ravi, po.ID, 1, nov5, rupees); err != nil {
		t.Fatalf("SetExpected with 200 ₹ characters (600 bytes): %v", err)
	}
	if r := expectedRow(t, env, po.ID); r.note != rupees || r.version != 2 {
		t.Errorf("row after 200 ₹ characters = version %d, note length %d bytes; want version 2 with the note", r.version, len(r.note))
	}
}

// PO-080, AD-13 ruling (2) on P18 (spec §27.1): ClearExpected with posted
// version 0 on a PO with no record is a no-op: nil, no audit entry, no row.
// The load and right checks still run first (CD-245, CD-259).
func TestPO_ExpectedOnClearNoRecord(t *testing.T) {
	env := purchasetest.New(t)
	ctx := t.Context()
	svc := env.Service
	po := purchasetest.NewOrder(t, env)

	before := expCountsOf(t, env, po.ID)
	if err := svc.ClearExpected(ctx, env.Arjun, po.ID, 0); err != nil {
		t.Errorf("ClearExpected with version 0 and no record = %v, want nil (a no-op)", err)
	}
	if after := expCountsOf(t, env, po.ID); after != before {
		t.Errorf("row counts and audit entries after the no-op clear = %+v, want %+v", after, before)
	}
	if r := expectedRow(t, env, po.ID); r.ok {
		t.Errorf("row after the no-op clear = %+v, want none", r)
	}

	// Still refused for who may not, and for a missing PO.
	expWantDenied(t, "ClearExpected as Suresh, version 0, no record", svc.ClearExpected(ctx, env.Suresh, po.ID, 0))
	expWantNotFound(t, "ClearExpected on a missing PO, version 0", svc.ClearExpected(ctx, env.Arjun, 999_999, 0))

	// After a set and a clear, a second clear with 0 is a no-op too.
	if err := svc.SetExpected(ctx, env.Arjun, po.ID, 0, expDate(2026, time.November, 5), ""); err != nil {
		t.Fatalf("SetExpected: %v", err)
	}
	if err := svc.ClearExpected(ctx, env.Arjun, po.ID, 1); err != nil {
		t.Fatalf("ClearExpected version 1: %v", err)
	}
	before = expCountsOf(t, env, po.ID)
	if err := svc.ClearExpected(ctx, env.Ravi, po.ID, 0); err != nil {
		t.Errorf("second ClearExpected with version 0 = %v, want nil (a no-op)", err)
	}
	if after := expCountsOf(t, env, po.ID); after != before {
		t.Errorf("row counts and audit entries after the second clear = %+v, want %+v", after, before)
	}
}

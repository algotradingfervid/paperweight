package naming

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

func ptr(s string) *string { return &s }

// withdraw is spec §3.2's withdraw: pending_approval → draft (NAM-025).
func (h *harness) withdraw(id int64) error {
	return h.transition(id, "withdraw", doctype.StatePending, doctype.StateDraft)
}

// wantDoc checks a document's state and number ("" means NULL).
func wantDoc(t *testing.T, h *harness, id int64, state doctype.State, number string) {
	t.Helper()
	got := h.doc(id)
	if got.State != string(state) {
		t.Errorf("doc %d state = %q, want %q", id, got.State, state)
	}
	switch {
	case number == "" && got.Number.Valid:
		t.Errorf("doc %d number = %q, want NULL", id, got.Number.String)
	case number != "" && (!got.Number.Valid || got.Number.String != number):
		t.Errorf("doc %d number = %v, want %s", id, got.Number, number)
	}
}

func wantCounter(t *testing.T, h *harness, dt, prefix string, want int64) {
	t.Helper()
	if c := h.counter(dt, prefix); c != want {
		t.Errorf("counter(%s, %s) = %d, want %d", dt, prefix, c, want)
	}
}

// ledgerSeqs returns the issued seqs for (doctype, prefix) in order.
func ledgerSeqs(t *testing.T, h *harness, dt, prefix string) []int64 {
	t.Helper()
	ctx := context.Background()
	var seqs []int64
	if err := h.db.Read(ctx, func(tx *db.Tx) error {
		rows, err := tx.SQL().QueryContext(ctx,
			`SELECT seq FROM naming_issued WHERE doctype = ? AND prefix = ? ORDER BY seq`, dt, prefix)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var s int64
			if err := rows.Scan(&s); err != nil {
				return err
			}
			seqs = append(seqs, s)
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("ledger seqs(%s, %s): %v", dt, prefix, err)
	}
	return seqs
}

// TestAssign_RejectedThenApproved covers NAM-025 (spec §3.2: reject →
// rejected, revise → draft, withdraw → draft).
func TestAssign_RejectedThenApproved(t *testing.T) {
	h := newHarness(t)
	d := day(2026, 10, 24)
	h.preset(dtPO, "PO/26-27/", 5)

	a := h.create(dtPO, d)
	must(t, h.submit(a, true))
	must(t, h.reject(a))
	wantDoc(t, h, a, doctype.StateRejected, "")
	if l, want := Label(h.doc(a).Number.String, a), fmt.Sprintf("Draft %d", a); l != want {
		t.Errorf("Label of rejected doc = %q, want %q", l, want)
	}
	wantCounter(t, h, dtPO, "PO/26-27/", 5)

	b := h.create(dtPO, d)
	must(t, h.submit(b, true))
	must(t, h.approve(b))
	wantDoc(t, h, b, doctype.StateApproved, "PO/26-27/00006")

	must(t, h.revise(a))
	wantDoc(t, h, a, doctype.StateDraft, "")
	must(t, h.submit(a, true))
	must(t, h.approve(a))
	wantDoc(t, h, a, doctype.StateApproved, "PO/26-27/00007")

	c := h.create(dtPO, d)
	must(t, h.submit(c, true))
	must(t, h.withdraw(c))
	wantDoc(t, h, c, doctype.StateDraft, "")
	wantCounter(t, h, dtPO, "PO/26-27/", 7)
	must(t, h.submit(c, true))
	must(t, h.approve(c))
	wantDoc(t, h, c, doctype.StateApproved, "PO/26-27/00008")
}

// TestAssign_DeleteDraftNoGap covers NAM-026.
func TestAssign_DeleteDraftNoGap(t *testing.T) {
	h := newHarness(t)
	d := day(2026, 10, 24)

	if got := approveNew(t, h, dtPO, d); got != "PO/26-27/00001" {
		t.Errorf("A = %q, want PO/26-27/00001", got)
	}
	b := h.create(dtPO, d)
	must(t, h.deleteDraft(b))
	if n := h.count(`SELECT COUNT(*) FROM nam_test_docs WHERE id = ?`, b); n != 0 {
		t.Errorf("draft B still present after delete")
	}
	if got := approveNew(t, h, dtPO, d); got != "PO/26-27/00002" {
		t.Errorf("C = %q, want PO/26-27/00002", got)
	}
	seqs := ledgerSeqs(t, h, dtPO, "PO/26-27/")
	if len(seqs) != 2 || seqs[0] != 1 || seqs[1] != 2 {
		t.Errorf("ledger seqs for PO/26-27/ = %v, want [1 2]", seqs)
	}
}

// TestAssign_ApprovalOrder covers NAM-027.
func TestAssign_ApprovalOrder(t *testing.T) {
	h := newHarness(t)
	d := day(2026, 10, 24)

	p1 := h.create(dtPO, d)
	p2 := h.create(dtPO, d)
	must(t, h.submit(p1, true))
	must(t, h.submit(p2, true))
	must(t, h.approve(p2))
	must(t, h.approve(p1))
	wantDoc(t, h, p2, doctype.StateApproved, "PO/26-27/00001")
	wantDoc(t, h, p1, doctype.StateApproved, "PO/26-27/00002")
}

// TestAssign_NumberStableAcrossTransitions covers NAM-029.
func TestAssign_NumberStableAcrossTransitions(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state doctype.State
		step  func(h *harness, id int64) error
	}{
		{"close", doctype.StateClosed, (*harness).close},
		{"cancel", doctype.StateCancelled, (*harness).cancel},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.preset(dtPO, "PO/26-27/", 6)
			x := h.create(dtPO, day(2026, 10, 24))
			must(t, h.submit(x, true))
			must(t, h.approve(x))
			wantDoc(t, h, x, doctype.StateApproved, "PO/26-27/00007")

			must(t, tc.step(h, x))
			wantDoc(t, h, x, tc.state, "PO/26-27/00007")
			wantCounter(t, h, dtPO, "PO/26-27/", 7)
		})
	}
}

// TestAssign_RollbackNoGap covers NAM-031 (GR series, CD-113).
func TestAssign_RollbackNoGap(t *testing.T) {
	h := newHarness(t)
	d := day(2026, 10, 24)

	g := h.create(dtGRN, d)
	must(t, h.submit(g, true))
	errEffect := errors.New("received qty 120 exceeds pending qty 100")
	err := h.approveWithEffect(g, func(*db.Tx) error { return errEffect })
	if !errors.Is(err, errEffect) {
		t.Fatalf("approve with failing effect: err = %v, want %v", err, errEffect)
	}
	wantDoc(t, h, g, doctype.StatePending, "")
	wantCounter(t, h, dtGRN, "GR/26-27/", 0)
	if n := h.count(`SELECT COUNT(*) FROM naming_issued`); n != 0 {
		t.Errorf("naming_issued has %d rows after rollback, want 0", n)
	}

	if got := approveNew(t, h, dtGRN, d); got != "GR/26-27/00001" {
		t.Errorf("valid GRN = %q, want GR/26-27/00001", got)
	}
}

// TestCancel_KeepsNumber covers NAM-038.
func TestCancel_KeepsNumber(t *testing.T) {
	h := newHarness(t)
	d := day(2026, 10, 24)
	h.preset(dtPO, "PO/26-27/", 6)

	a := h.create(dtPO, d)
	must(t, h.submit(a, true))
	must(t, h.approve(a))
	wantDoc(t, h, a, doctype.StateApproved, "PO/26-27/00007")

	must(t, h.cancel(a))
	wantDoc(t, h, a, doctype.StateCancelled, "PO/26-27/00007")
	wantCounter(t, h, dtPO, "PO/26-27/", 7)

	if got := approveNew(t, h, dtPO, d); got != "PO/26-27/00008" {
		t.Errorf("next PO = %q, want PO/26-27/00008", got)
	}
}

// TestNumber_ClientCannotSet covers NAM-048 (DOC calls GuardNumberField on
// save and create).
func TestNumber_ClientCannotSet(t *testing.T) {
	h := newHarness(t)
	id := h.create(dtPO, day(2026, 10, 24))

	err := h.save(id, ptr("PO/26-27/09999"))
	if !errors.Is(err, ErrClientNumber) {
		t.Fatalf("save with number: err = %v, want ErrClientNumber", err)
	}
	if want := "Document numbers are set automatically and can't be entered."; err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
	wantDoc(t, h, id, doctype.StateDraft, "")
	wantCounter(t, h, dtPO, "PO/26-27/", 0)
	if n := h.count(`SELECT COUNT(*) FROM naming_issued`); n != 0 {
		t.Errorf("naming_issued has %d rows, want 0", n)
	}
}

// TestNumber_Immutable covers NAM-049.
func TestNumber_Immutable(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.preset(dtPO, "PO/26-27/", 6)

	a := h.create(dtPO, day(2026, 10, 24))
	must(t, h.submit(a, true))
	must(t, h.approve(a))
	wantDoc(t, h, a, doctype.StateApproved, "PO/26-27/00007")

	const msg = "Document numbers can't be changed."

	// Form, API or CSV import: the save path's guard.
	err := h.save(a, ptr("PO/26-27/00008"))
	if !errors.Is(err, ErrNumberImmutable) {
		t.Errorf("save with changed number: err = %v, want ErrNumberImmutable", err)
	} else if err.Error() != msg {
		t.Errorf("save message = %q, want %q", err.Error(), msg)
	}
	wantDoc(t, h, a, doctype.StateApproved, "PO/26-27/00007")

	// A second Assign for the same document is refused before the counter moves.
	err = h.db.Write(ctx, func(tx *db.Tx) error {
		_, err := h.nm.Assign(ctx, tx, dtPO, a, day(2026, 10, 24))
		return err
	})
	if !errors.Is(err, ErrNumberImmutable) {
		t.Errorf("second Assign: err = %v, want ErrNumberImmutable", err)
	} else if err.Error() != msg {
		t.Errorf("second Assign message = %q, want %q", err.Error(), msg)
	}
	wantCounter(t, h, dtPO, "PO/26-27/", 7)
	wantDoc(t, h, a, doctype.StateApproved, "PO/26-27/00007")

	// The ledger itself can't be rewritten.
	err = h.db.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx,
			`UPDATE naming_issued SET number = 'PO/26-27/00008' WHERE doctype = ? AND doc_id = ?`, dtPO, a)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "naming_issued is append-only") {
		t.Errorf("raw ledger UPDATE: err = %v, want it to contain %q", err, "naming_issued is append-only")
	}
	if n := h.count(`SELECT COUNT(*) FROM naming_issued WHERE doctype = ? AND doc_id = ? AND number = ?`,
		dtPO, a, "PO/26-27/00007"); n != 1 {
		t.Errorf("ledger rows for doc %d with PO/26-27/00007 = %d, want 1", a, n)
	}
	if n := h.count(`SELECT COUNT(*) FROM naming_issued`); n != 1 {
		t.Errorf("naming_issued has %d rows, want 1", n)
	}
}

// TestCopy_ClearsNumber covers NAM-050.
func TestCopy_ClearsNumber(t *testing.T) {
	checkCopy := func(t *testing.T, h *harness, src int64, srcNumber, wantNext string) {
		t.Helper()
		c := h.copyDoc(src)
		got := h.doc(c)
		if got.State != string(doctype.StateDraft) || got.Number.Valid || got.AmendedFrom.Valid || got.AmendmentNo != 0 {
			t.Errorf("copy = %+v, want draft with number NULL, amended_from NULL, amendment_no 0", got)
		}
		if l, want := Label(got.Number.String, c), fmt.Sprintf("Draft %d", c); l != want {
			t.Errorf("Label of copy = %q, want %q", l, want)
		}
		must(t, h.submit(c, true))
		must(t, h.approve(c))
		wantDoc(t, h, c, doctype.StateApproved, wantNext)
		if n := h.doc(src).Number; !n.Valid || n.String != srcNumber {
			t.Errorf("source number after copy = %v, want %s", n, srcNumber)
		}
	}

	t.Run("approved", func(t *testing.T) {
		h := newHarness(t)
		h.preset(dtPO, "PO/26-27/", 6)
		a := h.create(dtPO, day(2026, 10, 24))
		must(t, h.submit(a, true))
		must(t, h.approve(a))
		wantDoc(t, h, a, doctype.StateApproved, "PO/26-27/00007")
		checkCopy(t, h, a, "PO/26-27/00007", "PO/26-27/00008")
	})

	t.Run("amendment", func(t *testing.T) {
		t.Skip("needs AssignAmendment") // remove in Task 10, after Task 9 merges
		h := newHarness(t)
		h.preset(dtPO, "PO/26-27/", 6)
		a := h.create(dtPO, day(2026, 10, 24))
		must(t, h.submit(a, true))
		must(t, h.approve(a))
		must(t, h.cancel(a))
		m, err := h.amend(a)
		must(t, err)
		must(t, h.submit(m, true))
		must(t, h.approve(m))
		wantDoc(t, h, m, doctype.StateApproved, "PO/26-27/00007-1")
		checkCopy(t, h, m, "PO/26-27/00007-1", "PO/26-27/00008")
	})
}

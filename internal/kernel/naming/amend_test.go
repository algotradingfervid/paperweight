package naming

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// issuedRow is one naming_issued ledger row.
type issuedRow struct {
	doctype     string
	docID       int64
	prefix      string
	seq         int64
	root        string
	amendmentNo int
}

// issued reads the ledger row for number. Test goroutine only.
func issued(t *testing.T, h *harness, number string) issuedRow {
	t.Helper()
	ctx := context.Background()
	var r issuedRow
	if err := h.db.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx,
			`SELECT doctype, doc_id, prefix, seq, root, amendment_no FROM naming_issued WHERE number = ?`, number).
			Scan(&r.doctype, &r.docID, &r.prefix, &r.seq, &r.root, &r.amendmentNo)
	}); err != nil {
		t.Fatalf("ledger row %s: %v", number, err)
	}
	return r
}

// approveAmendment submits an amendment draft for approval and approves it.
func approveAmendment(t *testing.T, h *harness, id int64) {
	t.Helper()
	must(t, h.submit(id, true))
	must(t, h.approve(id))
}

// TestAmend_FirstSuffix covers NAM-039.
func TestAmend_FirstSuffix(t *testing.T) {
	h := newHarness(t)
	d := day(2026, 10, 24)
	h.preset(dtPO, "PO/26-27/", 6)

	a := h.create(dtPO, d)
	must(t, h.submit(a, true))
	must(t, h.approve(a))
	wantDoc(t, h, a, doctype.StateApproved, "PO/26-27/00007")
	b := h.create(dtPO, d)
	must(t, h.submit(b, true))
	must(t, h.approve(b))
	wantDoc(t, h, b, doctype.StateApproved, "PO/26-27/00008")
	must(t, h.cancel(a))

	m, err := h.amend(a)
	must(t, err)
	got := h.doc(m)
	if got.State != string(doctype.StateDraft) {
		t.Errorf("amendment state = %q, want %q", got.State, doctype.StateDraft)
	}
	if got.Number.Valid {
		t.Errorf("amendment number = %q, want NULL", got.Number.String)
	}
	if !got.AmendedFrom.Valid || got.AmendedFrom.Int64 != a {
		t.Errorf("amendment AmendedFrom = %v, want %d", got.AmendedFrom, a)
	}
	if got.AmendmentNo != 1 {
		t.Errorf("amendment AmendmentNo = %d, want 1", got.AmendmentNo)
	}

	approveAmendment(t, h, m)
	wantDoc(t, h, m, doctype.StateApproved, "PO/26-27/00007-1")
	wantCounter(t, h, dtPO, "PO/26-27/", 8)

	// Ledger row: prefix and seq copied from the root row, root and k stored.
	want := issuedRow{doctype: dtPO, docID: m, prefix: "PO/26-27/", seq: 7, root: "PO/26-27/00007", amendmentNo: 1}
	if r := issued(t, h, "PO/26-27/00007-1"); r != want {
		t.Errorf("ledger row PO/26-27/00007-1 = %+v, want %+v", r, want)
	}
}

// TestAmend_SecondSuffix covers NAM-040.
func TestAmend_SecondSuffix(t *testing.T) {
	h := newHarness(t)
	h.preset(dtPO, "PO/26-27/", 6)

	a := h.create(dtPO, day(2026, 10, 24))
	must(t, h.submit(a, true))
	must(t, h.approve(a))
	must(t, h.cancel(a))
	m1, err := h.amend(a)
	must(t, err)
	approveAmendment(t, h, m1)
	wantDoc(t, h, m1, doctype.StateApproved, "PO/26-27/00007-1")

	must(t, h.cancel(m1))
	m2, err := h.amend(m1)
	must(t, err)
	approveAmendment(t, h, m2)
	wantDoc(t, h, m2, doctype.StateApproved, "PO/26-27/00007-2")
	if got := h.doc(m2).AmendedFrom; !got.Valid || got.Int64 != m1 {
		t.Errorf("doc(m2).AmendedFrom = %v, want %d", got, m1)
	}
	wantCounter(t, h, dtPO, "PO/26-27/", 7)
	want := issuedRow{doctype: dtPO, docID: m2, prefix: "PO/26-27/", seq: 7, root: "PO/26-27/00007", amendmentNo: 2}
	if r := issued(t, h, "PO/26-27/00007-2"); r != want {
		t.Errorf("ledger row PO/26-27/00007-2 = %+v, want %+v", r, want)
	}
}

// TestAmend_HyphenatedPrefix covers NAM-041: the suffix comes from stored
// fields, not from splitting the number on hyphens.
func TestAmend_HyphenatedPrefix(t *testing.T) {
	h := newHarness(t)
	const dtHyphen = "nam.HyphenPO"
	// SetTemplate is built in Task 11; seed the series directly.
	must(t, h.nm.EnsureSeries(context.Background(), dtHyphen, "Hyphen PO", "PO-{FY}-{#####}"))
	h.preset(dtHyphen, "PO-26-27-", 6)

	a := h.create(dtHyphen, day(2026, 10, 24))
	must(t, h.submit(a, true))
	must(t, h.approve(a))
	wantDoc(t, h, a, doctype.StateApproved, "PO-26-27-00007")

	must(t, h.cancel(a))
	m1, err := h.amend(a)
	must(t, err)
	approveAmendment(t, h, m1)
	wantDoc(t, h, m1, doctype.StateApproved, "PO-26-27-00007-1")

	must(t, h.cancel(m1))
	m2, err := h.amend(m1)
	must(t, err)
	approveAmendment(t, h, m2)
	wantDoc(t, h, m2, doctype.StateApproved, "PO-26-27-00007-2")
	wantCounter(t, h, dtHyphen, "PO-26-27-", 7)
}

// TestAmend_KeepsRootFY covers NAM-042.
func TestAmend_KeepsRootFY(t *testing.T) {
	h := newHarness(t)
	h.preset(dtPO, "PO/25-26/", 142)

	a := h.create(dtPO, day(2026, 3, 20))
	must(t, h.submit(a, true))
	must(t, h.approve(a))
	wantDoc(t, h, a, doctype.StateApproved, "PO/25-26/00143")

	must(t, h.cancel(a))
	m, err := h.amend(a)
	must(t, err)
	h.setDate(m, day(2026, 4, 5))
	approveAmendment(t, h, m)
	wantDoc(t, h, m, doctype.StateApproved, "PO/25-26/00143-1")
	wantCounter(t, h, dtPO, "PO/26-27/", 0)
	wantCounter(t, h, dtPO, "PO/25-26/", 143)
}

// TestAmend_OnlyOnce covers NAM-043 (DOC-065's wording, CD-27).
func TestAmend_OnlyOnce(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.preset(dtPO, "PO/26-27/", 6)

	a := h.create(dtPO, day(2026, 10, 24))
	must(t, h.submit(a, true))
	must(t, h.approve(a))
	wantDoc(t, h, a, doctype.StateApproved, "PO/26-27/00007")
	must(t, h.cancel(a))
	m, err := h.amend(a)
	must(t, err)

	// Draft case: DOC's rule (the harness) refuses a second amendment.
	_, err = h.amend(a)
	if !errors.Is(err, ErrAlreadyAmended) {
		t.Fatalf("second amend while draft: err = %v, want ErrAlreadyAmended", err)
	}
	if want := fmt.Sprintf("PO/26-27/00007 has already been amended as Draft %d. Open that document instead.", m); err.Error() != want {
		t.Errorf("draft case message = %q, want %q", err.Error(), want)
	}

	// Approved case: NAM's own guard in AssignAmendment.
	approveAmendment(t, h, m)
	wantDoc(t, h, m, doctype.StateApproved, "PO/26-27/00007-1")

	other := h.create(dtPO, day(2026, 10, 24))
	before := h.count(`SELECT COUNT(*) FROM naming_issued`)
	var num string
	err = h.db.Write(ctx, func(tx *db.Tx) error {
		var err error
		num, err = h.nm.AssignAmendment(ctx, tx, dtPO, other, "PO/26-27/00007", 1)
		return err
	})
	if !errors.Is(err, ErrAlreadyAmended) {
		t.Fatalf("AssignAmendment(PO/26-27/00007, 1) again: number %q, err = %v, want ErrAlreadyAmended", num, err)
	}
	if want := "PO/26-27/00007 has already been amended as PO/26-27/00007-1. Open that document instead."; err.Error() != want {
		t.Errorf("approved case message = %q, want %q", err.Error(), want)
	}
	if n := h.count(`SELECT COUNT(*) FROM naming_issued`); n != before {
		t.Errorf("naming_issued has %d rows after refused amendment, want %d", n, before)
	}
	if n := h.count(`SELECT COUNT(*) FROM naming_issued WHERE doc_id = ?`, other); n != 0 {
		t.Errorf("naming_issued has %d rows for doc %d, want 0", n, other)
	}
	wantCounter(t, h, dtPO, "PO/26-27/", 7)
}

// TestAmend_DeletedDraftFreesSuffix covers NAM-044.
func TestAmend_DeletedDraftFreesSuffix(t *testing.T) {
	h := newHarness(t)
	h.preset(dtPO, "PO/26-27/", 6)

	a := h.create(dtPO, day(2026, 10, 24))
	must(t, h.submit(a, true))
	must(t, h.approve(a))
	wantDoc(t, h, a, doctype.StateApproved, "PO/26-27/00007")
	must(t, h.cancel(a))

	m, err := h.amend(a)
	must(t, err)
	must(t, h.deleteDraft(m))

	m2, err := h.amend(a)
	must(t, err)
	approveAmendment(t, h, m2)
	wantDoc(t, h, m2, doctype.StateApproved, "PO/26-27/00007-1")
	wantCounter(t, h, dtPO, "PO/26-27/", 7)
	want := issuedRow{doctype: dtPO, docID: m2, prefix: "PO/26-27/", seq: 7, root: "PO/26-27/00007", amendmentNo: 1}
	if r := issued(t, h, "PO/26-27/00007-1"); r != want {
		t.Errorf("ledger row PO/26-27/00007-1 = %+v, want %+v", r, want)
	}
}

// TestAmend_Limit covers NAM-045.
func TestAmend_Limit(t *testing.T) {
	const wantMsg = "PO/26-27/00007 has been amended 9 times, which is the limit. Create a new purchase order instead."

	t.Run("check", func(t *testing.T) {
		err := CheckAmendmentNo("PO/26-27/00007", 10, "Purchase Order")
		if !errors.Is(err, ErrAmendmentLimit) {
			t.Fatalf("CheckAmendmentNo(PO/26-27/00007, 10, Purchase Order) = %v, want ErrAmendmentLimit", err)
		}
		if err.Error() != wantMsg {
			t.Errorf("CheckAmendmentNo message = %q, want %q", err.Error(), wantMsg)
		}
		if err := CheckAmendmentNo("PO/26-27/00007", 9, "Purchase Order"); err != nil {
			t.Errorf("CheckAmendmentNo(PO/26-27/00007, 9, Purchase Order) = %v, want nil", err)
		}
	})

	t.Run("chain", func(t *testing.T) {
		h := newHarness(t)
		ctx := context.Background()
		h.preset(dtPO, "PO/26-27/", 6)

		cur := h.create(dtPO, day(2026, 10, 24))
		must(t, h.submit(cur, true))
		must(t, h.approve(cur))
		wantDoc(t, h, cur, doctype.StateApproved, "PO/26-27/00007")
		for k := 1; k <= 9; k++ {
			must(t, h.cancel(cur))
			next, err := h.amend(cur)
			if err != nil {
				t.Fatalf("amend to -%d: %v", k, err)
			}
			approveAmendment(t, h, next)
			wantDoc(t, h, next, doctype.StateApproved, fmt.Sprintf("PO/26-27/00007-%d", k))
			cur = next
		}
		must(t, h.cancel(cur))

		docsBefore := h.count(`SELECT COUNT(*) FROM nam_test_docs`)
		_, err := h.amend(cur)
		if !errors.Is(err, ErrAmendmentLimit) {
			t.Fatalf("amend(-9): err = %v, want ErrAmendmentLimit", err)
		}
		if err.Error() != wantMsg {
			t.Errorf("amend(-9) message = %q, want %q", err.Error(), wantMsg)
		}
		if n := h.count(`SELECT COUNT(*) FROM nam_test_docs`); n != docsBefore {
			t.Errorf("nam_test_docs has %d rows after refused amend, want %d", n, docsBefore)
		}

		// NAM's own guard: AssignAmendment refuses k = 10 with the same text
		// (it calls CheckAmendmentNo with the series label) and writes nothing.
		other := h.create(dtPO, day(2026, 10, 24))
		before := h.count(`SELECT COUNT(*) FROM naming_issued`)
		var num string
		err = h.db.Write(ctx, func(tx *db.Tx) error {
			var err error
			num, err = h.nm.AssignAmendment(ctx, tx, dtPO, other, "PO/26-27/00007", 10)
			return err
		})
		if !errors.Is(err, ErrAmendmentLimit) {
			t.Fatalf("AssignAmendment(PO/26-27/00007, 10): number %q, err = %v, want ErrAmendmentLimit", num, err)
		}
		if err.Error() != wantMsg {
			t.Errorf("AssignAmendment(…, 10) message = %q, want %q", err.Error(), wantMsg)
		}
		if n := h.count(`SELECT COUNT(*) FROM naming_issued`); n != before {
			t.Errorf("naming_issued has %d rows after refused amendment, want %d", n, before)
		}
		wantCounter(t, h, dtPO, "PO/26-27/", 7)
	})
}

// TestAmend_UnknownRoot covers AssignAmendment's ErrUnknownRoot paths (Task 9
// review): each is refused with msgUnknownRoot naming the root and the
// series label, and no ledger row is written.
func TestAmend_UnknownRoot(t *testing.T) {
	cases := []struct {
		name string
		root string
		k    int
		want string
	}{
		{"other_doctype", "GR/26-27/00001", 1, "GR/26-27/00001 isn't a number issued for Purchase Order, so it can't be amended."},
		{"not_in_ledger", "PO/26-27/00099", 1, "PO/26-27/00099 isn't a number issued for Purchase Order, so it can't be amended."},
		{"missing_previous", "PO/26-27/00007", 2, "PO/26-27/00007 isn't a number issued for Purchase Order, so it can't be amended."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			ctx := context.Background()
			h.preset(dtPO, "PO/26-27/", 6)

			a := h.create(dtPO, day(2026, 10, 24))
			must(t, h.submit(a, true))
			must(t, h.approve(a))
			wantDoc(t, h, a, doctype.StateApproved, "PO/26-27/00007")
			g := h.create(dtGRN, day(2026, 10, 24))
			must(t, h.submit(g, false))
			wantDoc(t, h, g, doctype.StateApproved, "GR/26-27/00001")

			other := h.create(dtPO, day(2026, 10, 24))
			before := h.count(`SELECT COUNT(*) FROM naming_issued`)
			var num string
			err := h.db.Write(ctx, func(tx *db.Tx) error {
				var err error
				num, err = h.nm.AssignAmendment(ctx, tx, dtPO, other, c.root, c.k)
				return err
			})
			if !errors.Is(err, ErrUnknownRoot) {
				t.Fatalf("AssignAmendment(%s, %d): number %q, err = %v, want ErrUnknownRoot", c.root, c.k, num, err)
			}
			if err.Error() != c.want {
				t.Errorf("message = %q, want %q", err.Error(), c.want)
			}
			if n := h.count(`SELECT COUNT(*) FROM naming_issued`); n != before {
				t.Errorf("naming_issued has %d rows after refused amendment, want %d", n, before)
			}
			if n := h.count(`SELECT COUNT(*) FROM naming_issued WHERE doc_id = ?`, other); n != 0 {
				t.Errorf("naming_issued has %d rows for doc %d, want 0", n, other)
			}
			wantCounter(t, h, dtPO, "PO/26-27/", 7)
			wantCounter(t, h, dtGRN, "GR/26-27/", 1)
		})
	}
}

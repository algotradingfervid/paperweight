package purchase_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/types"
)

// expClose closes po as Priya (Purchase User: amend is the Order's
// CloseRight, CD-247) through Engine.Do.
func expClose(t *testing.T, env *purchasetest.Env, po doc.Doc) {
	t.Helper()
	res, err := env.Engine.Do(t.Context(), env.Priya, purchase.DocType, po.ID, doc.ActionClose,
		doc.ActionInput{Version: po.Version, Reason: "Balance not needed"})
	if err != nil || res.State != doctype.StateClosed {
		t.Fatalf("close PO %d as Priya = %+v, %v; want closed", po.ID, res, err)
	}
}

// expCancel cancels po as Ravi (Purchase Manager, cancel) through Engine.Do.
func expCancel(t *testing.T, env *purchasetest.Env, po doc.Doc) {
	t.Helper()
	res, err := env.Engine.Do(t.Context(), env.Ravi, purchase.DocType, po.ID, doc.ActionCancel,
		doc.ActionInput{Version: po.Version, Reason: "Vendor cannot supply"})
	if err != nil || res.State != doctype.StateCancelled {
		t.Fatalf("cancel PO %d as Ravi = %+v, %v; want cancelled", po.ID, res, err)
	}
}

// PO-080, AD-13 ruling on P18 fix round 1 (spec §27.1; CD-259): every
// ClearExpected checks load, right, version, then state. On a PO that isn't
// approved it is 422 with the not-approved text, with the row (if any) and
// the PO's audit entries unchanged, also with no row (posted 0). Version and
// right still come before the state. The no-row clear on an approved PO stays
// a no-op (TestPO_ExpectedOnClearNoRecord).
func TestPO_ExpectedOnClearNotApproved(t *testing.T) {
	env := purchasetest.New(t)
	ctx := t.Context()
	svc := env.Service
	nov5 := expDate(2026, time.November, 5)

	// withRow approves a PO, sets its Expected on (row version 1) and then
	// moves it out of approved with leave.
	withRow := func(t *testing.T, leave func(*testing.T, *purchasetest.Env, doc.Doc)) doc.Doc {
		t.Helper()
		po := purchasetest.NewOrder(t, env)
		if err := svc.SetExpected(ctx, env.Arjun, po.ID, 0, nov5, "Vendor called"); err != nil {
			t.Fatalf("SetExpected on the approved PO: %v", err)
		}
		leave(t, env, po)
		return po
	}

	for _, c := range []struct {
		name  string
		leave func(*testing.T, *purchasetest.Env, doc.Doc)
	}{
		{"closed", expClose},
		{"cancelled", expCancel},
	} {
		t.Run("clear on a "+c.name+" PO with a record", func(t *testing.T) {
			po := withRow(t, c.leave)
			before, row := expCountsOf(t, env, po.ID), expectedRow(t, env, po.ID)
			if !row.ok || row.version != 1 {
				t.Fatalf("row before the clear = %+v, want version 1", row)
			}
			expWantRefused(t, "ClearExpected on a "+c.name+" PO, version 1",
				svc.ClearExpected(ctx, env.Arjun, po.ID, 1), http.StatusUnprocessableEntity, msgExpectedNotApproved)
			if after := expCountsOf(t, env, po.ID); after != before {
				t.Errorf("row counts and audit entries after the refused clear = %+v, want %+v", after, before)
			}
			if after := expectedRow(t, env, po.ID); after != row {
				t.Errorf("row after the refused clear = %+v, want %+v (still there)", after, row)
			}

			// CD-259 order: the right and the version come before the state.
			expWantDenied(t, "ClearExpected as Suresh on a "+c.name+" PO", svc.ClearExpected(ctx, env.Suresh, po.ID, 1))
			expWantRefused(t, "ClearExpected on a "+c.name+" PO, stale version 2",
				svc.ClearExpected(ctx, env.Arjun, po.ID, 2), http.StatusConflict, expStale(naming.Label(po.Number, po.ID)))
			if after := expectedRow(t, env, po.ID); after != row {
				t.Errorf("row after the refused right and version = %+v, want %+v", after, row)
			}
		})
	}

	// No record (posted 0): still the state refusal on every state but
	// approved. Pending isn't reachable yet: amountOrder is P2's stub (0), so
	// every PO is approved on submit.
	closed := purchasetest.NewOrder(t, env)
	expClose(t, env, closed)
	cancelled := purchasetest.NewOrder(t, env)
	expCancel(t, env, cancelled)
	for _, c := range []struct {
		name string
		po   doc.Doc
	}{
		{"draft", purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())},
		{"closed", closed},
		{"cancelled", cancelled},
	} {
		t.Run("clear with no record on a "+c.name+" PO", func(t *testing.T) {
			before := expCountsOf(t, env, c.po.ID)
			// Priya owns every builder PO and holds amend on each.
			expWantRefused(t, "ClearExpected on a "+c.name+" PO, version 0, no record",
				svc.ClearExpected(ctx, env.Priya, c.po.ID, 0), http.StatusUnprocessableEntity, msgExpectedNotApproved)
			if after := expCountsOf(t, env, c.po.ID); after != before {
				t.Errorf("row counts and audit entries after the refused clear = %+v, want %+v", after, before)
			}
			if r := expectedRow(t, env, c.po.ID); r.ok {
				t.Errorf("row after the refused clear = %+v, want none", r)
			}
		})
	}
}

// PO-080 (spec §27.1): a set stores set_by and set_at = the clock's now, and
// ExpectedInfo returns them; the set's and the clear's audit entries are at
// the clock's now too, each a different instant here.
func TestPO_ExpectedOnClock(t *testing.T) {
	env := purchasetest.New(t)
	ctx := t.Context()
	svc := env.Service
	po := purchasetest.NewOrder(t, env)
	audits0 := len(expAudits(t, env, po.ID))

	setAt := time.Date(2026, time.November, 2, 9, 15, 30, 0, time.UTC)
	env.SetNow(t, setAt)
	if err := svc.SetExpected(ctx, env.Ravi, po.ID, 0, expDate(2026, time.November, 5), ""); err != nil {
		t.Fatalf("SetExpected: %v", err)
	}
	var stored string
	if err := env.DB.SQL().QueryRowContext(ctx, `SELECT set_at FROM purchase_order_expected WHERE po_id = ?`, po.ID).Scan(&stored); err != nil {
		t.Fatalf("read set_at: %v", err)
	}
	if ts, err := types.ParseTimestamp(stored); err != nil || !ts.Time().Equal(setAt) {
		t.Errorf("stored set_at = %q (%v), want %v", stored, err, setAt)
	}
	if got, ok := expectedInfo(t, env, po.ID); !ok || got.SetBy != env.Ravi.ID || !got.SetAt.Equal(setAt) {
		t.Errorf("ExpectedInfo = %+v, %v; want SetBy Ravi (%d), SetAt %v", got, ok, env.Ravi.ID, setAt)
	}

	clearAt := time.Date(2026, time.November, 3, 16, 45, 10, 0, time.UTC)
	env.SetNow(t, clearAt)
	if err := svc.ClearExpected(ctx, env.Arjun, po.ID, 1); err != nil {
		t.Fatalf("ClearExpected: %v", err)
	}
	got := expAudits(t, env, po.ID)[audits0:]
	if len(got) != 2 {
		t.Fatalf("new audit entries = %d (%+v), want 2", len(got), got)
	}
	for i, want := range []time.Time{setAt, clearAt} {
		if !got[i].At.Equal(want) {
			t.Errorf("audit entry %d (%q) At = %v, want %v", i, expHistoryWords(got[i]), got[i].At, want)
		}
	}
}

// ParseExpectedOn (spec §27.1): blank, also only spaces, is the zero date
// (SetExpected's "Enter the expected date."), never the invalid-value error.
func TestPO_ParseExpectedOnBlank(t *testing.T) {
	for _, s := range []string{"", "   ", "\t "} {
		d, err := purchase.ParseExpectedOn(s)
		if err != nil || d != (types.Date{}) {
			t.Errorf("ParseExpectedOn(%q) = %v, %v; want the zero date, nil", s, d, err)
		}
	}
	if d, err := purchase.ParseExpectedOn(" 05-11-2026 "); err != nil || d != expDate(2026, time.November, 5) {
		t.Errorf("ParseExpectedOn(\" 05-11-2026 \") = %v, %v; want 05-11-2026", d, err)
	}
}

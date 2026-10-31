package purchase_test

import (
	"errors"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

// MOD-PO P1 "Service shape" (CD-284): two purchasetest envs, each wired with
// its own clock and Comments, read their own through Get(tx), also after one
// env's clock moves.
func TestPurchase_WirePerEnv(t *testing.T) {
	a, b := purchasetest.New(t), purchasetest.New(t)
	if a.Clock == b.Clock {
		t.Fatal("two envs share one clock")
	}
	later := time.Date(2026, 11, 2, 5, 0, 0, 0, time.UTC)
	b.SetNow(t, later)

	for _, c := range []struct {
		what string
		env  *purchasetest.Env
		now  time.Time
	}{{"env a", a, testkit.Clock().Now()}, {"env b", b, later}} {
		var got purchase.HookDeps
		err := c.env.DB.Read(t.Context(), func(tx *db.Tx) error {
			var err error
			got, err = purchase.DepsFor(tx)
			return err
		})
		if err != nil {
			t.Fatalf("%s: Get(tx) = %v, want its wiring", c.what, err)
		}
		if got.Clock != c.env.Clock || got.Comments != c.env.Comments {
			t.Errorf("%s: wired Clock %p Comments %p, want the env's %p %p", c.what, got.Clock, got.Comments, c.env.Clock, c.env.Comments)
		}
		if now := got.Clock.Now(); !now.Equal(c.now) {
			t.Errorf("%s: wired clock reads %v, want %v", c.what, now, c.now)
		}
	}

	// Wire replaces the value for one database only.
	other := testkit.Clock()
	purchase.Wire(a.DB, purchase.HookDeps{Clock: other, Comments: a.Comments})
	if err := a.DB.Read(t.Context(), func(tx *db.Tx) error {
		got, err := purchase.DepsFor(tx)
		if err != nil || got.Clock != other {
			t.Errorf("env a after a second Wire: Clock %p, %v; want %p", got.Clock, err, other)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := b.DB.Read(t.Context(), func(tx *db.Tx) error {
		got, err := purchase.DepsFor(tx)
		if err != nil || got.Clock != b.Clock {
			t.Errorf("env b after env a's second Wire: Clock %p, %v; want its own %p", got.Clock, err, b.Clock)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// MOD-PO P1 Do 2 (CD-284): a hook run on a database nothing was wired for is
// a plain internal error, never a user message, even when the row exists. The
// same row read through the wired env gives the Date hook's order_date and
// request_date.
func TestPurchase_HookUnwired(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	pr := purchasetest.NewRequest(t, env, purchasetest.StopAtDraft())

	// A second handle on the same file is another database to the wiring.
	unwired, err := db.Open(testkit.DBPath(t))
	if err != nil {
		t.Fatalf("open a second handle: %v", err)
	}
	t.Cleanup(func() { _ = unwired.Close() })

	cases := []struct {
		dt string
		id int64
	}{{purchase.DocType, po.ID}, {purchase.RequestDocType, pr.ID}}
	for _, c := range cases {
		hooks := lookup(t, c.dt).Hooks
		if err := unwired.Read(t.Context(), func(tx *db.Tx) error {
			if _, err := purchase.DepsFor(tx); err == nil {
				t.Errorf("Get(tx) on the unwired handle: error = nil, want an error")
			}
			d, err := hooks.Date(t.Context(), tx, c.id)
			var he doctype.HTTPError
			switch {
			case err == nil:
				t.Errorf("%s Date on an unwired database = %v, nil; want an internal error", c.dt, d)
			case errors.As(err, &he):
				t.Errorf("%s Date on an unwired database = %v (status %d), want a plain internal error", c.dt, err, he.Status())
			case errors.Is(err, auth.ErrNotFound), errors.Is(err, doctype.ErrNotFound):
				t.Errorf("%s Date on an unwired database = %v, want a plain internal error, not not-found", c.dt, err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}

// MOD-PO P1 Do 2 (PO spec §3.1, §20.2; naming/assign.go:21): the Date hooks
// are real in P1 and return order_date and request_date, so numbering takes
// the FY from the document's date, not from the clock. A request's date is
// set again at submit (CD-330 (1)), so the back-dated request stays a draft:
// its hook still returns the stored date.
func TestPurchase_DateHooks(t *testing.T) {
	env := purchasetest.New(t)
	back := types.Date{Year: 2026, Month: 3, Day: 31}
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	pr := purchasetest.NewRequest(t, env, purchasetest.StopAtDraft())
	backPO := purchasetest.NewOrder(t, env, purchasetest.StopAtApproved(), purchasetest.OrderHeader(map[string]string{"order_date": "31-03-2026"}))
	backPR := purchasetest.NewRequest(t, env, purchasetest.StopAtDraft(), purchasetest.RequestHeader(map[string]string{"request_date": "31-03-2026"}))
	today := types.Date{Year: 2026, Month: 10, Day: 24}

	for _, c := range []struct {
		dt   string
		id   int64
		want types.Date
	}{
		{purchase.DocType, po.ID, today},
		{purchase.RequestDocType, pr.ID, today},
		{purchase.DocType, backPO.ID, back},
		{purchase.RequestDocType, backPR.ID, back},
	} {
		hooks := lookup(t, c.dt).Hooks
		if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
			got, err := hooks.Date(t.Context(), tx, c.id)
			if err != nil || got != c.want {
				t.Errorf("%s %d Date = %v, %v; want %v, nil", c.dt, c.id, got, err, c.want)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if backPO.Number != "PO/25-26/00001" {
		t.Errorf("back-dated number = %q; want PO/25-26/00001 (FY of the document date)", backPO.Number)
	}
}

// MOD-PO P1 Do 1 (PO spec §22; CD-276, M0-14b): both DocTypes are registered
// for comments, so (*modkit.Comments).AddTx saves on each, inside the
// caller's tx.
func TestPurchase_CommentsRegistered(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	pr := purchasetest.NewRequest(t, env, purchasetest.StopAtDraft())
	for _, c := range []struct {
		dt string
		id int64
	}{{purchase.DocType, po.ID}, {purchase.RequestDocType, pr.ID}} {
		const body = "Is this still needed?"
		if err := env.DB.Write(t.Context(), func(tx *db.Tx) error {
			got, err := env.Comments.AddTx(t.Context(), tx, env.Ravi.ID, c.dt, c.id, body)
			if err != nil {
				return err
			}
			if got.ID <= 0 || got.DocType != c.dt || got.RecordID != c.id || got.Body != body || got.CreatedBy != env.Ravi.ID {
				t.Errorf("AddTx on %s %d = %+v, want a saved comment by Ravi", c.dt, c.id, got)
			}
			return nil
		}); err != nil {
			t.Errorf("AddTx on %s %d: %v, want ok (RegisterComments)", c.dt, c.id, err)
		}
		if n := countRows(t, env.DB, "module_comment", `doctype = ? AND record_id = ?`, c.dt, c.id); n != 1 {
			t.Errorf("module_comment rows on %s %d = %d, want 1", c.dt, c.id, n)
		}
	}
}

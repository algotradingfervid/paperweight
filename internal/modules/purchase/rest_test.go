package purchase_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// restCall is the frozen MOD-GRN call (CD-276). Assigning
// purchase.SetRestNotComing to it makes any signature change a compile error.
type restCall func(ctx context.Context, tx *db.Tx, poID, grnID int64, by types.UserID, on bool) error

var setRestNotComing restCall = purchase.SetRestNotComing

// msgRestNotComing is PO spec §27.2's comment, copied exactly.
const msgRestNotComing = "Vendor says the rest won't come. Close this order if nothing more is due."

// restWrite drives SetRestNotComing inside env.DB.Write exactly as GRN's
// ApplyEffects and ReverseEffects will: a tx, no principal.
func restWrite(t *testing.T, env *purchasetest.Env, poID, grnID int64, by types.UserID, on bool) error {
	t.Helper()
	return env.DB.Write(t.Context(), func(tx *db.Tx) error {
		return setRestNotComing(t.Context(), tx, poID, grnID, by, on)
	})
}

// restRows counts purchase_order_rest rows for po (all rows when po is 0).
func restRows(t *testing.T, env *purchasetest.Env, po int64) int {
	t.Helper()
	if po == 0 {
		return countRows(t, env.DB, "purchase_order_rest", "1 = 1")
	}
	return countRows(t, env.DB, "purchase_order_rest", "po_id = ?", po)
}

// commentRows counts module_comment rows on PO po (all rows when po is 0).
func commentRows(t *testing.T, env *purchasetest.Env, po int64) int {
	t.Helper()
	if po == 0 {
		return countRows(t, env.DB, "module_comment", "1 = 1")
	}
	return countRows(t, env.DB, "module_comment", "doctype = ? AND record_id = ?", purchase.DocType, po)
}

// restRow reads the (po, grn) row's created_by and created_at.
func restRow(t *testing.T, env *purchasetest.Env, po, grn int64) (types.UserID, types.Timestamp) {
	t.Helper()
	var by types.UserID
	var at types.Timestamp
	if err := env.DB.SQL().QueryRowContext(t.Context(),
		`SELECT created_by, created_at FROM purchase_order_rest WHERE po_id = ? AND grn_id = ?`, po, grn).Scan(&by, &at); err != nil {
		t.Fatalf("read purchase_order_rest (%d, %d): %v", po, grn, err)
	}
	return by, at
}

// loadPO is the stored PO, to prove the calls leave it as it was.
func loadPO(t *testing.T, env *purchasetest.Env, id int64) doc.Doc {
	t.Helper()
	var d doc.Doc
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		d, err = doc.LoadDoc(t.Context(), tx, purchase.DocType, id)
		return err
	}); err != nil {
		t.Fatalf("load PO %d: %v", id, err)
	}
	return d
}

// wantPlainInternal fails unless err is a plain internal error: not nil, no
// user message (doctype.HTTPError) and not a not-found.
func wantPlainInternal(t *testing.T, what string, err error) {
	t.Helper()
	var he doctype.HTTPError
	switch {
	case err == nil:
		t.Errorf("%s: error = nil, want a plain internal error", what)
	case errors.As(err, &he):
		t.Errorf("%s: error = %v (status %d), want a plain internal error", what, err, he.Status())
	case errors.Is(err, auth.ErrNotFound), errors.Is(err, doctype.ErrNotFound):
		t.Errorf("%s: error = %v, want a plain internal error, not not-found", what, err)
	}
}

// terminal moves an approved PO to closed or cancelled through the engine as
// the administrator.
func terminal(t *testing.T, env *purchasetest.Env, po doc.Doc, a doctype.Action) doc.Doc {
	t.Helper()
	if _, err := env.Engine.Do(t.Context(), env.Admin, purchase.DocType, po.ID, a,
		doc.ActionInput{Version: po.Version, Reason: "Vendor stopped supplying"}); err != nil {
		t.Fatalf("%s PO %d: %v", a, po.ID, err)
	}
	return loadPO(t, env, po.ID)
}

// PO-082 (PO spec §27.2; CD-272 Gap 8, CD-276 frozen call, CD-284 wiring;
// research S43): SetRestNotComing, run inside db.Write as GRN's hooks run
// it, records "the rest won't come" on an approved PO with one row and one
// comment by `by` (created_at from the Wire clock), is idempotent, and on=false
// deletes the row and keeps the comment. A PO that isn't approved gets
// nothing on on=true, on=false deletes in any state, and an unknown PO id is
// a plain internal error that writes nothing. The PO's state, version and
// lines never change (R7).
func TestPO_RestNotComing(t *testing.T) {
	env := purchasetest.New(t)
	by := env.Suresh.ID
	const grn, grn2 int64 = 501, 502

	t.Run("on, idempotent, off", func(t *testing.T) {
		po := purchasetest.NewOrder(t, env, purchasetest.StopAtApproved())
		if po.State != doctype.StateApproved || po.Number != "PO/26-27/00001" {
			t.Fatalf("built PO %d = %s %q, want approved PO/26-27/00001", po.ID, po.State, po.Number)
		}
		before := loadPO(t, env, po.ID)

		// The row's created_at is the wired clock's, not time.Now.
		at := time.Date(2026, 10, 25, 6, 15, 0, 0, time.UTC)
		env.SetNow(t, at)
		if err := restWrite(t, env, po.ID, grn, by, true); err != nil {
			t.Fatalf("SetRestNotComing(on) on an approved PO: %v, want nil", err)
		}
		if n := restRows(t, env, po.ID); n != 1 {
			t.Errorf("rest rows after on = %d, want 1", n)
		}
		gotBy, gotAt := restRow(t, env, po.ID, grn)
		if gotBy != by || !gotAt.Time().Equal(types.TimestampOf(at).Time()) {
			t.Errorf("rest row = created_by %d, created_at %v; want %d, %v (the env clock)", gotBy, gotAt, by, types.TimestampOf(at))
		}
		comments, err := env.Comments.ListComments(t.Context(), env.Admin, purchase.DocType, po.ID)
		if err != nil {
			t.Fatalf("ListComments: %v", err)
		}
		if len(comments) != 1 {
			t.Fatalf("comments after on = %d (%+v), want 1", len(comments), comments)
		}
		c := comments[0]
		if c.Body != msgRestNotComing || c.CreatedBy != by || !c.CreatedAt.Equal(types.TimestampOf(at).Time()) {
			t.Errorf("comment = %q by %d at %v; want %q by %d at %v", c.Body, c.CreatedBy, c.CreatedAt, msgRestNotComing, by, at)
		}
		if after := loadPO(t, env, po.ID); !reflect.DeepEqual(after, before) {
			t.Errorf("PO after on = %+v, want unchanged %+v (state, version, lines)", after, before)
		}

		// Again for the same pair: no row, no comment.
		env.SetNow(t, at.Add(time.Hour))
		if err := restWrite(t, env, po.ID, grn, by, true); err != nil {
			t.Fatalf("SetRestNotComing(on) again: %v, want nil", err)
		}
		if n, m := restRows(t, env, po.ID), commentRows(t, env, po.ID); n != 1 || m != 1 {
			t.Errorf("after a second on for the same pair: %d rows, %d comments; want 1, 1", n, m)
		}
		if _, againAt := restRow(t, env, po.ID, grn); !againAt.Time().Equal(types.TimestampOf(at).Time()) {
			t.Errorf("rest row created_at after a second on = %v, want the first %v kept", againAt, types.TimestampOf(at))
		}

		// A second GRN on the same PO is its own pair.
		if err := restWrite(t, env, po.ID, grn2, by, true); err != nil {
			t.Fatalf("SetRestNotComing(on) for a second GRN: %v, want nil", err)
		}
		if n, m := restRows(t, env, po.ID), commentRows(t, env, po.ID); n != 2 || m != 2 {
			t.Errorf("after on for a second GRN: %d rows, %d comments; want 2, 2", n, m)
		}

		// Off deletes that pair's row only; the comments stay.
		if err := restWrite(t, env, po.ID, grn, by, false); err != nil {
			t.Fatalf("SetRestNotComing(off): %v, want nil", err)
		}
		if n := countRows(t, env.DB, "purchase_order_rest", "po_id = ? AND grn_id = ?", po.ID, grn); n != 0 {
			t.Errorf("row (%d, %d) after off = %d, want 0", po.ID, grn, n)
		}
		if n := countRows(t, env.DB, "purchase_order_rest", "po_id = ? AND grn_id = ?", po.ID, grn2); n != 1 {
			t.Errorf("row (%d, %d) after off for the other GRN = %d, want 1", po.ID, grn2, n)
		}
		if m := commentRows(t, env, po.ID); m != 2 {
			t.Errorf("comments after off = %d, want 2 (comments are append-only)", m)
		}

		// Off for a pair with no row is a no-op.
		if err := restWrite(t, env, po.ID, grn, by, false); err != nil {
			t.Errorf("SetRestNotComing(off) with no row: %v, want nil", err)
		}
		if n, m := restRows(t, env, po.ID), commentRows(t, env, po.ID); n != 1 || m != 2 {
			t.Errorf("after off with no row: %d rows, %d comments; want 1, 2", n, m)
		}
		if err := restWrite(t, env, po.ID, grn2, by, false); err != nil {
			t.Fatalf("SetRestNotComing(off) for the second GRN: %v", err)
		}
		if n := restRows(t, env, po.ID); n != 0 {
			t.Errorf("rest rows after both off = %d, want 0", n)
		}
		if after := loadPO(t, env, po.ID); !reflect.DeepEqual(after, before) {
			t.Errorf("PO after on and off = %+v, want unchanged %+v (state, version, lines)", after, before)
		}
	})

	// On a PO that isn't approved, on=true writes nothing and returns nil:
	// GRN refuses such receipts itself.
	notApproved := []struct {
		what  string
		build func(t *testing.T) doc.Doc
		state doctype.State
	}{
		{"draft", func(t *testing.T) doc.Doc { return purchasetest.NewOrder(t, env, purchasetest.StopAtDraft()) }, doctype.StateDraft},
		{"pending", func(t *testing.T) doc.Doc {
			// The default PO (₹94,164.00) waits on the Purchase Manager once
			// P2's amountOrder is in; before that it approves on submit.
			draft := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
			var amt types.Money
			if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
				var err error
				amt, err = lookup(t, purchase.DocType).Hooks.Amount(t.Context(), tx, draft.ID)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if amt == 0 {
				t.Skip("pending needs P2's amountOrder (the amount stub is 0, so a PO approves on submit); this case runs once P2 is on wp/mod-po")
			}
			return purchasetest.NewOrder(t, env, purchasetest.StopAtPending())
		}, doctype.StatePending},
		{"closed", func(t *testing.T) doc.Doc {
			return terminal(t, env, purchasetest.NewOrder(t, env, purchasetest.StopAtApproved()), doctype.ActionClose)
		}, doctype.StateClosed},
		{"cancelled", func(t *testing.T) doc.Doc {
			return terminal(t, env, purchasetest.NewOrder(t, env, purchasetest.StopAtApproved()), doctype.ActionCancel)
		}, doctype.StateCancelled},
	}
	for _, c := range notApproved {
		t.Run("on, "+c.what, func(t *testing.T) {
			po := c.build(t)
			if po.State != c.state {
				t.Fatalf("built PO %d is %s, want %s", po.ID, po.State, c.state)
			}
			before := loadPO(t, env, po.ID)
			rows, comments := restRows(t, env, 0), commentRows(t, env, 0)
			if err := restWrite(t, env, po.ID, grn, by, true); err != nil {
				t.Errorf("SetRestNotComing(on) on a %s PO: %v, want nil (GRN refuses such receipts)", c.what, err)
			}
			if n, m := restRows(t, env, 0), commentRows(t, env, 0); n != rows || m != comments {
				t.Errorf("on a %s PO: rows %d -> %d, comments %d -> %d; want no row and no comment", c.what, rows, n, comments, m)
			}
			if after := loadPO(t, env, po.ID); !reflect.DeepEqual(after, before) {
				t.Errorf("%s PO after on = %+v, want unchanged %+v", c.what, after, before)
			}
			// Off with no row is nil in any state.
			if err := restWrite(t, env, po.ID, grn, by, false); err != nil {
				t.Errorf("SetRestNotComing(off) on a %s PO with no row: %v, want nil", c.what, err)
			}
		})
	}

	// Off deletes in any state: the row set while approved goes after the PO
	// is closed or cancelled (a GRN cancelled later), and the comment stays.
	for _, a := range []doctype.Action{doctype.ActionClose, doctype.ActionCancel} {
		t.Run("off after "+string(a), func(t *testing.T) {
			po := purchasetest.NewOrder(t, env, purchasetest.StopAtApproved())
			if err := restWrite(t, env, po.ID, grn, by, true); err != nil {
				t.Fatalf("SetRestNotComing(on): %v", err)
			}
			if n := restRows(t, env, po.ID); n != 1 {
				t.Fatalf("rest rows after on = %d, want 1", n)
			}
			done := terminal(t, env, loadPO(t, env, po.ID), a)
			if err := restWrite(t, env, po.ID, grn, by, false); err != nil {
				t.Fatalf("SetRestNotComing(off) on a %s PO: %v, want nil", done.State, err)
			}
			if n, m := restRows(t, env, po.ID), commentRows(t, env, po.ID); n != 0 || m != 1 {
				t.Errorf("off on a %s PO: %d rows, %d comments; want 0, 1", done.State, n, m)
			}
			if after := loadPO(t, env, po.ID); !reflect.DeepEqual(after, done) {
				t.Errorf("%s PO after off = %+v, want unchanged %+v", done.State, after, done)
			}
		})
	}

	t.Run("unknown PO id", func(t *testing.T) {
		po := purchasetest.NewOrder(t, env, purchasetest.StopAtApproved())
		const unknown int64 = 999_999
		for _, on := range []bool{true, false} {
			rows, comments := restRows(t, env, 0), commentRows(t, env, 0)

			// The error rolls back the whole tx, including a valid call
			// made before it in the same tx.
			err := env.DB.Write(t.Context(), func(tx *db.Tx) error {
				if err := setRestNotComing(t.Context(), tx, po.ID, grn, by, true); err != nil {
					t.Errorf("SetRestNotComing(on) on PO %d before the unknown id: %v", po.ID, err)
				}
				return setRestNotComing(t.Context(), tx, unknown, grn, by, on)
			})
			wantPlainInternal(t, "SetRestNotComing on an unknown PO id", err)
			if n, m := restRows(t, env, 0), commentRows(t, env, 0); n != rows || m != comments {
				t.Errorf("on=%v unknown id, Write returned the error: rows %d -> %d, comments %d -> %d; want unchanged", on, rows, n, comments, m)
			}

			// The call itself writes nothing, even when the caller commits.
			if err := env.DB.Write(t.Context(), func(tx *db.Tx) error {
				wantPlainInternal(t, "SetRestNotComing on an unknown PO id", setRestNotComing(t.Context(), tx, unknown, grn, by, on))
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if n, m := restRows(t, env, 0), commentRows(t, env, 0); n != rows || m != comments {
				t.Errorf("on=%v unknown id, committed: rows %d -> %d, comments %d -> %d; want unchanged", on, rows, n, comments, m)
			}
			if m := countRows(t, env.DB, "module_comment", "record_id = ?", unknown); m != 0 {
				t.Errorf("comments on the unknown id = %d, want 0", m)
			}
		}
	})
}

// PO-082 (CD-284; MOD-PO P1 "Service shape"): two envs, two databases, each
// with its own clock. SetRestNotComing reads its clock and Comments through
// its own database's wiring, and writes only into the tx's database.
func TestPO_RestNotComingPerEnv(t *testing.T) {
	a, b := purchasetest.New(t), purchasetest.New(t)
	atA := time.Date(2026, 10, 25, 4, 0, 0, 0, time.UTC)
	atB := time.Date(2026, 10, 27, 9, 30, 0, 0, time.UTC)
	a.SetNow(t, atA)
	b.SetNow(t, atB)

	for _, c := range []struct {
		what string
		env  *purchasetest.Env
		at   time.Time
	}{{"env a", a, atA}, {"env b", b, atB}} {
		po := purchasetest.NewOrder(t, c.env, purchasetest.StopAtApproved())
		if err := restWrite(t, c.env, po.ID, 501, c.env.Suresh.ID, true); err != nil {
			t.Fatalf("%s: SetRestNotComing(on): %v", c.what, err)
		}
		if n, m := restRows(t, c.env, 0), commentRows(t, c.env, 0); n != 1 || m != 1 {
			t.Errorf("%s: %d rows, %d comments; want 1, 1 (each env writes its own database)", c.what, n, m)
		}
		_, gotAt := restRow(t, c.env, po.ID, 501)
		if !gotAt.Time().Equal(types.TimestampOf(c.at).Time()) {
			t.Errorf("%s: rest row created_at = %v, want its own clock %v", c.what, gotAt, types.TimestampOf(c.at))
		}
		comments, err := c.env.Comments.ListComments(t.Context(), c.env.Admin, purchase.DocType, po.ID)
		if err != nil {
			t.Fatalf("%s: ListComments: %v", c.what, err)
		}
		if len(comments) != 1 || comments[0].Body != msgRestNotComing || !comments[0].CreatedAt.Equal(types.TimestampOf(c.at).Time()) {
			t.Errorf("%s: comments = %+v, want one %q at its own clock %v", c.what, comments, msgRestNotComing, c.at)
		}
	}
}

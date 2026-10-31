package purchase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// qtyOf parses a quantity for a table literal; a bad literal is a test bug.
func qtyOf(s string) types.Qty {
	q, err := types.ParseQty(s)
	if err != nil {
		panic("qtyOf(" + s + "): " + err.Error())
	}
	return q
}

// PO-029 (PO spec §3.2, §18; CD-276 frozen signature): pending is
// max(0, qty − already_received − received_qty), never stored, and an
// over-receipt (or an already received above the quantity) gives 0.
func TestPO_PendingFormula(t *testing.T) {
	cases := []struct {
		what                   string
		qty, already, received string
		want                   string
	}{
		{"nothing received", "10", "0", "0", "10"},
		{"part received", "10", "0", "4", "6"},
		{"already received counts", "10", "3", "0", "7"},
		{"both count", "10", "3", "4", "3"},
		{"exactly full from both", "10", "6", "4", "0"},
		{"exactly full from receipts", "10", "0", "10", "0"},
		{"over-receipt gives 0", "10", "0", "12", "0"},
		{"already above qty gives 0", "10", "12", "0", "0"},
		{"both together above qty give 0", "10", "7", "7", "0"},
		{"fractional quantities", "1.5", "0.25", "0.5", "0.75"},
		{"fractional over-receipt", "1.5", "0", "1.501", "0"},
	}
	for _, c := range cases {
		got := purchase.Pending(qtyOf(c.qty), qtyOf(c.already), qtyOf(c.received))
		if got != qtyOf(c.want) {
			t.Errorf("%s: Pending(%s, %s, %s) = %s, want %s", c.what, c.qty, c.already, c.received, got.Plain(), c.want)
		}
	}
}

// poFacts is what RefreshReceiptStatus may and may not change on a PO.
type poFacts struct {
	status, state, updatedAt string
	version                  int
	updatedBy                int64
	audits, orders, lines    int
}

func readPOFacts(t *testing.T, env *purchasetest.Env, poID int64) poFacts {
	t.Helper()
	var f poFacts
	if err := env.DB.SQL().QueryRowContext(t.Context(),
		`SELECT receipt_status, state, version, updated_at, updated_by FROM purchase_order WHERE id = ?`, poID).
		Scan(&f.status, &f.state, &f.version, &f.updatedAt, &f.updatedBy); err != nil {
		t.Fatalf("read purchase_order %d: %v", poID, err)
	}
	f.audits = countRows(t, env.DB, "audit_log", "1 = 1")
	f.orders = countRows(t, env.DB, "purchase_order", "1 = 1")
	f.lines = countRows(t, env.DB, "purchase_order_line", "1 = 1")
	return f
}

// refresh runs purchase.RefreshReceiptStatus inside one db.Write, as GRN's
// hooks do.
func refresh(ctx context.Context, env *purchasetest.Env, poID int64, accepted map[int64]types.Qty) error {
	return env.DB.Write(ctx, func(tx *db.Tx) error {
		return purchase.RefreshReceiptStatus(ctx, tx, poID, accepted)
	})
}

// checkUntouched asserts the refresh wrote only receipt_status: no version
// bump, no updated_at/updated_by change and no audit entry (CD-280 D3), the
// PO's state is kept (a fully received PO stays approved, spec §6.3), and no
// row was added or removed.
func checkUntouched(t *testing.T, step string, before, after poFacts) {
	t.Helper()
	if after.state != before.state {
		t.Errorf("%s: state = %q, want %q kept (no automatic close, spec §6.3)", step, after.state, before.state)
	}
	if after.version != before.version || after.updatedAt != before.updatedAt || after.updatedBy != before.updatedBy {
		t.Errorf("%s: version %d, updated_at %q, updated_by %d; want %d, %q, %d unchanged (plain UPDATE, CD-280 D3)",
			step, after.version, after.updatedAt, after.updatedBy, before.version, before.updatedAt, before.updatedBy)
	}
	if after.audits != before.audits {
		t.Errorf("%s: audit_log has %d rows, want %d (no audit entry, CD-280 D3)", step, after.audits, before.audits)
	}
	if after.orders != before.orders || after.lines != before.lines {
		t.Errorf("%s: %d orders, %d lines; want %d, %d", step, after.orders, after.lines, before.orders, before.lines)
	}
}

// PO-030 (PO spec §6.3, §18; CD-276, CD-280 D3; research pattern "quantity
// walk tables"): RefreshReceiptStatus gives all three values from the
// accepted map the caller computed (GRN's hooks run before the roll-up, so
// the received_qty column is not what it reads), every PO line is a key and a
// missing one counts 0, and the write is a plain UPDATE of receipt_status.
// A fully received PO stays approved. The first-save part of PO-030 is P2's
// (PO-032).
func TestPO_ReceiptStatus(t *testing.T) {
	ctx := t.Context()

	t.Run("walk", func(t *testing.T) {
		env := purchasetest.New(t)
		po := purchasetest.NewOrder(t, env, purchasetest.StopAtApproved(),
			purchasetest.OrderLines(map[string]string{"qty": "10"}, map[string]string{"qty": "4"}))
		if len(po.Lines) != 2 {
			t.Fatalf("order has %d lines, want 2", len(po.Lines))
		}
		a, b := po.Lines[0], po.Lines[1]
		if a.LineNo > b.LineNo {
			a, b = b, a
		}
		// The received_qty column says "all received": the status must
		// follow the map instead (the roll-up hasn't run yet, spec §20.3).
		purchasetest.SetReceived(t, env, a.ID, qtyOf("10"))
		purchasetest.SetReceived(t, env, b.ID, qtyOf("4"))

		steps := []struct {
			step               string
			accepted           map[int64]types.Qty
			pendingA, pendingB string
			want               string
		}{
			{"receive 0", map[int64]types.Qty{a.ID: 0, b.ID: 0}, "10", "4", "not_received"},
			{"receive part of line 1", map[int64]types.Qty{a.ID: qtyOf("3"), b.ID: 0}, "7", "4", "part_received"},
			{"receive everything", map[int64]types.Qty{a.ID: qtyOf("10"), b.ID: qtyOf("4")}, "0", "0", "fully_received"},
			{"a receipt cancelled: back to part", map[int64]types.Qty{a.ID: qtyOf("10"), b.ID: qtyOf("1")}, "0", "3", "part_received"},
			{"over-receipt is full", map[int64]types.Qty{a.ID: qtyOf("12"), b.ID: qtyOf("4")}, "0", "0", "fully_received"},
			{"a missing line counts 0", map[int64]types.Qty{a.ID: qtyOf("10")}, "0", "4", "part_received"},
			{"every line missing is nothing received", map[int64]types.Qty{}, "10", "4", "not_received"},
			{"nil map is nothing received", nil, "10", "4", "not_received"},
		}
		for _, s := range steps {
			before := readPOFacts(t, env, po.ID)
			if err := refresh(ctx, env, po.ID, s.accepted); err != nil {
				t.Fatalf("%s: RefreshReceiptStatus = %v, want nil", s.step, err)
			}
			after := readPOFacts(t, env, po.ID)
			if after.status != s.want {
				t.Errorf("%s: receipt_status = %q, want %q", s.step, after.status, s.want)
			}
			if after.state != string(doctype.StateApproved) {
				t.Errorf("%s: state = %q, want approved (a fully received PO stays approved)", s.step, after.state)
			}
			checkUntouched(t, s.step, before, after)
			// The walk's pending figures, from the frozen formula.
			for _, l := range []struct {
				line      doc.Line
				qty, want string
			}{{a, "10", s.pendingA}, {b, "4", s.pendingB}} {
				got := purchase.Pending(qtyOf(l.qty), 0, s.accepted[l.line.ID])
				if got != qtyOf(l.want) {
					t.Errorf("%s: line %d pending = %s, want %s", s.step, l.line.LineNo, got.Plain(), l.want)
				}
			}
		}
	})

	t.Run("already received counts", func(t *testing.T) {
		env := purchasetest.New(t)
		cases := []struct {
			what     string
			already  [2]string // lines 1 and 2, qty 10 and 4
			accepted [2]string
			want     string
		}{
			{"already on one line, nothing accepted", [2]string{"3", "0"}, [2]string{"0", "0"}, "part_received"},
			{"already covers line 1, line 2 accepted", [2]string{"10", "0"}, [2]string{"0", "4"}, "fully_received"},
			{"already and accepted together cover each line", [2]string{"6", "1"}, [2]string{"4", "3"}, "fully_received"},
			{"already above qty is full", [2]string{"12", "4"}, [2]string{"0", "0"}, "fully_received"},
			{"already short on line 2", [2]string{"10", "1"}, [2]string{"0", "2"}, "part_received"},
		}
		for _, c := range cases {
			po := purchasetest.NewOrder(t, env, purchasetest.StopAtApproved(),
				purchasetest.OrderHeader(map[string]string{"earlier_po_no": "P-17"}),
				purchasetest.OrderLines(
					map[string]string{"qty": "10", "already_received": c.already[0]},
					map[string]string{"qty": "4", "already_received": c.already[1]}))
			if len(po.Lines) != 2 {
				t.Fatalf("%s: order has %d lines, want 2", c.what, len(po.Lines))
			}
			a, b := po.Lines[0], po.Lines[1]
			if a.LineNo > b.LineNo {
				a, b = b, a
			}
			before := readPOFacts(t, env, po.ID)
			if err := refresh(ctx, env, po.ID, map[int64]types.Qty{a.ID: qtyOf(c.accepted[0]), b.ID: qtyOf(c.accepted[1])}); err != nil {
				t.Fatalf("%s: RefreshReceiptStatus = %v, want nil", c.what, err)
			}
			after := readPOFacts(t, env, po.ID)
			if after.status != c.want {
				t.Errorf("%s: receipt_status = %q, want %q", c.what, after.status, c.want)
			}
			checkUntouched(t, c.what, before, after)
		}
	})

	t.Run("a line not on the PO is a plain internal error and writes nothing", func(t *testing.T) {
		env := purchasetest.New(t)
		po := purchasetest.NewOrder(t, env, purchasetest.StopAtApproved(),
			purchasetest.OrderLines(map[string]string{"qty": "10"}))
		other := purchasetest.NewOrder(t, env, purchasetest.StopAtApproved())
		line := po.Lines[0].ID

		// A real refresh first, so the refusal below has a status to keep.
		if err := refresh(ctx, env, po.ID, map[int64]types.Qty{line: qtyOf("3")}); err != nil {
			t.Fatalf("RefreshReceiptStatus(part) = %v, want nil", err)
		}
		if got := readPOFacts(t, env, po.ID).status; got != "part_received" {
			t.Fatalf("after a part receipt receipt_status = %q, want part_received", got)
		}

		for _, c := range []struct {
			what     string
			accepted map[int64]types.Qty
		}{
			{"another PO's line", map[int64]types.Qty{line: qtyOf("10"), other.Lines[0].ID: qtyOf("1")}},
			{"a line id that doesn't exist", map[int64]types.Qty{line: qtyOf("10"), other.Lines[0].ID + 1000: qtyOf("1")}},
		} {
			before := readPOFacts(t, env, po.ID)
			otherBefore := readPOFacts(t, env, other.ID)
			err := refresh(ctx, env, po.ID, c.accepted)
			var he doctype.HTTPError
			switch {
			case err == nil:
				t.Errorf("%s: RefreshReceiptStatus = nil, want a plain internal error", c.what)
			case errors.As(err, &he):
				t.Errorf("%s: RefreshReceiptStatus = %v (status %d), want a plain internal error", c.what, err, he.Status())
			case errors.Is(err, auth.ErrNotFound), errors.Is(err, doctype.ErrNotFound):
				t.Errorf("%s: RefreshReceiptStatus = %v, want a plain internal error, not not-found", c.what, err)
			}
			after := readPOFacts(t, env, po.ID)
			if after.status != before.status {
				t.Errorf("%s: receipt_status = %q, want %q unchanged", c.what, after.status, before.status)
			}
			checkUntouched(t, c.what, before, after)
			if got := readPOFacts(t, env, other.ID); got != otherBefore {
				t.Errorf("%s: the other PO changed: %+v, want %+v", c.what, got, otherBefore)
			}
		}
	})

	t.Run("a closed PO is refreshed and stays closed", func(t *testing.T) {
		env := purchasetest.New(t)
		po := purchasetest.NewOrder(t, env, purchasetest.StopAtApproved(),
			purchasetest.OrderLines(map[string]string{"qty": "10"}))
		line := po.Lines[0].ID
		if err := refresh(ctx, env, po.ID, map[int64]types.Qty{line: qtyOf("4")}); err != nil {
			t.Fatalf("RefreshReceiptStatus(part) = %v, want nil", err)
		}
		if _, err := env.Engine.Do(ctx, env.Priya, purchase.DocType, po.ID, doc.ActionClose,
			doc.ActionInput{Version: po.Version, Reason: "Balance not needed"}); err != nil {
			t.Fatalf("close the PO: %v", err)
		}
		// GRNs cancelled against the closed PO (D-DOC-09): one of two, then
		// the other.
		for _, s := range []struct {
			step     string
			accepted map[int64]types.Qty
			want     string
		}{
			{"one receipt cancelled", map[int64]types.Qty{line: qtyOf("1")}, "part_received"},
			{"the last receipt cancelled", map[int64]types.Qty{line: 0}, "not_received"},
		} {
			before := readPOFacts(t, env, po.ID)
			if before.state != string(doctype.StateClosed) {
				t.Fatalf("PO state = %q, want closed", before.state)
			}
			if err := refresh(ctx, env, po.ID, s.accepted); err != nil {
				t.Fatalf("%s: RefreshReceiptStatus on a closed PO = %v, want nil", s.step, err)
			}
			after := readPOFacts(t, env, po.ID)
			if after.status != s.want {
				t.Errorf("%s: receipt_status = %q, want %q", s.step, after.status, s.want)
			}
			checkUntouched(t, s.step, before, after)
		}
	})
}

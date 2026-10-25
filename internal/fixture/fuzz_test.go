package fixture_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Fuzz model (plan "Task 14: Lifecycle table and fuzz invariants").
const (
	fzMaxOrders   = 2  // orders created by createOrder (amendments not counted)
	fzMaxReceipts = 6  // receipts in the run, amendments included
	fzMaxSteps    = 64 // steps read from one input; the rest is ignored
	fzReason      = "fuzz reason"
)

// fzOps are the ops picked by b0 % 12, in the plan's order.
var fzOps = []string{
	"createOrder", "createReceipt", "save", "submit", "withdraw", "approve",
	"reject", "revise", "close", "cancel", "amend", "delete",
}

// fzActions maps the document ops to their engine action.
var fzActions = map[string]doc.Action{
	"save": doc.ActionSave, "submit": doc.ActionSubmit, "withdraw": doc.ActionWithdraw,
	"approve": doc.ActionApprove, "reject": doc.ActionReject, "revise": doc.ActionRevise,
	"close": doc.ActionClose, "cancel": doc.ActionCancel, "amend": doc.ActionAmend,
	"delete": doc.ActionDelete,
}

// fzHooks are the hooks a fault rotates through, one per injection.
var fzHooks = []string{"BeforeSubmit", "ApplyEffects", "ReverseEffects"}

// fzOrderLines are the three lines of every created order.
var fzOrderLines = []fixturetest.OL{
	{Item: "MS-ANGLE-50", Qty: "10", Rate: "62.5000"},
	{Item: "BOLT-M12", Qty: "10", Rate: "12.0000"},
	{Item: "WIRE-18G", Qty: "10", Rate: "96.0000"},
}

// fzAllowedAPR are the only Approvals.Calls entries the protocol allows (CD-23).
var fzAllowedAPR = []string{
	"APR.Evaluate", "APR.Open", "APR.CheckApprover",
	"APR.Record:approve", "APR.Record:reject", "APR.Record:revise", "APR.Record:withdraw",
}

var errFzInjected = errors.New("fuzz: injected hook failure")

// fzDoc is one document the run created.
type fzDoc struct {
	dt      string
	id      int64
	deleted bool
	snap    *doc.Doc // the document as it left draft, nil while in draft
}

// fzModel is the fuzz run's expected state.
type fzModel struct {
	t          *testing.T
	h          *fixturetest.H
	docs       []*fzDoc
	orderLines []int64 // line ids of the created (not amended) orders, in order
	orders     int
	receipts   int
	faults     int
	audit      int      // expected audit_log row count
	rows       []string // every audit row seen so far, in id order
}

// DOC-079
func FuzzDoc_LifecycleInvariants(f *testing.F) {
	// Plan seed: create order, create receipt, submit both, cancel both, amend.
	f.Add([]byte{0, 0, 0, 1, 0, 5, 3, 0, 0, 3, 1, 0, 9, 1, 0, 9, 0, 0, 10, 0, 0})
	// The same path without injected faults (b2 % 8 != 0), the order approved
	// before the receipt is made; then close the receipt, and cancel the order
	// (blocked by the closed receipt) and the closed receipt (refused).
	f.Add([]byte{0, 0, 1, 3, 0, 1, 1, 0, 5, 3, 1, 1, 8, 1, 1, 9, 0, 1, 9, 1, 1, 9, 0, 1, 10, 0, 1})
	// Reject and revise: an order at ₹50,000.00 needs approval.
	f.Add([]byte{0, 0, 50, 3, 0, 50, 6, 0, 50, 7, 0, 50, 2, 0, 50, 3, 0, 50, 5, 0, 50, 8, 0, 50})
	// Withdraw, save, delete, then act on the deleted draft.
	f.Add([]byte{0, 0, 51, 3, 0, 51, 4, 0, 51, 2, 0, 51, 11, 0, 51, 4, 0, 51, 3, 0, 51})
	// Faults: a receipt's submit fails in BeforeSubmit then ApplyEffects, then
	// succeeds; its cancel fails in ReverseEffects, then succeeds; the order is
	// cancelled and amended, and the amendment approved.
	f.Add([]byte{0, 0, 1, 3, 0, 1, 1, 0, 1, 3, 1, 8, 3, 1, 16, 3, 1, 1, 9, 1, 24, 9, 1, 1, 9, 0, 1, 10, 0, 1, 3, 2, 1})
	// Two orders, receipts on both, an amended receipt and its approval.
	f.Add([]byte{0, 0, 1, 0, 0, 2, 3, 0, 1, 3, 1, 1, 1, 0, 3, 1, 0, 4, 3, 2, 1, 3, 3, 1, 9, 2, 1, 10, 2, 1, 3, 4, 1})

	f.Fuzz(func(t *testing.T, data []byte) {
		m := &fzModel{t: t, h: fixturetest.New(t)}
		m.check(-1)
		for step := 0; step < fzMaxSteps && 3*step+2 < len(data); step++ {
			b := data[3*step : 3*step+3]
			m.step(step, b[0], b[1], b[2])
			m.check(step)
			if t.Failed() {
				t.FailNow()
			}
		}
	})
}

// step decodes and runs one step.
func (m *fzModel) step(step int, b0, b1, b2 byte) {
	t, h := m.t, m.h
	op := fzOps[int(b0)%len(fzOps)]
	probe := &fixture.Probe{}
	if b2%8 == 0 {
		probe.Fail = map[string]error{fzHooks[m.faults%len(fzHooks)]: errFzInjected}
		m.faults++
	}
	ctx := fixture.WithProbe(t.Context(), probe)
	before := m.receivedByOrder()

	var (
		res doc.Result
		err error
	)
	switch op {
	case "createOrder":
		if m.orders >= fzMaxOrders {
			return
		}
		in := doc.ActionInput{Values: map[string]string{
			"supplier_id": fzID(h.M.Party["SHREE"]), "project_id": fzID(h.M.Project["PRJ-METRO"]),
			"order_date": "2026-10-24", "amount": strconv.Itoa(int(b2)*1000) + ".00",
		}}
		for _, l := range fzOrderLines {
			in.Lines = append(in.Lines, map[string]string{"item_id": fzID(h.M.Item[l.Item]), "qty": l.Qty, "rate": l.Rate})
		}
		res, err = h.Engine.Do(ctx, h.Priya, fixture.Order, 0, doc.ActionSave, in)
		if err == nil {
			m.orders++
			m.add(fixture.Order, res.ID)
			for _, l := range h.Load(t, fixture.Order, res.ID).Lines {
				m.orderLines = append(m.orderLines, l.ID)
			}
		}
	case "createReceipt":
		if m.receipts >= fzMaxReceipts || len(m.orderLines) == 0 {
			return
		}
		// Order line b2 % 6, wrapped onto the lines that exist so far.
		line := m.orderLines[int(b2)%6%len(m.orderLines)]
		in := doc.ActionInput{
			Values: map[string]string{"receipt_date": "2026-10-24"},
			Lines:  []map[string]string{{"order_line_id": fzID(line), "qty": strconv.Itoa(int(b2)%20 + 1)}},
		}
		res, err = h.Engine.Do(ctx, h.Suresh, fixture.Receipt, 0, doc.ActionSave, in)
		if err == nil {
			m.receipts++
			m.add(fixture.Receipt, res.ID)
		}
	default:
		if len(m.docs) == 0 {
			return
		}
		action := fzActions[op]
		target := m.docs[int(b1)%len(m.docs)]
		if action == doc.ActionAmend && target.dt == fixture.Receipt && m.receipts >= fzMaxReceipts {
			return
		}
		in := doc.ActionInput{Version: 1, Reason: fzReason}
		if !target.deleted {
			cur := h.Load(t, target.dt, target.id)
			in.Version = cur.Version
			if action == doc.ActionSave {
				in = h.In(cur)
				in.Values["remarks"] = strconv.Itoa(step)
			}
		}
		p := h.Priya
		if target.dt == fixture.Receipt {
			p = h.Suresh
		}
		if action == doc.ActionApprove || action == doc.ActionReject {
			p = h.Ravi
		}
		res, err = h.Engine.Do(ctx, p, target.dt, target.id, action, in)
		if err == nil && target.deleted {
			t.Errorf("step %d: %s on deleted %s %d succeeded: %+v", step, op, target.dt, target.id, res)
		}
		if err == nil {
			switch action {
			case doc.ActionDelete:
				target.deleted = true
			case doc.ActionAmend:
				m.add(target.dt, res.ID)
				if target.dt == fixture.Receipt {
					m.receipts++
				}
			}
		}
	}

	t.Logf("step %d: %s -> %+v, err %v (fault %v)", step, op, res, err, probe.Fail)
	if err != nil {
		return // a failed action adds no audit row and changes nothing
	}
	m.audit++
	if op == "amend" {
		m.audit++
	}
	after := m.receivedByOrder()
	for orderID, q := range after {
		was, ok := before[orderID]
		if !ok { // created in this step: nothing received yet
			was = make([]int64, len(q))
		}
		if !slices.Equal(q, was) {
			m.audit++ // one effect row per order whose received_qty changed
		}
	}
	m.updateSnapshots()
}

// add records a new document. SQLite reuses the id of a deleted last row
// (INTEGER PRIMARY KEY without AUTOINCREMENT), so a deleted document with the
// same DocType and id is dropped: the id is now the new document's.
func (m *fzModel) add(dt string, id int64) {
	m.docs = slices.DeleteFunc(m.docs, func(d *fzDoc) bool { return d.deleted && d.dt == dt && d.id == id })
	m.docs = append(m.docs, &fzDoc{dt: dt, id: id})
}

// updateSnapshots records each document as it leaves draft and forgets the
// snapshot when it returns to draft.
func (m *fzModel) updateSnapshots() {
	for _, d := range m.docs {
		if d.deleted {
			continue
		}
		cur := m.h.Load(m.t, d.dt, d.id)
		switch {
		case cur.State == doc.StateDraft:
			d.snap = nil
		case d.snap == nil:
			c := cur
			d.snap = &c
		}
	}
}

// receivedByOrder returns every order's line received_qty values in line id order.
func (m *fzModel) receivedByOrder() map[int64][]int64 {
	out := map[int64][]int64{}
	m.query("received_qty", `SELECT parent_id, received_qty FROM fixture_order_line ORDER BY parent_id, id`,
		func(scan func(...any) error) error {
			var order, q int64
			if err := scan(&order, &q); err != nil {
				return err
			}
			out[order] = append(out[order], q)
			return nil
		})
	return out
}

// check asserts the invariants after step (-1: before the first step).
func (m *fzModel) check(step int) {
	t, h := m.t, m.h
	at := fmt.Sprintf("after step %d", step)

	// (a) received_qty is the sum over approved and closed receipt lines.
	m.query("(a)", `
		SELECT ol.id, ol.received_qty, COALESCE((SELECT SUM(rl.qty) FROM fixture_receipt_line rl
		  JOIN fixture_receipt r ON r.id = rl.parent_id
		  WHERE rl.order_line_id = ol.id AND r.state IN ('approved', 'closed')), 0)
		FROM fixture_order_line ol ORDER BY ol.id`,
		func(scan func(...any) error) error {
			var id, got, want int64
			if err := scan(&id, &got, &want); err != nil {
				return err
			}
			if got != want {
				t.Errorf("%s: (a) order line %d received_qty = %d, want %d (sum of approved and closed receipt lines)", at, id, got, want)
			}
			return nil
		})

	// (b) no pending, approved or closed receipt on a cancelled order.
	m.query("(b)", `
		SELECT r.id, r.state, o.id FROM fixture_receipt r
		JOIN fixture_receipt_line rl ON rl.parent_id = r.id
		JOIN fixture_order_line ol ON ol.id = rl.order_line_id
		JOIN fixture_order o ON o.id = ol.parent_id
		WHERE r.state IN ('pending_approval', 'approved', 'closed') AND o.state = 'cancelled'`,
		func(scan func(...any) error) error {
			var r, o int64
			var s string
			if err := scan(&r, &s, &o); err != nil {
				return err
			}
			t.Errorf("%s: (b) receipt %d is %s on cancelled order %d", at, r, s, o)
			return nil
		})

	// (c) and (f): each live document against its snapshot and its number.
	for _, d := range m.docs {
		if d.deleted {
			continue
		}
		cur := h.Load(t, d.dt, d.id)
		if d.snap != nil && cur.State != doc.StateDraft {
			if got, want := fzContent(cur), fzContent(*d.snap); !reflect.DeepEqual(got, want) {
				t.Errorf("%s: (c) %s %d (%s) changed since it left draft:\ngot  %#v\nwant %#v", at, d.dt, d.id, cur.State, got, want)
			}
		}
		numbered := cur.State == doc.StateApproved || cur.State == doc.StateClosed || cur.State == doc.StateCancelled
		if (cur.Number != "") != numbered {
			t.Errorf("%s: (f) %s %d is %s with number %q", at, d.dt, d.id, cur.State, cur.Number)
		}
	}
	m.checkGapless(at)

	// (d) audit row count.
	if got := h.AuditCount(t); got != m.audit {
		t.Errorf("%s: (d) audit.Count = %d, want %d", at, got, m.audit)
	}

	// The DOC <-> APR call protocol (CD-23).
	for _, c := range h.Approvals.Calls {
		if !slices.Contains(fzAllowedAPR, c) {
			t.Errorf("%s: Approvals.Calls has %q, not one of %v", at, c, fzAllowedAPR)
		}
	}

	// (e) every audit row seen before is byte-identical.
	var rows []string
	m.query("(e)", `
		SELECT id, doc_type, doc_id, action, from_state, to_state, reason, changes, user_id, at,
		       cause_doc_type, cause_doc_id, cause_number, cause_action
		FROM audit_log ORDER BY id`,
		func(scan func(...any) error) error {
			vals := make([]any, 14)
			ptrs := make([]any, len(vals))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := scan(ptrs...); err != nil {
				return err
			}
			rows = append(rows, fmt.Sprintf("%#v", vals))
			return nil
		})
	if len(rows) < len(m.rows) {
		t.Errorf("%s: (e) audit_log has %d rows, %d were seen before", at, len(rows), len(m.rows))
	} else if !slices.Equal(rows[:len(m.rows)], m.rows) {
		for i := range m.rows {
			if rows[i] != m.rows[i] {
				t.Errorf("%s: (e) audit row %d changed:\ngot  %s\nwant %s", at, i, rows[i], m.rows[i])
			}
		}
	}
	m.rows = rows
}

// checkGapless asserts (f)'s second half: per series, the non-amendment
// numbers are exactly 1..N.
func (m *fzModel) checkGapless(at string) {
	for _, table := range []string{"fixture_order", "fixture_receipt"} {
		counters := map[string][]int{}
		m.query("(f) "+table, `SELECT number FROM `+table+` WHERE number IS NOT NULL AND amendment_no = 0`,
			func(scan func(...any) error) error {
				var n string
				if err := scan(&n); err != nil {
					return err
				}
				i := strings.LastIndex(n, "/")
				k, err := strconv.Atoi(n[i+1:])
				if i < 0 || err != nil {
					m.t.Errorf("%s: (f) %s number %q is not SERIES/#####", at, table, n)
					return nil
				}
				counters[n[:i+1]] = append(counters[n[:i+1]], k)
				return nil
			})
		for series, ks := range counters {
			slices.Sort(ks)
			for i, k := range ks {
				if k != i+1 {
					m.t.Errorf("%s: (f) series %s numbers %v, want 1..%d", at, series, ks, len(ks))
					break
				}
			}
		}
	}
}

// fzContent is a document's header and lines without received_qty.
func fzContent(d doc.Doc) []any {
	out := []any{map[string]any(d.Header)}
	for _, l := range d.Lines {
		v := map[string]any{}
		for k, x := range l.Values {
			if k != "received_qty" {
				v[k] = x
			}
		}
		out = append(out, l.ID, l.LineNo, v)
	}
	return out
}

// query runs a read query and calls row for each row.
func (m *fzModel) query(what, q string, row func(scan func(...any) error) error) {
	m.t.Helper()
	err := m.h.DB.Read(context.Background(), func(tx *db.Tx) error {
		rows, err := tx.SQL().QueryContext(context.Background(), q)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			if err := row(rows.Scan); err != nil {
				return err
			}
		}
		return rows.Err()
	})
	if err != nil {
		m.t.Fatalf("query %s: %v", what, err)
	}
}

func fzID(v int64) string { return strconv.FormatInt(v, 10) }

package goodsreceipt_test

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/types"
)

// MOD-GRN G3 test helpers (GRN spec §5; PO spec §3.2, §6.3). Every PO and
// GRN is made and changed through Engine.Do with grntest's and
// purchasetest's builders; received_qty is never written by a test (CD-125:
// no purchasetest.SetReceived). The PO side is only read, with plain SELECTs.

// g3Q parses a quantity literal of a table; a bad literal is a test bug.
func g3Q(s string) types.Qty {
	q, err := types.ParseQty(s)
	if err != nil {
		panic("g3Q(" + s + "): " + err.Error())
	}
	return q
}

// g3POLine is one stored PO line's quantities.
type g3POLine struct {
	ID                     int64
	LineNo                 int
	Qty, Already, Received types.Qty
}

// g3POView is a stored PO's state, receipt_status and lines (line order).
type g3POView struct {
	State, Status string
	Version       int64
	Lines         []g3POLine
}

// g3ReadPO reads PO id as stored.
func g3ReadPO(t *testing.T, env *grntest.Env, id int64) g3POView {
	t.Helper()
	h := g2Query(t, env.DB, `SELECT state, receipt_status, version FROM purchase_order WHERE id = ?`, id)
	if len(h) != 1 {
		t.Fatalf("PO %d not found", id)
	}
	v := g3POView{State: h[0][0], Status: h[0][1], Version: g2Int(t, h[0][2])}
	for _, r := range g2Query(t, env.DB, `SELECT id, line_no, qty, COALESCE(already_received, 0), COALESCE(received_qty, 0)
		FROM purchase_order_line WHERE parent_id = ? ORDER BY line_no`, id) {
		v.Lines = append(v.Lines, g3POLine{
			ID: g2Int(t, r[0]), LineNo: int(g2Int(t, r[1])),
			Qty: types.Qty(g2Int(t, r[2])), Already: types.Qty(g2Int(t, r[3])), Received: types.Qty(g2Int(t, r[4])),
		})
	}
	return v
}

// g3StatusOf is the receipt_status PO spec §6.3 gives for lines, from
// purchase.Pending: not_received when no line has anything received
// (already_received counts), fully_received when every line's pending is 0,
// part_received otherwise.
func g3StatusOf(lines []g3POLine) string {
	anything, full := false, true
	for _, l := range lines {
		if l.Already+l.Received > 0 {
			anything = true
		}
		if purchase.Pending(l.Qty, l.Already, l.Received) > 0 {
			full = false
		}
	}
	switch {
	case !anything:
		return "not_received"
	case full:
		return "fully_received"
	}
	return "part_received"
}

// g3Want is what one walk row expects of the PO: per line, received_qty and
// pending (quantity literals, kg), receipt_status and the PO's state.
type g3Want struct {
	received, pending []string
	status            string
	state             doctype.State
}

// g3CheckPO asserts PO id against w with exact values (types.Qty units,
// stored status strings). A blank w.state is approved.
func g3CheckPO(t *testing.T, what string, env *grntest.Env, id int64, w g3Want) {
	t.Helper()
	po := g3ReadPO(t, env, id)
	state := w.state
	if state == "" {
		state = doctype.StateApproved
	}
	if po.State != string(state) {
		t.Errorf("%s: PO state = %s, want %s", what, po.State, state)
	}
	if po.Status != w.status {
		t.Errorf("%s: PO receipt_status = %q, want %q", what, po.Status, w.status)
	}
	if len(po.Lines) != len(w.received) {
		t.Fatalf("%s: PO has %d lines, want %d", what, len(po.Lines), len(w.received))
	}
	for i, l := range po.Lines {
		if want := g3Q(w.received[i]); l.Received != want {
			t.Errorf("%s: PO line %d received_qty = %d, want %d (%s)", what, l.LineNo, l.Received, want, w.received[i])
		}
		if got, want := purchase.Pending(l.Qty, l.Already, l.Received), g3Q(w.pending[i]); got != want {
			t.Errorf("%s: PO line %d pending = %d, want %d (%s)", what, l.LineNo, got, want, w.pending[i])
		}
	}
}

// g3Already asserts each line of PO id still holds already_received want
// (quantity literals; PO-031, CD-276).
func g3Already(t *testing.T, what string, env *grntest.Env, id int64, want ...string) {
	t.Helper()
	for i, l := range g3ReadPO(t, env, id).Lines {
		if l.Already != g3Q(want[i]) {
			t.Errorf("%s: PO line %d already_received = %d, want %d (%s)", what, l.LineNo, l.Already, g3Q(want[i]), want[i])
		}
	}
}

// g3Snapshot is every stored fact a refused action must leave as it was:
// each PO's state, version and receipt_status, each PO line's quantities,
// each GRN's state and version, and the row counts of grntest.RowCounts.
func g3Snapshot(t *testing.T, env *grntest.Env) string {
	t.Helper()
	return fmt.Sprint(
		g2Query(t, env.DB, `SELECT id, state, version, receipt_status FROM purchase_order ORDER BY id`),
		g2Query(t, env.DB, `SELECT id, qty, already_received, received_qty FROM purchase_order_line ORDER BY id`),
		g2Query(t, env.DB, `SELECT id, state, version, accepted_value FROM goods_receipt ORDER BY id`),
		g2Query(t, env.DB, `SELECT id, accepted_qty FROM goods_receipt_line ORDER BY id`),
		env.RowCounts(t),
	)
}

// g3Itoa is an id as text, for failure messages.
func g3Itoa(id int64) string { return strconv.FormatInt(id, 10) }

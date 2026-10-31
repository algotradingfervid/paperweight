package purchase_test

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/types"
)

// PO-057 (PO spec §20.2, §20.3, §20.5, §26; CD-180 property test): random
// sequences of PO save, submit, approve, reject, withdraw, revise, delete,
// cancel, close (after a random receipt) and amend over three PO slots
// (Priya, Arjun, Priya), plus the request's close, on one request with two
// rows (40 Bag, 100 kg). Every PO needs Ravi's approval (a rule from ₹0).
// After every step:
//   - each row's stored ordered_qty is the sum of lineOrdered over its PO
//     lines (§20.2, computed here from the stored states and quantities),
//     and order_status matches it (§20.3);
//   - pending to order is max(0, qty − ordered − held), 0 on a closed
//     request, so never below 0;
//   - ordered plus held is above the row's quantity only after a save that
//     over-ordered (more than the row's quantity minus the other POs');
//   - a save, submit or approve is refused with "is already on" exactly when
//     the other POs' ordered plus held reach a row it links (§20.5), and
//     otherwise goes through while the request is submitted;
//   - on a closed request it is refused, except that an amendment whose
//     every linked row is one its amended-from PO held keeps them and
//     follows the quantity rule above (CD-279 (10), G0c; AD-13 ruling).
func FuzzRequest_OrderInvariants(f *testing.F) {
	// A save's arg picks bags p6FuzzBags[arg%6] and kg p6FuzzKg[arg/6%4]
	// (4 = 40 Bag, 3 = 25 Bag, 5 = 45 Bag, 16 = 40 Bag and 100 kg); an op of
	// 11 + a acts on slot 1 (Arjun), 22 + a on slot 2.
	for _, seed := range [][]byte{
		{0, 4, 1, 0, 2, 0},                                            // save 40 Bag, submit, approve
		{0, 4, 1, 0, 2, 0, 7, 0, 9, 0, 1, 0, 2, 0},                    // approve, cancel, amend, submit, approve
		{0, 4, 1, 0, 2, 0, 8, 2},                                      // approve, short close after 25 received
		{0, 4, 11, 4, 11, 1},                                          // a second buyer's 40 and 10 Bag refused
		{0, 3, 11, 4, 1, 0, 12, 0, 13, 0},                             // 25 + 40 save; the first submit is refused
		{0, 4, 1, 0, 3, 0, 11, 4, 5, 0, 6, 0, 11, 4},                  // rejected holds; revise, delete frees
		{0, 4, 1, 0, 4, 0, 6, 0, 11, 4},                               // withdrawn holds; delete frees
		{0, 5, 1, 0, 2, 0, 10, 0, 22, 1, 8, 3},                        // over-order 45, request closed, save refused, close
		{0, 4, 1, 0, 2, 0, 8, 1, 22, 3, 23, 0, 24, 0},                 // close after 10 received; 25 of the freed 30 re-ordered
		{0, 16, 1, 0, 2, 0, 11, 8, 7, 0, 11, 8},                       // both rows ordered; another buyer refused until the cancel
		{11, 16, 12, 0, 13, 0, 18, 2, 0, 3, 1, 0, 2, 0},               // Arjun's PO closed at 25 received; Priya over-orders the rest
		{0, 1, 1, 0, 2, 0, 7, 0, 9, 0, 1, 0, 10, 0, 2, 0},             // 10 Bag approved, cancelled, amended, submitted; request closed; amendment approved (G0c)
		{0, 1, 1, 0, 2, 0, 7, 0, 10, 0, 9, 0, 0, 7, 0, 1, 1, 0, 2, 0}, // request closed after the cancel; the amendment's save adding row 2 refused, keeping only row 1 approved
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, ops []byte) {
		if len(ops) > 48 {
			ops = ops[:48]
		}
		w := p6NewWorld(t)
		for i := 0; i+1 < len(ops); i += 2 {
			w.step(t, ops[i], ops[i+1])
			w.check(t, i/2)
			if t.Failed() {
				return
			}
		}
	})
}

// p6World is one fuzz run: the env, the request's two rows and three PO
// slots.
type p6World struct {
	env        *purchasetest.Env
	req        doc.Doc
	rows       [2]int64
	want       [2]types.Qty // the rows' requested quantities
	slots      [3]int64     // PO id per slot, 0 = none
	owners     [3]auth.Principal
	closed     bool            // the request is closed
	amendedOf  map[int64]int64 // amendment PO id -> the PO it amends
	overOrders bool            // some save ordered more than the row's quantity minus the other POs'
}

func p6NewWorld(t *testing.T) *p6World {
	t.Helper()
	env := purchasetest.New(t, purchasetest.WithNoRules())
	p6RuleFromZero(t, env)
	req := p6Request(t, env, env.Suresh, p5Header(t, env, "METRO-SITE", "PRJ-METRO"), []map[string]string{
		p5Line(t, env, "CEMENT-OPC", "BAG", "40"), p5Line(t, env, "MS-ANGLE-50", "KGS", "100")})
	return &p6World{
		env: env, req: req,
		rows:      [2]int64{req.Lines[0].ID, req.Lines[1].ID},
		want:      [2]types.Qty{p6Q(t, "40"), p6Q(t, "100")},
		owners:    [3]auth.Principal{env.Priya, env.Arjun, env.Priya},
		amendedOf: map[int64]int64{},
	}
}

// p6Contrib is one stored PO line on a request row.
type p6Contrib struct {
	po, row                int64
	state                  doctype.State
	qty, already, received types.Qty
}

// p6LineOrderedModel is §20.2's table, written out here as the oracle.
func p6LineOrderedModel(state doctype.State, qty, already, received types.Qty) (ordered, held types.Qty) {
	switch state {
	case doctype.StateDraft, doctype.StatePending, doctype.StateRejected:
		return 0, qty
	case doctype.StateApproved:
		return qty, 0
	case doctype.StateClosed:
		return min(qty, already+received), 0
	}
	return 0, 0
}

// lines reads every PO line on the request's rows.
func (w *p6World) lines(t *testing.T) []p6Contrib {
	t.Helper()
	rs, err := w.env.DB.SQL().QueryContext(t.Context(), `SELECT h.id, l.request_line_id, h.state, COALESCE(l.qty, 0), COALESCE(l.already_received, 0), l.received_qty
		FROM purchase_order_line l JOIN purchase_order h ON h.id = l.parent_id WHERE l.request_line_id IN (?, ?) ORDER BY h.id, l.line_no`, w.rows[0], w.rows[1])
	if err != nil {
		t.Fatalf("read PO lines: %v", err)
	}
	defer func() { _ = rs.Close() }()
	var out []p6Contrib
	for rs.Next() {
		var c p6Contrib
		var state string
		if err := rs.Scan(&c.po, &c.row, &state, &c.qty, &c.already, &c.received); err != nil {
			t.Fatal(err)
		}
		c.state = doctype.State(state)
		out = append(out, c)
	}
	if err := rs.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// others is what POs other than po order and hold of row.
func (w *p6World) others(t *testing.T, po int64, row int64) types.Qty {
	t.Helper()
	var sum types.Qty
	for _, c := range w.lines(t) {
		if c.po != po && c.row == row {
			o, h := p6LineOrderedModel(c.state, c.qty, c.already, c.received)
			sum += o + h
		}
	}
	return sum
}

// state is PO id's stored state ("" when it no longer exists).
func (w *p6World) state(t *testing.T, id int64) doctype.State {
	t.Helper()
	var s sql.NullString
	err := w.env.DB.SQL().QueryRowContext(t.Context(), `SELECT state FROM purchase_order WHERE id = ?`, id).Scan(&s)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("state of PO %d: %v", id, err)
	}
	return doctype.State(s.String)
}

// linked is what PO id's lines put on each request row (by row index).
func (w *p6World) linked(t *testing.T, id int64) map[int]types.Qty {
	t.Helper()
	m := map[int]types.Qty{}
	for _, c := range w.lines(t) {
		if c.po == id {
			for i, r := range w.rows {
				if r == c.row {
					m[i] += c.qty
				}
			}
		}
	}
	return m
}

// expectRule asserts the §20.5 quantity rule on a save, submit or approve of
// PO id with what its lines put on each row (taken before the action).
func (w *p6World) expectRule(t *testing.T, what string, id int64, on map[int]types.Qty, others map[int]types.Qty, err error) {
	t.Helper()
	if id == 0 {
		what += " (new)"
	}
	blocked := false
	for i := range on {
		if others[i] >= w.want[i] {
			blocked = true
		}
	}
	switch {
	case w.closed && !w.keepsHeld(t, id, on):
		if err == nil {
			t.Errorf("%s of PO %d with the request closed: saved, want DOC's closed refusal", what, id)
		}
	case blocked:
		if err == nil || !strings.Contains(err.Error()+p6FieldText(err), " is already on ") {
			t.Errorf("%s of PO %d (others %v, rows %v): err %v %+v, want the \"is already on\" refusal", what, id, others, w.want, err, p5Fields(err))
		}
	case err != nil:
		t.Errorf("%s of PO %d (others %v below rows %v): %v %+v, want it through", what, id, others, w.want, err, p5Fields(err))
	}
}

// keepsHeld reports whether PO id is an amendment and every row in on is
// one its amended-from PO linked: G0c lets it keep them on a closed request
// (CD-279 (10)). Any other row, or a PO that amends nothing, is refused.
func (w *p6World) keepsHeld(t *testing.T, id int64, on map[int]types.Qty) bool {
	t.Helper()
	from := w.amendedOf[id]
	if id == 0 || from == 0 || len(on) == 0 {
		return false
	}
	held := w.linked(t, from)
	for i := range on {
		if _, ok := held[i]; !ok {
			return false
		}
	}
	return true
}

// p6FieldText joins err's field error messages.
func p6FieldText(err error) string {
	var b strings.Builder
	for _, fe := range p5Fields(err) {
		b.WriteString(" " + fe.Message)
	}
	return b.String()
}

var (
	p6FuzzBags = []string{"", "10", "15", "25", "40", "45"}
	p6FuzzKg   = []string{"", "50", "100", "110"}
	p6FuzzRecv = []string{"0", "10", "25", "50"}
)

// step runs one action: op%11 picks it, op/11%3 the slot; arg picks
// quantities.
func (w *p6World) step(t *testing.T, op, arg byte) {
	t.Helper()
	env, slot := w.env, int(op/11)%3
	id, owner := w.slots[slot], w.owners[slot]
	st := doctype.State("")
	if id != 0 {
		st = w.state(t, id)
	}
	ruled := func(what string, run func() error) {
		on := w.linked(t, id)
		others := map[int]types.Qty{}
		for i := range on {
			others[i] = w.others(t, id, w.rows[i])
		}
		w.expectRule(t, what, id, on, others, run())
	}
	switch op % 11 {
	case 0: // save: create in an empty slot, or replace a draft's lines
		bags, kg := p6FuzzBags[int(arg)%len(p6FuzzBags)], p6FuzzKg[int(arg)/len(p6FuzzBags)%len(p6FuzzKg)]
		if bags == "" && kg == "" {
			bags = "10"
		}
		var lines []map[string]string
		on := map[int]types.Qty{}
		if bags != "" {
			lines = append(lines, p6Bag(t, env, bags, w.rows[0]))
			on[0] = p6Q(t, bags)
		}
		if kg != "" {
			lines = append(lines, p6Link(p2Line(t, env, "MS-ANGLE-50", "KGS", kg, "62.5"), w.rows[1]))
			on[1] = p6Q(t, kg)
		}
		if id != 0 && st != doctype.StateDraft {
			return
		}
		others := map[int]types.Qty{}
		for i := range on {
			others[i] = w.others(t, id, w.rows[i])
		}
		var err error
		if id == 0 {
			var res doc.Result
			res, err = p2Create(t, env, owner, p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO"), lines)
			if err == nil {
				w.slots[slot] = res.ID
			}
		} else {
			_, err = p2Save(t, env, owner, id, nil, lines)
		}
		w.expectRule(t, "save", id, on, others, err)
		if err == nil {
			for i, q := range on {
				if q > w.want[i]-others[i] {
					w.overOrders = true
				}
			}
		}
	case 1:
		if st == doctype.StateDraft {
			ruled("submit", func() error { _, err := p2Do(t, env, owner, id, doc.ActionSubmit, ""); return err })
		}
	case 2:
		if st == doctype.StatePending {
			ruled("approve", func() error { _, err := p2Do(t, env, env.Ravi, id, doc.ActionApprove, ""); return err })
		}
	case 3:
		if st == doctype.StatePending {
			p2Must(t, "reject")(p2Do(t, env, env.Ravi, id, doc.ActionReject, p6RejectReason))
		}
	case 4:
		if st == doctype.StatePending {
			p2Must(t, "withdraw")(p2Do(t, env, owner, id, doc.ActionWithdraw, ""))
		}
	case 5:
		if st == doctype.StateRejected {
			p2Must(t, "revise")(p2Do(t, env, owner, id, doc.ActionRevise, ""))
		}
	case 6:
		if st == doctype.StateDraft {
			p2Must(t, "delete")(p2Do(t, env, owner, id, doc.ActionDelete, ""))
			w.slots[slot] = 0
		}
	case 7:
		if st == doctype.StateApproved {
			p2Must(t, "cancel")(p2Do(t, env, env.Ravi, id, doc.ActionCancel, p6CancelReason))
		}
	case 8:
		if st == doctype.StateApproved {
			for _, l := range p2Load(t, env, id).Lines {
				purchasetest.SetReceived(t, env, l.ID, p6Q(t, p6FuzzRecv[int(arg)%len(p6FuzzRecv)]))
			}
			p2Must(t, "close")(p2Do(t, env, owner, id, doc.ActionClose, p6CloseReason))
		}
	case 9:
		if st == doctype.StateCancelled {
			if res, err := p2Do(t, env, owner, id, doc.ActionAmend, ""); err == nil {
				w.slots[slot] = res.ID
				w.amendedOf[res.ID] = id
			}
		}
	case 10:
		if !w.closed {
			p5Must(t, "Suresh closes the request")(p5Do(t, env, env.Suresh, w.req.ID, doctype.ActionClose, p5CloseReason))
			w.closed = true
		}
	}
}

// check asserts the invariants after step n.
func (w *p6World) check(t *testing.T, n int) {
	t.Helper()
	var ordered, held [2]types.Qty
	for _, c := range w.lines(t) {
		o, h := p6LineOrderedModel(c.state, c.qty, c.already, c.received)
		for i, r := range w.rows {
			if r == c.row {
				ordered[i] += o
				held[i] += h
			}
		}
	}
	pend := p6Pending(t, w.env, w.rows[0], w.rows[1])
	status := "part_ordered"
	switch {
	case ordered[0] == 0 && ordered[1] == 0:
		status = "not_ordered"
	case ordered[0] >= w.want[0] && ordered[1] >= w.want[1]:
		status = "ordered"
	}
	for i, r := range w.rows {
		got := p6ReadRow(t, w.env, r)
		if got.ordered != ordered[i] {
			t.Errorf("step %d: row %d ordered_qty = %s, want %s (lineOrdered over its PO lines)", n, i+1, got.ordered.Plain(), ordered[i].Plain())
		}
		if got.status != status {
			t.Errorf("step %d: order_status = %q, want %q (ordered %s and %s)", n, got.status, status, ordered[0].Plain(), ordered[1].Plain())
		}
		wantPend := max(0, w.want[i]-ordered[i]-held[i])
		if w.closed {
			wantPend = 0
		}
		if q, ok := pend[r]; !ok || q != wantPend || q < 0 {
			t.Errorf("step %d: row %d pending to order = %s (present %v), want %s", n, i+1, q.Plain(), ok, wantPend.Plain())
		}
		if ordered[i]+held[i] > w.want[i] && !w.overOrders {
			t.Errorf("step %d: row %d ordered %s + held %s is above %s with no over-ordering save", n, i+1,
				ordered[i].Plain(), held[i].Plain(), w.want[i].Plain())
		}
	}
}

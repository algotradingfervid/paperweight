package goodsreceipt_test

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/types"
)

// GRN-035 and PO-044 (GRN spec §4, §5; PO spec §3.2, §6.3; CD-180, CD-276;
// lessons §1 item 6: every write path of the pending invariant): random
// sequences of GRN create, save, submit, cancel and amend, and PO saves of
// already_received on drafts, PO submit, close, cancel (refused while a GRN
// is live) and amend, on one or two POs, with over-delivery and rejected
// quantities. After every step, for every PO line: pending is never below 0,
// received_qty equals the sum of accepted (received − rejected) on approved
// and closed GRN lines, receipt_status is what purchase.Pending gives for
// the lines, and already_received is what the PO saves set (no GRN action
// changes it). Every action goes through Engine.Do as the user whose role
// does it (CD-125, CD-213). A refused action writes nothing.
//
// Plain go test runs the fixed seeds below (hand-written scenarios plus
// PCG-generated sequences, at most g3MaxOps steps each); go test -fuzz runs
// the same harness on new inputs.

// g3Kind is one action of the sequence.
type g3Kind int

const (
	g3GRNApprove g3Kind = iota // Suresh creates and submits a GRN
	g3GRNDraft                 // Suresh creates a draft GRN
	g3GRNSave                  // Suresh saves a draft's line 1
	g3GRNSubmit                // Suresh submits a draft
	g3GRNCancel                // Ravi cancels an approved GRN
	g3GRNAmend                 // Suresh amends a cancelled GRN
	g3POAlready                // Priya saves already_received on a draft PO
	g3POSubmit                 // Priya submits a draft PO (no rule: approves)
	g3POClose                  // Priya closes an approved PO
	g3POCancel                 // Ravi cancels an approved PO
	g3POAmend                  // Priya amends a cancelled PO
)

var g3KindNames = [...]string{"GRN approve", "GRN draft", "GRN save", "GRN submit", "GRN cancel", "GRN amend",
	"PO already", "PO submit", "PO close", "PO cancel", "PO amend"}

// g3GRNKinds weights GRN-035's sequences evenly; g3POKinds weights PO-044's
// toward the PO's own lifecycle.
var (
	g3GRNKinds = []g3Kind{g3GRNApprove, g3GRNDraft, g3GRNSave, g3GRNSubmit, g3GRNCancel, g3GRNAmend,
		g3POAlready, g3POSubmit, g3POClose, g3POCancel, g3POAmend}
	g3POKinds = []g3Kind{g3POAlready, g3POSubmit, g3POClose, g3POCancel, g3POAmend, g3POAlready, g3POCancel,
		g3GRNApprove, g3GRNApprove, g3GRNCancel, g3GRNAmend, g3GRNSubmit, g3GRNDraft, g3GRNSave}
)

// g3MaxOps bounds a sequence, so each seed runs in seconds under -race.
const g3MaxOps = 24

// g3Op is one decoded action: target picks among the candidates, a and b
// pick lines and quantities.
type g3Op struct {
	kind      g3Kind
	target, a byte
	b         byte
}

// g3Decode reads data: two setup bytes, then four bytes per action (kind
// index into kinds, target, a, b).
func g3Decode(data []byte, kinds []g3Kind) (setup [2]byte, ops []g3Op) {
	if len(data) >= 2 {
		setup = [2]byte{data[0], data[1]}
		data = data[2:]
	}
	for len(data) >= 4 && len(ops) < g3MaxOps {
		ops = append(ops, g3Op{kind: kinds[int(data[0])%len(kinds)], target: data[1], a: data[2], b: data[3]})
		data = data[4:]
	}
	return setup, ops
}

// g3Encode is g3Decode's inverse, for the hand-written seeds.
func g3Encode(kinds []g3Kind, setup [2]byte, ops ...g3Op) []byte {
	out := []byte{setup[0], setup[1]}
	for _, o := range ops {
		k := slices.Index(kinds, o.kind)
		if k < 0 {
			panic(fmt.Sprintf("g3Encode: %s is not in the kinds", g3KindNames[o.kind]))
		}
		out = append(out, byte(k), o.target, o.a, o.b)
	}
	return out
}

// g3Random is n bytes from a fixed PCG seed: deterministic seed runs.
func g3Random(seed uint64, n int) []byte {
	r := rand.New(rand.NewPCG(seed, 0x67726e)) //nolint:gosec // G404: test data, not security
	out := make([]byte, n)
	for i := range out {
		out[i] = byte(r.UintN(256))
	}
	return out
}

// GRN-035.
func FuzzGRN_Invariants(f *testing.F) {
	k := g3GRNKinds
	// Over-delivery, a draft saved with a rejected quantity and submitted,
	// cancel and amend (also against a closed PO, G0c), PO cancel refused
	// while a GRN is live and allowed after its cancel, PO amend, an opening
	// quantity saved on the amended draft, and receipts on it.
	f.Add(g3Encode(k, [2]byte{1, 1},
		g3Op{g3GRNApprove, 0, 0, 3}, // PO A line 1: 1,320 of 1,200 (200 already)
		g3Op{g3GRNDraft, 1, 0, 0},   // PO B: 25 of 100
		g3Op{g3GRNSave, 0, 2, 1},    // that draft: 100 received, 25 rejected
		g3Op{g3GRNSubmit, 0, 0, 0},  //
		g3Op{g3GRNCancel, 0, 0, 0},  // the over-delivery
		g3Op{g3GRNAmend, 0, 0, 0},   //
		g3Op{g3GRNSubmit, 0, 0, 0},  // the amendment
		g3Op{g3POClose, 0, 0, 0},    // PO A
		g3Op{g3GRNCancel, 1, 0, 0},  // the amendment, on the closed PO
		g3Op{g3GRNAmend, 1, 0, 0},   // again, on the closed PO
		g3Op{g3GRNSubmit, 0, 0, 0},  //
		g3Op{g3POCancel, 0, 0, 0},   // PO B: refused, its GRN is live
		g3Op{g3GRNCancel, 0, 0, 0},  // PO B's GRN
		g3Op{g3POCancel, 0, 0, 0},   // PO B: now allowed
		g3Op{g3POAmend, 0, 0, 0},    //
		g3Op{g3POAlready, 0, 0, 2},  // 50 of 100 already received
		g3Op{g3POSubmit, 0, 0, 0},   //
		g3Op{g3GRNApprove, 1, 0, 4}, // the amended PO: 200 of 100
		g3Op{g3GRNApprove, 1, 0, 7}, // 100 received, 25 rejected
		g3Op{g3GRNDraft, 1, 0, 0},   // the amended PO: 25 of 100
		g3Op{g3GRNSave, 0, 0, 3},    // that draft: rejected above received, refused
		g3Op{g3GRNCancel, 0, 0, 0},  //
		g3Op{g3GRNCancel, 0, 0, 0},  //
		g3Op{g3GRNApprove, 0, 1, 4}, // closed PO A: refused
	))
	for seed := uint64(1); seed <= 4; seed++ {
		f.Add(g3Random(seed, 2+4*g3MaxOps))
	}
	f.Fuzz(func(t *testing.T, data []byte) { g3Run(t, data, k) })
}

// PO-044.
func FuzzPO_ReceiptInvariants(f *testing.F) {
	k := g3POKinds
	// Opening quantities saved on a draft PO up to its full quantity, then
	// receipts above it; PO cancel refused while a GRN is live; close with a
	// GRN cancelled after it; PO cancel, amend, opening saved and submitted.
	f.Add(g3Encode(k, [2]byte{2, 2},
		g3Op{g3POAlready, 0, 0, 3},  // PO B: 100 of 100 already received
		g3Op{g3POAlready, 0, 0, 1},  // then 25
		g3Op{g3POSubmit, 0, 0, 0},   //
		g3Op{g3GRNApprove, 1, 0, 3}, // PO B: 110 of 100
		g3Op{g3GRNApprove, 0, 1, 4}, // PO A line 2
		g3Op{g3POCancel, 0, 0, 0},   // PO A: refused
		g3Op{g3POClose, 1, 0, 0},    // PO B
		g3Op{g3GRNCancel, 0, 0, 0},  // PO B's GRN, on the closed PO
		g3Op{g3GRNAmend, 0, 0, 0},   //
		g3Op{g3GRNSubmit, 0, 0, 0},  //
		g3Op{g3GRNCancel, 0, 0, 0},  //
		g3Op{g3GRNCancel, 0, 0, 0},  //
		g3Op{g3POCancel, 0, 0, 0},   // PO A, once nothing is live
		g3Op{g3POAmend, 0, 0, 0},    //
		g3Op{g3POAlready, 0, 1, 2},  // line 2: 25 of 50
		g3Op{g3POAlready, 0, 0, 3},  // line 1: all 1,200
		g3Op{g3POSubmit, 0, 0, 0},   //
		g3Op{g3GRNApprove, 1, 2, 9}, // both lines, over and rejected
		g3Op{g3GRNDraft, 1, 0, 0},   //
		g3Op{g3GRNSave, 0, 4, 2},    // 2x ordered, all rejected
		g3Op{g3GRNSubmit, 0, 0, 0},  //
		g3Op{g3POClose, 0, 0, 0},    //
		g3Op{g3GRNCancel, 1, 0, 0},  //
	))
	for seed := uint64(11); seed <= 14; seed++ {
		f.Add(g3Random(seed, 2+4*g3MaxOps))
	}
	f.Fuzz(func(t *testing.T, data []byte) { g3Run(t, data, k) })
}

// g3World is one sequence's world and model: the already_received each PO
// line must hold (set only by PO saves and copied by PO amend), and the
// actions run so far, for failure messages.
type g3World struct {
	t       *testing.T
	env     *grntest.Env
	already map[int64]types.Qty
	log     []string
}

// g3Run builds the world from setup and runs ops, checking the invariants
// after setup and after every action.
func g3Run(t *testing.T, data []byte, kinds []g3Kind) {
	setup, ops := g3Decode(data, kinds)
	w := &g3World{t: t, env: grntest.New(t), already: map[int64]types.Qty{}}
	w.setup(setup)
	w.check("setup")
	for i, o := range ops {
		what := fmt.Sprintf("step %d %s (target %d, a %d, b %d)", i+1, g3KindNames[o.kind], o.target, o.a, o.b)
		w.log = append(w.log, what)
		w.step(o)
		w.check(what)
	}
}

// setup makes PO A (approved, earlier PO OLD-77: line 1 1,200 kg
// MS-ANGLE-50 with s[0] choosing 0, 200 or 600 kg already received, line 2
// 50 kg WIRE-18G) and, by s[1], no second PO, PO B approved or PO B a draft
// (one line of 100 kg MS-ANGLE-50, earlier PO OLD-78).
func (w *g3World) setup(s [2]byte) {
	t, env := w.t, w.env
	already := []string{"0", "200", "600"}[int(s[0])%3]
	purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved(),
		purchasetest.OrderHeader(map[string]string{"earlier_po_no": "OLD-77"}),
		purchasetest.OrderLines(
			map[string]string{"qty": "1200", "price": "62.5", "already_received": already},
			map[string]string{"item_id": g1MasterID(t, env.DB, "item", "WIRE-18G"), "qty": "50", "price": "96", "hsn_sac": "7217"},
		))
	switch int(s[1]) % 3 {
	case 1:
		purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved(),
			purchasetest.OrderHeader(map[string]string{"earlier_po_no": "OLD-78"}),
			purchasetest.OrderLines(map[string]string{"qty": "100"}))
	case 2:
		purchasetest.NewOrder(t, env.Env, purchasetest.StopAtDraft(),
			purchasetest.OrderHeader(map[string]string{"earlier_po_no": "OLD-78"}),
			purchasetest.OrderLines(map[string]string{"qty": "100"}))
	}
	for _, r := range g2Query(t, env.DB, `SELECT id, COALESCE(already_received, 0) FROM purchase_order_line`) {
		w.already[g2Int(t, r[0])] = types.Qty(g2Int(t, r[1]))
	}
}

// ids are the ids of table's rows in one of states, oldest first.
func (w *g3World) ids(table string, states ...doctype.State) []int64 {
	q := `SELECT id FROM ` + table + ` WHERE state IN ('` + strings.Join(g3Strings(states), `','`) + `') ORDER BY id` //nolint:gosec // G202: fixed names
	var out []int64
	for _, r := range g2Query(w.t, w.env.DB, q) {
		out = append(out, g2Int(w.t, r[0]))
	}
	return out
}

func g3Strings(states []doctype.State) []string {
	out := make([]string, len(states))
	for i, s := range states {
		out[i] = string(s)
	}
	return out
}

// pick is the candidate b chooses; 0 and false when there is none.
func g3Pick(ids []int64, b byte) (int64, bool) {
	if len(ids) == 0 {
		return 0, false
	}
	return ids[int(b)%len(ids)], true
}

// poState is PO id's stored state.
func (w *g3World) poState(id int64) doctype.State {
	return doctype.State(g2Query(w.t, w.env.DB, `SELECT state FROM purchase_order WHERE id = ?`, id)[0][0])
}

// grnPO is the PO GRN id is on.
func (w *g3World) grnPO(id int64) int64 {
	return g2Int(w.t, g2Query(w.t, w.env.DB, `SELECT purchase_order_id FROM goods_receipt WHERE id = ?`, id)[0][0])
}

// g3Received is the received quantity menu over a PO line's quantity q
// (whole kg): a quarter, half, all, 10% over, and twice (the most the
// twice-ordered stop lets through, CD-272 Gap 3).
func g3Received(q int64) []int64 {
	return []int64{max(1, q/4), max(1, q/2), q, q + max(1, q/10), 2 * q}
}

// whole is q in whole kg (every quantity here is whole).
func g3Whole(q types.Qty) int64 { return int64(q) / 1000 }

// step runs o, holding each outcome to what the rules decide: where the
// rule is clear the action must succeed or be refused; where it belongs to
// another task (G0c's amendment on a closed PO) either is accepted. A
// refusal must be a 4xx engine error and write nothing.
func (w *g3World) step(o g3Op) {
	t, env := w.t, w.env
	switch o.kind {
	case g3GRNApprove, g3GRNDraft:
		poID, ok := g3Pick(w.ids("purchase_order", doctype.StateApproved, doctype.StateClosed), o.target)
		if !ok {
			return
		}
		po := g3ReadPO(t, env, poID)
		n := len(po.Lines)
		mask := int(o.a)%((1<<n)-1) + 1
		var lines []map[string]string
		for i, l := range po.Lines {
			if mask&(1<<i) == 0 {
				continue
			}
			recv := g3Received(g3Whole(l.Qty))[(int(o.b)+i)%5]
			rej := []int64{0, recv / 4, recv}[(int(o.b)/5+i)%3]
			m := map[string]string{"po_line_id": strconv.FormatInt(l.ID, 10), "received_qty": strconv.FormatInt(recv, 10)}
			if rej > 0 {
				m["rejected_qty"], m["reject_reason"] = strconv.FormatInt(rej, 10), "Bent"
			}
			lines = append(lines, m)
		}
		header := g2Header(t, env, env.Load(t, purchase.DocType, poID))
		before := g3Snapshot(t, env)
		res, err := g2Create(t, env, env.Suresh, header, lines...)
		if err != nil {
			w.refused(err, before, po.State != string(doctype.StateApproved), "create GRN on PO "+g3Itoa(poID))
			return
		}
		if o.kind == g3GRNApprove {
			w.submit(res.ID)
		}
	case g3GRNSave:
		id, ok := g3Pick(w.ids("goods_receipt", doctype.StateDraft), o.target)
		if !ok {
			return
		}
		stored := env.Load(t, goodsreceipt.DocType, id)
		if len(stored.Lines) == 0 {
			return
		}
		plID, _ := stored.Lines[0].Values["po_line_id"].(int64)
		q := g3Whole(types.Qty(g2Int(t, g2Query(t, env.DB, `SELECT qty FROM purchase_order_line WHERE id = ?`, plID)[0][0])))
		recv := g3Received(q)[int(o.a)%5]
		rej := []int64{0, recv / 4, recv, recv + 1}[int(o.b)%4]
		lines := g2StoredLines(t, env, id, map[int]map[string]string{1: {
			"received_qty": strconv.FormatInt(recv, 10), "rejected_qty": strconv.FormatInt(rej, 10), "reject_reason": "Bent",
		}})
		before := g3Snapshot(t, env)
		_, err := g2Save(t, env, env.Suresh, id, nil, lines)
		approved := w.poState(w.grnPO(id)) == doctype.StateApproved
		switch {
		case err != nil:
			w.refused(err, before, !approved || rej > recv, "save GRN "+g3Itoa(id))
		case rej > recv:
			t.Fatalf("%s: save GRN %d with %d rejected of %d received was accepted, want refused", w.trail(), id, rej, recv)
		}
	case g3GRNSubmit:
		if id, ok := g3Pick(w.ids("goods_receipt", doctype.StateDraft), o.target); ok {
			w.submit(id)
		}
	case g3GRNCancel:
		id, ok := g3Pick(w.ids("goods_receipt", doctype.StateApproved), o.target)
		if !ok {
			return
		}
		before := g3Snapshot(t, env)
		if err := w.act(goodsreceipt.DocType, id, doctype.ActionCancel, "Wrong entry", env.Ravi); err != nil {
			w.refused(err, before, false, "cancel GRN "+g3Itoa(id))
		}
	case g3GRNAmend:
		id, ok := g3Pick(w.ids("goods_receipt", doctype.StateCancelled), o.target)
		if !ok {
			return
		}
		amended := g2Query(t, env.DB, `SELECT count(*) FROM goods_receipt WHERE amended_from_id = ?`, id)[0][0] != "0"
		approved := w.poState(w.grnPO(id)) == doctype.StateApproved
		before := g3Snapshot(t, env)
		if err := w.act(goodsreceipt.DocType, id, doctype.ActionAmend, "", env.Suresh); err != nil {
			w.refused(err, before, amended || !approved, "amend GRN "+g3Itoa(id))
		}
	case g3POAlready:
		id, ok := g3Pick(w.ids("purchase_order", doctype.StateDraft), o.target)
		if !ok {
			return
		}
		po := g3ReadPO(t, env, id)
		n := int(o.a) % len(po.Lines)
		var lines []map[string]string
		var want types.Qty
		for i, l := range po.Lines {
			v := l.Already
			if i == n {
				q := g3Whole(l.Qty)
				v = types.Qty([]int64{0, q / 4, q / 2, q}[int(o.b)%4] * 1000)
				want = v
			}
			lines = append(lines, map[string]string{"id": strconv.FormatInt(l.ID, 10), "already_received": v.Plain()})
		}
		_, err := env.Engine.Do(t.Context(), env.Priya, purchase.DocType, id, doctype.ActionSave,
			doc.ActionInput{Version: int(po.Version), Lines: lines})
		if err != nil {
			t.Fatalf("%s: save already_received on draft PO %d: %v %+v", w.trail(), id, err, g1FieldErrors(err))
		}
		w.already[po.Lines[n].ID] = want
	case g3POSubmit:
		id, ok := g3Pick(w.ids("purchase_order", doctype.StateDraft), o.target)
		if !ok {
			return
		}
		if err := w.act(purchase.DocType, id, doctype.ActionSubmit, "", env.Priya); err != nil {
			t.Fatalf("%s: submit draft PO %d: %v %+v", w.trail(), id, err, g1FieldErrors(err))
		}
		if s := w.poState(id); s != doctype.StateApproved {
			t.Fatalf("%s: PO %d is %s after submit with no rule, want approved", w.trail(), id, s)
		}
	case g3POClose:
		id, ok := g3Pick(w.ids("purchase_order", doctype.StateApproved), o.target)
		if !ok {
			return
		}
		if err := w.act(purchase.DocType, id, doctype.ActionClose, "Nothing more due", env.Priya); err != nil {
			t.Fatalf("%s: close PO %d: %v", w.trail(), id, err)
		}
	case g3POCancel:
		id, ok := g3Pick(w.ids("purchase_order", doctype.StateApproved), o.target)
		if !ok {
			return
		}
		live := g2Query(t, env.DB, `SELECT count(*) FROM goods_receipt WHERE purchase_order_id = ? AND state IN ('pending', 'approved', 'closed')`, id)[0][0] != "0"
		before := g3Snapshot(t, env)
		err := w.act(purchase.DocType, id, doctype.ActionCancel, "Vendor backed out", env.Ravi)
		var de *doc.Error
		switch {
		case live && err == nil:
			t.Fatalf("%s: PO %d cancelled while a GRN on it is live, want DOC-052's refusal", w.trail(), id)
		case live && (!errors.As(err, &de) || de.Kind != doc.KindBlocked || !strings.HasPrefix(de.Message, "Can't cancel ")):
			t.Fatalf("%s: cancel PO %d with a live GRN: %v (%T), want DOC-052's blocked refusal", w.trail(), id, err, err)
		case live:
			w.refused(err, before, true, "cancel PO "+g3Itoa(id))
		case err != nil:
			t.Fatalf("%s: cancel PO %d with no live GRN: %v", w.trail(), id, err)
		}
	case g3POAmend:
		id, ok := g3Pick(w.ids("purchase_order", doctype.StateCancelled), o.target)
		if !ok {
			return
		}
		if g2Query(t, env.DB, `SELECT count(*) FROM purchase_order WHERE amended_from_id = ?`, id)[0][0] != "0" {
			return
		}
		res, err := env.Engine.Do(t.Context(), env.Priya, purchase.DocType, id, doctype.ActionAmend,
			doc.ActionInput{Version: env.Load(t, purchase.DocType, id).Version})
		if err != nil {
			t.Fatalf("%s: amend PO %d: %v %+v", w.trail(), id, err, g1FieldErrors(err))
		}
		old := g3ReadPO(t, env, id).Lines
		for _, l := range g3ReadPO(t, env, res.ID).Lines {
			for _, o := range old {
				if o.LineNo == l.LineNo {
					w.already[l.ID] = w.already[o.ID]
				}
			}
		}
	}
}

// submit submits draft GRN id as Suresh: on an approved PO it must be
// approved; on a cancelled PO refused (DOC-038); on a closed PO either
// (G0c's amendment rule, G4's tests).
func (w *g3World) submit(id int64) {
	t := w.t
	state := w.poState(w.grnPO(id))
	before := g3Snapshot(t, w.env)
	res, err := g2Submit(t, w.env, w.env.Suresh, id)
	switch {
	case err != nil:
		w.refused(err, before, state != doctype.StateApproved, "submit GRN "+g3Itoa(id))
	case state == doctype.StateCancelled:
		t.Fatalf("%s: GRN %d on a cancelled PO was submitted, want DOC-038's refusal", w.trail(), id)
	case res.State != doctype.StateApproved:
		t.Fatalf("%s: GRN %d is %s after submit with no rule, want approved", w.trail(), id, res.State)
	}
}

// act runs action a on document id of dt as p at its stored version.
func (w *g3World) act(dt string, id int64, a doctype.Action, reason string, p auth.Principal) error {
	_, err := w.env.Engine.Do(w.t.Context(), p, dt, id, a, doc.ActionInput{Version: w.env.Load(w.t, dt, id).Version, Reason: reason})
	return err
}

// refused checks err is a 4xx engine refusal that wrote nothing; expected
// says whether the rules allow a refusal here at all.
func (w *g3World) refused(err error, before string, expected bool, what string) {
	t := w.t
	var he doctype.HTTPError
	if !errors.As(err, &he) || he.Status() < 400 || he.Status() >= 500 {
		t.Fatalf("%s: %s: %v (%T), want success or a 4xx refusal", w.trail(), what, err, err)
	}
	if !expected {
		t.Fatalf("%s: %s refused: %v %+v, want it to succeed", w.trail(), what, err, he.FieldErrors())
	}
	if after := g3Snapshot(t, w.env); after != before {
		t.Fatalf("%s: refused %s wrote:\nbefore %s\nafter  %s", w.trail(), what, before, after)
	}
}

// check asserts the invariants on every PO line after what.
func (w *g3World) check(what string) {
	t, env := w.t, w.env
	for _, r := range g2Query(t, env.DB, `SELECT id FROM purchase_order ORDER BY id`) {
		poID := g2Int(t, r[0])
		po := g3ReadPO(t, env, poID)
		for _, l := range po.Lines {
			if p := purchase.Pending(l.Qty, l.Already, l.Received); p < 0 {
				t.Fatalf("%s: %s: PO %d line %d pending %d, below 0", w.trail(), what, poID, l.LineNo, p)
			}
			sum := types.Qty(g2Int(t, g2Query(t, env.DB, `SELECT COALESCE(SUM(l.received_qty - COALESCE(l.rejected_qty, 0)), 0)
				FROM goods_receipt_line l JOIN goods_receipt g ON g.id = l.parent_id
				WHERE l.po_line_id = ? AND g.state IN ('approved', 'closed')`, l.ID)[0][0]))
			if l.Received != sum {
				t.Fatalf("%s: %s: PO %d line %d received_qty %d, want %d, the accepted on approved and closed GRNs",
					w.trail(), what, poID, l.LineNo, l.Received, sum)
			}
			if want := w.already[l.ID]; l.Already != want {
				t.Fatalf("%s: %s: PO %d line %d already_received %d, want %d (only PO saves set it)",
					w.trail(), what, poID, l.LineNo, l.Already, want)
			}
		}
		if want := g3StatusOf(po.Lines); po.Status != want {
			t.Fatalf("%s: %s: PO %d (%s) receipt_status %q, want %q from purchase.Pending over %+v",
				w.trail(), what, poID, po.State, po.Status, want, po.Lines)
		}
	}
}

// trail is the actions run so far, for a failure message.
func (w *g3World) trail() string { return "after [" + strings.Join(w.log, "; ") + "]" }

package purchase_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// PO-055 (PO spec §20.2, §20.3, §20.5, §26; CD-280 D3): a request with two
// rows (40 Bag, 100 kg) and Priya's PO for both, with 5 Bag already received
// on line 1. After each action ordered_qty and order_status follow
// lineOrdered: approve counts qty; cancel counts 0; the amendment copies
// request_line_id and holds the rows until it is approved; a short close
// counts min(qty, already_received + received_qty) = min(40, 5 + 20) = 25, so
// 15 Bag are freed and go on Arjun's new PO. No step bumps the request's
// version, changes updated_at/updated_by or writes an audit entry on it.
func TestRequest_OrderStatus(t *testing.T) {
	env := purchasetest.New(t)
	req := p6Request(t, env, env.Suresh, p5Header(t, env, "METRO-SITE", "PRJ-METRO"), []map[string]string{
		p5Line(t, env, "CEMENT-OPC", "BAG", "40"), p5Line(t, env, "MS-ANGLE-50", "KGS", "100")})
	bags, kg := req.Lines[0].ID, req.Lines[1].ID
	header := p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO")
	header["earlier_po_no"] = "P-17"
	first := p6Bag(t, env, "40", bags)
	first["already_received"] = "5"
	po := p6Order(t, env, env.Priya, header, []map[string]string{
		first, p6Link(p2Line(t, env, "MS-ANGLE-50", "KGS", "100", "62.5"), kg)}, doctype.StateDraft)

	var am doc.Doc
	steps := []struct {
		step             string
		do               func(t *testing.T)
		bags, kg         string // ordered_qty of each row
		pendBags, pendKg string // pending to order of each row
		status           string
	}{
		{"draft saved", func(*testing.T) {}, "0", "0", "0", "0", "not_ordered"},
		{"approved", func(t *testing.T) {
			p2Must(t, "submit")(p2Do(t, env, env.Priya, po.ID, doc.ActionSubmit, ""))
			p2Must(t, "Ravi approves")(p2Do(t, env, env.Ravi, po.ID, doc.ActionApprove, ""))
		}, "40", "100", "0", "0", "ordered"},
		{"cancelled", func(t *testing.T) {
			p2Must(t, "Ravi cancels")(p2Do(t, env, env.Ravi, po.ID, doc.ActionCancel, p6CancelReason))
		}, "0", "0", "40", "100", "not_ordered"},
		{"amended (draft holds)", func(t *testing.T) {
			res := p2Must(t, "Priya amends")(p2Do(t, env, env.Priya, po.ID, doc.ActionAmend, ""))
			am = p2Load(t, env, res.ID)
			got := p2LineSnap(t, env, am.ID, "request_line_id", "already_received")
			if len(got) != 2 || got[0]["request_line_id"] != p5Plain(bags) || got[1]["request_line_id"] != p5Plain(kg) {
				t.Fatalf("amendment lines %v, want request_line_id %d and %d copied", got, bags, kg)
			}
		}, "0", "0", "0", "0", "not_ordered"},
		{"amendment approved", func(t *testing.T) {
			p2Must(t, "submit the amendment")(p2Do(t, env, env.Priya, am.ID, doc.ActionSubmit, ""))
			p2Must(t, "Ravi approves the amendment")(p2Do(t, env, env.Ravi, am.ID, doc.ActionApprove, ""))
		}, "40", "100", "0", "0", "ordered"},
		{"20 Bag and 100 kg received, short closed", func(t *testing.T) {
			purchasetest.SetReceived(t, env, am.Lines[0].ID, p6Q(t, "20"))
			purchasetest.SetReceived(t, env, am.Lines[1].ID, p6Q(t, "100"))
			p2Must(t, "Priya closes")(p2Do(t, env, env.Priya, am.ID, doc.ActionClose, p6CloseReason))
		}, "25", "100", "15", "0", "part_ordered"},
		{"the freed 15 Bag on Arjun's PO, approved", func(t *testing.T) {
			p6Order(t, env, env.Arjun, nil, []map[string]string{p6Bag(t, env, "15", bags)}, doctype.StateApproved)
		}, "40", "100", "0", "0", "ordered"},
	}
	for _, s := range steps {
		before := p6ReadFacts(t, env, purchase.RequestDocType, req.ID)
		s.do(t)
		b, k := p6ReadRow(t, env, bags), p6ReadRow(t, env, kg)
		pend := p6Pending(t, env, bags, kg)
		if b.ordered != p6Q(t, s.bags) || k.ordered != p6Q(t, s.kg) || b.status != s.status {
			t.Errorf("%s: ordered %s and %s, order_status %q; want %s and %s, %q", s.step, b.ordered.Plain(), k.ordered.Plain(), b.status, s.bags, s.kg, s.status)
		}
		if pend[bags] != p6Q(t, s.pendBags) || pend[kg] != p6Q(t, s.pendKg) {
			t.Errorf("%s: pending to order %s and %s, want %s and %s", s.step, pend[bags].Plain(), pend[kg].Plain(), s.pendBags, s.pendKg)
		}
		if after := p6ReadFacts(t, env, purchase.RequestDocType, req.ID); after != before {
			t.Errorf("%s: request facts %+v, want %+v unchanged (plain UPDATEs, no audit; CD-280 D3)", s.step, after, before)
		}
	}
}

// PO-076 (PO spec §20.2 lineOrdered, §27.5; research S09): PR-A line 1 (40
// Bag) on Priya's PO for 40 Bag (₹70,800.00, so it waits for Ravi). A
// rejected PO still holds the row, so Arjun's PO for 40 Bag is refused
// naming it; revised, resubmitted and withdrawn it still holds; deleting the
// draft frees the 40 Bag and Arjun's PO saves.
func TestRequest_HeldByRejectedWithdrawnDeleted(t *testing.T) {
	env := purchasetest.New(t)
	req := p6CementRequest(t, env, "40")
	row := req.Lines[0].ID
	var po doc.Doc
	arjunSaves := func(t *testing.T) (doc.Result, error) {
		t.Helper()
		return p2Create(t, env, env.Arjun, p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO"), []map[string]string{p6Bag(t, env, "40", row)})
	}
	steps := []p6Walk{
		{"draft saved", func(t *testing.T) {
			po = p6Order(t, env, env.Priya, nil, []map[string]string{p6Bag(t, env, "40", row)}, doctype.StateDraft)
		}, "0", "40", "0", "not_ordered"},
		{"submitted", func(t *testing.T) {
			p2Must(t, "submit")(p2Do(t, env, env.Priya, po.ID, doc.ActionSubmit, ""))
		}, "0", "40", "0", "not_ordered"},
		{"rejected", func(t *testing.T) {
			p2Must(t, "Ravi rejects")(p2Do(t, env, env.Ravi, po.ID, doc.ActionReject, p6RejectReason))
			_, err := arjunSaves(t)
			p2WantInvalid(t, "Arjun's PO while the rejected PO holds the row", err,
				doctype.FieldError{Field: "request_line_id", Line: 1, Message: p6Fmt(p6MsgAlreadyOn, 1, req.Number, 1, p6Label(po))})
		}, "0", "40", "0", "not_ordered"},
		{"revised and resubmitted", func(t *testing.T) {
			p2Must(t, "revise")(p2Do(t, env, env.Priya, po.ID, doc.ActionRevise, ""))
			p2Must(t, "resubmit")(p2Do(t, env, env.Priya, po.ID, doc.ActionSubmit, ""))
		}, "0", "40", "0", "not_ordered"},
		{"withdrawn", func(t *testing.T) {
			p2Must(t, "withdraw")(p2Do(t, env, env.Priya, po.ID, doc.ActionWithdraw, ""))
		}, "0", "40", "0", "not_ordered"},
		{"draft deleted", func(t *testing.T) {
			p2Must(t, "delete")(p2Do(t, env, env.Priya, po.ID, doc.ActionDelete, ""))
		}, "0", "0", "40", "not_ordered"},
		{"Arjun's PO for 40 Bag saves", func(t *testing.T) {
			if _, err := arjunSaves(t); err != nil {
				t.Errorf("Arjun's PO after the delete: %v %+v, want saved", err, p5Fields(err))
			}
		}, "0", "40", "0", "not_ordered"},
	}
	p6RunWalk(t, env, req, row, steps, nil)
}

// PO-077 (PO spec §20.3, §27.5; research S03): PR-A line 1 (40 Bag) on
// approved PO/26-27/00001 for 40 Bag, 25 received, and PR-A closed by
// Suresh. Priya (Purchase User, CloseRight amend) closes the PO: the close
// succeeds and the row's ordered_qty reads 25 with order_status
// part_ordered, with no version bump and no audit entry on PR-A.
func TestRequest_ClosePOOfClosedRequest(t *testing.T) {
	env := purchasetest.New(t)
	req := p6CementRequest(t, env, "40")
	row := req.Lines[0].ID
	po := p6Order(t, env, env.Priya, nil, []map[string]string{p6Bag(t, env, "40", row)}, doctype.StateApproved)
	if po.Number != "PO/26-27/00001" {
		t.Fatalf("PO number %q, want PO/26-27/00001", po.Number)
	}
	steps := []p6Walk{
		{"PO approved", func(*testing.T) {}, "40", "0", "0", "ordered"},
		{"25 received", func(t *testing.T) { purchasetest.SetReceived(t, env, po.Lines[0].ID, p6Q(t, "25")) }, "40", "0", "0", "ordered"},
		{"PR-A closed", func(t *testing.T) {
			p5Must(t, "Suresh closes PR-A")(p5Do(t, env, env.Suresh, req.ID, doctype.ActionClose, p5CloseReason))
		}, "40", "0", "0", "ordered"},
		{"PO closed", func(t *testing.T) {
			res, err := p2Do(t, env, env.Priya, po.ID, doc.ActionClose, p6CloseReason)
			if err != nil || res.State != doctype.StateClosed {
				t.Fatalf("Priya closes the PO: %v (%s), want closed", err, res.State)
			}
		}, "25", "0", "0", "part_ordered"},
	}
	p6RunWalk(t, env, req, row, steps, map[string]bool{"PR-A closed": true})
}

// PO-078 (PO spec §20.4, §27.5; research S08): PR-A line 1 (40 Bag) split:
// Priya orders 25 from SHREE and Arjun 15 from BHARAT, both approved; the row
// reads ordered 40 and the request ordered. HeldBy and LinkedOrders list both
// POs, oldest first. Cancelling the BHARAT PO puts 15 back as pending, and
// both lists drop it.
func TestRequest_SplitAcrossVendors(t *testing.T) {
	env := purchasetest.New(t)
	req := p6CementRequest(t, env, "40")
	row := req.Lines[0].ID
	var shree, bharat doc.Doc
	steps := []p6Walk{
		{"Priya's 25 from SHREE approved", func(t *testing.T) {
			shree = p6Order(t, env, env.Priya, nil, []map[string]string{p6Bag(t, env, "25", row)}, doctype.StateApproved)
		}, "25", "0", "15", "part_ordered"},
		{"Arjun's 15 from BHARAT approved", func(t *testing.T) {
			bharat = p6Order(t, env, env.Arjun, p2Header(t, env, "BHARAT", "METRO-SITE", "PRJ-METRO"),
				[]map[string]string{p6Bag(t, env, "15", row)}, doctype.StateApproved)
		}, "40", "0", "0", "ordered"},
	}
	p6RunWalk(t, env, req, row, steps, nil)

	qtys := map[int64]string{shree.ID: "25", bharat.ID: "15"}
	wantHolders := func(pos ...doc.Doc) []purchase.Holder {
		var out []purchase.Holder
		for _, d := range pos {
			out = append(out, purchase.Holder{RequestLineID: row, POID: d.ID, PODisplay: d.Number, State: doctype.StateApproved,
				Owner: d.CreatedBy, Qty: p6Q(t, qtys[d.ID])})
		}
		return out
	}
	wantLinked := func(pos ...doc.Doc) []purchase.LinkedOrder {
		names := map[int64]string{shree.ID: "Shree Steel Traders", bharat.ID: "Bharat Tools"}
		var out []purchase.LinkedOrder
		for _, d := range pos {
			out = append(out, purchase.LinkedOrder{ID: d.ID, Display: d.Number, VendorName: names[d.ID], State: doctype.StateApproved, ReceiptStatus: "not_received"})
		}
		return out
	}
	if got, want := p6HeldBy(t, env, req.ID), wantHolders(shree, bharat); !p6SameHolders(got, want) {
		t.Errorf("HeldBy = %+v, want %+v", got, want)
	}
	if got, want := p6Linked(t, env, req.ID), wantLinked(shree, bharat); !p6SameLinked(got, want) {
		t.Errorf("LinkedOrders = %+v, want %+v", got, want)
	}

	p6RunWalk(t, env, req, row, []p6Walk{
		{"BHARAT PO cancelled", func(t *testing.T) {
			p2Must(t, "Ravi cancels the BHARAT PO")(p2Do(t, env, env.Ravi, bharat.ID, doc.ActionCancel, p6CancelReason))
		}, "25", "0", "15", "part_ordered"},
	}, nil)
	if got, want := p6HeldBy(t, env, req.ID), wantHolders(shree); !p6SameHolders(got, want) {
		t.Errorf("HeldBy after the cancel = %+v, want %+v", got, want)
	}
	if got, want := p6Linked(t, env, req.ID), wantLinked(shree); !p6SameLinked(got, want) {
		t.Errorf("LinkedOrders after the cancel = %+v, want %+v", got, want)
	}
}

// p6SameHolders and p6SameLinked compare in order, nil equal to empty.
func p6SameHolders(a, b []purchase.Holder) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func p6SameLinked(a, b []purchase.LinkedOrder) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// MOD-PO P6 Do 5 (PO spec §20.3, CD-276 S59, CD-280 D3; AD-13 from the P4
// review): purchase.RefreshReceiptStatus on a closed PO refreshes the linked
// request from the accepted map it was given, min(qty, already_received +
// accepted), never from the received_qty column: GRN's hooks run before the
// roll-up rewrites it (GRN-040 relies on this). On an approved PO the same
// call leaves the request rows alone (approved counts qty). Neither bumps a
// version or writes an audit entry, on the request or the PO.
func TestPO_RefreshReceiptStatusClosedRequests(t *testing.T) {
	refresh := func(t *testing.T, env *purchasetest.Env, poID int64, accepted map[int64]types.Qty) {
		t.Helper()
		if err := env.DB.Write(t.Context(), func(tx *db.Tx) error {
			return purchase.RefreshReceiptStatus(t.Context(), tx, poID, accepted)
		}); err != nil {
			t.Fatalf("RefreshReceiptStatus(%d, %v): %v", poID, accepted, err)
		}
	}
	received := func(t *testing.T, env *purchasetest.Env, lineID int64) types.Qty {
		t.Helper()
		var q types.Qty
		if err := env.DB.SQL().QueryRowContext(t.Context(), `SELECT received_qty FROM purchase_order_line WHERE id = ?`, lineID).Scan(&q); err != nil {
			t.Fatalf("received_qty of line %d: %v", lineID, err)
		}
		return q
	}

	t.Run("closed PO", func(t *testing.T) {
		t.Parallel()
		env := purchasetest.New(t)
		req := p6CementRequest(t, env, "40")
		row := req.Lines[0].ID
		po := p6Order(t, env, env.Priya, nil, []map[string]string{p6Bag(t, env, "40", row)}, doctype.StateApproved)
		line := po.Lines[0].ID
		purchasetest.SetReceived(t, env, line, p6Q(t, "25"))
		p2Must(t, "Priya closes")(p2Do(t, env, env.Priya, po.ID, doc.ActionClose, p6CloseReason))
		if got := p6ReadRow(t, env, row); got.ordered != p6Q(t, "25") || got.status != "part_ordered" {
			t.Fatalf("after the close the row reads %s %q, want 25 part_ordered", got.ordered.Plain(), got.status)
		}

		for _, s := range []struct {
			accepted string // nil map key when ""
			ordered  string
			status   string
		}{
			{"15", "15", "part_ordered"}, // a GRN of 10 cancelled: 25 → 15
			{"40", "40", "ordered"},
			{"50", "40", "ordered"},  // capped at the line's qty
			{"", "0", "not_ordered"}, // a missing line counts 0
		} {
			acc := map[int64]types.Qty{}
			if s.accepted != "" {
				acc[line] = p6Q(t, s.accepted)
			}
			reqBefore, poBefore := p6ReadFacts(t, env, purchase.RequestDocType, req.ID), p6ReadFacts(t, env, purchase.DocType, po.ID)
			refresh(t, env, po.ID, acc)
			if got := p6ReadRow(t, env, row); got.ordered != p6Q(t, s.ordered) || got.status != s.status {
				t.Errorf("accepted %q: row reads %s %q, want %s %q", s.accepted, got.ordered.Plain(), got.status, s.ordered, s.status)
			}
			if got := received(t, env, line); got != p6Q(t, "25") {
				t.Errorf("accepted %q: received_qty = %s, want 25 (the column is not what the refresh reads or writes)", s.accepted, got.Plain())
			}
			if after := p6ReadFacts(t, env, purchase.RequestDocType, req.ID); after != reqBefore {
				t.Errorf("accepted %q: request facts %+v, want %+v (no version, no audit)", s.accepted, after, reqBefore)
			}
			if after := p6ReadFacts(t, env, purchase.DocType, po.ID); after != poBefore {
				t.Errorf("accepted %q: PO facts %+v, want %+v (no version, no audit)", s.accepted, after, poBefore)
			}
		}
	})

	t.Run("approved PO", func(t *testing.T) {
		t.Parallel()
		env := purchasetest.New(t)
		req := p6CementRequest(t, env, "40")
		row := req.Lines[0].ID
		po := p6Order(t, env, env.Priya, nil, []map[string]string{p6Bag(t, env, "40", row)}, doctype.StateApproved)
		purchasetest.SetReceived(t, env, po.Lines[0].ID, p6Q(t, "25"))
		reqBefore := p6ReadFacts(t, env, purchase.RequestDocType, req.ID)
		refresh(t, env, po.ID, map[int64]types.Qty{po.Lines[0].ID: p6Q(t, "15")})
		if got := p6ReadRow(t, env, row); got.ordered != p6Q(t, "40") || got.status != "ordered" {
			t.Errorf("approved PO: row reads %s %q, want 40 ordered (approved counts qty)", got.ordered.Plain(), got.status)
		}
		if after := p6ReadFacts(t, env, purchase.RequestDocType, req.ID); after != reqBefore {
			t.Errorf("approved PO: request facts %+v, want %+v", after, reqBefore)
		}
	})
}

// CD-299 two-record tests (PO spec §20.3; lessons §8): refreshing one
// request never changes another's ordered_qty or order_status. R2's row is on
// Arjun's closed PO with 10 received, so it reads 10; then the line's
// received_qty moves to 20 with no refresh, so a recompute of R2 would read
// 20. Every action on R1's PO (approve, cancel, amend and approve, close,
// RefreshReceiptStatus) and RefreshOrderStatus([R1]) leave R2 at 10;
// RefreshOrderStatus([R2]) then moves it to 20, which proves the setup can
// tell.
func TestRequest_RefreshOnlyLinked(t *testing.T) {
	env := purchasetest.New(t)
	r1 := p6CementRequest(t, env, "40")
	r2 := p6CementRequest(t, env, "30")
	row1, row2 := r1.Lines[0].ID, r2.Lines[0].ID

	other := p6Order(t, env, env.Arjun, nil, []map[string]string{p6Bag(t, env, "30", row2)}, doctype.StateApproved)
	purchasetest.SetReceived(t, env, other.Lines[0].ID, p6Q(t, "10"))
	p2Must(t, "Arjun closes his PO")(p2Do(t, env, env.Arjun, other.ID, doc.ActionClose, p6CloseReason))
	if got := p6ReadRow(t, env, row2); got.ordered != p6Q(t, "10") || got.status != "part_ordered" {
		t.Fatalf("R2 after its PO's close reads %s %q, want 10 part_ordered", got.ordered.Plain(), got.status)
	}
	purchasetest.SetReceived(t, env, other.Lines[0].ID, p6Q(t, "20"))
	r2Before := p6ReadFacts(t, env, purchase.RequestDocType, r2.ID)

	po := p6Order(t, env, env.Priya, nil, []map[string]string{p6Bag(t, env, "40", row1)}, doctype.StatePending)
	var am doc.Result
	for _, s := range []struct {
		step   string
		do     func(t *testing.T)
		r1     string
		status string
	}{
		{"R1's PO approved", func(t *testing.T) { p2Must(t, "approve")(p2Do(t, env, env.Ravi, po.ID, doc.ActionApprove, "")) }, "40", "ordered"},
		{"R1's PO cancelled", func(t *testing.T) {
			p2Must(t, "cancel")(p2Do(t, env, env.Ravi, po.ID, doc.ActionCancel, p6CancelReason))
		}, "0", "not_ordered"},
		{"R1's PO amended and approved", func(t *testing.T) {
			am = p2Must(t, "amend")(p2Do(t, env, env.Priya, po.ID, doc.ActionAmend, ""))
			p2Must(t, "submit")(p2Do(t, env, env.Priya, am.ID, doc.ActionSubmit, ""))
			p2Must(t, "approve")(p2Do(t, env, env.Ravi, am.ID, doc.ActionApprove, ""))
		}, "40", "ordered"},
		{"R1's PO closed with 5 received", func(t *testing.T) {
			purchasetest.SetReceived(t, env, p2Load(t, env, am.ID).Lines[0].ID, p6Q(t, "5"))
			p2Must(t, "close")(p2Do(t, env, env.Priya, am.ID, doc.ActionClose, p6CloseReason))
		}, "5", "part_ordered"},
		{"RefreshReceiptStatus on R1's closed PO", func(t *testing.T) {
			if err := env.DB.Write(t.Context(), func(tx *db.Tx) error {
				return purchase.RefreshReceiptStatus(t.Context(), tx, am.ID, map[int64]types.Qty{p2Load(t, env, am.ID).Lines[0].ID: p6Q(t, "8")})
			}); err != nil {
				t.Fatalf("RefreshReceiptStatus: %v", err)
			}
		}, "8", "part_ordered"},
		{"RefreshOrderStatus([R1])", func(t *testing.T) {
			if err := p6Refresh(t, env, r1.ID); err != nil {
				t.Fatalf("RefreshOrderStatus([R1]): %v", err)
			}
		}, "5", "part_ordered"}, // recomputed from the received_qty column (5)
	} {
		s.do(t)
		if got := p6ReadRow(t, env, row1); got.ordered != p6Q(t, s.r1) || got.status != s.status {
			t.Errorf("%s: R1 reads %s %q, want %s %q", s.step, got.ordered.Plain(), got.status, s.r1, s.status)
		}
		if got := p6ReadRow(t, env, row2); got.ordered != p6Q(t, "10") || got.status != "part_ordered" {
			t.Errorf("%s: R2 reads %s %q, want 10 part_ordered (another request is never refreshed)", s.step, got.ordered.Plain(), got.status)
		}
		if after := p6ReadFacts(t, env, purchase.RequestDocType, r2.ID); after != r2Before {
			t.Errorf("%s: R2 facts %+v, want %+v", s.step, after, r2Before)
		}
	}

	if err := p6Refresh(t, env, r2.ID); err != nil {
		t.Fatalf("RefreshOrderStatus([R2]): %v", err)
	}
	if got := p6ReadRow(t, env, row2); got.ordered != p6Q(t, "20") || got.status != "part_ordered" {
		t.Errorf("after RefreshOrderStatus([R2]) R2 reads %s %q, want 20 part_ordered (min(30, 0 + 20))", got.ordered.Plain(), got.status)
	}
	if after := p6ReadFacts(t, env, purchase.RequestDocType, r2.ID); after != r2Before {
		t.Errorf("RefreshOrderStatus([R2]): R2 facts %+v, want %+v (plain UPDATE, no audit)", after, r2Before)
	}
}

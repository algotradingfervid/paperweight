//go:build e2e

package e2e

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// The goods receipt flows end to end (MOD-GRN S6): GRN-036 on a phone, the
// cross-module flows X1 and X2 (GRN-049, GRN-050; CD-271), and X5
// (EXP-067), which MOD-GRN builds because it finished after MOD-EXP
// (CD-271). Each ends with a stored-state check through the services.

const (
	boltName        = "Hex Bolt M12x60"
	grnChallan      = "CH-5521"
	grnRejectReason = "Bent pieces"
	shortClose      = "Vendor can't supply the rest"
	shreeGSTIN      = "27AABCS1429B1ZU"
	cashInvoice     = "INV-901"
	grnApproved     = "Approved. " + firstGRN + " has been numbered."
	closedPOText    = firstNumber + " is closed. Remove the line or pick another purchase order." // DOC-017
)

// pendingOf is the purchase service's pending report for PO id, the sum
// of its lines' pending; a PO with nothing pending, or closed, has no row.
func (h *Harness) pendingOf(id int64) types.Qty {
	h.t.Helper()
	svc := purchase.New(purchase.ServiceDeps{
		DB: h.App.DB(), Engine: h.App.Engine(), Auth: h.App.Auth(), Audit: audit.Default, Approvals: h.App.Approvals(), Clock: h.Clock,
	})
	v, err := svc.PendingReport(context.Background(), h.Principal(admin), purchase.PendingFilter{}, 0)
	if err != nil {
		h.t.Fatalf("pending report: %v", err)
	}
	var sum types.Qty
	for _, r := range v.Rows {
		if r.POID == id {
			sum += r.Pending
		}
	}
	return sum
}

// qty is a stored quantity value.
func qty(t *testing.T, v string) types.Qty {
	t.Helper()
	q, err := types.ParseQty(v)
	if err != nil {
		t.Fatalf("stored quantity %q: %v", v, err)
	}
	return q
}

// wantQty fails unless the stored value v is want.
func wantQty(t *testing.T, what, v, want string) {
	t.Helper()
	if qty(t, v) != qty(t, want) {
		t.Errorf("%s = %s, want %s", what, v, want)
	}
}

// wantPO fails unless PO id is stored in state with receipt status status
// and the pending report reads pending for it.
func (h *Harness) wantPO(step string, id int64, state doctype.State, status, pending string) {
	h.t.Helper()
	r := h.load(purchase.DocType, id)
	if r.Info.State != state || r.Values["receipt_status"] != status {
		h.t.Errorf("%s: PO %s %s, want %s %s", step, r.Info.State, r.Values["receipt_status"], state, status)
	}
	if got := h.pendingOf(id); got != qty(h.t, pending) {
		h.t.Errorf("%s: PO pending %s, want %s", step, got.Plain(), pending)
	}
}

// grnCount is the number of goods receipts stored.
func (h *Harness) grnCount() int {
	h.t.Helper()
	return Scalar[int](h, `SELECT count(*) FROM goods_receipt`)
}

// startGRN opens the Goods Receipts list as s's user and taps New goods
// receipt; with one PO at the user's sites it is already chosen (UX §5.2),
// so Save and continue creates the draft. It returns the draft's id on
// Delivery.
func startGRN(s *Session) int64 {
	s.t.Helper()
	s.Press("New goods receipt")
	onStep(s, "Step 1 of 4")
	noOverflow(s, "Order")
	if !Eval[bool](s, `!!document.querySelector("input[name=purchase_order_id]:checked")`) {
		s.t.Errorf("the only PO at Suresh's sites isn't chosen on step 1\n%s", s.page())
	}
	s.Press(saveContinue)
	onStep(s, "Step 2 of 4")
	return s.DocID()
}

// grnDelivery fills Delivery with a delivery challan and goes on
// to Quantities.
func grnDelivery(s *Session, challan string) {
	s.t.Helper()
	noOverflow(s, "Delivery")
	pickRadio(s, "challan_kind", "Delivery challan")
	s.Fill("challan_no", challan)
	s.Press(saveContinue)
	onStep(s, "Step 3 of 4")
	noOverflow(s, "Quantities")
}

// cardInput is the script finding a Quantities input by its
// accessible name, e.g. "Rejected, line 1, MS Angle 50x50x5" (UX §5.4).
func cardInput(name string) string {
	return `document.querySelector('[aria-label=` + strconv.Quote(name) + `]')`
}

// fillCard types value into the card input named name.
func fillCard(s *Session, name, value string) {
	s.t.Helper()
	s.Until(name, `!!`+cardInput(name))
	s.FillID(Eval[string](s, cardInput(name)+`.id`), value)
}

// rejectOn opens Some rejected on PO line n's card and types the rejected
// quantity and its reason.
func rejectOn(s *Session, n int, item, rejected, reason string) {
	s.t.Helper()
	line := ", line " + strconv.Itoa(n) + ", " + item
	summary := cardInput("Rejected"+line) + `.closest("details").querySelector("summary")`
	var c tally
	c.tap(s, "Some rejected on line "+strconv.Itoa(n), summary)
	s.Until("Some rejected open", cardInput("Rejected"+line)+`.closest("details").open`)
	s.Until("Some rejected settled", `document.getAnimations().length === 0`)
	fillCard(s, "Rejected"+line, rejected)
	fillCard(s, "Reason for rejecting"+line, reason)
}

// submitGRN goes on to Check and submits; with no approval rule
// the receipt is approved and numbered at once (UX §5.5).
func submitGRN(s *Session, banner string) {
	s.t.Helper()
	s.Press(saveContinue)
	onStep(s, "Step 4 of 4")
	noOverflow(s, "GRN Check")
	s.Press("Submit")
	s.WaitText(banner)
	noOverflow(s, "the approved GRN")
}

// TestE2E_GRNFlow (GRN-036; GRN spec §11, design system §1.3): at 360 px
// Suresh picks Priya's approved five-line PO on METRO-SITE, enters the
// challan, receives all five lines with line 1 partly rejected and its
// reason, and submits. The GRN is approved and numbered, no step scrolls
// sideways, and every step answers within the harness's step deadline.
// UX §1's under-a-minute goal is measured and reported, not asserted
// (plan S6).
func TestE2E_GRNFlow(t *testing.T) {
	h := Start(t)
	lines := []map[string]string{
		{"item_id": h.idOf("item", "MS-ANGLE-50"), "uom_id": h.idOf("uom", "KGS"), "qty": "1200", "price": "62.5"},
		{"item_id": h.idOf("item", "BOLT-M12"), "uom_id": h.idOf("uom", "NOS"), "qty": "500", "price": "12"},
		{"item_id": h.idOf("item", "WIRE-18G"), "uom_id": h.idOf("uom", "KGS"), "qty": "50", "price": "96"},
		{"item_id": h.idOf("item", "CABLE-4C"), "uom_id": h.idOf("uom", "MTR"), "qty": "100", "price": "450"},
		{"item_id": h.idOf("item", "CEMENT-OPC"), "uom_id": h.idOf("uom", "BAG"), "qty": "40", "price": "1300"},
	}
	r := h.Do(priya, purchase.DocType, 0, doctype.ActionSave, doc.ActionInput{Values: map[string]string{
		"vendor_id": h.idOf("party", "SHREE"), "site_id": h.idOf("site", "METRO-SITE"), "project_id": h.idOf("project", "PRJ-METRO"), "order_date": plainDate,
	}, Lines: lines})
	po := h.Do(priya, purchase.DocType, r.ID, doctype.ActionSubmit, doc.ActionInput{Version: r.Version}).ID // no rule: approved on submit

	s := h.SignIn(suresh, phone360)
	s.Open("/doc/" + goodsreceipt.DocType)
	noOverflow(s, "the Goods Receipts list")
	start := time.Now()
	grn := startGRN(s)
	grnDelivery(s, grnChallan)
	for i, c := range []struct{ name, pending string }{
		{"MS Angle 50x50x5", "1,200"}, {boltName, "500"}, {"Binding Wire 18G", "50"}, {"Armoured Cable 4 core 16 sq mm", "100"}, {"OPC Cement 53 Grade", "40"},
	} {
		if got := Eval[string](s, cardInput("Received now, line "+strconv.Itoa(i+1)+", "+c.name)+`?.value ?? ""`); got != c.pending {
			t.Errorf("%s's Received now = %q, want its pending %q", c.name, got, c.pending)
		}
	}
	rejectOn(s, 1, "MS Angle 50x50x5", "10", grnRejectReason)
	noOverflow(s, "Quantities with a reject")
	submitGRN(s, grnApproved)
	t.Logf("five-line GRN with one reject, New goods receipt to the banner: %v (UX §1 goal under 1m, reported only)", time.Since(start))

	// Stored state (CD-271).
	g := h.load(goodsreceipt.DocType, grn)
	if g.Info.State != doctype.StateApproved || g.Info.Number != firstGRN || len(g.Lines) != 5 {
		t.Fatalf("stored GRN = %s %s with %d lines, want approved %s with 5", g.Info.State, g.Info.Number, len(g.Lines), firstGRN)
	}
	if g.Values["challan_kind"] != "Delivery challan" || g.Values["challan_no"] != grnChallan || g.Values["accepted_value"] != "182175.00" {
		t.Errorf("stored GRN header = %q %q accepted %s, want Delivery challan %s accepted 182175.00",
			g.Values["challan_kind"], g.Values["challan_no"], g.Values["accepted_value"], grnChallan)
	}
	l := g.Lines[0]
	wantQty(t, "line 1 received", l["received_qty"], "1200")
	wantQty(t, "line 1 rejected", l["rejected_qty"], "10")
	wantQty(t, "line 1 accepted", l["accepted_qty"], "1190")
	if l["reject_reason"] != grnRejectReason {
		t.Errorf("line 1 reason = %q, want %q", l["reject_reason"], grnRejectReason)
	}
	h.wantPO("after the GRN", po, doctype.StateApproved, "part_received", "10") // the rejected 10 kg stays pending (CD-234)
}

// TestE2E_RequestToShortClose (GRN-049; research (expense) §4 X1, CD-271,
// after ERPNext test_purchase_order.py:95-122): at 360 px Suresh requests
// 10 Nos of BOLT-M12, Priya creates the PO from it, Ravi approves it
// (₹60,000.00, past the Purchase Manager's ₹50,000 rule), Priya marks it
// sent, Suresh receives 6 on a GRN, and Priya closes the PO with a reason.
// After each step the services read the stored state: after the GRN the PO
// is part received with 4 pending; after the close nothing is pending, a
// new GRN against it is refused with DOC-017's text, and the request shows
// 6 ordered (PO-055's short close).
func TestE2E_RequestToShortClose(t *testing.T) {
	h := startPurchase(t)

	s := h.SignIn(suresh, phone360)
	s.Open("/modules/purchase")
	s.Press("New purchase request")
	onStep(s, "Step 1 of 3")
	s.Pick("site_id", "METRO-SITE")
	s.Fill("required_by", neededBy)
	s.Press(saveContinue)
	onStep(s, "Step 2 of 3")
	pr := s.DocID()
	s.Press(addItem)
	s.WaitText(saveLine)
	s.Pick("item_id", "BOLT-M12")
	s.Fill("qty", "10")
	s.Press(saveLine)
	s.Until("back on Items", `location.pathname.endsWith("/edit/items")`)
	s.Press(saveContinue)
	onStep(s, "Step 3 of 3")
	noOverflow(s, "request Check")
	s.Press("Submit")
	s.WaitText("Submitted. " + firstPR + " has been numbered.")
	if q := h.load(purchase.RequestDocType, pr); q.Info.State != doctype.StateApproved || len(q.Lines) != 1 || q.Values["order_status"] != "not_ordered" {
		t.Fatalf("stored request = %s %s with %d lines, want approved not_ordered with 1", q.Info.State, q.Values["order_status"], len(q.Lines))
	}

	p := h.SignIn(priya, phone360)
	p.Open(DocPath(purchase.RequestDocType, pr))
	p.Press("Create PO")
	p.WaitText("Create a purchase order")
	p.Pick("vendor_id", "SHREE")
	p.Press("Create purchase order")
	p.WaitText("was created from " + firstPR + ".")
	po := p.DocID()
	p.Fill("order_date", fixtureDate)
	p.Press(saveContinue)
	onStep(p, "Step 2 of 4")
	p.Press(saveContinue)
	onStep(p, "Step 3 of 4")
	openLink(p, DocPath(purchase.DocType, po)+"/lines/1")
	p.WaitText(saveLine)
	p.Fill("price", "6,000")
	p.Press(saveLine)
	p.Until("back on Items", `location.pathname.endsWith("/edit/items")`)
	p.WaitText("10 Numbers × ₹6,000.00")
	noOverflow(p, "PO Items")
	p.Press(saveContinue)
	onStep(p, "Step 4 of 4")
	p.WaitText(poApprove)
	p.Press(sendApproval)
	p.WaitText("Submitted for approval to Purchase Manager or Director.")
	if o := h.load(purchase.DocType, po); o.Info.State != doctype.StatePending || len(o.Lines) != 1 {
		t.Fatalf("stored PO after submit = %s with %d lines, want pending with 1", o.Info.State, len(o.Lines))
	}

	r := h.SignIn(ravi, phone360)
	approveFromInbox(r, po, firstNumber)
	h.wantPO("after the approval", po, doctype.StateApproved, "not_received", "10")
	if q := h.load(purchase.RequestDocType, pr); q.Values["order_status"] != "ordered" {
		t.Errorf("request after the approval = %s, want ordered", q.Values["order_status"])
	} else {
		wantQty(t, "request ordered after the approval", q.Lines[0]["ordered_qty"], "10")
	}

	p.Open(DocPath(purchase.DocType, po))
	p.Press("Send to vendor")
	p.WaitText("Record how it was sent")
	clickOn(p, `input[name=sent_via][value=whatsapp]`)
	p.Press("Mark as sent")
	p.WaitText(firstNumber + " marked as sent.")
	var sent bool
	if err := h.App.DB().Read(context.Background(), func(tx *db.Tx) error {
		var err error
		_, sent, err = purchase.SentInfo(context.Background(), tx, po)
		return err
	}); err != nil || !sent {
		t.Errorf("PO sent record after Mark as sent: %v, sent %v; want one", err, sent)
	}

	s.Open("/doc/" + goodsreceipt.DocType)
	grn := startGRN(s)
	grnDelivery(s, grnChallan)
	fillCard(s, "Received now, line 1, "+boltName, "6")
	submitGRN(s, grnApproved)
	if g := h.load(goodsreceipt.DocType, grn); g.Info.State != doctype.StateApproved || len(g.Lines) != 1 {
		t.Fatalf("stored GRN = %s with %d lines, want approved with 1", g.Info.State, len(g.Lines))
	} else {
		wantQty(t, "GRN line 1 accepted", g.Lines[0]["accepted_qty"], "6")
	}
	h.wantPO("after the GRN", po, doctype.StateApproved, "part_received", "4")

	p.Open(DocPath(purchase.DocType, po))
	p.Press("Close")
	p.WaitText("Reason for closing")
	noOverflow(p, "Close the PO")
	p.Fill("reason", shortClose)
	p.Press("Close purchase order")
	p.WaitText("Success: Closed.")
	noOverflow(p, "the closed PO")

	// Stored state after the close (CD-271).
	h.wantPO("after the close", po, doctype.StateClosed, "part_received", "0")
	before := h.grnCount()
	_, err := h.App.Engine().Do(context.Background(), h.Principal(suresh), goodsreceipt.DocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{
			"purchase_order_id": strconv.FormatInt(po, 10), "site_id": h.idOf("site", "METRO-SITE"), "project_id": h.idOf("project", "PRJ-METRO"),
			"receipt_date": plainDate, "challan_kind": "Delivery challan", "challan_no": grnChallan,
		},
		Lines: []map[string]string{{"po_line_id": h.lineIDs(po)[0], "received_qty": "4"}},
	})
	var de *doc.Error
	if !errors.As(err, &de) || de.Kind != doc.KindInvalid || len(de.Fields) != 1 || de.Fields[0].Field != "purchase_order_id" || de.Fields[0].Message != closedPOText {
		t.Errorf("a new GRN on the closed PO: %v, want DOC-017's %q on purchase_order_id", err, closedPOText)
	}
	if got := h.grnCount(); got != before {
		t.Errorf("goods receipts after the refused one = %d, want %d", got, before)
	}
	q := h.load(purchase.RequestDocType, pr)
	wantQty(t, "request ordered after the close", q.Lines[0]["ordered_qty"], "6")
}

// x1Received is X1's PO before the close (GRN-050's Given), through the
// engine: Suresh's submitted request for 10 Nos of BOLT-M12, Priya's PO
// holding its row at ₹6,000.00, approved by Ravi, and Suresh's GRN of 6,
// approved. It returns the PO and GRN ids.
func (h *Harness) x1Received() (po, grn int64) {
	h.t.Helper()
	r := h.Do(suresh, purchase.RequestDocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{"site_id": h.idOf("site", "METRO-SITE"), "project_id": h.idOf("project", "PRJ-METRO"), "request_date": plainDate},
		Lines:  []map[string]string{{"item_id": h.idOf("item", "BOLT-M12"), "uom_id": h.idOf("uom", "NOS"), "qty": "10"}},
	})
	pr := h.Do(suresh, purchase.RequestDocType, r.ID, doctype.ActionSubmit, doc.ActionInput{Version: r.Version}).ID
	row := strconv.FormatInt(Scalar[int64](h, `SELECT id FROM purchase_request_line WHERE parent_id = ?`, pr), 10)
	r = h.Do(priya, purchase.DocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{
			"vendor_id": h.idOf("party", "SHREE"), "site_id": h.idOf("site", "METRO-SITE"), "project_id": h.idOf("project", "PRJ-METRO"), "order_date": plainDate,
		},
		Lines: []map[string]string{{"item_id": h.idOf("item", "BOLT-M12"), "uom_id": h.idOf("uom", "NOS"), "qty": "10", "price": "6000", "request_line_id": row}},
	})
	r = h.Do(priya, purchase.DocType, r.ID, doctype.ActionSubmit, doc.ActionInput{Version: r.Version})
	po = h.Do(ravi, purchase.DocType, r.ID, doctype.ActionApprove, doc.ActionInput{Version: r.Version}).ID
	r = h.Do(suresh, goodsreceipt.DocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{
			"purchase_order_id": strconv.FormatInt(po, 10), "site_id": h.idOf("site", "METRO-SITE"), "project_id": h.idOf("project", "PRJ-METRO"),
			"receipt_date": plainDate, "challan_kind": "Delivery challan", "challan_no": grnChallan,
		},
		Lines: []map[string]string{{"po_line_id": h.lineIDs(po)[0], "received_qty": "6"}},
	})
	grn = h.Do(suresh, goodsreceipt.DocType, r.ID, doctype.ActionSubmit, doc.ActionInput{Version: r.Version}).ID
	return po, grn
}

// TestE2E_GRNCancelRestores (GRN-050; research (expense) §4 X2, CD-271,
// after ERPNext test_purchase_order.py:118-122; GRN-018): with X1's PO and
// its approved 6-unit GRN, before the close, Ravi cancels the GRN with a
// reason in the browser at 360 px; the services then read the PO's pending
// as 10 and its receipt status not received.
func TestE2E_GRNCancelRestores(t *testing.T) {
	h := startPurchase(t)
	po, grn := h.x1Received()
	h.wantPO("before the cancel", po, doctype.StateApproved, "part_received", "4")

	r := h.SignIn(ravi, phone360)
	cancelDoc(r, DocPath(goodsreceipt.DocType, grn), "Cancel goods receipt", reasonGRN)
	r.WaitText("Cancelled")
	noOverflow(r, "the cancelled GRN")

	// Stored state (CD-271).
	if g := h.load(goodsreceipt.DocType, grn); g.Info.State != doctype.StateCancelled {
		t.Errorf("stored GRN = %s, want cancelled", g.Info.State)
	}
	h.wantPO("after the cancel", po, doctype.StateApproved, "not_received", "10")
	wantQty(t, "PO line 1 received after the cancel", h.load(purchase.DocType, po).Lines[0]["received_qty"], "0")
}

// TestE2E_BuyerCashClaim (EXP-067; research (expense) §4 X5, GRN spec §1
// and §12, CD-271): Priya buys site materials for cash and files a claim
// at 360 px with a Site miscellaneous line on METRO-SITE (project
// PRJ-METRO from the site, EXP-068), the bill in the company GSTIN with
// Shree's GSTIN, INV-901 and Billed to PUNE. She has no Approve button on
// her own claim; Ravi, her manager, approves. The expense lines report
// filtered to PRJ-METRO lists the line with the GSTIN and invoice no., and
// the claim moves no PO pending figure and makes no GRN.
func TestE2E_BuyerCashClaim(t *testing.T) {
	h := startExpense(t)
	po := h.metroDraft(priya)
	r := h.load(purchase.DocType, po)
	h.Do(priya, purchase.DocType, po, doctype.ActionSubmit, doc.ActionInput{Version: r.Version}) // no PO rule here: approved
	pendingBefore := h.pendingOf(po)

	p := h.SignIn(priya, phone360)
	claim := newClaim(p, "", "")
	p.Press("Add an expense")
	p.WaitText("Take photo")
	settle(p)
	p.SetFile(claimPhoto, receiptPhoto(t))
	p.Until("the receipt's thumbnail", `!!document.querySelector('#receipt-files img')?.complete`)
	pickRadio(p, "expense_type_id", "Site miscellaneous")
	p.Fill("amount", "1,180")
	openMoreDetails(p) // the line's choices sit in its More details (UI v2 E7)
	openDisclosure(p, "Bill in company GSTIN")
	pickRadio(p, "company_bill", "The bill shows our GSTIN")
	pickRadio(p, "billed_to_id", "PUNE")
	p.Fill("supplier_gstin", shreeGSTIN)
	p.Fill("invoice_no", cashInvoice)
	openDisclosure(p, "Project or site")
	p.Pick("site_id", "METRO-SITE")
	noOverflow(p, "the cash buy's line page")
	p.Press(saveLine)
	p.Until("back on Expenses", `location.pathname.endsWith("/edit/items")`)
	submitClaim(p)
	p.Open(DocPath(expense.ClaimDocType, claim))
	p.WaitText("Pending approval")
	if Eval[bool](p, `!!(`+button("Approve")+`)`) {
		t.Error("Priya is offered Approve on her own claim")
	}

	rv := h.SignIn(ravi, phone360)
	approveFromInbox(rv, claim, firstClaim)

	// The report's place and GST columns are low priority, shown from
	// tablet width (design system §8.1), so it is read on a desktop.
	d := h.SignIn(priya, desktop)
	d.Open("/reports/expense/lines?project=" + h.idOf("project", "PRJ-METRO"))
	for _, want := range []string{firstClaim + " · line 1", shreeGSTIN, cashInvoice, "PRJ-METRO · METRO-SITE"} {
		d.WaitText(want)
	}

	// Stored state (research T10, CD-271).
	c := h.load(expense.ClaimDocType, claim)
	if c.Info.State != doctype.StateApproved || len(c.Lines) != 1 {
		t.Fatalf("stored claim = %s with %d lines, want approved with 1", c.Info.State, len(c.Lines))
	}
	l := c.Lines[0]
	for f, want := range map[string]string{
		"expense_type_id": h.idOf("expense_type", "SITE-MISC"), "site_id": h.idOf("site", "METRO-SITE"), "project_id": h.idOf("project", "PRJ-METRO"),
		"billed_to_id": h.idOf("branch", "PUNE"), "supplier_gstin": shreeGSTIN, "invoice_no": cashInvoice, "amount": "1180.00",
	} {
		if l[f] != want {
			t.Errorf("line 1 %s = %q, want %q", f, l[f], want)
		}
	}
	h.wantApprovedBy(expense.ClaimDocType, claim, ravi)
	if got := h.pendingOf(po); got != pendingBefore {
		t.Errorf("PO pending after the claim = %s, want %s as before", got.Plain(), pendingBefore.Plain())
	}
	if got := h.grnCount(); got != 0 {
		t.Errorf("goods receipts after the claim = %d, want none", got)
	}
}

//go:build e2e

package e2e

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/ui/screens"
)

const (
	arjun     = "arjun@vardhan.test"
	firstPR   = "PR/26-27/00001"
	neededBy  = "05-11-2026"
	notOwner  = "You can only change Purchase Orders you created." // AUTH's 403 on the owner rule
	poApprove = "This purchase order needs approval from a Purchase Manager or Director because it is ₹50,000 or more."
)

// startPurchase is Start with the test seed's purchase.Order approval rules
// (Purchase Manager from ₹50,000, Director from ₹5,00,000; 00-fixtures.md),
// which the harness leaves out because the fixture names "Purchase Order".
func startPurchase(t *testing.T) *Harness {
	t.Helper()
	h := Start(t)
	p := h.Principal(admin)
	ctx := auth.WithPrincipal(context.Background(), p) // the app's role directory reads it, as in approvalRules
	for _, r := range []approval.RuleInput{
		{DocType: purchase.DocType, FromAmount: 50_000_00, ApproverType: approval.ApproverRole, Role: "Purchase Manager"},
		{DocType: purchase.DocType, FromAmount: 5_00_000_00, ApproverType: approval.ApproverRole, Role: "Director"},
	} {
		if _, err := h.App.Approvals().CreateRule(ctx, p, r); err != nil {
			t.Fatalf("approval rule from %d: %v", r.FromAmount, err)
		}
	}
	return h
}

// addArjun adds the second Purchase User (CD-257) with the fixture password.
func (h *Harness) addArjun() {
	h.t.Helper()
	p := purchasetest.InsertArjun(h.t, h.App.DB(), h.Clock)
	h.Fx.Users = append(h.Fx.Users, testkit.FixtureUser{ID: p.ID, Email: arjun, Name: p.Name})
	if err := setPasswords(context.Background(), h.App.DB()); err != nil {
		h.t.Fatalf("set Arjun's password: %v", err)
	}
}

// idOf is the id of the master row with this code, as a plain value.
func (h *Harness) idOf(table, code string) string {
	h.t.Helper()
	return strconv.FormatInt(Scalar[int64](h, `SELECT id FROM `+table+` WHERE code = ?`, code), 10)
}

// metroDraft saves a one-line SHREE draft for METRO-SITE as email.
func (h *Harness) metroDraft(email string) int64 {
	h.t.Helper()
	return h.Do(email, purchase.DocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{
			"vendor_id": h.idOf("party", "SHREE"), "site_id": h.idOf("site", "METRO-SITE"), "project_id": h.idOf("project", "PRJ-METRO"), "order_date": plainDate,
		},
		Lines: []map[string]string{{"item_id": h.idOf("item", "WIRE-18G"), "uom_id": h.idOf("uom", "KGS"), "qty": "10", "price": "96"}},
	}).ID
}

// noOverflow fails when the current page scrolls sideways (research §6.4).
func noOverflow(s *Session, step string) {
	s.t.Helper()
	if bad := Eval[[]string](s, screens.OverflowJS); len(bad) != 0 {
		s.t.Errorf("%s at %d px overflows: %q", step, s.width, bad)
	}
}

// clickOn clicks the visible element sel selects, by position, as Press does.
func clickOn(s *Session, sel string) {
	s.t.Helper()
	find := `(() => {
  const m = [...document.querySelectorAll(` + strconv.Quote(sel) + `)].find((e) => e.checkVisibility());
  if (!m) return [];
  m.scrollIntoView({block: "center", behavior: "instant"});
  const r = m.getBoundingClientRect();
  return [r.x + r.width / 2, r.y + r.height / 2];
})()`
	s.Until("a visible "+sel, `(`+find+`).length === 2`)
	at := Eval[[]float64](s, find)
	if len(at) != 2 {
		s.t.Fatalf("%s went away\n%s", sel, s.page())
	}
	s.Do("click "+sel, chromedp.MouseClickXY(at[0], at[1]))
}

// openLink clicks the link to href in the page's main region.
func openLink(s *Session, href string) {
	s.t.Helper()
	clickOn(s, `main a[href="`+href+`"]`)
	s.Until("the page "+href, `location.pathname === `+strconv.Quote(href))
}

// settle waits for htmx to finish a swap: it wires a swapped-in form and
// applies autofocus only when the swap settles, and a click before then
// posts the form without htmx.
func settle(s *Session) {
	s.t.Helper()
	s.Until("the page to settle", `document.readyState === "complete" && !document.querySelector(".htmx-request, .htmx-swapping, .htmx-settling")`)
}

// onStep waits for a wizard step and for htmx to settle, since a step
// arrives as a swap and focus can move while it settles.
func onStep(s *Session, text string) {
	s.t.Helper()
	s.WaitText(text)
	settle(s)
}

// within reports what took and fails when it is goal or more (UX §1).
func within(t *testing.T, what string, took, goal time.Duration) {
	t.Helper()
	if took >= goal {
		t.Errorf("%s took %v, want under %v", what, took, goal)
		return
	}
	t.Logf("%s: %v (goal under %v)", what, took, goal)
}

// lastBought saves and submits an approved SHREE order on PRJ-METRO dated
// 12-10-2026 under ₹50,000 (no rule, so approved on submit), so the prices
// of CEMENT-OPC and WIRE-18G are the last paid (PO spec §24.2).
func (h *Harness) lastBought() {
	h.t.Helper()
	r := h.Do(priya, purchase.DocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{
			"vendor_id": h.idOf("party", "SHREE"), "site_id": h.idOf("site", "METRO-SITE"), "project_id": h.idOf("project", "PRJ-METRO"), "order_date": "2026-10-12",
		},
		Lines: []map[string]string{
			{"item_id": h.idOf("item", "CEMENT-OPC"), "uom_id": h.idOf("uom", "BAG"), "qty": "10", "price": "1300"},
			{"item_id": h.idOf("item", "WIRE-18G"), "uom_id": h.idOf("uom", "KGS"), "qty": "5", "price": "96"},
		},
	})
	h.Do(priya, purchase.DocType, r.ID, doctype.ActionSubmit, doc.ActionInput{Version: r.Version})
}

// addLine adds a line through the line page, from the Items step.
func addLine(s *Session, item, qty, price string) {
	s.t.Helper()
	s.Press(addItem)
	s.WaitText(saveLine)
	noOverflow(s, "the line page for "+item)
	s.Pick("item_id", item)
	s.Fill("qty", qty)
	s.Fill("price", price)
	s.Press(saveLine)
	s.Until("back on Items", `location.pathname.endsWith("/edit/items")`)
}

// TestE2E_PurchaseOrderFlow (PO-045): Priya raises a two-line PO at 360 px
// through the wizard and submits it; Ravi approves it from his phone. Every
// page passes the overflow check, and the stored order matches.
func TestE2E_PurchaseOrderFlow(t *testing.T) {
	h := startPurchase(t)
	p := h.SignIn(priya, phone360)
	p.Open("/modules/purchase")
	noOverflow(p, "the Purchase home")
	p.Press("New purchase order")
	onStep(p, "Step 1 of 4")
	noOverflow(p, "Details")
	p.Pick("vendor_id", "SHREE")
	p.Pick("site_id", "METRO-SITE")
	p.Fill("order_date", fixtureDate)
	p.Press(saveContinue)
	onStep(p, "Step 2 of 4")
	po := p.DocID()
	noOverflow(p, "Terms")
	p.Fill("vendor_quote_ref", "QT-2026-118")
	p.Press(saveContinue)
	onStep(p, "Step 3 of 4")
	noOverflow(p, "Items, empty")
	addLine(p, "CEMENT-OPC", "40", "1,300")
	addLine(p, "WIRE-18G", "25", "96")
	p.WaitText("40 BAG × ₹1,300.00")
	noOverflow(p, "Items, two lines")
	p.Press(saveContinue)
	onStep(p, "Step 4 of 4")
	p.WaitText(poApprove)
	noOverflow(p, "Check and submit")
	p.Press(sendApproval)
	p.WaitText("Submitted for approval to Purchase Manager or Director.")
	noOverflow(p, "the pending PO page")

	r := h.SignIn(ravi, phone360)
	r.WaitText(draft(po))
	noOverflow(r, "the approval inbox")
	r.Press("Open " + draft(po))
	r.WaitText("Needs your approval")
	noOverflow(r, "the PO page as the approver")
	r.Press("Approve")
	r.WaitText("Approved. " + firstNumber + " has been numbered.")
	noOverflow(r, "the approved PO page")

	// Stored state (CD-271): what the screens did is what the database holds.
	type order struct {
		Number, State, Ref, Date string
		By                       int64
		Net, Lines               int64
	}
	var got order
	if err := h.App.DB().SQL().QueryRowContext(context.Background(),
		`SELECT number, state, vendor_quote_ref, order_date, created_by, net_total, (SELECT count(*) FROM purchase_order_line WHERE parent_id = o.id)
		 FROM purchase_order o WHERE id = ?`, po).Scan(&got.Number, &got.State, &got.Ref, &got.Date, &got.By, &got.Net, &got.Lines); err != nil {
		t.Fatal(err)
	}
	want := order{Number: firstNumber, State: "approved", Ref: "QT-2026-118", Date: plainDate, By: int64(h.Principal(priya).ID), Net: 54_400_00, Lines: 2}
	if got != want {
		t.Errorf("stored order = %+v, want %+v", got, want)
	}
	if got := Scalar[int64](h, `SELECT user_id FROM approval_history WHERE doctype = ? AND doc_id = ? AND action = 'approved'`, purchase.DocType, po); got != int64(h.Principal(ravi).ID) {
		t.Errorf("approved by user %d, want Ravi (%d)", got, h.Principal(ravi).ID)
	}
}

// TestE2E_RequestToSentFlow (PO-068, PUX-029): at 360 px Suresh raises and
// submits a two-line request, Priya creates the PO from it at the last
// prices and submits it, Ravi approves, Priya marks it sent, and Suresh sees
// Ordered. No page scrolls sideways, and the UX §1 times hold.
func TestE2E_RequestToSentFlow(t *testing.T) {
	h := startPurchase(t)
	h.lastBought() // PO/26-27/00001

	s := h.SignIn(suresh, phone360)
	s.Open("/modules/purchase")
	noOverflow(s, "Suresh's Purchase home")
	start := time.Now()
	s.Press("New purchase request")
	onStep(s, "Step 1 of 3")
	noOverflow(s, "request Details")
	s.Pick("site_id", "METRO-SITE")
	s.Fill("required_by", neededBy)
	s.Press(saveContinue)
	onStep(s, "Step 2 of 3")
	pr := s.DocID()
	noOverflow(s, "request Items")
	s.Press(addItem)
	s.WaitText(saveLine)
	noOverflow(s, "the request line page")
	s.Pick("item_id", "CEMENT-OPC")
	s.Fill("qty", "40")
	s.Press(saveLine)
	s.Until("back on Items", `location.pathname.endsWith("/edit/items")`)
	s.Press(addItem)
	s.WaitText(saveLine)
	s.Pick("item_id", "WIRE-18G")
	s.Fill("qty", "25")
	s.Press(saveLine)
	s.Until("back on Items", `location.pathname.endsWith("/edit/items")`)
	s.WaitText("25 KGS")
	s.Press(saveContinue)
	onStep(s, "Step 3 of 3")
	noOverflow(s, "request Check and submit")
	s.Press("Submit")
	s.WaitText("Submitted. " + firstPR + " has been numbered.")
	within(t, "Suresh's two-line request", time.Since(start), 90*time.Second)
	s.WaitText("Not ordered")
	noOverflow(s, "the submitted request")

	p := h.SignIn(priya, phone360)
	p.Open("/modules/purchase")
	p.WaitText("1 request with items to order")
	noOverflow(p, "Priya's Purchase home")
	p.Press("Open Requests to order")
	p.WaitText(firstPR)
	noOverflow(p, "the Requests to order queue")
	openLink(p, DocPath(purchase.RequestDocType, pr))
	noOverflow(p, "the request as the buyer")
	start = time.Now()
	p.Press("Create PO")
	p.WaitText("Create a purchase order")
	noOverflow(p, "Create PO")
	p.Pick("vendor_id", "SHREE")
	p.Press("Create purchase order")
	p.WaitText("was created from " + firstPR + ".")
	po := p.DocID()
	noOverflow(p, "PO Details")
	p.Fill("order_date", fixtureDate)
	p.Press(saveContinue)
	onStep(p, "Step 2 of 4")
	noOverflow(p, "PO Terms")
	p.Press(saveContinue)
	onStep(p, "Step 3 of 4")
	p.WaitText("40 BAG × ₹1,300.00")
	p.WaitText("25 KGS × ₹96.00")
	p.WaitText("Last bought at")
	noOverflow(p, "PO Items")
	p.Press(saveContinue)
	onStep(p, "Step 4 of 4")
	within(t, "Priya from Create PO to Check and submit", time.Since(start), 60*time.Second)
	p.WaitText(poApprove)
	noOverflow(p, "PO Check and submit")
	p.Press(sendApproval)
	p.WaitText("Submitted for approval to Purchase Manager or Director.")

	r := h.SignIn(ravi, phone360)
	r.Press("Open " + draft(po))
	r.WaitText("Needs your approval")
	noOverflow(r, "the PO as the approver")
	r.Press("Approve")
	const number = "PO/26-27/00002"
	r.WaitText("Approved. " + number + " has been numbered.")

	p.Open("/modules/purchase")
	p.WaitText("2 approved orders not sent yet") // with lastBought's
	p.Press("Open To send")
	p.WaitText(number)
	noOverflow(p, "the To send queue")
	openLink(p, DocPath(purchase.DocType, po))
	p.WaitText("Not sent yet")
	p.Press("Send to vendor")
	p.WaitText("Record how it was sent")
	noOverflow(p, "Send to vendor")
	clickOn(p, `input[name=sent_via][value=whatsapp]`)
	p.Press("Mark as sent")
	p.WaitText(number + " marked as sent.")
	p.WaitText("Sent to vendor on " + fixtureDate + " by WhatsApp (Priya Shah)")
	noOverflow(p, "the sent PO")

	s.Open("/modules/purchase")
	s.Press("Open My purchase requests")
	s.WaitText(firstPR)
	openLink(s, DocPath(purchase.RequestDocType, pr))
	s.WaitText("Requested 40 BAG · Ordered 40 BAG · Pending to order 0 BAG")
	if got := Eval[string](s, `[...document.querySelectorAll("main dt")].find((e) => e.textContent.trim() === "Ordered")?.nextElementSibling?.textContent.trim() ?? ""`); got != "Ordered" {
		t.Errorf("Suresh's request reads Ordered %q, want Ordered", got)
	}
	s.WaitText(number)
	noOverflow(s, "the ordered request")

	// Stored state (CD-271).
	if got := Scalar[string](h, `SELECT order_status FROM purchase_request WHERE id = ?`, pr); got != "ordered" {
		t.Errorf("request order_status = %q, want ordered", got)
	}
	if got := Scalar[int](h, `SELECT count(*) FROM purchase_request_line WHERE parent_id = ? AND ordered_qty = qty`, pr); got != 2 {
		t.Errorf("request rows fully ordered = %d, want 2", got)
	}
	if got := Scalar[string](h, `SELECT state || ' ' || number || ' ' || required_by FROM purchase_order WHERE id = ?`, po); got != "approved "+number+" 2026-11-05" {
		t.Errorf("order = %q, want approved %s 2026-11-05", got, number)
	}
	if got := Scalar[int](h, `SELECT count(*) FROM purchase_order_line l JOIN purchase_request_line r ON r.id = l.request_line_id
		WHERE l.parent_id = ? AND r.parent_id = ? AND l.qty = r.qty`, po, pr); got != 2 {
		t.Errorf("order lines on the request's rows = %d, want 2", got)
	}
	if got := Scalar[int64](h, `SELECT user_id FROM approval_history WHERE doctype = ? AND doc_id = ? AND action = 'approved'`, purchase.DocType, po); got != int64(h.Principal(ravi).ID) {
		t.Errorf("approved by user %d, want Ravi", got)
	}
	if got := Scalar[string](h, `SELECT sent_on || ' ' || sent_via || ' ' || sent_by FROM purchase_order_sent WHERE po_id = ?`, po); got != "2026-10-24 whatsapp "+strconv.FormatInt(int64(h.Principal(priya).ID), 10) {
		t.Errorf("sent record = %q, want 2026-10-24 whatsapp by Priya", got)
	}
}

// TestE2E_PUX008_HandOverMidEdit (PUX-008, PO-059): Arjun and Ravi hold
// Priya's draft open and Priya its Terms. Ravi hands it to Arjun with a
// note; Priya's save is then refused with her typed value kept, nothing is
// written, and her reload is the read-only page naming Arjun.
func TestE2E_PUX008_HandOverMidEdit(t *testing.T) {
	h := startPurchase(t)
	h.addArjun()
	po := h.metroDraft(priya)
	page := DocPath(purchase.DocType, po)

	p := h.SignIn(priya, phone360)
	p.Open(page + "/edit/terms")
	p.WaitText("Vendor quote ref")
	p.Fill("vendor_quote_ref", "QT-2026-118")
	a := h.SignIn(arjun, phone360)
	a.Open(page)
	a.WaitText("Owned by Priya Shah.")
	r := h.SignIn(ravi, phone360)
	r.Open(page)
	r.WaitText("Owned by Priya Shah.")

	r.Press("Hand over")
	r.WaitText("Hand over to")
	clickOn(r, `input[name=to][value="`+strconv.FormatInt(int64(h.Principal(arjun).ID), 10)+`"]`)
	r.Fill("note", "Priya is on leave")
	r.Press("Hand over")
	r.WaitText(draft(po) + " is now Arjun Mehta's.")
	if got := Eval[string](r, `[...document.querySelectorAll("main li, main article")].filter((e) => e.textContent.includes("Priya is on leave")).at(-1)?.textContent ?? ""`); !strings.Contains(got, "Ravi Kulkarni") {
		t.Errorf("the last comment = %q, want Ravi's note", got)
	}
	r.Do("open History", chromedp.Click(chromedp.CSS(".doc-audit summary")))
	r.WaitText("Handed over from Priya Shah to Arjun Mehta")

	// CD-259 checks rights before the version, so the old owner's save is
	// AUTH's 403, not DOC-069's 409 as PUX-008 has it (CD-260). htmx
	// swaps no 403, so it must reach her as a notice in #alerts.
	p.Front()
	p.Press(saveContinue)
	p.Until("the refusal in the alerts region", `document.getElementById("alerts").innerText.includes(`+strconv.Quote(notOwner)+`)`)
	if got := Eval[string](p, `document.getElementById("f-vendor_quote_ref").value`); got != "QT-2026-118" {
		t.Errorf("Priya's typed quote ref after the refusal = %q, want it kept", got)
	}
	p.Open(page + "/edit/terms")
	p.Until("the document page", `location.pathname === `+strconv.Quote(page))
	p.WaitText("Owned by Arjun Mehta.")
	if p.Shows("Submit") {
		t.Error("Priya, no longer the owner, is offered Submit")
	}

	// Stored state (CD-271).
	if got := Scalar[int64](h, `SELECT created_by FROM purchase_order WHERE id = ?`, po); got != int64(h.Principal(arjun).ID) {
		t.Errorf("owner = %d, want Arjun", got)
	}
	if got := Scalar[string](h, `SELECT vendor_quote_ref FROM purchase_order WHERE id = ?`, po); got != "" {
		t.Errorf("vendor_quote_ref = %q, want Priya's stale save unwritten", got)
	}
	if got := Scalar[string](h, `SELECT body || ' ' || created_by FROM module_comment WHERE doctype = ? AND record_id = ? ORDER BY id DESC LIMIT 1`, purchase.DocType, po); got != "Priya is on leave "+strconv.FormatInt(int64(h.Principal(ravi).ID), 10) {
		t.Errorf("last comment = %q, want Ravi's note", got)
	}
}

// TestE2E_PUX013_TwoBuyersSameRequestRow (PUX-013, PO-054, review focus
// #1): Priya and Arjun tick the same request row on Add from requests; the
// first returns to Items with the line, the second gets the 422 naming the
// first PO, with the tick and quantity kept.
func TestE2E_PUX013_TwoBuyersSameRequestRow(t *testing.T) {
	h := startPurchase(t)
	h.addArjun()
	pr := h.purchaseRequest()
	row := Scalar[int64](h, `SELECT id FROM purchase_request_line WHERE parent_id = ?`, pr)
	mine, theirs := h.metroDraft(priya), h.metroDraft(arjun)
	// By name: the 422 redraw gives the inputs its error ids (f-lines-{n}-…).
	tick := `input[name=rq_line][value="` + strconv.FormatInt(row, 10) + `"]`
	qty := `input[name=rq_qty_` + strconv.FormatInt(row, 10) + `]`

	p := h.SignIn(priya, phone360)
	p.Open(DocPath(purchase.DocType, mine) + "/edit/items?from=requests")
	p.WaitText("Pending 100 KGS")
	a := h.SignIn(arjun, phone360)
	a.Open(DocPath(purchase.DocType, theirs) + "/edit/items?from=requests")
	a.WaitText("Pending 100 KGS")
	clickOn(a, tick)
	a.Do("type 60", chromedp.Evaluate[chromedp.Void](`(() => { const e = document.querySelector("`+qty+`"); e.focus(); e.select(); })()`), chromedp.KeyEvent("60"))

	p.Front()
	clickOn(p, tick)
	p.Press("Add selected")
	p.Until("back on Items", `location.pathname.endsWith("/edit/items") && !location.search.includes("from=requests")`)
	p.WaitText("100 KGS")
	p.WaitText("MS Angle 50x50x5")

	a.Front()
	a.Press("Add selected")
	a.WaitText("Line 2: Request " + firstPR + " line 1 is already on " + draft(mine) + ".")
	if !Eval[bool](a, `document.querySelector(`+strconv.Quote(tick)+`).checked`) {
		t.Error("Arjun's tick is gone after the 422")
	}
	if got := Eval[string](a, `document.querySelector(`+strconv.Quote(qty)+`).value`); got != "60" {
		t.Errorf("Arjun's quantity after the 422 = %q, want 60", got)
	}

	// Stored state (CD-271).
	if got := Scalar[int64](h, `SELECT request_line_id FROM purchase_order_line WHERE parent_id = ? AND line_no = 2`, mine); got != row {
		t.Errorf("Priya's line 2 request row = %d, want %d", got, row)
	}
	if got := Scalar[int](h, `SELECT count(*) FROM purchase_order_line WHERE parent_id = ?`, theirs); got != 1 {
		t.Errorf("Arjun's draft has %d lines, want 1 (nothing written)", got)
	}
}

// TestE2E_PUX019_CommentBox (PUX-019, PO-061): an empty and a
// 1,001-character comment show their messages under the box with the text
// kept; a valid one is appended, the box cleared and focused, and announced.
func TestE2E_PUX019_CommentBox(t *testing.T) {
	h := startPurchase(t)
	po := h.metroDraft(priya)
	p := h.SignIn(priya, phone360)
	p.Open(DocPath(purchase.DocType, po) + "/edit/check")
	p.WaitText("No comments yet.")
	box := `document.getElementById("f-body")`
	underBox := func(text string) string {
		return box + `.closest("form").innerText.includes(` + strconv.Quote(text) + `)`
	}

	p.Press("Post comment")
	p.Until("the empty comment's message under the box", underBox("Write a comment first."))
	settle(p)

	// The box's maxlength stops typing past 1,000; a pasted or scripted
	// value is the server's to refuse.
	long := strings.Repeat("a", 1001)
	p.Do("put 1,001 characters in the box", chromedp.Evaluate[chromedp.Void](box+`.value = `+strconv.Quote(long)))
	p.Press("Post comment")
	p.Until("the long comment's message under the box", underBox("A comment can be at most 1,000 characters."))
	settle(p)
	if got := Eval[int](p, box+`.value.length`); got != 1001 {
		t.Errorf("box after the 422 holds %d characters, want the 1,001 typed", got)
	}

	p.FillID("f-body", "Please check the rate")
	p.Press("Post comment")
	p.WaitText("Comment posted.")
	settle(p)
	p.WaitGone("No comments yet.")
	p.WaitText("Please check the rate")
	if got := Eval[string](p, box+`.value`); got != "" {
		t.Errorf("box after the post = %q, want it cleared", got)
	}
	if got := Eval[string](p, `document.activeElement.id`); got != "f-body" {
		t.Errorf("focus after the post is on %q, want the box", got)
	}

	// Stored state (CD-271).
	if got := Scalar[string](h, `SELECT group_concat(body, '|') FROM module_comment WHERE doctype = ? AND record_id = ?`, purchase.DocType, po); got != "Please check the rate" {
		t.Errorf("stored comments = %q, want only the valid one", got)
	}
}

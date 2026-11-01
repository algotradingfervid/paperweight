//go:build e2e

package e2e

import (
	"context"
	"strconv"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// The expense flows end to end (MOD-EXP S6): EXP-038 and the research flows
// EXP-063 to EXP-066, each ending with a stored-state check (research T10).

const (
	claimPhoto = "#receipt input[type=file][capture]" // the line page's Take photo
	firstAdv   = "EA/26-27/00001"
	phoneTall  = 640 // UX §5.5's tap table is at 360 × 640
	kavya      = "kavya@vardhan.test"
)

// startExpense is Start with expensetest's approval rules for both DocTypes
// (₹0, reporting manager, fallback role Fallback Approver), which the harness
// leaves out because the fixture names "Expense Claim", and Meena with the
// Employee role too (spec §15 item 3).
func startExpense(t *testing.T) *Harness {
	t.Helper()
	h := Start(t)
	p := h.Principal(admin)
	ctx := auth.WithPrincipal(context.Background(), p) // the app's role directory reads it, as in approvalRules
	for _, dt := range []string{expense.ClaimDocType, expense.AdvanceDocType} {
		if _, err := h.App.Approvals().CreateRule(ctx, p, approval.RuleInput{
			DocType: dt, ApproverType: approval.ApproverManager, FallbackRole: expense.RoleFallbackApprover,
		}); err != nil {
			t.Fatalf("approval rule for %s: %v", dt, err)
		}
	}
	h.giveRole(meena, auth.RoleEmployee)
	return h
}

// giveRole gives the fixture user with email the named role.
func (h *Harness) giveRole(email, role string) {
	h.t.Helper()
	ctx := context.Background()
	err := h.App.DB().Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, `INSERT OR IGNORE INTO user_roles (user_id, role_id)
			SELECT u.id, r.id FROM users u, roles r WHERE u.email = ? AND r.name_key = lower(?)`, email, role)
		return err
	})
	if err != nil {
		h.t.Fatalf("give %s %s: %v", email, role, err)
	}
}

// load is the stored document, through the engine.
func (h *Harness) load(dt string, id int64) doc.Record {
	h.t.Helper()
	var r doc.Record
	err := h.App.DB().Read(context.Background(), func(tx *db.Tx) error {
		var err error
		r, err = h.App.Engine().Load(context.Background(), tx, dt, id)
		return err
	})
	if err != nil {
		h.t.Fatalf("load %s %d: %v", dt, id, err)
	}
	return r
}

// tally counts a person's taps and scrolls, as UX §5.5's tap table does.
type tally struct{ taps, scrolls int }

// reach brings the visible element find (a script expression) within reach
// of the thumb: on screen and not under the sticky bar, else one scroll
// brings it to the middle. It returns the element's centre.
func (c *tally) reach(s *Session, what, find string) []float64 {
	s.t.Helper()
	box := `(() => {
  const m = ` + find + `;
  if (!m || !m.checkVisibility()) return [];
  const r = m.getBoundingClientRect(), x = r.x + r.width / 2, y = r.y + r.height / 2;
  const hit = y >= 0 && y < innerHeight ? document.elementFromPoint(x, y) : null;
  return [x, y, hit && (m === hit || m.contains(hit) || hit.closest("label") === m) ? 1 : 0];
})()`
	s.Until(what+" on the page", `(`+box+`).length === 3`)
	at := Eval[[]float64](s, box)
	if len(at) == 3 && at[2] == 1 {
		return at[:2]
	}
	c.scrolls++
	s.Do("scroll to "+what, chromedp.Evaluate[chromedp.Void](`(`+find+`).scrollIntoView({block: "center", behavior: "instant"})`))
	at = Eval[[]float64](s, box)
	if len(at) != 3 || at[2] != 1 {
		s.t.Fatalf("%s can't be tapped after a scroll: %v\n%s", what, at, s.page())
	}
	return at[:2]
}

// tap taps the element find, counting the tap and any scroll.
func (c *tally) tap(s *Session, what, find string) {
	s.t.Helper()
	at := c.reach(s, what, find)
	c.taps++
	s.Do("tap "+what, chromedp.MouseClickXY(at[0], at[1]))
}

// button is the script finding the visible, enabled button or link labelled label.
func button(label string) string {
	return `[...document.querySelectorAll("button, a")].find((e) => e.checkVisibility() && !e.disabled && e.textContent.replace(/\s+/g, " ").trim() === ` + strconv.Quote(label) + `)`
}

// choice is the script finding the label row of radio name's option text.
func choice(name, text string) string {
	return `[...document.querySelectorAll("input[name=` + name + `]")].map((e) => e.closest("label")).find((l) => l && l.textContent.replace(/\s+/g, " ").trim() === ` + strconv.Quote(text) + `)`
}

// TestE2E_ExpenseFlow (EXP-038, EUX-001): at 360 px Deepak starts a claim
// and adds one expense with a photo in no more taps and scrolls than UX
// §5.5's table (7 taps and 1 scroll at 360 × 640: Add an expense, Take
// photo, shutter, confirm, Food, Amount, Save line), submits, and Priya
// approves it. No screen scrolls sideways, and the stored claim matches.
func TestE2E_ExpenseFlow(t *testing.T) {
	h := startExpense(t)
	d := h.SignIn(deepak, phone360)
	d.Open("/doc/" + expense.ClaimDocType)
	noOverflow(d, "the Expense Claims list")
	d.Press("New expense claim")
	onStep(d, "Step 1 of 3")
	noOverflow(d, "Claim details")
	d.Fill("purpose", "Pune Metro site visit")
	d.Press(saveContinue)
	onStep(d, "Step 2 of 3")
	claim := d.DocID()
	noOverflow(d, "Expenses, empty")

	d.Do("a 360 × 640 phone", chromedp.EmulateViewport(phone360, phoneTall, chromedp.EmulateMobile, chromedp.EmulateTouch))
	var c tally
	c.tap(d, "Add an expense", button("Add an expense"))
	d.WaitText("Take photo")
	settle(d)
	noOverflow(d, "the expense line page")
	c.reach(d, "Take photo", `document.querySelector(`+strconv.Quote(claimPhoto)+`).closest("label")`)
	c.taps += 3 // Take photo, the shutter and the phone's own confirm hand over the file
	d.SetFile(claimPhoto, receiptPhoto(t))
	// A person taps the type while it uploads; here the upload ends first,
	// because its thumbnail moves the form down under a tap in flight.
	d.Until("the receipt's thumbnail", `!!document.querySelector('#receipt-files img[alt="receipt.jpg"]')?.complete`)
	c.tap(d, "Food", choice("expense_type_id", "Food"))
	d.Until("Food chosen", `(`+choice("expense_type_id", "Food")+`).querySelector("input").checked`)
	c.tap(d, "Amount", `document.getElementById("f-amount")`)
	d.Keys("450")
	c.tap(d, "Save line", button("Save line"))
	d.Until("back on Expenses", `location.pathname.endsWith("/edit/items")`)
	d.WaitText("₹450.00")
	if c.taps > 7 || c.scrolls > 1 {
		t.Errorf("one expense with a photo took %d taps and %d scrolls, want at most 7 and 1 (UX §5.5)", c.taps, c.scrolls)
	}
	t.Logf("one expense with a photo: %d taps, %d scrolls", c.taps, c.scrolls)
	for _, want := range []string{"Food", fixtureDate} {
		d.WaitText(want)
	}
	if !Eval[bool](d, `!!document.querySelector('main a[href^="/files/"] img')`) {
		t.Error("the expense card has no thumbnail linking to its receipt")
	}
	noOverflow(d, "Expenses, one line")
	d.Press(saveContinue)
	onStep(d, "Step 3 of 3")
	d.WaitText("This expense claim needs approval from Priya Shah.")
	noOverflow(d, "Check and submit")
	d.Press(sendApproval)
	d.WaitText("Pending approval")
	noOverflow(d, "the pending claim")

	p := h.SignIn(priya, phone360)
	p.WaitText(draft(claim))
	noOverflow(p, "the approval inbox")
	p.Press("Open " + draft(claim))
	p.WaitText("Needs your approval")
	noOverflow(p, "the claim as the approver")
	p.Press("Approve")
	p.WaitText("Approved. " + firstClaim + " has been numbered.")
	noOverflow(p, "the approved claim")

	// Stored state (research T10).
	r := h.load(expense.ClaimDocType, claim)
	if r.Info.Number != firstClaim || r.Info.State != doctype.StateApproved || r.Values["total"] != "450.00" || len(r.Lines) != 1 {
		t.Errorf("stored claim = %s %s total %s with %d lines, want %s approved total 450.00 with 1 line",
			r.Info.Number, r.Info.State, r.Values["total"], len(r.Lines), firstClaim)
	}
	if got := Scalar[string](h, `SELECT sha256 FROM attachment WHERE parent_doctype = ? AND parent_id = ?`, expense.ClaimDocType, claim); len(r.Lines) == 1 && r.Lines[0]["receipt_sha256"] != got {
		t.Errorf("line 1 receipt_sha256 = %q, want the photo's %q", r.Lines[0]["receipt_sha256"], got)
	}
	h.wantApprovedBy(expense.ClaimDocType, claim, priya)
}

// wantApprovedBy fails unless email approved the document.
func (h *Harness) wantApprovedBy(dt string, id int64, email string) {
	h.t.Helper()
	if got, want := Scalar[int64](h, `SELECT user_id FROM approval_history WHERE doctype = ? AND doc_id = ? AND action = 'approved'`, dt, id), int64(h.Principal(email).ID); got != want {
		h.t.Errorf("%s %d approved by user %d, want %s (%d)", dt, id, got, email, want)
	}
}

// openDisclosure opens the closed disclosure whose summary reads summary.
func openDisclosure(s *Session, summary string) {
	s.t.Helper()
	find := `[...document.querySelectorAll("details > summary")].find((e) => e.checkVisibility() && e.textContent.trim() === ` + strconv.Quote(summary) + `)`
	s.Until("the disclosure "+summary, `!!(`+find+`)`)
	if !Eval[bool](s, `(`+find+`).parentElement.open`) {
		var c tally
		c.tap(s, summary, find)
		s.Until(summary+" open", `(`+find+`).parentElement.open`)
		// The content grows in with a transition; a tap before it ends can
		// land on the field below.
		s.Until(summary+" settled", `document.getAnimations().length === 0`)
	}
}

// pickRadio taps the radio of field name whose label starts with text.
func pickRadio(s *Session, name, text string) {
	s.t.Helper()
	row := `[...document.querySelectorAll("input[name=` + name + `]")].map((e) => e.closest("label")).find((l) => l && l.textContent.replace(/\s+/g, " ").trim().startsWith(` + strconv.Quote(text) + `))`
	var c tally
	c.tap(s, text, row)
	s.Until(text+" chosen", `(`+row+`).querySelector("input").checked`)
}

// newAdvance raises a one-line advance of amount as s's user, from the list
// to Submit for approval, and returns its id.
func newAdvance(s *Session, amount string) int64 {
	s.t.Helper()
	s.Open("/doc/" + expense.AdvanceDocType)
	noOverflow(s, "the Employee Advances list")
	s.Press("New employee advance")
	onStep(s, "Step 1 of 3")
	noOverflow(s, "advance Details")
	s.Fill("advance_date", fixtureDate)
	s.Fill("purpose", "Pune Metro site visit")
	s.Press(saveContinue)
	onStep(s, "Step 2 of 3")
	id := s.DocID()
	s.Press("Add an item")
	s.WaitText(saveLine)
	noOverflow(s, "the advance line page")
	s.Pick("expense_type_id", "TRAVEL")
	s.Fill("description", "Bus and local travel")
	s.Fill("line_amount", amount)
	s.Press(saveLine)
	s.Until("back on Items", `location.pathname.endsWith("/edit/items")`)
	s.Press(saveContinue)
	onStep(s, "Step 3 of 3")
	noOverflow(s, "advance Check and submit")
	s.Press(sendApproval)
	s.WaitText("Pending approval")
	return id
}

// approveFromInbox opens draft id from s's approval inbox and approves it,
// numbered number.
func approveFromInbox(s *Session, id int64, number string) {
	s.t.Helper()
	s.Open("/approvals")
	s.WaitText(draft(id))
	s.Press("Open " + draft(id))
	s.WaitText("Needs your approval")
	noOverflow(s, "the approver's page")
	s.Press("Approve")
	s.WaitText("Approved. " + number + " has been numbered.")
}

// markPaid marks the advance paid today from its page, as Meena does, and
// waits for the advance page with the flash (UX §5.18).
func markPaid(m *Session, adv int64, label string) {
	m.t.Helper()
	m.Open(DocPath(expense.AdvanceDocType, adv))
	m.Press("Mark as paid")
	m.WaitText("Mark " + label + " as paid")
	noOverflow(m, "Mark as paid")
	m.Press("Mark as paid")
	m.Until("back on the advance", `location.pathname === `+strconv.Quote(DocPath(expense.AdvanceDocType, adv)))
	m.WaitText("Marked as paid.")
	noOverflow(m, "the paid advance")
}

// newClaim starts a claim from the list as s's user, set against the
// advance labelled adv with Less advance less when adv isn't "", and
// returns its id on the Expenses step.
func newClaim(s *Session, adv, less string) int64 {
	s.t.Helper()
	s.Open("/doc/" + expense.ClaimDocType)
	s.Press("New expense claim")
	onStep(s, "Step 1 of 3")
	s.Fill("purpose", "Pune Metro site visit")
	if adv != "" {
		openDisclosure(s, "Set against an advance")
		pickRadio(s, "advance_id", adv)
		if less != "" {
			s.Fill("less_advance", less)
		}
	}
	noOverflow(s, "Claim details")
	s.Press(saveContinue)
	onStep(s, "Step 2 of 3")
	return s.DocID()
}

// addExpense adds one expense from the Expenses step: a photo when photo
// isn't "", else the reason under No receipt.
func addExpense(s *Session, typ, amount, photo, reason string) {
	s.t.Helper()
	s.Press("Add an expense")
	s.WaitText("Take photo")
	settle(s)
	if photo != "" {
		s.SetFile(claimPhoto, photo)
		s.Until("the receipt's thumbnail", `!!document.querySelector('#receipt-files img')?.complete`)
	}
	pickRadio(s, "expense_type_id", typ)
	s.Fill("amount", amount)
	if reason != "" {
		openDisclosure(s, "No receipt")
		s.Fill("no_receipt_reason", reason)
	}
	s.Press(saveLine)
	s.Until("back on Expenses", `location.pathname.endsWith("/edit/items")`)
}

// submitClaim goes on from the Expenses step to Check and submit, checks
// want there and submits.
func submitClaim(s *Session, want ...string) {
	s.t.Helper()
	s.Press(saveContinue)
	onStep(s, "Step 3 of 3")
	for _, w := range want {
		s.WaitText(w)
	}
	noOverflow(s, "Check and submit")
	s.Press(sendApproval)
	s.WaitText("Pending approval")
}

// paidAdvance is Deepak's advance of amount (plain), submitted, approved by
// Priya and marked as paid on its date by expensetest.PaidAdvance's fixture
// query, for a case whose Given it is.
func (h *Harness) paidAdvance(amount string) int64 {
	h.t.Helper()
	r := h.Do(deepak, expense.AdvanceDocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{"employee_id": strconv.FormatInt(h.employee(deepak), 10), "advance_date": plainDate, "purpose": "Pune Metro site visit"},
		Lines:  []map[string]string{{"expense_type_id": h.idOf("expense_type", "TRAVEL"), "line_amount": amount}},
	})
	r = h.Do(deepak, expense.AdvanceDocType, r.ID, doctype.ActionSubmit, doc.ActionInput{Version: r.Version})
	h.Do(priya, expense.AdvanceDocType, r.ID, doctype.ActionApprove, doc.ActionInput{Version: r.Version})
	if _, err := h.App.DB().SQL().ExecContext(context.Background(), `UPDATE employee_advance SET paid_on = advance_date WHERE id = ?`, r.ID); err != nil {
		h.t.Fatalf("mark advance %d paid: %v", r.ID, err)
	}
	return r.ID
}

// effects counts the advance's "effect" audit entries (a claim's approval
// changing its used amount).
func (h *Harness) effects(adv int64) int {
	h.t.Helper()
	return Scalar[int](h, `SELECT count(*) FROM audit_log WHERE doc_type = ? AND doc_id = ? AND action = 'effect'`, expense.AdvanceDocType, adv)
}

// TestE2E_PaidFlashAfterHTMXPost: Mark as paid and Undo paid post through
// htmx, and each lands on the advance page, at its address, with its flash
// (UX §5.18, EUX-040, EUX-042; the AD's fix in S6).
func TestE2E_PaidFlashAfterHTMXPost(t *testing.T) {
	h := startExpense(t)
	adv := h.paidAdvance("10000.00")
	if _, err := h.App.DB().SQL().ExecContext(context.Background(), `UPDATE employee_advance SET paid_on = NULL WHERE id = ?`, adv); err != nil {
		t.Fatal(err)
	}
	m := h.SignIn(meena, phone360)
	markPaid(m, adv, firstAdv)
	m.Press("Undo paid")
	m.WaitText("Undo paid on " + firstAdv + "?")
	m.Press("Undo paid")
	m.Until("back on the advance", `location.pathname === `+strconv.Quote(DocPath(expense.AdvanceDocType, adv)))
	m.WaitText("No longer marked as paid.")
	m.WaitText("Paid on Not paid yet")
}

// TestE2E_AdvanceTwoClaimsClose (EXP-063, research X3): at 360 px Deepak
// requests ₹10,000.00, Priya approves it, Meena marks it paid; two claims
// set ₹6,000.00 and ₹2,500.00 against it and are approved, the advance page
// showing what is used and left after each; Meena closes it, and a third
// claim isn't offered it. The service shows used ₹8,500.00, closed, and two
// effect entries.
func TestE2E_AdvanceTwoClaimsClose(t *testing.T) {
	h := startExpense(t)
	d := h.SignIn(deepak, phone360)
	adv := newAdvance(d, "10,000")
	p := h.SignIn(priya, phone360)
	approveFromInbox(p, adv, firstAdv)
	m := h.SignIn(meena, phone360)
	markPaid(m, adv, firstAdv)

	claim1 := newClaim(d, firstAdv+" · ₹10,000.00 left", "6,000")
	addExpense(d, "Travel", "6,000", "", "Bus ticket lost")
	submitClaim(d)
	approveFromInbox(p, claim1, firstClaim)
	d.Open(DocPath(expense.AdvanceDocType, adv))
	d.WaitText("Used ₹6,000.00")
	d.WaitText("Left ₹4,000.00")
	noOverflow(d, "the advance with one claim")

	claim2 := newClaim(d, firstAdv+" · ₹4,000.00 left", "2,500")
	addExpense(d, "Food", "2,500", "", "Paid in cash at the site canteen")
	submitClaim(d)
	approveFromInbox(p, claim2, "EX/26-27/00002")
	d.Open(DocPath(expense.AdvanceDocType, adv))
	d.WaitText("Used ₹8,500.00")
	d.WaitText("Left ₹1,500.00")

	m.Open(DocPath(expense.AdvanceDocType, adv))
	m.Press("Close")
	m.WaitText("Reason for closing")
	noOverflow(m, "Close the advance")
	m.Fill("reason", "₹1,500 returned by NEFT 1234")
	m.Press("Close employee advance")
	m.WaitText("₹1,500 returned by NEFT 1234")
	noOverflow(m, "the closed advance")

	d.Open("/doc/" + expense.ClaimDocType)
	d.Press("New expense claim")
	onStep(d, "Step 1 of 3")
	if Eval[bool](d, `document.querySelector("main").textContent.includes(`+strconv.Quote(firstAdv)+`)`) {
		t.Errorf("a third claim's details step offers the closed %s", firstAdv)
	}

	// Stored state (research T10).
	a := h.load(expense.AdvanceDocType, adv)
	if a.Info.State != doctype.StateClosed || a.Values["used_amount"] != "8500.00" {
		t.Errorf("stored advance = %s used %s, want closed used 8500.00", a.Info.State, a.Values["used_amount"])
	}
	if got := h.effects(adv); got != 2 {
		t.Errorf("effect audit entries on the advance = %d, want 2", got)
	}
	for id, less := range map[int64]string{claim1: "6000.00", claim2: "2500.00"} {
		if c := h.load(expense.ClaimDocType, id); c.Info.State != doctype.StateApproved || c.Values["less_advance"] != less {
			t.Errorf("claim %d = %s less advance %s, want approved %s", id, c.Info.State, c.Values["less_advance"], less)
		}
	}
}

// TestE2E_ClaimAboveAdvance (EXP-064, research X4): Deepak's claim of
// ₹12,000.00 against his paid ₹10,000.00 advance sets the whole advance off
// and pays the balance: the check page and the print say so, and the
// service shows the advance used up.
func TestE2E_ClaimAboveAdvance(t *testing.T) {
	h := startExpense(t)
	adv := h.paidAdvance("10000.00")
	d := h.SignIn(deepak, phone360)
	claim := newClaim(d, firstAdv+" · ₹10,000.00 left", "")
	addExpense(d, "Lodging", "12,000", receiptPhoto(t), "")
	submitClaim(d, "Less advance ("+firstAdv+") −₹10,000.00", "To pay ₹2,000.00")

	p := h.SignIn(priya, phone360)
	approveFromInbox(p, claim, firstClaim)
	d.Open(printPrefix + expense.ClaimDocType + "/" + strconv.FormatInt(claim, 10))
	d.WaitText("Rupees Two Thousand Only")

	// Stored state (research T10).
	c := h.load(expense.ClaimDocType, claim)
	if c.Info.State != doctype.StateApproved || c.Values["less_advance"] != "10000.00" || c.Values["net_payable"] != "2000.00" {
		t.Errorf("stored claim = %s less %s to pay %s, want approved 10000.00 2000.00", c.Info.State, c.Values["less_advance"], c.Values["net_payable"])
	}
	if a := h.load(expense.AdvanceDocType, adv); a.Values["used_amount"] != "10000.00" {
		t.Errorf("advance used = %s, want 10000.00", a.Values["used_amount"])
	}
}

// TestE2E_DirectorClaimFallback (EXP-065, research X6): Meena, who has no
// reporting manager, files a claim; with Fallback Approver held by her and
// Kavya, she has no Approve button on it and Kavya approves it.
func TestE2E_DirectorClaimFallback(t *testing.T) {
	h := startExpense(t)
	h.giveRole(meena, expense.RoleFallbackApprover)
	h.giveRole(kavya, expense.RoleFallbackApprover)
	m := h.SignIn(meena, phone360)
	claim := newClaim(m, "", "")
	addExpense(m, "Travel", "1,800", receiptPhoto(t), "")
	submitClaim(m)
	m.Open(DocPath(expense.ClaimDocType, claim))
	m.WaitText("Pending approval")
	if Eval[bool](m, `!!(`+button("Approve")+`)`) {
		t.Error("Meena is offered Approve on her own claim")
	}

	k := h.SignIn(kavya, phone360)
	approveFromInbox(k, claim, firstClaim)

	// Stored state (research T10).
	if c := h.load(expense.ClaimDocType, claim); c.Info.State != doctype.StateApproved || c.Info.Number != firstClaim {
		t.Errorf("stored claim = %s %s, want approved %s", c.Info.State, c.Info.Number, firstClaim)
	}
	h.wantApprovedBy(expense.ClaimDocType, claim, kavya)
}

// TestE2E_RejectReviseReceipt (EXP-066, research X7): Priya rejects
// Deepak's pending claim whose line 2 has a reason instead of a receipt;
// Deepak revises, adds a photo to line 2 and resubmits; Priya approves. The
// service shows line 2 with its receipt and no reason.
func TestE2E_RejectReviseReceipt(t *testing.T) {
	h := startExpense(t)
	typ := h.idOf("expense_type", "TRAVEL")
	r := h.Do(deepak, expense.ClaimDocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{"employee_id": strconv.FormatInt(h.employee(deepak), 10), "claim_date": plainDate, "purpose": "Pune Metro site visit"},
		Lines: []map[string]string{
			{"expense_date": plainDate, "expense_type_id": typ, "amount": "300.00", "no_receipt_reason": "Auto rickshaw, no bill given"},
			{"expense_date": plainDate, "expense_type_id": typ, "amount": "1250.00", "no_receipt_reason": "Lost it"},
		},
	})
	claim := h.Do(deepak, expense.ClaimDocType, r.ID, doctype.ActionSubmit, doc.ActionInput{Version: r.Version}).ID

	p := h.SignIn(priya, phone360)
	p.Open("/approvals")
	p.Press("Open " + draft(claim))
	p.WaitText("Needs your approval")
	p.Press("Reject")
	p.WaitText("Reason for rejecting")
	noOverflow(p, "Reject")
	p.Fill("reason", "Line 2 has no receipt")
	p.Press("Reject and send back")
	p.WaitText("Rejected")

	d := h.SignIn(deepak, phone360)
	d.Open(DocPath(expense.ClaimDocType, claim))
	d.WaitText("Line 2 has no receipt")
	noOverflow(d, "the rejected claim")
	d.Press("Revise")
	onStep(d, "Step 3 of 3")
	d.Open(DocPath(expense.ClaimDocType, claim) + "/edit/items")
	onStep(d, "Step 2 of 3")
	openLink(d, DocPath(expense.ClaimDocType, claim)+"/lines/2")
	d.WaitText("Take photo")
	settle(d)
	d.SetFile(claimPhoto, receiptPhoto(t))
	d.Until("the receipt's thumbnail", `!!document.querySelector('#receipt-files img')?.complete`)
	d.Press(saveLine)
	d.Until("back on Expenses", `location.pathname.endsWith("/edit/items")`)
	submitClaim(d)

	approveFromInbox(p, claim, firstClaim)

	// Stored state (research T10).
	c := h.load(expense.ClaimDocType, claim)
	if c.Info.State != doctype.StateApproved || len(c.Lines) != 2 {
		t.Fatalf("stored claim = %s with %d lines, want approved with 2", c.Info.State, len(c.Lines))
	}
	if l := c.Lines[1]; l["receipt_sha256"] == "" || l["no_receipt_reason"] != "" {
		t.Errorf("line 2 receipt_sha256 %q, reason %q; want a receipt and no reason", l["receipt_sha256"], l["no_receipt_reason"])
	}
}

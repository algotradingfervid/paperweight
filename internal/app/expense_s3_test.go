package app_test

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
)

// MOD-EXP S3 (UX §5.7 to §5.12, §5.18): the claim and advance pages, their
// reason pages and the Mark as paid and Undo paid pages, on the real app.

const s3Advance = "EA/26-27/00001"

// expWorld is a canonical world with Deepak's expense documents. With no
// approval rule a submit approves at once; withRules adds spec §4's.
type expWorld struct {
	*world
	admin  auth.Principal
	travel string
}

func newExpWorld(t *testing.T) *expWorld {
	t.Helper()
	w := newWorld(t)
	admin, _, _ := w.canonical()
	return &expWorld{world: w, admin: admin, travel: w.text(`SELECT CAST(id AS TEXT) FROM expense_type WHERE code = 'TRAVEL'`)}
}

func (e *expWorld) p(id auth.UserID) auth.Principal { return e.principal(id) }

// withRules adds the reporting-manager rule for dt.
func (e *expWorld) withRules(dt string) {
	e.t.Helper()
	ctx := auth.WithPrincipal(context.Background(), e.admin)
	if _, err := e.app.Approvals().CreateRule(ctx, e.admin, approval.RuleInput{
		DocType: dt, ApproverType: approval.ApproverManager, FallbackRole: expense.RoleFallbackApprover,
	}); err != nil {
		e.t.Fatalf("approval rule for %s: %v", dt, err)
	}
}

func (e *expWorld) do(p auth.Principal, dt string, id int64, a doctype.Action, in doc.ActionInput) doc.Result {
	e.t.Helper()
	res, err := e.app.Engine().Do(context.Background(), p, dt, id, a, in)
	if err != nil {
		e.t.Fatalf("%s %s %d: %v", a, dt, id, err)
	}
	return res
}

// advance is Deepak's advance of amount dated 20-10-2026, submitted.
func (e *expWorld) advance(amount string) doc.Result {
	e.t.Helper()
	deepak := e.p(deepakID)
	res := e.do(deepak, expense.AdvanceDocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{"advance_date": "2026-10-20", "purpose": "Pune Metro site visit"},
		Lines:  []map[string]string{{"expense_type_id": e.travel, "line_amount": amount, "description": "Bus and local travel"}},
	})
	return e.do(deepak, expense.AdvanceDocType, res.ID, doctype.ActionSubmit, doc.ActionInput{Version: res.Version})
}

// draftAdvance is Deepak's unsaved-for-submit advance draft.
func (e *expWorld) draftAdvance() doc.Result {
	e.t.Helper()
	return e.do(e.p(deepakID), expense.AdvanceDocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{"advance_date": "2026-10-20", "purpose": "Pune Metro site visit"},
		Lines:  []map[string]string{{"expense_type_id": e.travel, "line_amount": "100.00"}},
	})
}

// act posts action (close, cancel, reject) with reason from its page as b.
func (e *expWorld) act(b *browser, dt string, id int64, action, reason string) {
	e.t.Helper()
	path := docPath(dt, id) + "/" + action
	v := b.get(path).doc.Find(`form input[name="version"]`).AttrOr("value", "")
	if r := b.post(path, url.Values{"version": {v}, "reason": {reason}}); r.status != http.StatusSeeOther {
		e.t.Fatalf("POST %s = %d (%q)", path, r.status, r.alert())
	}
}

// bannerMessage is the message of the first banner whose text holds part,
// without the banner's kind word.
func bannerMessage(r *response, part string) string {
	var msg string
	r.doc.Find(".banner").EachWithBreak(func(_ int, b *goquery.Selection) bool {
		text := strings.Join(strings.Fields(b.Find(".banner-body p").First().Text()), " ")
		if !strings.Contains(text, part) {
			return true
		}
		_, msg, _ = strings.Cut(text, ": ")
		return false
	})
	return msg
}

// autofocused is the elements that carry autofocus.
func autofocused(r *response) *goquery.Selection { return r.doc.Find("[autofocus]") }

// claim is Deepak's claim with lines of amount each, dated on dates, set
// against advanceID for less (none when advanceID is 0); submitted unless
// draft.
func (e *expWorld) claim(advanceID int64, less string, draft bool, amount string, dates ...string) doc.Result {
	e.t.Helper()
	deepak := e.p(deepakID)
	in := doc.ActionInput{Values: map[string]string{"claim_date": "2026-10-24", "purpose": "Pune Metro site visit"}}
	if advanceID != 0 {
		in.Values["advance_id"], in.Values["less_advance"] = strconv.FormatInt(advanceID, 10), less
	}
	for _, d := range dates {
		in.Lines = append(in.Lines, map[string]string{
			"expense_type_id": e.travel, "expense_date": d, "amount": amount, "no_receipt_reason": "Auto rickshaw",
		})
	}
	res := e.do(deepak, expense.ClaimDocType, 0, doctype.ActionSave, in)
	if draft {
		return res
	}
	return e.do(deepak, expense.ClaimDocType, res.ID, doctype.ActionSubmit, doc.ActionInput{Version: res.Version})
}

func (e *expWorld) paid(id int64) {
	e.t.Helper()
	if _, err := e.app.DB().SQL().ExecContext(context.Background(),
		`UPDATE employee_advance SET paid_on = advance_date WHERE id = ?`, id); err != nil {
		e.t.Fatalf("mark advance %d paid: %v", id, err)
	}
}

// revokeRead takes read on dt from every role of user id.
func (e *expWorld) revokeRead(id auth.UserID, dt string) {
	e.t.Helper()
	for _, q := range []string{
		`DELETE FROM role_rights WHERE doctype = ? AND right_name = 'read' AND role_id IN (SELECT role_id FROM user_roles WHERE user_id = ?)`,
		`UPDATE auth_meta SET perm_version = perm_version + 1 WHERE id = 1`,
	} {
		args := []any{dt, int64(id)}
		if !strings.Contains(q, "?") {
			args = nil
		}
		if _, err := e.app.DB().SQL().ExecContext(context.Background(), q, args...); err != nil {
			e.t.Fatalf("revoke read: %v", err)
		}
	}
}

func docPath(dt string, id int64) string { return "/doc/" + dt + "/" + strconv.FormatInt(id, 10) }

func paidPath(id int64, route string) string {
	return "/modules/expense/advances/" + strconv.FormatInt(id, 10) + "/" + route
}

func wantText(t *testing.T, what string, r *response, want ...string) {
	t.Helper()
	text := r.pageText()
	for _, s := range want {
		if !strings.Contains(text, s) {
			t.Errorf("%s: page doesn't carry %q (status %d)", what, s, r.status)
		}
	}
}

func wantNoText(t *testing.T, what string, r *response, unwanted ...string) {
	t.Helper()
	text := r.pageText()
	for _, s := range unwanted {
		if strings.Contains(text, s) {
			t.Errorf("%s: page carries %q", what, s)
		}
	}
}

// pairValue is the value of Details' pair labelled label; "" when absent.
// It fails when the label shows more than once. The header's key facts
// repeat some pairs by design (UI v2 §5.13 item 3) and aren't Details.
func pairValue(t *testing.T, r *response, label string) string {
	t.Helper()
	var vals []string
	r.doc.Find("dl:not(.doc-head__facts) dt").Each(func(_ int, dt *goquery.Selection) {
		if strings.TrimSpace(dt.Text()) == label {
			vals = append(vals, strings.Join(strings.Fields(dt.Next().Text()), " "))
		}
	})
	if len(vals) > 1 {
		t.Errorf("pair %q shows %d times", label, len(vals))
	}
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}

// EUX-040, EUX-042 (the page side), EUX-020, CD-271 G1: the advance page's
// pairs and actions as it is paid, used and named by claims.
func TestExpS3_AdvancePage(t *testing.T) {
	e := newExpWorld(t)
	adv := e.advance("10000.00")
	page := docPath(expense.AdvanceDocType, adv.ID)
	meena, deepak := e.signedIn(meenaEmail), e.signedIn(deepakEmail)

	r := meena.get(page)
	if r.status != http.StatusOK {
		t.Fatalf("GET %s as meena = %d", page, r.status)
	}
	for label, want := range map[string]string{"Paid on": "Not paid yet", "Used": "₹0.00", "Left": "₹10,000.00"} {
		if got := pairValue(t, r, label); got != want {
			t.Errorf("unpaid, meena: pair %s = %q, want %q", label, got, want)
		}
	}
	if href := r.doc.Find(`a:contains("Mark as paid")`).AttrOr("href", ""); href != paidPath(adv.ID, "paid") {
		t.Errorf("unpaid, meena: Mark as paid links to %q, want %q", href, paidPath(adv.ID, "paid"))
	}
	wantText(t, "unpaid, meena", r, "No claims set against this advance yet.")
	wantNoText(t, "unpaid, meena", r, "Undo paid")

	r = deepak.get(page)
	if got := pairValue(t, r, "Paid on"); got != "Not paid yet" {
		t.Errorf("unpaid, deepak: Paid on = %q", got)
	}
	wantNoText(t, "unpaid, deepak", r, "Mark as paid", "Undo paid")
	// CD-327 (2): a submitted advance shows its employee field to everyone,
	// once (pairValue fails on a second row).
	for who, b := range map[string]*browser{"deepak": deepak, "admin": e.signedIn(adminEmail)} {
		if got := pairValue(t, b.get(page), "Employee"); got != "EMP-007 Deepak Nair" {
			t.Errorf("%s: Employee = %q, want EMP-007 Deepak Nair", who, got)
		}
	}
	// UX §5.10: the line card names the type, the amount and the note.
	card := r.doc.Find(".lines-card").First()
	if text := strings.Join(strings.Fields(card.Text()), " "); !strings.Contains(text, "Travel") || !strings.Contains(text, "₹10,000.00") ||
		!strings.Contains(text, "Bus and local travel") || strings.Contains(text, "TRAVEL") {
		t.Errorf("advance line card = %q, want Travel, ₹10,000.00 and its note, no type code", text)
	}

	e.paid(adv.ID)
	r = meena.get(page)
	if got := pairValue(t, r, "Paid on"); got != "20-10-2026" {
		t.Errorf("paid: Paid on = %q, want 20-10-2026", got)
	}
	if href := r.doc.Find(`a:contains("Undo paid")`).AttrOr("href", ""); href != paidPath(adv.ID, "unpaid") {
		t.Errorf("paid, meena: Undo paid links to %q, want %q", href, paidPath(adv.ID, "unpaid"))
	}
	wantNoText(t, "paid, meena", r, "Mark as paid")
	wantNoText(t, "paid, deepak", deepak.get(page), "Undo paid")

	// A draft naming it keeps Undo paid; an approved claim uses it.
	e.claim(adv.ID, "100.00", true, "100.00", "2026-10-24")
	if r = meena.get(page); r.doc.Find(`a:contains("Undo paid")`).Length() != 1 {
		t.Error("a draft claim naming the advance hides Undo paid")
	}
	used := e.claim(adv.ID, "6000.00", false, "6000.00", "2026-10-24")
	e.withRules(expense.ClaimDocType)
	pending := e.claim(adv.ID, "1000.00", false, "1000.00", "2026-10-23")
	if pending.State != doctype.StatePending {
		t.Fatalf("second claim is %s, want pending", pending.State)
	}
	r = meena.get(page)
	for label, want := range map[string]string{"Used": "₹6,000.00", "Left": "₹4,000.00"} {
		if got := pairValue(t, r, label); got != want {
			t.Errorf("used: pair %s = %q, want %q", label, got, want)
		}
	}
	wantNoText(t, "used, meena", r, "Undo paid", "Mark as paid", "Other claims")
	wantText(t, "used, meena", r, "Claims set against this advance", used.Number, "₹6,000.00 set against",
		"Draft "+strconv.FormatInt(pending.ID, 10), "₹1,000.00 set against", "(counts once approved)")
	pendingRow := r.doc.Find(`a[href="` + docPath(expense.ClaimDocType, pending.ID) + `"]`)
	if text := pendingRow.Text(); !strings.Contains(text, "(counts once approved)") {
		t.Errorf("the pending row = %q, want (counts once approved)", text)
	}
	if pendingRow.Find(`.datalist-nowrap:contains("counts once approved"), [class*="num"]:contains("counts once approved")`).Length() != 0 {
		t.Error("(counts once approved) sits in a cell that doesn't wrap")
	}
	if r.doc.Find(`a[href="`+docPath(expense.ClaimDocType, used.ID)+`"]`).Length() == 0 {
		t.Errorf("the card doesn't link %s", used.Number)
	}

	// CD-312: Priya, restricted to PRJ-METRO, reads Deepak's documents (team).
	priya := e.signedIn(priyaEmail)
	wantText(t, "priya, advance page", priya.get(page), used.Number, "₹6,000.00 set against", "Deepak Nair")
	wantText(t, "priya, claim page", priya.get(docPath(expense.ClaimDocType, used.ID)), "Deepak Nair")
	if got := pairValue(t, priya.get(docPath(expense.ClaimDocType, used.ID)), "Advance"); got != s3Advance {
		t.Errorf("priya's claim page: Advance = %q, want %q", got, s3Advance)
	}

	// CD-248 (1): without read on claims, Meena sees only the sum.
	e.revokeRead(meenaID, expense.ClaimDocType)
	r = meena.get(page)
	if r.status != http.StatusOK {
		t.Fatalf("GET %s after revoking claim read = %d", page, r.status)
	}
	expensetest.WantNotNamed(t, r.pageText(), used.Number, "Draft "+strconv.FormatInt(pending.ID, 10), "₹1,000.00", "counts once approved")
	if got := pairValue(t, r, "Other claims"); got != "₹6,000.00 set against" {
		t.Errorf("hidden claims: Other claims = %q, want ₹6,000.00 set against", got)
	}
}

// EUX-051, CD-305: a pending advance warns about money left on Deepak's
// paid advance, on the approver's page.
func TestExpS3_AdvanceWarning(t *testing.T) {
	e := newExpWorld(t)
	first := e.advance("4000.00")
	e.paid(first.ID)
	e.withRules(expense.AdvanceDocType)
	second := e.advance("2000.00")
	if second.State != doctype.StatePending {
		t.Fatalf("second advance is %s, want pending", second.State)
	}
	r := e.signedIn(priyaEmail).get(docPath(expense.AdvanceDocType, second.ID))
	wantText(t, "pending advance, priya", r, "Deepak Nair still has ₹4,000.00 left on "+s3Advance+".")
	if pairValue(t, r, "Paid on") != "" || pairValue(t, r, "Left") != "" {
		t.Error("a pending advance shows Paid on or Left")
	}

	// A rejected advance has no pairs, no card and no warning.
	rejected := e.do(e.p(priyaID), expense.AdvanceDocType, second.ID, doctype.ActionReject, doc.ActionInput{Version: second.Version, Reason: "Not needed"})
	r = e.signedIn(meenaEmail).get(docPath(expense.AdvanceDocType, rejected.ID))
	wantNoText(t, "rejected advance", r, "Paid on", "Claims set against this advance", "still has")
}

// EXP-059, EUX-021, EUX-052: the advance close page names what's left.
func TestAdv_ClosePageShowsLeft(t *testing.T) {
	e := newExpWorld(t)
	adv := e.advance("10000.00")
	e.paid(adv.ID)
	e.claim(adv.ID, "8500.00", false, "8500.00", "2026-10-24")
	meena := e.signedIn(meenaEmail)
	closeURL := docPath(expense.AdvanceDocType, adv.ID) + "/close"

	r := meena.get(closeURL)
	if r.status != http.StatusOK {
		t.Fatalf("GET %s = %d", closeURL, r.status)
	}
	wantText(t, "close page", r, "₹1,500.00 is left on this advance. Closing treats it as returned.",
		`For example "₹2,000 returned by NEFT 1234 on 30-11-2026"`)
	v := r.doc.Find(`form input[name="version"]`).AttrOr("value", "")
	if r = meena.post(closeURL, url.Values{"version": {v}, "reason": {"₹1,500 returned by NEFT 1234 on 30-11-2026"}}); r.status != http.StatusSeeOther {
		t.Fatalf("POST close = %d (%q)", r.status, r.alert())
	}
	if got := e.text(`SELECT state FROM employee_advance WHERE id = ?`, adv.ID); got != string(doctype.StateClosed) {
		t.Errorf("advance state %q, want closed", got)
	}
	r = meena.get(docPath(expense.AdvanceDocType, adv.ID))
	if got := pairValue(t, r, "Closed"); got != "₹1,500 returned by NEFT 1234 on 30-11-2026" {
		t.Errorf("closed advance: Closed = %q", got)
	}
	wantNoText(t, "closed advance", r, "Mark as paid", "Undo paid")

	wantNoText(t, "advance cancel page", meena.get(docPath(expense.AdvanceDocType, adv.ID)+"/cancel"), "For example", "is left on")

	// Nothing left: no note, the claims' hint.
	empty := e.advance("500.00")
	e.paid(empty.ID)
	e.claim(empty.ID, "500.00", false, "500.00", "2026-10-24")
	r = meena.get(docPath(expense.AdvanceDocType, empty.ID) + "/close")
	wantText(t, "close, nothing left", r, "For example how it was paid")
	wantNoText(t, "close, nothing left", r, "is left on this advance")
}

// UX §5.7, §5.9, EUX-014, EUX-015, EUX-032: the claim page's notice,
// warnings and pairs, and the close hint.
func TestExpS3_ClaimPage(t *testing.T) {
	e := newExpWorld(t)
	first := e.claim(0, "", false, "450.00", "2026-10-22")
	toCancel := e.claim(0, "", false, "300.00", "2026-10-21")
	meena := e.signedIn(meenaEmail)

	draft := e.claim(0, "", true, "450.00", "2026-10-20", "2026-10-22")
	r := meena.get(docPath(expense.ClaimDocType, draft.ID))
	if r.status != http.StatusOK {
		t.Fatalf("draft page as meena = %d", r.status)
	}
	if got := bannerMessage(r, "hasn't submitted"); got != "Deepak Nair hasn't submitted this claim yet." {
		t.Errorf("draft notice = %q, want Deepak Nair hasn't submitted this claim yet.", got)
	}
	wantText(t, "draft, meena", r, "Line 2: This looks the same as a line on "+first.Number+".")
	if got := pairValue(t, r, "Period"); got != "20-10-2026 to 22-10-2026" {
		t.Errorf("Period = %q, want 20-10-2026 to 22-10-2026", got)
	}

	// Approved: no warnings; Close carries the hint, and Closed the reason.
	page := docPath(expense.ClaimDocType, first.ID)
	wantNoText(t, "approved, meena", meena.get(page), "This looks the same", "hasn't submitted")
	wantNoText(t, "claim cancel page", meena.get(page+"/cancel"), "For example")
	r = meena.get(page + "/close")
	wantText(t, "claim close page", r, "For example how it was paid")
	wantNoText(t, "claim close page", r, "is left on")
	v := r.doc.Find(`form input[name="version"]`).AttrOr("value", "")
	if r = meena.post(page+"/close", url.Values{"version": {v}, "reason": {"Paid by NEFT 1234"}}); r.status != http.StatusSeeOther {
		t.Fatalf("POST close = %d (%q)", r.status, r.alert())
	}
	if got := pairValue(t, meena.get(page), "Closed"); got != "Paid by NEFT 1234" {
		t.Errorf("closed claim: Closed = %q, want the reason", got)
	}

	// Pending, as the approver, and rejected: the warnings show.
	e.withRules(expense.ClaimDocType)
	pending := e.claim(0, "", false, "450.00", "2026-10-22")
	if pending.State != doctype.StatePending {
		t.Fatalf("claim is %s, want pending", pending.State)
	}
	dup := "Line 1: This looks the same as a line on " + first.Number + "."
	priya := e.signedIn(priyaEmail)
	wantText(t, "pending, priya", priya.get(docPath(expense.ClaimDocType, pending.ID)), dup)
	e.act(priya, expense.ClaimDocType, pending.ID, "reject", "Duplicate of the first claim")
	wantText(t, "rejected, meena", meena.get(docPath(expense.ClaimDocType, pending.ID)), dup)

	// Cancelled: no Closed pair.
	e.act(meena, expense.ClaimDocType, toCancel.ID, "cancel", "Entered twice")
	if got := pairValue(t, meena.get(docPath(expense.ClaimDocType, toCancel.ID)), "Closed"); got != "" {
		t.Errorf("cancelled claim: Closed = %q, want none", got)
	}
}

// UX §5.18, EUX-040 to EUX-042: the Mark as paid and Undo paid pages, their
// guard, and the 422 and 409 redraws keeping the typed date.
func TestExpS3_PaidPages(t *testing.T) {
	e := newExpWorld(t)
	adv := e.advance("8000.00")
	meena := e.signedIn(meenaEmail)

	r := meena.get(paidPath(adv.ID, "paid"))
	if r.status != http.StatusOK {
		t.Fatalf("GET paid as meena = %d", r.status)
	}
	wantText(t, "mark page", r, "Mark "+s3Advance+" as paid", s3Advance+" · Deepak Nair · ₹8,000.00",
		"The day the money reached Deepak Nair.", "Today")
	in := r.doc.Find(`input[name="paid_on"]`)
	if got := in.AttrOr("value", ""); got != "24-10-2026" {
		t.Errorf("Paid on default = %q, want 24-10-2026", got)
	}
	if _, ok := in.Attr("autofocus"); !ok {
		t.Error("Paid on isn't focused")
	}
	form := r.doc.Find(`form[action="` + paidPath(adv.ID, "paid") + `"]`)
	if got := r.doc.Find(`.actionbar button[type="submit"][form="` + form.AttrOr("id", "none") + `"]`).Text(); !strings.Contains(got, "Mark as paid") {
		t.Errorf("the form's button in the sticky bar reads %q", got)
	}
	v := r.doc.Find(`form input[name="version"]`).AttrOr("value", "")
	if v != e.text(`SELECT CAST(version AS TEXT) FROM employee_advance WHERE id = ?`, adv.ID) {
		t.Errorf("the form posts version %q, not the stored one", v)
	}

	for _, c := range []struct {
		email  string
		path   string
		status int
		msg    string
	}{
		{deepakEmail, paidPath(adv.ID, "paid"), http.StatusForbidden, "You don't have permission to cancel Employee Advance."},
		{deepakEmail, paidPath(adv.ID, "unpaid"), http.StatusForbidden, "You don't have permission to cancel Employee Advance."},
		{kavyaEmail, paidPath(adv.ID, "paid"), http.StatusNotFound, auth.MsgNotFound},
		{meenaEmail, paidPath(adv.ID+100, "paid"), http.StatusNotFound, auth.MsgNotFound},
		{meenaEmail, "/modules/expense/advances/x/unpaid", http.StatusNotFound, auth.MsgNotFound},
	} {
		b := meena
		if c.email != meenaEmail {
			b = e.signedIn(c.email)
		}
		r := b.get(c.path)
		if r.status != c.status {
			t.Errorf("GET %s as %s = %d, want %d", c.path, c.email, r.status, c.status)
		}
		wantText(t, c.path+" as "+c.email, r, c.msg)
	}

	// A date that doesn't parse is redrawn before E9's checks run: the
	// redraw keeps AUTH's answers, so it never shows the advance to others.
	for email, status := range map[string]int{deepakEmail: http.StatusForbidden, kavyaEmail: http.StatusNotFound} {
		r := e.signedIn(email).post(paidPath(adv.ID, "paid"), url.Values{"version": {v}, "paid_on": {"31-02-2026"}})
		if r.status != status {
			t.Errorf("POST a bad date as %s = %d, want %d", email, r.status, status)
		}
		expensetest.WantNotNamed(t, r.pageText(), s3Advance)
	}

	// 422: a future date, kept and shown at the field with the label.
	r = meena.post(paidPath(adv.ID, "paid"), url.Values{"version": {v}, "paid_on": {"25-10-2026"}})
	if r.status != http.StatusUnprocessableEntity {
		t.Fatalf("POST a future date = %d", r.status)
	}
	if got := r.doc.Find(`input[name="paid_on"]`).AttrOr("value", ""); got != "25-10-2026" {
		t.Errorf("422: Paid on = %q, want the typed 25-10-2026", got)
	}
	if _, ok := r.doc.Find(`input[name="paid_on"]`).Attr("aria-invalid"); !ok {
		t.Error("422: Paid on isn't marked invalid")
	}
	wantText(t, "422", r, "Mark "+s3Advance+" as paid", "Paid on can't be in the future.")
	if f := autofocused(r); f.Length() != 1 || !f.HasClass("error-summary") {
		t.Errorf("422 at the field: %d autofocused, want the error summary only", f.Length())
	}
	if got := r.doc.Find(`form input[name="version"]`).AttrOr("value", ""); got != v {
		t.Errorf("422: version %q, want the posted %q", got, v)
	}

	// 409: a stale version, with the typed date and a Reload link.
	r = meena.post(paidPath(adv.ID, "paid"), url.Values{"version": {"1"}, "paid_on": {"21-10-2026"}})
	if r.status != http.StatusConflict {
		t.Fatalf("POST a stale version = %d", r.status)
	}
	if got := r.doc.Find(`input[name="paid_on"]`).AttrOr("value", ""); got != "21-10-2026" {
		t.Errorf("409: Paid on = %q, want the typed 21-10-2026", got)
	}
	wantText(t, "409", r, "Someone else changed "+s3Advance+" after you opened it.")
	if href := r.doc.Find(`a:contains("Reload `+s3Advance+`")`).AttrOr("href", ""); href != paidPath(adv.ID, "paid") {
		t.Errorf("409: Reload links to %q", href)
	}
	if f := autofocused(r); f.Length() != 1 || f.Find(".banner--warning").Length() != 1 {
		t.Errorf("409: %d autofocused, want the conflict banner only", f.Length())
	}

	// Undo paid: the confirm page, and a refusal in the danger banner.
	r = meena.get(paidPath(adv.ID, "unpaid"))
	wantText(t, "unpaid, not paid", r, "Undo paid on "+s3Advance+"?", "Claims can't use this advance until it is marked as paid again.")
	if r.doc.Find(`input[name="paid_on"]`).Length() != 0 {
		t.Error("the Undo paid page has a date")
	}
	if f := autofocused(r); f.Length() != 1 || goquery.NodeName(f) != "h1" || !strings.Contains(f.Text(), "Undo paid on") {
		t.Errorf("Undo paid confirm: %d autofocused, want the heading only", f.Length())
	}
	r = meena.post(paidPath(adv.ID, "unpaid"), url.Values{"version": {v}})
	if r.status != http.StatusUnprocessableEntity {
		t.Fatalf("POST undo on an unpaid advance = %d", r.status)
	}
	if got := r.doc.Find(".banner--danger").Text(); !strings.Contains(got, "This advance isn't marked as paid.") {
		t.Errorf("undo refusal alert = %q", got)
	}
	wantText(t, "undo 422", r, "Undo paid on "+s3Advance+"?")
	if f := autofocused(r); f.Length() != 1 || f.Find(".banner--danger").Length() != 1 {
		t.Errorf("undo 422: %d autofocused, want the danger banner only", f.Length())
	}

	e.paid(adv.ID)
	wantText(t, "unpaid, paid", meena.get(paidPath(adv.ID, "unpaid")), s3Advance+" · Deepak Nair · ₹8,000.00 · Paid on 20-10-2026")
}

// EUX-040, EUX-042: after Mark as paid and Undo paid the advance page reads
// the flash.
func TestExpS3_PaidFlash(t *testing.T) {
	e := newExpWorld(t)
	adv := e.advance("8000.00")
	meena := e.signedIn(meenaEmail)
	page := docPath(expense.AdvanceDocType, adv.ID)
	for _, c := range []struct{ route, date, flash string }{
		{"paid", "21-10-2026", "Marked as paid."},
		{"unpaid", "", "No longer marked as paid."},
	} {
		v := meena.get(paidPath(adv.ID, c.route)).doc.Find(`form input[name="version"]`).AttrOr("value", "")
		f := url.Values{"version": {v}}
		if c.date != "" {
			f.Set("paid_on", c.date)
		}
		if r := meena.post(paidPath(adv.ID, c.route), f); r.status != http.StatusSeeOther || r.location() != page {
			t.Fatalf("POST %s = %d to %q", c.route, r.status, r.location())
		}
		if got := bannerMessage(meena.get(page), c.flash); got != c.flash {
			t.Errorf("after %s: flash %q, want %q", c.route, got, c.flash)
		}
	}
}

// CD-271 G1: Mark as paid and Undo paid show only on an approved advance; a
// closed one, paid or not and named by no claim, has neither, and a
// cancelled one has no Closed pair.
func TestExpS3_PaidActionsApprovedOnly(t *testing.T) {
	e := newExpWorld(t)
	meena := e.signedIn(meenaEmail)
	unpaid, paid := e.advance("500.00"), e.advance("600.00")
	e.paid(paid.ID)
	for _, a := range []doc.Result{unpaid, paid} {
		e.act(meena, expense.AdvanceDocType, a.ID, "close", "Returned in cash")
		wantNoText(t, "closed advance", meena.get(docPath(expense.AdvanceDocType, a.ID)), "Mark as paid", "Undo paid")
	}
	cancelled := e.advance("700.00")
	e.act(meena, expense.AdvanceDocType, cancelled.ID, "cancel", "Raised twice")
	r := meena.get(docPath(expense.AdvanceDocType, cancelled.ID))
	if got := pairValue(t, r, "Closed"); got != "" {
		t.Errorf("cancelled advance: Closed = %q, want none", got)
	}
	wantNoText(t, "cancelled advance", r, "Mark as paid", "Undo paid")
}

// CD-327 (2): the employee field is hidden from a non-Administrator only on
// a new advance and a draft; a reader of someone's draft gets the employee
// in Details instead, since the party line hides with the field (CD-319).
func TestExpS3_AdvanceEmployeeHide(t *testing.T) {
	e := newExpWorld(t)
	deepak, admin := e.signedIn(deepakEmail), e.signedIn(adminEmail)
	if n := deepak.get("/doc/" + expense.AdvanceDocType + "/new").doc.Find(`[name="employee_id"]`).Length(); n != 0 {
		t.Errorf("deepak's new advance has %d employee inputs, want none", n)
	}
	if n := admin.get("/doc/" + expense.AdvanceDocType + "/new").doc.Find(`[name="employee_id"]`).Length(); n == 0 {
		t.Error("the administrator's new advance has no employee input")
	}
	draft := e.draftAdvance()
	if n := deepak.get(docPath(expense.AdvanceDocType, draft.ID) + "/edit/details").doc.Find(`[name="employee_id"]`).Length(); n != 0 {
		t.Errorf("deepak's own draft has %d employee inputs, want none", n)
	}
	r := e.signedIn(priyaEmail).get(docPath(expense.AdvanceDocType, draft.ID))
	if r.status != http.StatusOK {
		t.Fatalf("priya reads deepak's draft = %d", r.status)
	}
	if got := pairValue(t, r, "Employee"); got != "EMP-007 Deepak Nair" {
		t.Errorf("priya, deepak's draft: Employee = %q, want EMP-007 Deepak Nair once", got)
	}
	submitted := e.advance("900.00")
	wantText(t, "close page", e.signedIn(meenaEmail).get(docPath(expense.AdvanceDocType, submitted.ID)+"/close"), "Deepak Nair")
}

// EUX-033, CD-248 (1): Kavya reads the advance and the pending claim she
// owns, not Deepak's approved one: the card lists the pending claim and one
// Other claims row with the approved claim's amount; Anil sees both claims
// and no Other claims row.
func TestExpS3_ClaimsCardMixed(t *testing.T) {
	e := newExpWorld(t)
	ctx := context.Background()
	if err := e.app.Auth().SetRights(ctx, e.admin, "Masters Manager", expense.AdvanceDocType, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll}); err != nil {
		t.Fatalf("give Kavya read on advances: %v", err)
	}
	adv := e.advance("10000.00")
	e.paid(adv.ID)
	hidden := e.claim(adv.ID, "2500.00", false, "2500.00", "2026-10-24")
	e.withRules(expense.ClaimDocType)
	mine := e.claim(adv.ID, "1000.00", false, "1000.00", "2026-10-23")
	if _, err := e.app.DB().SQL().ExecContext(ctx, `UPDATE expense_claim SET created_by = ? WHERE id = ?`, int64(kavyaID), mine.ID); err != nil {
		t.Fatalf("give Kavya the pending claim: %v", err)
	}
	page := docPath(expense.AdvanceDocType, adv.ID)

	r := e.signedIn(kavyaEmail).get(page)
	if r.status != http.StatusOK {
		t.Fatalf("GET %s as kavya = %d", page, r.status)
	}
	wantText(t, "kavya", r, "Draft "+strconv.FormatInt(mine.ID, 10), "₹1,000.00 set against")
	expensetest.WantNotNamed(t, r.pageText(), hidden.Number)
	if got := pairValue(t, r, "Other claims"); got != "₹2,500.00 set against" {
		t.Errorf("kavya: Other claims = %q, want ₹2,500.00 set against", got)
	}

	r = e.signedIn(adminEmail).get(page)
	wantText(t, "anil", r, hidden.Number, "Draft "+strconv.FormatInt(mine.ID, 10))
	if got := pairValue(t, r, "Other claims"); got != "" {
		t.Errorf("anil: Other claims = %q, want none", got)
	}
}

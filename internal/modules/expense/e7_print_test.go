package expense_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/kernel/print/printtest"
	printweb "github.com/vardhan/internaltools/internal/kernel/print/web"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// E7 · Prints (plan MOD-EXP E7; spec §10). The prints are fetched through
// the real print handler (print/web) over the real auth.Service, so the print
// right, the read check and the views registered on print.Default are all on
// the path.
//
// E2 (totals, snapshots) and E4 (less advance written at submit) aren't
// merged yet: e7Derive stands in for them with test-only fixture SQL, setting
// the stored values E2's Validate will compute (snapshots, totals, period,
// to pay). less_advance is set through expensetest's Against builder.

const (
	e7Deepak = "deepak@vardhan.test" // Employee, EMP-007, reports to Priya
	e7Priya  = "priya@vardhan.test"  // Employee, EMP-004, reports to Ravi
	e7Ravi   = "ravi@vardhan.test"   // Priya's and Suresh's manager
	e7Suresh = "suresh@vardhan.test" // Priya's colleague, also reports to Ravi
	e7Kavya  = "kavya@vardhan.test"  // Employee, reports to Meena
	e7Meena  = "meena@vardhan.test"  // Director: print (all)
	e7Admin  = "admin@vardhan.test"

	e7AdvanceNo = "EA/26-27/00001"
	// e7NoPrint is AUTH's 403 for print on a claim (print/web, CD-73).
	e7NoPrint = "You don't have permission to print Expense Claim. Ask your administrator for a role that allows it.\n"
)

// e7Get fetches the print page of document dt id as p.
func e7Get(t *testing.T, e *expensetest.Env, p auth.Principal, dt string, id int64) (int, string) {
	t.Helper()
	h := printweb.Routes(printweb.Config{DB: e.DB, Auth: e.Auth, Info: e.Engine, Letterhead: e.Masters.Letterhead})
	req := httptest.NewRequest(http.MethodGet, "/print/"+dt+"/"+strconv.FormatInt(id, 10), nil)
	req = req.WithContext(auth.WithPrincipal(req.Context(), p))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// e7Print is e7Get that fails the test unless the page is served.
func e7Print(t *testing.T, e *expensetest.Env, p auth.Principal, dt string, id int64) string {
	t.Helper()
	code, body := e7Get(t, e, p, dt, id)
	if code != http.StatusOK {
		t.Fatalf("print %s %d as %s: status %d, want 200:\n%s", dt, id, p.Name, code, body)
	}
	return body
}

// e7Contains fails for each of want missing from html.
func e7Contains(t *testing.T, html string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(html, w) {
			t.Errorf("print lacks %q:\n%s", w, html)
		}
	}
}

// e7Exec runs fixture SQL in one write.
func e7Exec(t *testing.T, e *expensetest.Env, query string, args ...any) {
	t.Helper()
	err := e.DB.Write(context.Background(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(context.Background(), query, args...)
		return err
	})
	if err != nil {
		t.Fatalf("fixture SQL %q: %v", query, err)
	}
}

// e7Derive is the stand-in for E2's Validate (test-only fixture SQL): the
// snapshots, totals and period of claim claimID (0 for none) and advance
// advanceID (0 for none), and the claim's to pay (total − less advance).
func e7Derive(t *testing.T, e *expensetest.Env, claimID, advanceID int64) {
	t.Helper()
	e7Exec(t, e, `UPDATE expense_claim_line SET
		snap_type_code = (SELECT code FROM expense_type WHERE id = expense_type_id),
		snap_type_name = (SELECT name FROM expense_type WHERE id = expense_type_id),
		snap_project_code = COALESCE((SELECT code FROM project WHERE id = project_id), '')
		WHERE parent_id = ?`, claimID)
	e7Exec(t, e, `UPDATE expense_claim SET
		snap_employee_name = (SELECT name FROM employee WHERE id = employee_id),
		snap_employee_code = (SELECT code FROM employee WHERE id = employee_id),
		total = (SELECT SUM(amount) FROM expense_claim_line WHERE parent_id = expense_claim.id),
		period_from = (SELECT MIN(expense_date) FROM expense_claim_line WHERE parent_id = expense_claim.id),
		period_to = (SELECT MAX(expense_date) FROM expense_claim_line WHERE parent_id = expense_claim.id)
		WHERE id = ?`, claimID)
	e7Exec(t, e, `UPDATE expense_claim SET net_payable = total - COALESCE(less_advance, 0) WHERE id = ?`, claimID)
	e7Exec(t, e, `UPDATE employee_advance_line SET
		snap_type_code = (SELECT code FROM expense_type WHERE id = expense_type_id),
		snap_type_name = (SELECT name FROM expense_type WHERE id = expense_type_id),
		snap_project_code = COALESCE((SELECT code FROM project WHERE id = project_id), '')
		WHERE parent_id = ?`, advanceID)
	e7Exec(t, e, `UPDATE employee_advance SET
		snap_employee_name = (SELECT name FROM employee WHERE id = employee_id),
		snap_employee_code = (SELECT code FROM employee WHERE id = employee_id),
		amount = (SELECT SUM(amount) FROM employee_advance_line WHERE parent_id = employee_advance.id)
		WHERE id = ?`, advanceID)
}

// e7Do runs action a as p at the document's stored version.
func e7Do(t *testing.T, e *expensetest.Env, p auth.Principal, dt string, id int64, a doc.Action) doc.Result {
	t.Helper()
	res, err := e.Engine.Do(context.Background(), p, dt, id, a, doc.ActionInput{Version: e.LoadDoc(t, dt, id).Version})
	if err != nil {
		t.Fatalf("%s %s %d as %s: %v", a, dt, id, p.Name, err)
	}
	return res
}

// e7Project is the id of the fixture project with code, as a form value.
func e7Project(t *testing.T, e *expensetest.Env, code string) string {
	t.Helper()
	for _, p := range e.F.Projects {
		if p.Code == code {
			return strconv.FormatInt(p.ID, 10)
		}
	}
	t.Fatalf("no fixture project %s", code)
	return ""
}

// e7World is EXP-026's approved claim and EXP-024's approved advance, both
// Deepak's and approved by Priya: an advance of ₹10,000.00 (one TRAVEL line
// on PRJ-METRO), and a claim of ₹12,000.00 setting off ₹10,000.00 against it,
// to pay ₹2,000.00. Line 1 (PRJ-METRO) has its receipt train-ticket.jpg;
// line 2 has a reason, and hotel-note.pdf is attached but is no line's
// receipt.
func e7World(t *testing.T) (e *expensetest.Env, claimID, advanceID int64) {
	t.Helper()
	e = expensetest.New(t)
	deepak, priya := e.P(t, e7Deepak), e.P(t, e7Priya)
	metro := e7Project(t, e, "PRJ-METRO")

	adv := e.DraftAdvance(t, deepak, nil, expensetest.Values{"line_amount": "10000.00", "project_id": metro, "description": "Train and taxi"})
	e7Do(t, e, deepak, expense.AdvanceDocType, adv.ID, doc.ActionSubmit)
	if res := e7Do(t, e, priya, expense.AdvanceDocType, adv.ID, doc.ActionApprove); res.Number != e7AdvanceNo {
		t.Fatalf("advance number = %q, want %s", res.Number, e7AdvanceNo)
	}

	claim := e.DraftClaim(t, deepak, nil,
		expensetest.Values{"expense_date": "2026-10-20", "description": "Train to Pune", "amount": "7000.00", "project_id": metro},
		expensetest.Values{"expense_date": "2026-10-22", "expense_type_id": strconv.FormatInt(e.F.ExpenseTypeIDs["LODGING"], 10),
			"description": "Hotel, one night", "amount": "5000.00"},
	)
	ticket := e.MustAttach(t, deepak, expense.ClaimDocType, claim.ID, "train-ticket.jpg", filestest.JPEG(2048))
	e.MustAttach(t, deepak, expense.ClaimDocType, claim.ID, "hotel-note.pdf", filestest.PDF(3000))
	if _, err := e.EditClaim(t, deepak, claim.ID, nil, map[int]expensetest.Values{1: {"receipt_sha256": ticket.SHA256, "no_receipt_reason": ""}}); err != nil {
		t.Fatalf("set line 1's receipt: %v", err)
	}
	e.Against(t, deepak, claim.ID, adv.ID, 1_000_000)
	e7Do(t, e, deepak, expense.ClaimDocType, claim.ID, doc.ActionSubmit)
	e7Do(t, e, priya, expense.ClaimDocType, claim.ID, doc.ActionApprove)
	e7Derive(t, e, claim.ID, adv.ID)
	return e, claim.ID, adv.ID
}

// EXP-032: the claim print shows the purpose, less advance, the advance
// number, to pay with its words, each line's project and receipt marker, the
// attachment names and who approved it; the advance print shows its title,
// employee, date, purpose, lines, total, words and approved by. Both match
// their golden files.
func TestExp_PrintGolden(t *testing.T) {
	e, claimID, advanceID := e7World(t)
	deepak := e.P(t, e7Deepak)

	claim := e7Print(t, e, deepak, expense.ClaimDocType, claimID)
	printtest.Golden(t, "claim_approved.golden.html", []byte(claim))
	e7Contains(t, claim,
		"Vardhan Infra Projects Pvt Ltd", "Expense Claim", "EX/26-27/00001",
		"Employee: Deepak Nair (EMP-007)", "Period: 20-10-2026 to 22-10-2026", "Purpose: Pune Metro site visit",
		"Train to Pune", "Project: PRJ-METRO", "Hotel, one night", "₹7,000.00", "₹5,000.00",
		"Grand Total ₹12,000.00", "Advance: "+e7AdvanceNo, "Less Advance ₹10,000.00", "To Pay ₹2,000.00",
		`<p class="words">Rupees Two Thousand Only</p>`,
		"Attachments: hotel-note.pdf, train-ticket.jpg",
		"Approved by Priya Shah on 24-10-2026 10:30",
	)
	if n := strings.Count(claim, "Project: PRJ-METRO"); n != 1 {
		t.Errorf("Project: PRJ-METRO %d times, want 1 (line 2 has no project)", n)
	}
	// Line 1 holds an attached receipt; line 2 has none.
	if i, j := strings.Index(claim, "Receipt: Yes"), strings.Index(claim, "Receipt: No"); i < 0 || j < 0 || i > j {
		t.Errorf("receipt markers: Yes at %d, No at %d, want line 1 Yes then line 2 No:\n%s", i, j, claim)
	}

	adv := e7Print(t, e, deepak, expense.AdvanceDocType, advanceID)
	printtest.Golden(t, "advance_approved.golden.html", []byte(adv))
	e7Contains(t, adv,
		"Vardhan Infra Projects Pvt Ltd", `<h1 class="doc-title">Employee Advance`, e7AdvanceNo,
		"Employee: Deepak Nair (EMP-007)", "Date: 24-10-2026", "Purpose: Pune Metro site visit",
		"Train and taxi", "Project: PRJ-METRO", "Total ₹10,000.00",
		`<p class="words">Rupees Ten Thousand Only</p>`, "Approved by Priya Shah on 24-10-2026 10:30",
	)
}

// A line's receipt prints Yes only while its hash is among the claim's
// attachments (ListTx), not merely because the line holds a hash.
func TestExp_PrintReceiptNeedsAttachment(t *testing.T) {
	e, claimID, _ := e7World(t)
	e7Exec(t, e, `UPDATE expense_claim_line SET receipt_sha256 = ? WHERE parent_id = ? AND line_no = 2`,
		strings.Repeat("ab", 32), claimID)
	html := e7Print(t, e, e.P(t, e7Deepak), expense.ClaimDocType, claimID)
	if n := strings.Count(html, "Receipt: Yes"); n != 1 {
		t.Errorf("Receipt: Yes %d times, want 1 (line 2's hash isn't attached):\n%s", n, html)
	}
}

// CD-233: the advance number prints only for a viewer who can read the
// advance. A role printing every claim but no advance sees the claim's own
// amounts and no advance number.
func TestExp_PrintHidesUnreadableAdvance(t *testing.T) {
	e, claimID, _ := e7World(t)
	admin := e.P(t, e7Admin)
	if err := e.Auth.CreateRole(t.Context(), admin, "Claim Auditor"); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	if err := e.Auth.SetRights(t.Context(), admin, "Claim Auditor", expense.ClaimDocType,
		map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightPrint: auth.ScopeAll}); err != nil {
		t.Fatalf("SetRights: %v", err)
	}
	e.AddRoles(t, e7Suresh, "Claim Auditor") // Suresh can't read Deepak's advance (not his team)

	html := e7Print(t, e, e.P(t, e7Suresh), expense.ClaimDocType, claimID)
	expensetest.WantNotNamed(t, html, e7AdvanceNo)
	if strings.Contains(html, "Advance: ") {
		t.Errorf("the print has an advance line for a viewer who can't read the advance:\n%s", html)
	}
	e7Contains(t, html, "Less Advance ₹10,000.00", "To Pay ₹2,000.00", `<p class="words">Rupees Two Thousand Only</p>`)

	// Meena reads every advance: she sees the number.
	e7Contains(t, e7Print(t, e, e.P(t, e7Meena), expense.ClaimDocType, claimID), "Advance: "+e7AdvanceNo)
}

// A draft has no approval and no stored to pay yet: neither prints. An
// approval with no rule (approved_no_rule, at submit) still names who
// approved.
func TestExp_PrintDraftAndNoRuleApproval(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, e7Deepak)

	draft := e.DraftClaim(t, deepak, nil)
	html := e7Print(t, e, deepak, expense.ClaimDocType, draft.ID)
	for _, absent := range []string{"Approved by", "To Pay"} {
		if strings.Contains(html, absent) {
			t.Errorf("draft print has %q:\n%s", absent, html)
		}
	}

	admin := e.P(t, e7Admin)
	rules, err := e.Approvals.Rules(t.Context(), admin)
	if err != nil {
		t.Fatalf("Rules: %v", err)
	}
	for _, r := range rules {
		if r.DocType == expense.AdvanceDocType {
			if err := e.Approvals.SetRuleEnabled(t.Context(), admin, r.ID, false); err != nil {
				t.Fatalf("disable rule %d: %v", r.ID, err)
			}
		}
	}
	adv := e.DraftAdvance(t, deepak, nil)
	if res := e7Do(t, e, deepak, expense.AdvanceDocType, adv.ID, doc.ActionSubmit); res.State != doc.StateApproved {
		t.Fatalf("submit with no rule: state %q, want approved", res.State)
	}
	var hist []approval.HistoryRow
	if err := e.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		hist, err = e.Approvals.History(t.Context(), tx, expense.AdvanceDocType, adv.ID)
		return err
	}); err != nil {
		t.Fatalf("History: %v", err)
	}
	if last := hist[len(hist)-1]; last.Action != approval.HistAutoApproved {
		t.Fatalf("last history action = %q, want %q", last.Action, approval.HistAutoApproved)
	}
	e7Contains(t, e7Print(t, e, deepak, expense.AdvanceDocType, adv.ID), "Approved by Deepak Nair on 24-10-2026 10:30")
}

// EXP-033: printing needs `print`. Priya prints her own approved claim and
// Meena prints it (all). Suresh, her colleague under Ravi, can't read it and
// gets the same 404 as a missing record. Ravi, her manager (read team, print
// own), and Kavya holding Fallback Approver (read all, no print) get AUTH's
// 403. Real auth.Service throughout.
func TestExp_PrintRights(t *testing.T) {
	e := expensetest.New(t)
	priya := e.P(t, e7Priya)
	claim := e.ApprovedClaim(t, priya, nil)
	if claim.State != doc.StateApproved {
		t.Fatalf("Priya's claim state = %q, want approved", claim.State)
	}
	e.AddRoles(t, e7Kavya, expense.RoleFallbackApprover)

	for _, who := range []string{e7Priya, e7Meena} {
		e7Contains(t, e7Print(t, e, e.P(t, who), expense.ClaimDocType, claim.ID), "Expense Claim")
	}

	missingCode, missingBody := e7Get(t, e, e.P(t, e7Suresh), expense.ClaimDocType, claim.ID+1000)
	if missingCode != http.StatusNotFound {
		t.Fatalf("missing claim: status %d, want 404", missingCode)
	}
	if code, body := e7Get(t, e, e.P(t, e7Suresh), expense.ClaimDocType, claim.ID); code != missingCode || body != missingBody {
		t.Errorf("suresh: %d %q, want the missing record's %d %q", code, body, missingCode, missingBody)
	}

	for _, who := range []string{e7Ravi, e7Kavya} {
		if code, body := e7Get(t, e, e.P(t, who), expense.ClaimDocType, claim.ID); code != http.StatusForbidden || body != e7NoPrint {
			t.Errorf("%s: %d %q, want 403 %q", who, code, body, e7NoPrint)
		}
	}
	// Kavya's Fallback Approver does read the claim: the 403 is for print, not read.
	if err := e.Auth.Can(t.Context(), e.P(t, e7Kavya), expense.ClaimDocType, auth.RightRead, &auth.RecordRef{
		DocType: expense.ClaimDocType, ID: claim.ID, CreatedBy: priya.ID,
	}); err != nil {
		t.Errorf("kavya read on Priya's claim: %v, want nil", err)
	}
}

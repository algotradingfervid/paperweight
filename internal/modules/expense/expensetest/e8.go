package expensetest

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// E8 (Reports and CSV) helpers. The report inputs that other tasks compute
// (E2: totals, to pay, snapshots; E3: receipts; E4: used amount; E9: paid
// on) are stored here with test-only fixture SQL, as the value those tasks
// write, so the report cases don't wait for them. States, numbers, approved
// on and approved by come from the real engine and approval service.

// ist is India Standard Time, the business time zone (00-fixtures).
var ist = time.FixedZone("IST", 5*60*60+30*60)

// E8At sets the clock to the given day and time in IST.
func (e *Env) E8At(y int, m time.Month, d, hour, minute int) {
	e.Clock.Set(time.Date(y, m, d, hour, minute, 0, 0, ist))
}

// E8Step runs action a on document id as p, with its stored version and
// reason.
func (e *Env) E8Step(t testing.TB, p auth.Principal, dt string, id int64, a doc.Action, reason string) doc.Result {
	t.Helper()
	var rec doc.Record
	err := e.DB.Read(context.Background(), func(tx *db.Tx) error {
		var err error
		rec, err = e.Engine.Load(context.Background(), tx, dt, id)
		return err
	})
	if err != nil {
		t.Fatalf("expensetest: load %s %d: %v", dt, id, err)
	}
	return e.do(t, p, dt, id, a, doc.ActionInput{Version: rec.Version, Reason: reason})
}

// E8Approve submits draft res as p and approves it as the employee's
// reporting manager, as ApprovedClaim does.
func (e *Env) E8Approve(t testing.TB, p auth.Principal, dt string, res doc.Result) doc.Result {
	t.Helper()
	return e.approve(t, p, dt, res)
}

// E8Fill stores, on every claim and advance, what E2 and E4 compute: the
// employee snapshots; the claim's total, to pay (total − less advance) and
// period; the line snapshots (type code and name, project and site codes,
// the billed-to branch's GSTIN); the advance's amount; and its used amount,
// the sum of less advance over approved and closed claims naming it
// (D-DOC-13). Run it after the last state change of a case.
func (e *Env) E8Fill(t testing.TB) {
	t.Helper()
	e.e8Exec(t, `
UPDATE expense_claim SET
	snap_employee_name = (SELECT name FROM employee WHERE id = expense_claim.employee_id),
	snap_employee_code = (SELECT code FROM employee WHERE id = expense_claim.employee_id),
	total = (SELECT SUM(amount) FROM expense_claim_line WHERE parent_id = expense_claim.id),
	period_from = (SELECT MIN(expense_date) FROM expense_claim_line WHERE parent_id = expense_claim.id),
	period_to = (SELECT MAX(expense_date) FROM expense_claim_line WHERE parent_id = expense_claim.id)`)
	e.e8Exec(t, `UPDATE expense_claim SET net_payable = total - COALESCE(less_advance, 0)`)
	e.e8Exec(t, `
UPDATE expense_claim_line SET
	snap_type_code = COALESCE((SELECT code FROM expense_type WHERE id = expense_claim_line.expense_type_id), ''),
	snap_type_name = COALESCE((SELECT name FROM expense_type WHERE id = expense_claim_line.expense_type_id), ''),
	snap_project_code = COALESCE((SELECT code FROM project WHERE id = expense_claim_line.project_id), ''),
	snap_site_code = COALESCE((SELECT code FROM site WHERE id = expense_claim_line.site_id), ''),
	snap_billed_gstin = COALESCE((SELECT gstin FROM branch WHERE id = expense_claim_line.billed_to_id), '')`)
	e.e8Exec(t, `
UPDATE employee_advance SET
	snap_employee_name = (SELECT name FROM employee WHERE id = employee_advance.employee_id),
	snap_employee_code = (SELECT code FROM employee WHERE id = employee_advance.employee_id),
	amount = (SELECT SUM(amount) FROM employee_advance_line WHERE parent_id = employee_advance.id),
	used_amount = COALESCE((SELECT SUM(less_advance) FROM expense_claim
		WHERE advance_id = employee_advance.id AND state IN ('approved', 'closed')), 0)`)
	e.e8Exec(t, `
UPDATE employee_advance_line SET
	snap_type_code = COALESCE((SELECT code FROM expense_type WHERE id = employee_advance_line.expense_type_id), ''),
	snap_type_name = COALESCE((SELECT name FROM expense_type WHERE id = employee_advance_line.expense_type_id), ''),
	snap_project_code = COALESCE((SELECT code FROM project WHERE id = employee_advance_line.project_id), ''),
	snap_site_code = COALESCE((SELECT code FROM site WHERE id = employee_advance_line.site_id), '')`)
}

// E8SetReceipt stores sha as the receipt of line lineNo of claim claimID
// (E3 links it through the line page).
func (e *Env) E8SetReceipt(t testing.TB, claimID int64, lineNo int, sha string) {
	t.Helper()
	e.e8Exec(t, `UPDATE expense_claim_line SET receipt_sha256 = ? WHERE parent_id = ? AND line_no = ?`, sha, claimID, lineNo)
}

// E8SetPaidOn stores paid_on (ISO date) on advance advanceID (E9's Mark as
// paid sets it).
func (e *Env) E8SetPaidOn(t testing.TB, advanceID int64, iso string) {
	t.Helper()
	e.e8Exec(t, `UPDATE employee_advance SET paid_on = ? WHERE id = ?`, iso, advanceID)
}

func (e *Env) e8Exec(t testing.TB, q string, args ...any) {
	t.Helper()
	err := e.DB.Write(context.Background(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(context.Background(), q, args...)
		return err
	})
	if err != nil {
		t.Fatalf("expensetest: fixture SQL: %v", err)
	}
}

// E8Receipt is a receipt hash for the report cases.
const E8Receipt = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"

// E8OpenScenario builds the open reports' world (EXP-034). Advances, made
// at 2026-10-24 10:30 IST:
//   - EA/26-27/00001 Deepak, 2026-10-24, `Pune Metro site visit`, ₹5,000.00,
//     approved by Priya, paid on 2026-10-25;
//   - EA/26-27/00002 Kavya, 2026-10-20, `Bengaluru client visit`,
//     ₹3,000.50, approved by Meena, unpaid;
//   - EA/26-27/00003 Deepak ₹700.00, approved, then closed by Meena;
//   - EA/26-27/00004 Kavya ₹900.00, approved, then cancelled by Meena;
//   - a pending advance of Deepak's for ₹600.00.
//
// Claims, all dated 2026-10-24:
//   - EX/26-27/00001 Deepak, `Pune Metro site visit`, lines ₹1,250.00 (with a
//     receipt) and ₹830.55 (none), less advance ₹1,000.00 from
//     EA/26-27/00001, approved by Priya at 2026-10-27 01:00 IST;
//   - EX/26-27/00002 Kavya, `Whitefield client meeting`, lines ₹1,999.99 and
//     ₹0.02 (no receipts), no advance, approved by Meena at 2026-10-28
//     10:30 IST;
//   - EX/26-27/00003 Deepak ₹400.00, approved, then closed by Meena;
//   - EX/26-27/00004 Kavya ₹300.00, approved, then cancelled by Meena;
//   - a pending claim of Deepak's for ₹200.00.
//
// The clock is then 2026-11-03 00:30 IST.
func (e *Env) E8OpenScenario(t testing.TB) {
	t.Helper()
	deepak, kavya, meena := e.P(t, "deepak@vardhan.test"), e.P(t, "kavya@vardhan.test"), e.P(t, "meena@vardhan.test")
	e.E8At(2026, time.October, 24, 10, 30)

	advD := e.E8Approve(t, deepak, expense.AdvanceDocType, e.DraftAdvance(t, deepak, nil, Values{"line_amount": "5000.00"}))
	e.E8Approve(t, kavya, expense.AdvanceDocType, e.DraftAdvance(t, kavya,
		Values{"advance_date": "2026-10-20", "purpose": "Bengaluru client visit"}, Values{"line_amount": "3000.50"}))
	advClosed := e.E8Approve(t, deepak, expense.AdvanceDocType, e.DraftAdvance(t, deepak, nil, Values{"line_amount": "700.00"}))
	e.E8Step(t, meena, expense.AdvanceDocType, advClosed.ID, doc.ActionClose, "Returned in cash.")
	advCancelled := e.E8Approve(t, kavya, expense.AdvanceDocType, e.DraftAdvance(t, kavya, nil, Values{"line_amount": "900.00"}))
	e.E8Step(t, meena, expense.AdvanceDocType, advCancelled.ID, doc.ActionCancel, "Trip called off.")
	advPending := e.DraftAdvance(t, deepak, nil, Values{"line_amount": "600.00"})
	e.E8Step(t, deepak, expense.AdvanceDocType, advPending.ID, doc.ActionSubmit, "")
	e.E8SetPaidOn(t, advD.ID, "2026-10-25")

	e.E8At(2026, time.October, 27, 1, 0)
	d1 := e.ApprovedClaim(t, deepak, Values{"advance_id": e8itoa(advD.ID), "less_advance": "1000.00"},
		Values{"amount": "1250.00"}, Values{"amount": "830.55"})
	e.E8SetReceipt(t, d1.ID, 1, E8Receipt)

	e.E8At(2026, time.October, 28, 10, 30)
	e.ApprovedClaim(t, kavya, Values{"purpose": "Whitefield client meeting"},
		Values{"amount": "1999.99"}, Values{"amount": "0.02"})
	d2 := e.ApprovedClaim(t, deepak, nil, Values{"amount": "400.00"})
	e.E8Step(t, meena, expense.ClaimDocType, d2.ID, doc.ActionClose, "Paid by NEFT 1234.")
	k2 := e.ApprovedClaim(t, kavya, nil, Values{"amount": "300.00"})
	e.E8Step(t, meena, expense.ClaimDocType, k2.ID, doc.ActionCancel, "Entered twice.")
	d3 := e.DraftClaim(t, deepak, nil, Values{"amount": "200.00"})
	e.E8Step(t, deepak, expense.ClaimDocType, d3.ID, doc.ActionSubmit, "")

	e.E8Fill(t)
	e.E8At(2026, time.November, 3, 0, 30)
}

// E8LinesScenario builds the expense lines report's world (EXP-035), at
// 2026-10-24 10:30 IST:
//   - EX/26-27/00001 Deepak, approved by Priya. Line 1: 2026-09-30, LODGING,
//     `Hotel Pune 2 nights`, ₹4,720.00, PRJ-METRO, METRO-SITE, a company bill
//     from 27AABCS1429B1ZU, invoice `INV/778`, billed to BLR, GST ₹720.00,
//     with a receipt. Line 2: 2026-10-22, FOOD, `Team lunch`, ₹830.55,
//     PRJ-TECHPARK, TECH-SITE, no company bill, no GST, no receipt;
//   - EX/26-27/00002 Kavya, approved by Meena, then closed by Meena. Line 1:
//     2026-10-23, FUEL, `Diesel for site jeep`, ₹0.05, no project or site,
//     no receipt.
func (e *Env) E8LinesScenario(t testing.TB) {
	t.Helper()
	deepak, kavya, meena := e.P(t, "deepak@vardhan.test"), e.P(t, "kavya@vardhan.test"), e.P(t, "meena@vardhan.test")
	e.E8At(2026, time.October, 24, 10, 30)
	l1 := e.ApprovedClaim(t, deepak, nil,
		Values{
			"expense_date": "2026-09-30", "expense_type_id": e.expenseType(t, "LODGING"), "description": "Hotel Pune 2 nights",
			"amount": "4720.00", "project_id": e8itoa(e.E8Project(t, "PRJ-METRO")), "site_id": e8itoa(e.E8Site(t, "METRO-SITE")),
			"company_bill": "true", "supplier_gstin": "27AABCS1429B1ZU", "invoice_no": "INV/778",
			"billed_to_id": e8itoa(e.E8Branch(t, "BLR")), "gst_amount": "720.00",
		},
		Values{
			"expense_date": "2026-10-22", "expense_type_id": e.expenseType(t, "FOOD"), "description": "Team lunch",
			"amount": "830.55", "project_id": e8itoa(e.E8Project(t, "PRJ-TECHPARK")), "site_id": e8itoa(e.E8Site(t, "TECH-SITE")),
		})
	e.E8SetReceipt(t, l1.ID, 1, E8Receipt)
	l2 := e.ApprovedClaim(t, kavya, nil, Values{
		"expense_date": "2026-10-23", "expense_type_id": e.expenseType(t, "FUEL"), "description": "Diesel for site jeep",
		"amount": "0.05",
	})
	e.E8Step(t, meena, expense.ClaimDocType, l2.ID, doc.ActionClose, "Paid by NEFT 1234.")
	e.E8Fill(t)
}

// E8Employee is the database id of the fixture employee with code.
func (e *Env) E8Employee(t testing.TB, code string) int64 {
	t.Helper()
	for _, emp := range e.F.Employees {
		if emp.Code == code {
			return emp.ID
		}
	}
	t.Fatalf("expensetest: no employee %s", code)
	return 0
}

// E8Project is the database id of the fixture project with code.
func (e *Env) E8Project(t testing.TB, code string) int64 {
	t.Helper()
	for _, p := range e.F.Projects {
		if p.Code == code {
			return p.ID
		}
	}
	t.Fatalf("expensetest: no project %s", code)
	return 0
}

// E8Site is the database id of the fixture site with code.
func (e *Env) E8Site(t testing.TB, code string) int64 {
	t.Helper()
	for _, s := range e.F.Sites {
		if s.Code == code {
			return s.ID
		}
	}
	t.Fatalf("expensetest: no site %s", code)
	return 0
}

// E8Branch is the database id of the fixture branch with code.
func (e *Env) E8Branch(t testing.TB, code string) int64 {
	t.Helper()
	for _, b := range e.F.Branches {
		if b.Code == code {
			return b.ID
		}
	}
	t.Fatalf("expensetest: no branch %s", code)
	return 0
}

// E8Type is the database id of the expense type with code.
func (e *Env) E8Type(t testing.TB, code string) int64 {
	t.Helper()
	id, ok := e.F.ExpenseTypeIDs[code]
	if !ok {
		t.Fatalf("expensetest: no expense type %s", code)
	}
	return id
}

func e8itoa(n int64) string { return strconv.FormatInt(n, 10) }

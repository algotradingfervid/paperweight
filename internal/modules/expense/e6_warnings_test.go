package expense_test

import (
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
)

// Expense spec §5 (Warnings): warnings never refuse; a claim the viewer can't
// read is never named and the text says "another claim" (CD-264 (4), CD-240,
// CD-248); cancelled claims don't count. Plan MOD-EXP E6 Do 2. Every case runs
// on the real auth.Service (expensetest.Env), and the warnings are read
// through ClaimWarnings, which the check page and the approval page call.

const (
	e6ShreeGSTIN = "27AABCS1429B1ZU" // SHREE Mumbai, 00-fixtures
	e6CancelWhy  = "Entered twice by mistake."
)

// EXP-014: a receipt on another live claim of any employee, and a line the
// same as one on another live claim of the same employee, warn and never
// refuse; once the other claim is cancelled they don't. A claim the viewer
// can't read (Suresh's, for Deepak and Priya) is "another claim", and its
// number appears nowhere; the Director sees it.
func TestExp_DuplicateWarnings(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, "deepak@vardhan.test")
	priya := e.P(t, "priya@vardhan.test") // Deepak's manager; can't read Suresh's claims
	suresh := e.P(t, "suresh@vardhan.test")
	ravi := e.P(t, "ravi@vardhan.test") // Suresh's manager
	meena := e.P(t, "meena@vardhan.test")
	billH := filestest.JPEG(40_001) // Deepak's taxi bill
	billS := filestest.JPEG(40_002) // the shared taxi bill Suresh also claims

	// Deepak's approved EX/26-27/00001: 2026-10-20, TRAVEL, ₹1,250.00, receipt H.
	old := e.DraftClaim(t, deepak, nil, expensetest.Values{"expense_date": "2026-10-20", "amount": "1250.00"}).ID
	h := e6Attach(t, e, deepak, old, "taxi.jpg", billH)
	e6SetReceipts(t, e, deepak, old, map[int]string{1: h})
	oldNo := e6Approve(t, e, deepak, priya, expense.ClaimDocType, old)
	if oldNo != "EX/26-27/00001" {
		t.Fatalf("Deepak's first claim number = %q, want EX/26-27/00001", oldNo)
	}

	// Suresh's approved claim holding the shared bill, same date, type and
	// amount as Deepak's line 3 (another employee: no same-line warning).
	other := e.DraftClaim(t, suresh, nil, expensetest.Values{"expense_date": "2026-10-21", "amount": "450.00"}).ID
	s := e6Attach(t, e, suresh, other, "shared-taxi.jpg", billS)
	e6SetReceipts(t, e, suresh, other, map[int]string{1: s})
	otherNo := e6Approve(t, e, suresh, ravi, expense.ClaimDocType, other)

	// Deepak's new claim: line 1 holds H, line 2 is the same as the old line,
	// line 3 holds the shared bill.
	claim := e.DraftClaim(t, deepak, nil,
		expensetest.Values{"expense_date": "2026-10-23", "expense_type_id": e6Type(t, e, "FOOD"), "amount": "300.00"},
		expensetest.Values{"expense_date": "2026-10-20", "amount": "1250.00"},
		expensetest.Values{"expense_date": "2026-10-21", "amount": "450.00"},
	).ID
	e6Attach(t, e, deepak, claim, "taxi.jpg", billH)
	e6Attach(t, e, deepak, claim, "shared-taxi.jpg", billS)
	e6SetReceipts(t, e, deepak, claim, map[int]string{1: h, 3: s})

	hidden := []string{otherNo, "Suresh"}
	wantHidden := []string{
		"Line 1: This receipt is also on EX/26-27/00001.",
		"Line 2: This looks the same as a line on EX/26-27/00001.",
		"Line 3: This receipt is also on another claim.",
	}

	// The check page (Deepak, before submit).
	got := e6Warnings(t, e, deepak, claim)
	e6WantWarnings(t, "deepak (check page)", got, wantHidden...)
	expensetest.WantNotNamed(t, e6Joined(got), hidden...)

	// Warnings never refuse.
	if res := e6Do(t, e, deepak, expense.ClaimDocType, claim, doc.ActionSubmit, ""); res.State != doc.StatePending {
		t.Fatalf("submit with warnings: state %q, want pending", res.State)
	}

	// The approval page (Priya, Deepak's manager; she can't read Suresh's).
	got = e6Warnings(t, e, priya, claim)
	e6WantWarnings(t, "priya (approval page)", got, wantHidden...)
	expensetest.WantNotNamed(t, e6Joined(got), hidden...)

	// The Director reads every claim, so Suresh's number is named.
	e6WantWarnings(t, "meena", e6Warnings(t, e, meena, claim),
		"Line 1: This receipt is also on EX/26-27/00001.",
		"Line 2: This looks the same as a line on EX/26-27/00001.",
		"Line 3: This receipt is also on "+otherNo+".",
	)

	// Cancelled claims don't count: first Deepak's old claim, then Suresh's.
	e6Do(t, e, meena, expense.ClaimDocType, old, doc.ActionCancel, e6CancelWhy)
	got = e6Warnings(t, e, deepak, claim)
	e6WantWarnings(t, "deepak, old claim cancelled", got, "Line 3: This receipt is also on another claim.")
	expensetest.WantNotNamed(t, e6Joined(got), hidden...)
	e6Do(t, e, meena, expense.ClaimDocType, other, doc.ActionCancel, e6CancelWhy)
	e6WantWarnings(t, "deepak, both cancelled", e6Warnings(t, e, deepak, claim))
}

// EXP-056: two lines of one claim with the same date, type and amount and
// different receipts warn "Line 2: This looks the same as line 1."; submit
// still succeeds. Two such lines sharing one receipt (a split bill) don't
// warn (CD-271 G2).
func TestExp_DuplicateWithinClaim(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, "deepak@vardhan.test")
	kavya := e.P(t, "kavya@vardhan.test")

	same := expensetest.Values{"expense_date": "2026-10-22", "amount": "800.00"}
	claim := e.DraftClaim(t, deepak, nil, same, same).ID
	a := e6Attach(t, e, deepak, claim, "bill-a.jpg", filestest.JPEG(41_001))
	b := e6Attach(t, e, deepak, claim, "bill-b.jpg", filestest.JPEG(41_002))
	e6SetReceipts(t, e, deepak, claim, map[int]string{1: a, 2: b})

	e6WantWarnings(t, "deepak", e6Warnings(t, e, deepak, claim), "Line 2: This looks the same as line 1.")
	if res := e6Do(t, e, deepak, expense.ClaimDocType, claim, doc.ActionSubmit, ""); res.State != doc.StatePending {
		t.Fatalf("submit with a warning: state %q, want pending", res.State)
	}

	// A split bill: both lines hold one receipt on purpose (Same receipt as
	// line 1). Kavya's claim, so nothing matches Deepak's.
	split := expensetest.Values{"expense_date": "2026-10-23", "expense_type_id": e6Type(t, e, "FOOD"), "amount": "640.00"}
	shared := e.DraftClaim(t, kavya, nil, split, split).ID
	c := e6Attach(t, e, kavya, shared, "dinner.jpg", filestest.JPEG(41_003))
	e6SetReceipts(t, e, kavya, shared, map[int]string{1: c, 2: c})
	e6WantWarnings(t, "kavya (split bill)", e6Warnings(t, e, kavya, shared))
}

// EXP-057: the same supplier GSTIN and invoice no. on another live claim of
// any employee warns, naming the claim only to a viewer who can read it; a
// different invoice no. or a cancelled claim doesn't; submit still succeeds
// (CD-271 G3).
func TestExp_DuplicateInvoice(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, "deepak@vardhan.test")
	suresh := e.P(t, "suresh@vardhan.test")
	ravi := e.P(t, "ravi@vardhan.test")
	meena := e.P(t, "meena@vardhan.test")
	pune := e6Branch(t, e, "PUNE")
	companyBill := func(date, amount, invoice string) expensetest.Values {
		return expensetest.Values{"expense_date": date, "amount": amount, "company_bill": "true",
			"supplier_gstin": e6ShreeGSTIN, "invoice_no": invoice, "billed_to_id": pune}
	}

	// Suresh's approved claim with a company bill, INV-778.
	other := e.DraftClaim(t, suresh, nil, companyBill("2026-10-21", "2360.00", "INV-778")).ID
	r := e6Attach(t, e, suresh, other, "inv-778.jpg", filestest.JPEG(42_001))
	e6SetReceipts(t, e, suresh, other, map[int]string{1: r})
	otherNo := e6Approve(t, e, suresh, ravi, expense.ClaimDocType, other)

	// Deepak's claim: line 1 the same invoice (a re-shot photo, other bytes),
	// line 2 the same supplier with another invoice no.
	claim := e.DraftClaim(t, deepak, nil,
		companyBill("2026-10-22", "2360.00", "INV-778"),
		companyBill("2026-10-23", "1180.00", "INV-779"),
	).ID
	q := e6Attach(t, e, deepak, claim, "inv-778-again.jpg", filestest.JPEG(42_002))
	e6SetReceipts(t, e, deepak, claim, map[int]string{1: q})

	got := e6Warnings(t, e, deepak, claim)
	e6WantWarnings(t, "deepak (check page)", got,
		"Line 1: Invoice INV-778 from 27AABCS1429B1ZU is also on another claim.")
	expensetest.WantNotNamed(t, e6Joined(got), otherNo, "Suresh")

	e6WantWarnings(t, "meena", e6Warnings(t, e, meena, claim),
		"Line 1: Invoice INV-778 from 27AABCS1429B1ZU is also on "+otherNo+".")

	if res := e6Do(t, e, deepak, expense.ClaimDocType, claim, doc.ActionSubmit, ""); res.State != doc.StatePending {
		t.Fatalf("submit with a warning: state %q, want pending", res.State)
	}

	e6Do(t, e, meena, expense.ClaimDocType, other, doc.ActionCancel, e6CancelWhy)
	e6WantWarnings(t, "deepak, Suresh's claim cancelled", e6Warnings(t, e, deepak, claim))
}

// EXP-061: a company bill dated in a financial year whose following 30
// November has passed, by the engine's clock, warns (CGST s.16(4), CD-271
// G8); a later one and a non-company bill don't; the claim still submits.
func TestExp_TimeBarWarning(t *testing.T) {
	e := expensetest.New(t) // clock 2026-10-24 10:30 IST
	admin := e.P(t, "admin@vardhan.test")
	pune := e6Branch(t, e, "PUNE")
	companyBill := func(date, amount, invoice string) expensetest.Values {
		return expensetest.Values{"expense_date": date, "amount": amount, "company_bill": "true",
			"supplier_gstin": e6ShreeGSTIN, "invoice_no": invoice, "billed_to_id": pune}
	}

	// An Administrator back-enters Deepak's claim.
	claim := e.DraftClaim(t, admin, expensetest.Values{"employee_id": e6Employee(t, e, "EMP-007")},
		companyBill("2025-03-31", "1000.00", "INV-101"),                       // FY 24-25: closed 30-11-2025
		companyBill("2026-03-31", "1100.00", "INV-102"),                       // FY 25-26: closes 30-11-2026
		expensetest.Values{"expense_date": "2025-03-31", "amount": "1200.00"}, // not a company bill
	).ID

	line1 := "Line 1: This bill is dated in FY 24-25. Input tax credit on it closed on 30-11-2025."
	e6WantWarnings(t, "admin on 24-10-2026", e6Warnings(t, e, admin, claim), line1)

	if res := e6Do(t, e, admin, expense.ClaimDocType, claim, doc.ActionSubmit, ""); res.State != doc.StatePending {
		t.Fatalf("submit with a warning: state %q, want pending", res.State)
	}

	// The engine's clock decides: on 30 November the date hasn't passed yet;
	// from 1 December the FY 25-26 bill warns too.
	ist := time.FixedZone("IST", 5*3600+1800)
	e.Clock.Set(time.Date(2026, 11, 30, 10, 30, 0, 0, ist))
	e6WantWarnings(t, "admin on 30-11-2026", e6Warnings(t, e, admin, claim), line1)
	e.Clock.Set(time.Date(2026, 12, 1, 10, 30, 0, 0, ist))
	e6WantWarnings(t, "admin on 01-12-2026", e6Warnings(t, e, admin, claim), line1,
		"Line 2: This bill is dated in FY 25-26. Input tax credit on it closed on 30-11-2026.")
}

// EXP-062: with no approval rule for expense.Claim, an Administrator's
// back-entered claim whose line duplicates one on Deepak's approved claim
// shows the duplicate warning before submit, and submit approves and numbers
// it at once (CD-271 G12).
func TestExp_BackEntryWarnings(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, "deepak@vardhan.test")
	admin := e.P(t, "admin@vardhan.test")
	line := expensetest.Values{"expense_date": "2026-10-20", "amount": "1250.00"}

	if res := e.ApprovedClaim(t, deepak, nil, line); res.Number != "EX/26-27/00001" {
		t.Fatalf("Deepak's approved claim number = %q, want EX/26-27/00001", res.Number)
	}

	// No approval rule for expense.Claim: the cutover's back-entry window.
	rules, err := e.Approvals.Rules(t.Context(), admin)
	if err != nil {
		t.Fatalf("Rules: %v", err)
	}
	for _, r := range rules {
		if r.DocType == expense.ClaimDocType {
			if err := e.Approvals.SetRuleEnabled(t.Context(), admin, r.ID, false); err != nil {
				t.Fatalf("disable rule %d: %v", r.ID, err)
			}
		}
	}

	claim := e.DraftClaim(t, admin, expensetest.Values{"employee_id": e6Employee(t, e, "EMP-007")}, line).ID
	e6WantWarnings(t, "admin (check page)", e6Warnings(t, e, admin, claim),
		"Line 1: This looks the same as a line on EX/26-27/00001.")

	res := e6Do(t, e, admin, expense.ClaimDocType, claim, doc.ActionSubmit, "")
	if res.State != doc.StateApproved || res.Number != "EX/26-27/00002" {
		t.Errorf("submit with no rule: state %q, number %q, want approved EX/26-27/00002", res.State, res.Number)
	}
}

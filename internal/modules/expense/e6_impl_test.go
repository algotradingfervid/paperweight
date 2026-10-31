package expense_test

import (
	"errors"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
)

// Implementer's tests for MOD-EXP E6 Do 2 (ClaimWarnings), beyond the
// committed e6_*_test.go files; all on the real auth.Service.

// A line with no date yet matches no other line: two undated lines of one
// claim aren't "the same", and an undated company bill isn't time-barred.
func TestExp_WarningsSkipUndatedLines(t *testing.T) {
	e := expensetest.New(t)
	admin := e.P(t, "admin@vardhan.test")
	undated := expensetest.Values{"expense_date": "", "amount": "500.00", "company_bill": "true",
		"supplier_gstin": e6ShreeGSTIN, "invoice_no": "INV-900", "billed_to_id": e6Branch(t, e, "PUNE")}
	claim := e.DraftClaim(t, admin, expensetest.Values{"employee_id": e6Employee(t, e, "EMP-007")}, undated, undated).ID
	if d := e6Load(t, e, expense.ClaimDocType, claim).Lines[0]["expense_date"]; d != "" {
		t.Fatalf("line 1 expense_date = %q after the save, want blank", d)
	}
	e6WantWarnings(t, "admin", e6Warnings(t, e, admin, claim))
}

// A claim the viewer can't read and a missing id both answer doc.Gone with no
// warnings: hidden looks the same as missing (CD-245).
func TestExp_WarningsHiddenOrMissingClaim(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, "deepak@vardhan.test")
	suresh := e.P(t, "suresh@vardhan.test")
	sureshs := e.DraftClaim(t, suresh, nil).ID
	for _, c := range []struct {
		name string
		id   int64
	}{{"suresh's claim", sureshs}, {"missing id", sureshs + 1000}} {
		got, err := e.Service.ClaimWarnings(t.Context(), deepak, c.id)
		var de *doc.Error
		if !errors.As(err, &de) || de.Kind != doc.KindNotFound || de.Message != doc.Gone().Message {
			t.Errorf("ClaimWarnings(deepak, %s) error = %v, want doc.Gone()", c.name, err)
		}
		if got != nil {
			t.Errorf("ClaimWarnings(deepak, %s) = %q, want none", c.name, got)
		}
	}
}

// Only pending, approved and closed claims count: a draft and a rejected
// claim sharing the receipt, the same line and the same supplier invoice
// don't warn.
func TestExp_WarningsIgnoreDraftAndRejected(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, "deepak@vardhan.test")
	priya := e.P(t, "priya@vardhan.test")
	bill := filestest.JPEG(43_001)
	line := expensetest.Values{"expense_date": "2026-10-20", "amount": "2360.00", "company_bill": "true",
		"supplier_gstin": e6ShreeGSTIN, "invoice_no": "INV-555", "billed_to_id": e6Branch(t, e, "PUNE")}
	withReceipt := func() int64 {
		id := e.DraftClaim(t, deepak, nil, line).ID
		h := e6Attach(t, e, deepak, id, "inv-555.jpg", bill)
		e6SetReceipts(t, e, deepak, id, map[int]string{1: h})
		return id
	}
	withReceipt() // stays a draft
	rejected := withReceipt()
	e6Do(t, e, deepak, expense.ClaimDocType, rejected, doc.ActionSubmit, "")
	if res := e6Do(t, e, priya, expense.ClaimDocType, rejected, doc.ActionReject, "Wrong project."); res.State != doc.StateRejected {
		t.Fatalf("reject: state %q, want rejected", res.State)
	}
	claim := withReceipt()
	e6WantWarnings(t, "deepak", e6Warnings(t, e, deepak, claim))
}

// With two matching claims, the warning names the one the viewer can read,
// even when an unreadable one has the lower id.
func TestExp_WarningsNameTheReadableClaim(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, "deepak@vardhan.test")
	priya := e.P(t, "priya@vardhan.test")
	suresh := e.P(t, "suresh@vardhan.test")
	ravi := e.P(t, "ravi@vardhan.test")
	bill := filestest.JPEG(44_001)
	approved := func(p, approver auth.Principal) string {
		id := e.DraftClaim(t, p, nil, expensetest.Values{"amount": "450.00"}).ID
		h := e6Attach(t, e, p, id, "taxi.jpg", bill)
		e6SetReceipts(t, e, p, id, map[int]string{1: h})
		return e6Approve(t, e, p, approver, expense.ClaimDocType, id)
	}
	hiddenNo := approved(suresh, ravi) // lower id, unreadable by Deepak
	ownNo := approved(deepak, priya)
	claim := e.DraftClaim(t, deepak, nil, expensetest.Values{"expense_date": "2026-10-23", "amount": "450.00"}).ID
	h := e6Attach(t, e, deepak, claim, "taxi.jpg", bill)
	e6SetReceipts(t, e, deepak, claim, map[int]string{1: h})

	got := e6Warnings(t, e, deepak, claim)
	e6WantWarnings(t, "deepak", got, "Line 1: This receipt is also on "+ownNo+".")
	expensetest.WantNotNamed(t, e6Joined(got), hiddenNo)
}

// Two lines of one claim with the same date, type and amount and no receipt
// at all still warn: only a shared receipt makes them a split bill.
func TestExp_WarningsSameLinesWithoutReceipts(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, "deepak@vardhan.test")
	same := expensetest.Values{"expense_date": "2026-10-22", "amount": "800.00"}
	claim := e.DraftClaim(t, deepak, nil, same, same).ID
	e6WantWarnings(t, "deepak", e6Warnings(t, e, deepak, claim), "Line 2: This looks the same as line 1.")
}

// The time bar goes by the IST date: at 00:30 IST on 01-12-2026 (still
// 30-11 in UTC) a FY 25-26 company bill warns.
func TestExp_TimeBarUsesISTDate(t *testing.T) {
	e := expensetest.New(t)
	admin := e.P(t, "admin@vardhan.test")
	claim := e.DraftClaim(t, admin, expensetest.Values{"employee_id": e6Employee(t, e, "EMP-007")},
		expensetest.Values{"expense_date": "2026-03-31", "amount": "1100.00", "company_bill": "true",
			"supplier_gstin": e6ShreeGSTIN, "invoice_no": "INV-102", "billed_to_id": e6Branch(t, e, "PUNE")}).ID
	e.Clock.Set(time.Date(2026, 12, 1, 0, 30, 0, 0, time.FixedZone("IST", 5*3600+1800)))
	e6WantWarnings(t, "admin at 01-12-2026 00:30 IST", e6Warnings(t, e, admin, claim),
		"Line 1: This bill is dated in FY 25-26. Input tax credit on it closed on 30-11-2026.")
}

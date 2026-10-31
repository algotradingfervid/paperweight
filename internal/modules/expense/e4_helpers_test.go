package expense_test

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Helpers for the E4 tests (advance settlement and invariants). Names carry
// the e4 prefix so the parallel MOD-EXP lanes' test files can't clash.

// The 00-fixtures users E4's cases use.
const (
	e4Deepak = "deepak@vardhan.test" // Employee, EMP-007, reports to Priya
	e4Priya  = "priya@vardhan.test"  // EMP-004, Deepak's reporting manager; reports to Ravi
	e4Meena  = "meena@vardhan.test"  // Director (cancel, close, Mark as paid); Employee in the expense seed
	e4Kavya  = "kavya@vardhan.test"  // EMP-006, reports to Meena: can't read Deepak's advances
	e4Ravi   = "ravi@vardhan.test"   // EMP-003, Priya's manager: Deepak isn't his direct report
)

// The first advance and claim numbers of a fresh world (FY 2026-27).
const (
	e4EA1 = "EA/26-27/00001"
	e4EX1 = "EX/26-27/00001"
)

// Spec §5 and §4 texts (E4's catalogue), copied exactly; {advance} is the
// advance's number.
const (
	e4MsgChooseAdvance = "Choose the advance to set this against."
	e4MsgAboveTotal    = "Less advance can't be more than the total."
	e4FmtAboveLeft     = "Less advance can't be more than the ₹%s left on %s."
	e4FmtZero          = "Enter the amount to set against %s, or remove the advance."
	e4FmtOtherEmployee = "%s belongs to another employee."
	e4FmtNotPaid       = "%s hasn't been marked as paid yet."
	e4FmtWarning       = "%s still has ₹%s left on %s." // CD-271 G7: employee, left, advance
)

// Other packages' texts (CD-142).
const (
	// DOC-017 family (doc/messages.go): the source's display, then
	// "employee advance".
	e4FmtSourceClosed      = "%s is closed. Remove the line or pick another employee advance."
	e4FmtSourceCancelled   = "%s is cancelled. Remove the line or pick another employee advance."
	e4FmtSourcePending     = "%s is waiting for approval. Pick an approved employee advance."
	e4FmtSourceNotApproved = "%s isn't approved yet. Pick an approved employee advance."
	// DOC-052 with one approved blocker: the advance, the claim, the claim.
	e4FmtBlocked = "Can't cancel %s because %s (approved) is linked to it. Cancel %s first."
	// AUTH's link refusal (CD-240).
	e4MsgLinkRefused = "You can't use this Employee Advance. It isn't available to you."
	// E9's paid-cancel text (CD-281 b).
	e4MsgPaidCancel = "This advance is marked as paid. Undo paid first, or close it with the balance noted."
)

// e4AboveLeft is the left-on-advance refusal for left paise on adv.
func e4AboveLeft(left types.Money, adv string) string {
	return fmt.Sprintf(e4FmtAboveLeft, types.FormatMoney(left), adv)
}

// e4Rs is rupees as paise: e4Rs(4000) is ₹4,000.00.
func e4Rs(r int64) types.Money { return types.Money(r * 100) }

// e4ID is id as a form value.
func e4ID(id int64) string { return e2ID(id) }

// e4Claim saves a draft claim for p's employee with one line of amount (a
// form value such as "6000.00") and header merged over DraftClaim's
// defaults.
func e4Claim(t *testing.T, e *expensetest.Env, p auth.Principal, amount string, header expensetest.Values) doc.Result {
	t.Helper()
	return e.DraftClaim(t, p, header, expensetest.Values{"amount": amount})
}

// e4On is header values naming advance adv with less advance less ("" sends
// it blank).
func e4On(adv int64, less string) expensetest.Values {
	return expensetest.Values{"advance_id": e4ID(adv), "less_advance": less}
}

// e4WantUsed fails unless the advance's used amount and left (amount − used)
// are used and left after step (research T3).
func e4WantUsed(t *testing.T, e *expensetest.Env, adv int64, step string, used, left types.Money) {
	t.Helper()
	gu, gl := e.UsedLeft(t, adv)
	if gu != used || gl != left {
		t.Errorf("%s: advance %d used ₹%s, left ₹%s; want used ₹%s, left ₹%s", step, adv,
			types.FormatMoney(gu), types.FormatMoney(gl), types.FormatMoney(used), types.FormatMoney(left))
	}
}

// e4State is the stored state of document id of dt.
func e4State(t *testing.T, e *expensetest.Env, dt string, id int64) doc.State {
	t.Helper()
	var s string
	e2Scan(t, e, `SELECT state FROM `+e4Table(dt)+` WHERE id = ?`, []any{id}, &s)
	return doc.State(s)
}

// e4Version is the stored version of document id of dt.
func e4Version(t *testing.T, e *expensetest.Env, dt string, id int64) int {
	t.Helper()
	return e6Load(t, e, dt, id).Version
}

// e4Number is the stored number of document id of dt ("" before approval).
func e4Number(t *testing.T, e *expensetest.Env, dt string, id int64) string {
	t.Helper()
	var n string
	e2Scan(t, e, `SELECT COALESCE(number, '') FROM `+e4Table(dt)+` WHERE id = ?`, []any{id}, &n)
	return n
}

func e4Table(dt string) string {
	if dt == expense.AdvanceDocType {
		return "employee_advance"
	}
	return "expense_claim"
}

// e4UpdatedBy is the claim's stored updated_by.
func e4UpdatedBy(t *testing.T, e *expensetest.Env, claim int64) types.UserID {
	t.Helper()
	var u int64
	e2Scan(t, e, `SELECT updated_by FROM expense_claim WHERE id = ?`, []any{claim}, &u)
	return types.UserID(u)
}

// e4Effects are the advance's audit entries with action effect, oldest first.
func e4Effects(t *testing.T, e *expensetest.Env, adv int64) []audit.Entry {
	t.Helper()
	var all []audit.Entry
	err := e.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		all, err = audit.ForDoc(t.Context(), tx, expense.AdvanceDocType, adv)
		return err
	})
	if err != nil {
		t.Fatalf("audit of advance %d: %v", adv, err)
	}
	var out []audit.Entry
	for _, en := range all {
		if en.Action == "effect" {
			out = append(out, en)
		}
	}
	slices.SortFunc(out, func(a, b audit.Entry) int { return a.At.Compare(b.At) })
	return out
}

// e4WantEffect fails unless effects holds exactly one entry caused by claim
// (with its number) with action cause, by the claim's updated_by.
func e4WantEffect(t *testing.T, e *expensetest.Env, effects []audit.Entry, claim int64, cause string) {
	t.Helper()
	want := audit.Ref{DocType: expense.ClaimDocType, DocID: claim, Number: e4Number(t, e, expense.ClaimDocType, claim), Action: cause}
	by := e4UpdatedBy(t, e, claim)
	var n int
	for _, en := range effects {
		if en.CausedBy != nil && *en.CausedBy == want {
			n++
			if en.By != by {
				t.Errorf("effect entry caused by %+v: By %d, want the claim's updated_by %d", want, en.By, by)
			}
		}
	}
	if n != 1 {
		t.Errorf("effect entries caused by %+v = %d, want 1 (all effect entries: %+v)", want, n, effects)
	}
}

// e4Paid marks advance adv as paid on the business date through E9's
// MarkPaid, as meena.
func e4Paid(t *testing.T, e *expensetest.Env, adv int64) {
	t.Helper()
	on, err := types.DateFromISO(expensetest.Date)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Service.MarkPaid(t.Context(), e.P(t, e4Meena), adv, e4Version(t, e, expense.AdvanceDocType, adv), on); err != nil {
		t.Fatalf("MarkPaid advance %d: %v", adv, err)
	}
}

// --- E4's Service methods, asserted at run time so the tests compile before
// E4 builds them and fail with a message naming the missing method. A missing
// method is an Errorf, not a Fatalf, so the rest of the case still runs.

// e4LeftOner is the plan's E4 Do 6.
type e4LeftOner interface {
	LeftOn(ctx context.Context, p auth.Principal, advanceID int64) (types.Money, error)
}

// e4LeftOn is (*expense.Service).LeftOn(ctx, p, adv).
func e4LeftOn(t *testing.T, e *expensetest.Env, p auth.Principal, adv int64) types.Money {
	t.Helper()
	s, ok := any(e.Service).(e4LeftOner)
	if !ok {
		t.Errorf("*expense.Service has no LeftOn(ctx, p auth.Principal, advanceID int64) (types.Money, error) (MOD-EXP E4 Do 6)")
		return -1
	}
	m, err := s.LeftOn(t.Context(), p, adv)
	if err != nil {
		t.Fatalf("LeftOn(%s, advance %d): %v", p.Name, adv, err)
	}
	return m
}

// e4AdvanceWarner is the plan's E4 Do 7 (CD-271 G7).
type e4AdvanceWarner interface {
	AdvanceWarnings(ctx context.Context, p auth.Principal, advanceID int64) ([]string, error)
}

// e4Warnings is (*expense.Service).AdvanceWarnings(ctx, p, adv).
func e4Warnings(t *testing.T, e *expensetest.Env, p auth.Principal, adv int64) []string {
	t.Helper()
	s, ok := any(e.Service).(e4AdvanceWarner)
	if !ok {
		t.Errorf("*expense.Service has no AdvanceWarnings(ctx, p auth.Principal, advanceID int64) ([]string, error) (MOD-EXP E4 Do 7)")
		return nil
	}
	ws, err := s.AdvanceWarnings(t.Context(), p, adv)
	if err != nil {
		t.Fatalf("AdvanceWarnings(%s, advance %d): %v", p.Name, adv, err)
	}
	return ws
}

// e4Choices is (*expense.Service).AdvanceChoices(ctx, p, employeeID)
// (E4 Do 5): the ids of the returned AdvanceChoice values, and the whole
// result printed with %+v for the hidden-number check. AdvanceChoice isn't
// declared before E4, so the method is called through reflection; each
// AdvanceChoice is assumed to carry the advance's id in a field `ID int64`
// (S1 posts it as advance_id).
func e4Choices(t *testing.T, e *expensetest.Env, p auth.Principal, employeeID int64) (ids []int64, printed string) {
	t.Helper()
	const want = "AdvanceChoices(ctx, p auth.Principal, employeeID int64) ([]AdvanceChoice, error) (MOD-EXP E4 Do 5)"
	m := reflect.ValueOf(e.Service).MethodByName("AdvanceChoices")
	if !m.IsValid() {
		t.Errorf("*expense.Service has no %s", want)
		return nil, "(no AdvanceChoices)"
	}
	mt := m.Type()
	if mt.NumIn() != 3 || mt.NumOut() != 2 || mt.Out(0).Kind() != reflect.Slice || mt.Out(0).Elem().Kind() != reflect.Struct ||
		!mt.Out(1).Implements(reflect.TypeFor[error]()) || !reflect.TypeOf(p).AssignableTo(mt.In(1)) ||
		!reflect.TypeFor[int64]().ConvertibleTo(mt.In(2)) {
		t.Fatalf("(*expense.Service).AdvanceChoices is %s, want %s", mt, want)
	}
	out := m.Call([]reflect.Value{reflect.ValueOf(t.Context()), reflect.ValueOf(p), reflect.ValueOf(employeeID).Convert(mt.In(2))})
	if err, _ := out[1].Interface().(error); err != nil {
		t.Fatalf("AdvanceChoices(%s, employee %d): %v", p.Name, employeeID, err)
	}
	f, ok := mt.Out(0).Elem().FieldByName("ID")
	if !ok || f.Type.Kind() != reflect.Int64 {
		t.Fatalf("expense.AdvanceChoice has no field ID int64 (the advance's id)")
	}
	for i := range out[0].Len() {
		ids = append(ids, out[0].Index(i).FieldByIndex(f.Index).Int())
	}
	return ids, fmt.Sprintf("%+v", out[0].Interface())
}

// e4Employee is the database id of the fixture employee with code.
func e4Employee(t *testing.T, e *expensetest.Env, code string) int64 {
	t.Helper()
	return e2Employee(t, e, code)
}

// e4WantWarnings fails unless AdvanceWarnings' got holds exactly want, in any
// order.
func e4WantWarnings(t *testing.T, who string, got []string, want ...string) {
	t.Helper()
	g, w := slices.Clone(got), slices.Clone(want)
	slices.Sort(g)
	slices.Sort(w)
	if !slices.Equal(g, w) {
		t.Errorf("AdvanceWarnings as %s =\n  %q\nwant\n  %q", who, g, w)
	}
}

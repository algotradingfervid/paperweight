package doc_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// MOD-EXP X1 test DocTypes (Field.AllowBlocked, doc.Invalid; CD-237, CD-264).
// x1.Allow and x1.Strict are the same document except that x1.Allow's links
// set AllowBlocked. employee_id links to the real Employee master, so a Left
// employee is Blocked with MST-120's text from employee's own lookup;
// vendor_id links to x1.Vendor, a test master whose lookup reports Disabled
// from the x1_vendor table. Their tables are created by x1Tables in the tests
// that use them.
const (
	x1Allow  = "x1.Allow"
	x1Strict = "x1.Strict"
	x1Vendor = "x1.Vendor"
)

const (
	// MST-120 (employee's lookup): an employee code.
	x1MsgLeftFmt = "Employee %s has left. Pick an active employee."
	// DOC-034's wording (CD-38, CD-68): label, code and name, lower-cased label.
	x1MsgVendorDisabled = "Vendor VND-CLOSED Closed Vendor is disabled. Pick an active vendor, or ask an admin to enable it."
	// Every engine KindInvalid error's message.
	x1MsgFixFields = "Fix the highlighted fields and save again."

	x1Left       = "EMP-008" // Left in the fixtures
	x1Active     = "EMP-007" // Active in the fixtures; made Left mid-test
	x1VendorCode = "VND-CLOSED"
)

var _ = func() bool {
	for _, s := range []struct {
		name, label, table string
		allow              bool
	}{
		{x1Allow, "Allow-blocked document", "x1_allow", true},
		{x1Strict, "Strict document", "x1_strict", false},
	} {
		doc.Register(doc.DocType{
			Name: s.name, Label: s.label, PluralLabel: s.label + "s",
			Table: s.table, LinesTable: s.table + "_line", Submittable: true, Approvable: true,
			Hooks: x1Hooks{},
			Fields: []doc.Field{
				{Name: "employee_id", Label: "Employee", Type: doc.Link, Link: employee.DocType, AllowBlocked: s.allow},
				{Name: "vendor_id", Label: "Vendor", Type: doc.Link, Link: x1Vendor, AllowBlocked: s.allow},
				{Name: "note", Label: "Note", Type: doc.Text},
				{Name: "line_note", Label: "Line note", Type: doc.Text, OnLine: true},
			},
		})
	}
	doc.Register(doc.DocType{
		Name: x1Vendor, Label: "Vendor", PluralLabel: "Vendors", Table: "x1_vendor",
		Fields: []doc.Field{
			{Name: "code", Label: "Code", Type: doc.Text, Required: true},
			{Name: "name", Label: "Name", Type: doc.Text, Required: true},
		},
		Master: x1VendorLookup{},
	})
	return true
}()

// x1VendorLookup is x1.Vendor's master lookup: it reads x1_vendor and reports
// Disabled only (never Blocked or Warning).
type x1VendorLookup struct{}

func (x1VendorLookup) Lookup(ctx context.Context, tx *db.Tx, id int64) (doctype.MasterInfo, bool, error) {
	var info doctype.MasterInfo
	err := tx.SQL().QueryRowContext(ctx, `SELECT code, name, disabled FROM x1_vendor WHERE id = ?`, id).
		Scan(&info.Code, &info.Name, &info.Disabled)
	if errors.Is(err, sql.ErrNoRows) {
		return doctype.MasterInfo{}, false, nil
	}
	if err != nil {
		return doctype.MasterInfo{}, false, err
	}
	return info, true, nil
}

// x1Hooks are p2lHooks whose BeforeSubmit returns the error the context
// carries (x1WithSubmitErr), so setup submits are untouched.
type x1Hooks struct{ p2lHooks }

type x1Key struct{}

func x1WithSubmitErr(ctx context.Context, err error) context.Context {
	return context.WithValue(ctx, x1Key{}, err)
}

func (x1Hooks) BeforeSubmit(ctx context.Context, _ *db.Tx, _ int64) error {
	err, _ := ctx.Value(x1Key{}).(error)
	return err
}

// x1Tables creates the x1 tables (the document table convention) and the
// x1_vendor master table with VND-CLOSED (enabled) on h's database.
func x1Tables(t *testing.T, h *fixturetest.H) {
	t.Helper()
	for _, table := range []string{"x1_allow", "x1_strict"} {
		h.Exec(t, `CREATE TABLE `+table+` (
			id INTEGER PRIMARY KEY, number TEXT UNIQUE, state TEXT NOT NULL DEFAULT 'draft',
			state_reason TEXT NOT NULL DEFAULT '', amended_from_id INTEGER, amendment_root_id INTEGER,
			amendment_no INTEGER NOT NULL DEFAULT 0, version INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL, created_by INTEGER NOT NULL, updated_at TEXT NOT NULL, updated_by INTEGER NOT NULL,
			employee_id INTEGER, vendor_id INTEGER, note TEXT NOT NULL DEFAULT '')`)
		h.Exec(t, `CREATE TABLE `+table+`_line (
			id INTEGER PRIMARY KEY, parent_id INTEGER NOT NULL REFERENCES `+table+` (id) ON DELETE CASCADE,
			line_no INTEGER NOT NULL, line_note TEXT NOT NULL DEFAULT '')`)
	}
	h.Exec(t, `CREATE TABLE x1_vendor (
		id INTEGER PRIMARY KEY, code TEXT NOT NULL, name TEXT NOT NULL, disabled INTEGER NOT NULL DEFAULT 0)`)
	h.Exec(t, `INSERT INTO x1_vendor (id, code, name) VALUES (1, ?, 'Closed Vendor')`, x1VendorCode)
	// Every x1 document needs approval, so submit leaves it pending for approve.
	h.Approvals.Needed = func(string, types.Money) bool { return true }
	// Approve assigns a number: add series prefixes for the x1 DocTypes,
	// keeping any already set (a nil map means the fixture defaults).
	prefixes := map[string]string{fixture.Order: "PO", fixture.Receipt: "GR", fixture.Claim: "EX"}
	if h.Naming.Prefix != nil {
		prefixes = map[string]string{}
		for k, v := range h.Naming.Prefix {
			prefixes[k] = v
		}
	}
	prefixes[x1Allow], prefixes[x1Strict] = "XA", "XS"
	h.Naming.Prefix = prefixes
}

// x1Employee returns the id of the fixture employee code and fails unless its
// status is status.
func x1Employee(t *testing.T, h *fixturetest.H, code, status string) string {
	t.Helper()
	var id int64
	var got string
	if err := h.DB.SQL().QueryRowContext(t.Context(), `SELECT id, status FROM employee WHERE code = ?`, code).Scan(&id, &got); err != nil {
		t.Fatalf("employee %s: %v", code, err)
	}
	if got != status {
		t.Fatalf("employee %s status = %q, want %q (fixture)", code, got, status)
	}
	return lkID(id)
}

// x1SetLeft marks the fixture employee code Left (disabled mirrors status).
func x1SetLeft(t *testing.T, h *fixturetest.H, code string) {
	t.Helper()
	h.Exec(t, `UPDATE employee SET status = 'Left', disabled = 1 WHERE code = ?`, code)
}

// x1SetVendorDisabled disables VND-CLOSED.
func x1SetVendorDisabled(t *testing.T, h *fixturetest.H) {
	t.Helper()
	h.Exec(t, `UPDATE x1_vendor SET disabled = 1 WHERE code = ?`, x1VendorCode)
}

// x1In is an x1 document's input with header values vals and one line.
func x1In(version int, vals map[string]string) doc.ActionInput {
	return doc.ActionInput{Version: version, Values: vals, Lines: []map[string]string{{"line_note": "Bay 1"}}}
}

// x1Refused fails unless err refuses field with msg and dt's document id is
// still in state s at version v; with id 0, unless nothing was created.
func x1Refused(t *testing.T, h *fixturetest.H, what string, err error, field, msg string, dt string, id int64, s doc.State, v int, count int) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: no error, want %q on %s", what, msg, field)
		return
	}
	lkInvalid(t, err, doctype.FieldError{Field: field, Line: 0, Message: msg})
	if id != 0 {
		trState(t, h, dt, id, s, v)
	}
	if got := h.AuditCount(t); got != count {
		t.Errorf("%s: AuditCount = %d, want %d (nothing written)", what, got, count)
	}
}

// x1Rows counts the rows of an x1 document table.
func x1Rows(t *testing.T, h *fixturetest.H, table string) int {
	t.Helper()
	var n int
	if err := h.DB.SQL().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM `+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// A Link with AllowBlocked accepts a master whose lookup is Blocked (a Left
// employee, MST-120) on save, submit and approve; the same field without it
// is refused with MST-120's text on each of them.
func TestDoc_AllowBlocked_SkipsBlocked(t *testing.T) {
	h := fixturetest.New(t)
	x1Tables(t, h)
	ctx := t.Context()
	left := x1Employee(t, h, x1Left, "Left")
	leftMsg := fmt.Sprintf(x1MsgLeftFmt, x1Left)

	// AllowBlocked: create, save, submit and approve with the Left employee.
	res, err := h.Do(t, ctx, h.Priya, x1Allow, 0, doc.ActionSave, x1In(0, map[string]string{"employee_id": left, "note": "Travel"}))
	if err != nil {
		t.Fatalf("create %s with Left %s = %v, want success (AllowBlocked)", x1Allow, x1Left, err)
	}
	d := h.Load(t, x1Allow, res.ID)
	if got := d.Header["employee_id"]; fmt.Sprint(got) != left {
		t.Errorf("employee_id = %#v, want %s", got, left)
	}
	res, err = h.Do(t, ctx, h.Priya, x1Allow, d.ID, doc.ActionSave, x1In(d.Version, map[string]string{"employee_id": left, "note": "Travel, revised"}))
	if err != nil {
		t.Fatalf("save %s with Left %s = %v, want success (AllowBlocked)", x1Allow, x1Left, err)
	}
	res, err = h.Do(t, ctx, h.Priya, x1Allow, d.ID, doc.ActionSubmit, trIn(res.Version))
	if err != nil {
		t.Fatalf("submit %s with Left %s = %v, want success (AllowBlocked)", x1Allow, x1Left, err)
	}
	if res.State != doc.StatePending {
		t.Fatalf("submit Result.State = %q, want pending", res.State)
	}
	res, err = h.Do(t, ctx, h.Ravi, x1Allow, d.ID, doc.ActionApprove, trIn(res.Version))
	if err != nil {
		t.Fatalf("approve %s with Left %s = %v, want success (AllowBlocked)", x1Allow, x1Left, err)
	}
	if res.State != doc.StateApproved {
		t.Errorf("approve Result.State = %q, want approved", res.State)
	}
	trState(t, h, x1Allow, d.ID, doc.StateApproved, res.Version)

	// Without AllowBlocked: create refused, nothing written.
	count := h.AuditCount(t)
	_, err = h.Do(t, ctx, h.Priya, x1Strict, 0, doc.ActionSave, x1In(0, map[string]string{"employee_id": left}))
	x1Refused(t, h, "create "+x1Strict, err, "employee_id", leftMsg, x1Strict, 0, "", 0, count)
	if n := x1Rows(t, h, "x1_strict"); n != 0 {
		t.Errorf("x1_strict rows = %d, want 0 (create refused)", n)
	}

	// Without AllowBlocked: save, submit and approve refused once the
	// employee has left.
	active := x1Employee(t, h, x1Active, "Active")
	activeMsg := fmt.Sprintf(x1MsgLeftFmt, x1Active)
	draft := h.MustDo(t, ctx, h.Priya, x1Strict, 0, doc.ActionSave, x1In(0, map[string]string{"employee_id": active}))
	pending := h.MustDo(t, ctx, h.Priya, x1Strict, 0, doc.ActionSave, x1In(0, map[string]string{"employee_id": active}))
	pending = h.MustDo(t, ctx, h.Priya, x1Strict, pending.ID, doc.ActionSubmit, trIn(pending.Version))
	x1SetLeft(t, h, x1Active)
	count = h.AuditCount(t)

	_, err = h.Do(t, ctx, h.Priya, x1Strict, draft.ID, doc.ActionSave, x1In(draft.Version, map[string]string{"employee_id": active, "note": "Edited"}))
	x1Refused(t, h, "save "+x1Strict, err, "employee_id", activeMsg, x1Strict, draft.ID, doc.StateDraft, draft.Version, count)
	_, err = h.Do(t, ctx, h.Priya, x1Strict, draft.ID, doc.ActionSubmit, trIn(draft.Version))
	x1Refused(t, h, "submit "+x1Strict, err, "employee_id", activeMsg, x1Strict, draft.ID, doc.StateDraft, draft.Version, count)
	_, err = h.Do(t, ctx, h.Ravi, x1Strict, pending.ID, doc.ActionApprove, trIn(pending.Version))
	x1Refused(t, h, "approve "+x1Strict, err, "employee_id", activeMsg, x1Strict, pending.ID, doc.StatePending, pending.Version, count)
}

// AllowBlocked skips Blocked only: a disabled master is refused with
// DOC-034's text on save, submit and approve, with and without it.
func TestDoc_AllowBlocked_StillRefusesDisabled(t *testing.T) {
	h := fixturetest.New(t)
	x1Tables(t, h)
	ctx := t.Context()
	const vendor = "1"
	for _, dt := range []string{x1Allow, x1Strict} {
		t.Run(dt, func(t *testing.T) {
			// Set up while VND-CLOSED is enabled: a draft and a pending document.
			h.Exec(t, `UPDATE x1_vendor SET disabled = 0`)
			draft := h.MustDo(t, ctx, h.Priya, dt, 0, doc.ActionSave, x1In(0, map[string]string{"vendor_id": vendor}))
			pending := h.MustDo(t, ctx, h.Priya, dt, 0, doc.ActionSave, x1In(0, map[string]string{"vendor_id": vendor}))
			pending = h.MustDo(t, ctx, h.Priya, dt, pending.ID, doc.ActionSubmit, trIn(pending.Version))
			x1SetVendorDisabled(t, h)
			count := h.AuditCount(t)
			table := map[string]string{x1Allow: "x1_allow", x1Strict: "x1_strict"}[dt]
			rows := x1Rows(t, h, table)

			_, err := h.Do(t, ctx, h.Priya, dt, 0, doc.ActionSave, x1In(0, map[string]string{"vendor_id": vendor}))
			x1Refused(t, h, "create", err, "vendor_id", x1MsgVendorDisabled, dt, 0, "", 0, count)
			if n := x1Rows(t, h, table); n != rows {
				t.Errorf("%s rows = %d, want %d (create refused)", table, n, rows)
			}
			_, err = h.Do(t, ctx, h.Priya, dt, draft.ID, doc.ActionSave, x1In(draft.Version, map[string]string{"vendor_id": vendor, "note": "Edited"}))
			x1Refused(t, h, "save", err, "vendor_id", x1MsgVendorDisabled, dt, draft.ID, doc.StateDraft, draft.Version, count)
			_, err = h.Do(t, ctx, h.Priya, dt, draft.ID, doc.ActionSubmit, trIn(draft.Version))
			x1Refused(t, h, "submit", err, "vendor_id", x1MsgVendorDisabled, dt, draft.ID, doc.StateDraft, draft.Version, count)
			_, err = h.Do(t, ctx, h.Ravi, dt, pending.ID, doc.ActionApprove, trIn(pending.Version))
			x1Refused(t, h, "approve", err, "vendor_id", x1MsgVendorDisabled, dt, pending.ID, doc.StatePending, pending.Version, count)
		})
	}
}

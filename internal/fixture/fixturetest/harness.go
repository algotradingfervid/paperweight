package fixturetest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// testDate is the 00-fixtures business date of every document the helpers make.
const testDate = "2026-10-24"

// H is a document engine on a fresh fixture database with DOC's doubles.
type H struct {
	DB        *db.DB
	Engine    doc.Engine
	Clock     *StepClock
	Auth      *Auth
	Naming    *Naming
	Approvals *Approvals
	Files     *Files
	M         fixture.Masters
	F         testkit.Fixtures

	Admin, Meena, Ravi, Priya, Suresh, Deepak auth.Principal
}

// OL is an order line: item code, quantity and rate as form values.
type OL struct{ Item, Qty, Rate string }

// RL is a receipt line on an order line.
type RL struct {
	OrderLineID int64
	Qty         string
}

// CL is a claim line: expense type and amount.
type CL struct{ Type, Amount string }

// New returns a harness: testkit.NewDB, testkit.Seed, the fixture migrations
// and SeedMasters, with the clock at 2026-10-24T05:00:00Z.
func New(t testing.TB) *H {
	t.Helper()
	d := testkit.NewDB(t)
	fx := testkit.Seed(t, d)
	if err := db.Migrate(context.Background(), d, fixture.MigrationsFS, "goose_fixture"); err != nil {
		t.Fatalf("fixturetest.New: fixture migrations: %v", err)
	}
	h := &H{
		DB: d, Clock: NewStepClock("2026-10-24T05:00:00Z"), Auth: NewAuth(), Naming: &Naming{},
		Approvals: &Approvals{TB: t}, Files: &Files{}, M: fixture.SeedMasters(t, d), F: fx,
		Admin: testkit.Principal(t, "admin@vardhan.test"), Meena: testkit.Principal(t, "meena@vardhan.test"),
		Ravi: testkit.Principal(t, "ravi@vardhan.test"), Priya: testkit.Principal(t, "priya@vardhan.test"),
		Suresh: testkit.Principal(t, "suresh@vardhan.test"), Deepak: testkit.Principal(t, "deepak@vardhan.test"),
	}
	for _, p := range []auth.Principal{h.Admin, h.Meena, h.Ravi, h.Priya, h.Suresh, h.Deepak} {
		h.Auth.Remember(p)
	}
	h.Engine = h.EngineOn(d)
	return h
}

// EngineOn returns an engine with h's doubles on another DB handle.
func (h *H) EngineOn(d *db.DB) doc.Engine {
	return doc.NewEngine(doc.Deps{
		DB: d, Clock: h.Clock, Auth: h.Auth, Naming: h.Naming, Numbers: Numbers(),
		Approval: h.Approvals, Files: h.Files,
	})
}

// Do runs h.Engine.Do.
func (h *H) Do(t testing.TB, ctx context.Context, p auth.Principal, dt string, id int64, a doc.Action, in doc.ActionInput) (doc.Result, error) {
	t.Helper()
	return h.Engine.Do(ctx, p, dt, id, a, in)
}

// MustDo runs h.Engine.Do and fails the test on an error.
func (h *H) MustDo(t testing.TB, ctx context.Context, p auth.Principal, dt string, id int64, a doc.Action, in doc.ActionInput) doc.Result {
	t.Helper()
	res, err := h.Engine.Do(ctx, p, dt, id, a, in)
	if err != nil {
		t.Fatalf("Do(%s %d %s): %v", dt, id, a, err)
	}
	return res
}

// NewOrder creates an order by p: supplier SHREE, project PRJ-METRO, order
// date 2026-10-24, amount 0.
func (h *H) NewOrder(t testing.TB, p auth.Principal, lines ...OL) doc.Result {
	t.Helper()
	return h.NewOrderWith(t, p, nil, lines...)
}

// NewOrderWith is NewOrder with header overriding the default header values.
func (h *H) NewOrderWith(t testing.TB, p auth.Principal, header map[string]string, lines ...OL) doc.Result {
	t.Helper()
	values := map[string]string{
		"supplier_id": id(h.M.Party["SHREE"]), "project_id": id(h.M.Project["PRJ-METRO"]),
		"order_date": testDate, "amount": "0",
	}
	for k, v := range header {
		values[k] = v
	}
	in := doc.ActionInput{Values: values}
	for _, l := range lines {
		m := map[string]string{"item_id": h.itemID(t, l.Item), "qty": l.Qty}
		if l.Rate != "" {
			m["rate"] = l.Rate
		}
		in.Lines = append(in.Lines, m)
	}
	return h.MustDo(t, context.Background(), p, fixture.Order, 0, doc.ActionSave, in)
}

func (h *H) itemID(t testing.TB, code string) string {
	t.Helper()
	if code == "" {
		return ""
	}
	v, ok := h.M.Item[code]
	if !ok {
		t.Fatalf("fixturetest: no item %s", code)
	}
	return id(v)
}

// RefPO creates the 00-fixtures reference PO by Priya: MS-ANGLE-50 1200.000 @
// 62.5000, WIRE-18G 50.000 @ 96.0000, amount 94164.00.
func (h *H) RefPO(t testing.TB) doc.Result {
	t.Helper()
	ref := h.F.ReferencePO
	var lines []OL
	for _, l := range ref.Lines {
		lines = append(lines, OL{
			Item: l.Item,
			Qty:  doc.FormatPlain(doc.Field{Type: doc.Qty}, l.Qty),
			Rate: doc.FormatPlain(doc.Field{Type: doc.Rate}, l.Rate),
		})
	}
	return h.NewOrderWith(t, h.Priya, map[string]string{
		"supplier_id": id(h.M.Party[ref.Party]),
		"amount":      doc.FormatPlain(doc.Field{Type: doc.Money}, ref.GrandTotal),
	}, lines...)
}

// ApprovedOrder creates an order by Priya (amount 0) and submits it, which
// auto-approves it.
func (h *H) ApprovedOrder(t testing.TB, lines ...OL) doc.Doc {
	t.Helper()
	res := h.NewOrder(t, h.Priya, lines...)
	h.MustDo(t, context.Background(), h.Priya, fixture.Order, res.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version})
	return h.Load(t, fixture.Order, res.ID)
}

// PendingOrder creates the reference PO and submits it (it needs approval).
func (h *H) PendingOrder(t testing.TB) doc.Doc {
	t.Helper()
	res := h.RefPO(t)
	h.MustDo(t, context.Background(), h.Priya, fixture.Order, res.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version})
	return h.Load(t, fixture.Order, res.ID)
}

// NewReceipt creates a receipt by p dated 2026-10-24.
func (h *H) NewReceipt(t testing.TB, p auth.Principal, lines ...RL) doc.Result {
	t.Helper()
	in := doc.ActionInput{Values: map[string]string{"receipt_date": testDate}}
	for _, l := range lines {
		in.Lines = append(in.Lines, map[string]string{"order_line_id": id(l.OrderLineID), "qty": l.Qty})
	}
	return h.MustDo(t, context.Background(), p, fixture.Receipt, 0, doc.ActionSave, in)
}

// NewClaim creates a claim by p for the employee with employeeCode, dated 2026-10-24.
func (h *H) NewClaim(t testing.TB, p auth.Principal, employeeCode string, lines ...CL) doc.Result {
	t.Helper()
	var emp int64
	for _, e := range h.F.Employees {
		if e.Code == employeeCode {
			emp = e.ID
		}
	}
	if emp == 0 {
		t.Fatalf("fixturetest: no employee %s", employeeCode)
	}
	in := doc.ActionInput{Values: map[string]string{"employee_id": id(emp), "claim_date": testDate}}
	for _, l := range lines {
		in.Lines = append(in.Lines, map[string]string{"expense_type": l.Type, "amount": l.Amount})
	}
	return h.MustDo(t, context.Background(), p, fixture.Claim, 0, doc.ActionSave, in)
}

// read runs fn in a read transaction and fails the test on an error.
func (h *H) read(t testing.TB, what string, fn func(tx *db.Tx) error) {
	t.Helper()
	if err := h.DB.Read(context.Background(), fn); err != nil {
		t.Fatalf("fixturetest: %s: %v", what, err)
	}
}

// Load reads the document through doc.LoadDoc.
func (h *H) Load(t testing.TB, dt string, docID int64) doc.Doc {
	t.Helper()
	var d doc.Doc
	h.read(t, fmt.Sprintf("LoadDoc(%s, %d)", dt, docID), func(tx *db.Tx) (err error) {
		d, err = doc.LoadDoc(context.Background(), tx, dt, docID)
		return err
	})
	return d
}

// Record reads the document through Engine.Load.
func (h *H) Record(t testing.TB, dt string, docID int64) doc.Record {
	t.Helper()
	var rec doc.Record
	h.read(t, fmt.Sprintf("Load(%s, %d)", dt, docID), func(tx *db.Tx) (err error) {
		rec, err = h.Engine.Load(context.Background(), tx, dt, docID)
		return err
	})
	return rec
}

// Info reads the document through Engine.Info.
func (h *H) Info(t testing.TB, dt string, docID int64) doctype.Info {
	t.Helper()
	var info doctype.Info
	h.read(t, fmt.Sprintf("Info(%s, %d)", dt, docID), func(tx *db.Tx) (err error) {
		info, err = h.Engine.Info(context.Background(), tx, dt, docID)
		return err
	})
	return info
}

// Display is the document's display name ("PO/26-27/00001" or "Draft 42").
func (h *H) Display(t testing.TB, dt string, docID int64) string {
	t.Helper()
	return h.Info(t, dt, docID).Display
}

// Audit returns the document's audit entries, oldest first.
func (h *H) Audit(t testing.TB, dt string, docID int64) []audit.Entry {
	t.Helper()
	var entries []audit.Entry
	h.read(t, fmt.Sprintf("audit.ForDoc(%s, %d)", dt, docID), func(tx *db.Tx) (err error) {
		entries, err = audit.ForDoc(context.Background(), tx, dt, docID)
		return err
	})
	return entries
}

// AuditCount is the number of audit_log rows.
func (h *H) AuditCount(t testing.TB) int {
	t.Helper()
	var n int
	h.read(t, "audit.Count", func(tx *db.Tx) (err error) {
		n, err = audit.Count(context.Background(), tx)
		return err
	})
	return n
}

// ForceState sets state and number ("" is NULL) with a raw UPDATE, for test
// setup only.
func (h *H) ForceState(t testing.TB, dt string, docID int64, s doc.State, number string) {
	t.Helper()
	typ, ok := doc.Lookup(dt)
	if !ok {
		t.Fatalf("fixturetest.ForceState: unknown DocType %s", dt)
	}
	h.Exec(t, `UPDATE "`+typ.Table+`" SET state = ?, number = NULLIF(?, '') WHERE id = ?`, string(s), number, docID)
}

// Exec runs a raw statement in a write transaction.
func (h *H) Exec(t testing.TB, query string, args ...any) {
	t.Helper()
	err := h.DB.Write(context.Background(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(context.Background(), query, args...)
		return err
	})
	if err != nil {
		t.Fatalf("fixturetest.Exec(%q): %v", query, err)
	}
}

// In returns d's current values and version as an ActionInput, ready to edit.
func (h *H) In(d doc.Doc) doc.ActionInput {
	dt, _ := doc.Lookup(d.DocType)
	in := doc.ActionInput{Version: d.Version, Values: map[string]string{}}
	for _, f := range dt.HeaderFields() {
		in.Values[f.Name] = doc.FormatPlain(f, d.Header[f.Name])
	}
	for _, l := range d.Lines {
		m := map[string]string{"id": id(l.ID)}
		for _, f := range dt.LineFields() {
			m[f.Name] = doc.FormatPlain(f, l.Values[f.Name])
		}
		in.Lines = append(in.Lines, m)
	}
	return in
}

// Eq fails the test unless got and want are deeply equal (CD-110).
func Eq[T any](t testing.TB, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %#v\nwant %#v", got, want)
	}
}

// ErrIs fails the test unless err is a *doc.Error of kind, with Message msg
// when msg != "".
func ErrIs(t testing.TB, err error, kind doc.ErrorKind, msg string) {
	t.Helper()
	var de *doc.Error
	if !errors.As(err, &de) {
		t.Fatalf("err = %v (%T), want a *doc.Error of kind %q", err, err, kind)
	}
	if de.Kind != kind {
		t.Errorf("Kind = %q, want %q (err %v)", de.Kind, kind, err)
	}
	if msg != "" && de.Message != msg {
		t.Errorf("Message = %q, want %q", de.Message, msg)
	}
}

func id(v int64) string { return strconv.FormatInt(v, 10) }

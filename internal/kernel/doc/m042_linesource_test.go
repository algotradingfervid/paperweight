package doc_test

import (
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// M0-42 (CD-314): a posted LinkSpec source id that names no row of its
// target is refused with checkSource's text before anything is written, so
// the module tables' foreign keys never fail. The fixture tables have no
// foreign key on the source columns, so these tests rebuild them with one, as
// goods_receipt_line.po_line_id has.

const (
	m042Missing     = "99999"
	m042LineMissing = "Purchase Order line not found. Pick one from the list."
	m042QtyZero     = "Line 2: Quantity must be more than 0."
)

// m042ReceiptFK rebuilds fixture_receipt_line with a foreign key on
// order_line_id (the table is empty when called).
func m042ReceiptFK(t *testing.T, h *fixturetest.H) {
	t.Helper()
	h.Exec(t, `DROP TABLE fixture_receipt_line`)
	h.Exec(t, `CREATE TABLE fixture_receipt_line (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		parent_id INTEGER NOT NULL REFERENCES fixture_receipt (id) ON DELETE CASCADE,
		line_no INTEGER NOT NULL,
		order_line_id INTEGER REFERENCES fixture_order_line (id) ON DELETE RESTRICT,
		qty INTEGER)`)
}

// m042Count returns the number of rows in table.
func m042Count(t *testing.T, h *fixturetest.H, table string) int {
	t.Helper()
	var n int
	err := h.DB.Read(t.Context(), func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table).Scan(&n)
	})
	if err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// m042Setup returns h with the foreign key in place and an approved order
// with one line.
func m042Setup(t *testing.T) (*fixturetest.H, doc.Doc) {
	t.Helper()
	h := fixturetest.New(t)
	m042ReceiptFK(t, h)
	po := h.ApprovedOrder(t, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10.000", Rate: "62.5000"})
	return h, po
}

// Create: line 1's source is missing, line 2 has a field error; both are
// reported in position order and no row is written.
func TestM042_Create_MissingLineSource(t *testing.T) {
	h, po := m042Setup(t)
	audits := h.AuditCount(t)
	_, err := h.Do(t, t.Context(), h.Priya, fixture.Receipt, 0, doc.ActionSave, doc.ActionInput{
		Values: map[string]string{"receipt_date": "2026-10-24"},
		Lines: []map[string]string{
			{"order_line_id": m042Missing, "qty": "1.000"},
			{"order_line_id": lkID(po.Lines[0].ID), "qty": "0"},
		},
	})
	lkInvalid(t, err,
		doctype.FieldError{Field: "order_line_id", Line: 1, Message: "Line 1: " + m042LineMissing},
		doctype.FieldError{Field: "qty", Line: 2, Message: m042QtyZero})
	for _, table := range []string{"fixture_receipt", "fixture_receipt_line"} {
		if got := m042Count(t, h, table); got != 0 {
			t.Errorf("rows in %s = %d, want 0 (nothing written)", table, got)
		}
	}
	if got := h.AuditCount(t); got != audits {
		t.Errorf("AuditCount = %d, want %d (nothing written)", got, audits)
	}
}

// m042Saved creates a receipt for po's line and returns it.
func m042Saved(t *testing.T, h *fixturetest.H, po doc.Doc) doc.Doc {
	t.Helper()
	res := h.NewReceipt(t, h.Priya, fixturetest.RL{OrderLineID: po.Lines[0].ID, Qty: "1.000"})
	return h.Load(t, fixture.Receipt, res.ID)
}

// m042Unchanged fails unless the receipt and its lines are as before.
func m042Unchanged(t *testing.T, h *fixturetest.H, before doc.Doc, audits int) {
	t.Helper()
	fixturetest.Eq(t, h.Load(t, fixture.Receipt, before.ID), before)
	if got := m042Count(t, h, "fixture_receipt_line"); got != len(before.Lines) {
		t.Errorf("rows in fixture_receipt_line = %d, want %d", got, len(before.Lines))
	}
	if got := h.AuditCount(t); got != audits {
		t.Errorf("AuditCount = %d, want %d (nothing written)", got, audits)
	}
}

// Save of a draft that adds a line with a missing source (the insert path).
func TestM042_Save_NewLineMissingSource(t *testing.T) {
	h, po := m042Setup(t)
	d := m042Saved(t, h, po)
	audits := h.AuditCount(t)
	in := h.In(d)
	in.Lines = append(in.Lines, map[string]string{"order_line_id": m042Missing, "qty": "2.000"})
	_, err := h.Do(t, t.Context(), h.Priya, fixture.Receipt, d.ID, doc.ActionSave, in)
	lkInvalid(t, err, doctype.FieldError{Field: "order_line_id", Line: 2, Message: "Line 2: " + m042LineMissing})
	m042Unchanged(t, h, d, audits)
}

// Save that changes a kept line's source to a missing one (the update path).
func TestM042_Save_ChangedLineMissingSource(t *testing.T) {
	h, po := m042Setup(t)
	d := m042Saved(t, h, po)
	audits := h.AuditCount(t)
	in := h.In(d)
	in.Lines[0]["order_line_id"] = m042Missing
	_, err := h.Do(t, t.Context(), h.Priya, fixture.Receipt, d.ID, doc.ActionSave, in)
	lkInvalid(t, err, doctype.FieldError{Field: "order_line_id", Line: 1, Message: "Line 1: " + m042LineMissing})
	m042Unchanged(t, h, d, audits)
}

// A step save (Only) whose lines step adds a missing source; a line error out
// of scope still can't be written, so it refuses too.
func TestM042_StepSave_MissingLineSource(t *testing.T) {
	for name, only := range map[string][]string{"lines step": {"lines"}, "header step": {"remarks"}} {
		t.Run(name, func(t *testing.T) {
			h, po := m042Setup(t)
			d := m042Saved(t, h, po)
			audits := h.AuditCount(t)
			in := h.In(d)
			in.Only = only
			in.Lines = append(in.Lines, map[string]string{"order_line_id": m042Missing, "qty": "2.000"})
			_, err := h.Do(t, t.Context(), h.Priya, fixture.Receipt, d.ID, doc.ActionSave, in)
			lkInvalid(t, err, doctype.FieldError{Field: "order_line_id", Line: 2, Message: "Line 2: " + m042LineMissing})
			m042Unchanged(t, h, d, audits)
		})
	}
}

// m042HeaderFKTables creates the p2l tables with a foreign key on
// p2l_child.head_id.
func m042HeaderFKTables(t *testing.T, h *fixturetest.H) {
	t.Helper()
	p2lHeaderTables(t, h)
	h.Exec(t, `DROP TABLE p2l_child_line`)
	h.Exec(t, `DROP TABLE p2l_child`)
	h.Exec(t, `CREATE TABLE p2l_child (
		id INTEGER PRIMARY KEY, number TEXT UNIQUE, state TEXT NOT NULL DEFAULT 'draft',
		state_reason TEXT NOT NULL DEFAULT '', amended_from_id INTEGER, amendment_root_id INTEGER,
		amendment_no INTEGER NOT NULL DEFAULT 0, version INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL, created_by INTEGER NOT NULL, updated_at TEXT NOT NULL, updated_by INTEGER NOT NULL,
		head_id INTEGER REFERENCES p2l_head (id))`)
	h.Exec(t, `CREATE TABLE p2l_child_line (
		id INTEGER PRIMARY KEY, parent_id INTEGER NOT NULL REFERENCES p2l_child (id) ON DELETE CASCADE,
		line_no INTEGER NOT NULL, child_note TEXT NOT NULL DEFAULT '')`)
}

// A missing header source, where AUTH's CheckLinkTx lets it through (the
// fixture double), is refused before the insert and on a save's update.
func TestM042_HeaderMissingSource(t *testing.T) {
	h := fixturetest.New(t)
	m042HeaderFKTables(t, h)
	head := h.MustDo(t, t.Context(), h.Priya, p2lHead, 0, doc.ActionSave,
		doc.ActionInput{Values: map[string]string{"title": "Head"}})
	h.ForceState(t, p2lHead, head.ID, doc.StateApproved, "HD/1")
	want := doctype.FieldError{Field: "head_id", Line: 0, Message: p2lMsgHeadMissing}

	_, err := h.Do(t, t.Context(), h.Priya, p2lChild, 0, doc.ActionSave,
		doc.ActionInput{Values: map[string]string{"head_id": m042Missing}})
	lkInvalid(t, err, want)
	if got := m042Count(t, h, "p2l_child"); got != 0 {
		t.Errorf("rows in p2l_child = %d, want 0", got)
	}

	child := h.MustDo(t, t.Context(), h.Priya, p2lChild, 0, doc.ActionSave,
		doc.ActionInput{Values: map[string]string{"head_id": lkID(head.ID)}})
	d := h.Load(t, p2lChild, child.ID)
	audits := h.AuditCount(t)
	_, err = h.Do(t, t.Context(), h.Priya, p2lChild, d.ID, doc.ActionSave,
		doc.ActionInput{Version: d.Version, Values: map[string]string{"head_id": m042Missing}})
	lkInvalid(t, err, want)
	fixturetest.Eq(t, h.Load(t, p2lChild, d.ID), d)
	if got := h.AuditCount(t); got != audits {
		t.Errorf("AuditCount = %d, want %d (nothing written)", got, audits)
	}
}

// On the real AUTH, CheckLinkTx answers a missing header source with
// auth.ErrNotFound before the existence check runs (recorded, not changed by
// M0-42): the header case never reaches an insert there.
func TestM042_HeaderMissingSource_RealAuthNotFound(t *testing.T) {
	e := nsNew(t)
	m042HeaderFKTables(t, e.H)
	_, err := e.do(t, e.Admin, p2lChild, 0, doc.ActionSave,
		doc.ActionInput{Values: map[string]string{"head_id": m042Missing}})
	if !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("create with a missing head on the real AUTH = %v (%T), want auth.ErrNotFound", err, err)
	}
	if got := m042Count(t, e.H, "p2l_child"); got != 0 {
		t.Errorf("rows in p2l_child = %d, want 0", got)
	}
}

// An empty source value is left to the required check (on submit), so a draft
// line without one saves; the existence check doesn't refuse it.
func TestM042_EmptyLineSourceSaves(t *testing.T) {
	h, _ := m042Setup(t)
	res := h.MustDo(t, t.Context(), h.Priya, fixture.Receipt, 0, doc.ActionSave, doc.ActionInput{
		Values: map[string]string{"receipt_date": "2026-10-24"},
		Lines:  []map[string]string{{"order_line_id": "", "qty": "1.000"}},
	})
	if got := h.Load(t, fixture.Receipt, res.ID).Lines; len(got) != 1 || got[0].Values["order_line_id"] != nil {
		t.Errorf("lines = %+v, want one line with no order_line_id", got)
	}
}

// A source line that disappears after the save (the fixture table has no
// foreign key) is still reported on submit with checkSource's text.
func TestM042_SubmitSourceLineGone(t *testing.T) {
	h := fixturetest.New(t)
	po := h.ApprovedOrder(t, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10.000", Rate: "62.5000"})
	d := m042Saved(t, h, po)
	h.Exec(t, `DELETE FROM fixture_order_line WHERE id = ?`, po.Lines[0].ID)
	_, err := h.Do(t, t.Context(), h.Priya, fixture.Receipt, d.ID, doc.ActionSubmit, doc.ActionInput{Version: d.Version})
	lkInvalid(t, err, doctype.FieldError{Field: "order_line_id", Line: 1, Message: "Line 1: " + m042LineMissing})
}

package doc_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// tsLayout is the stored timestamp layout (plan Global Constraints).
const tsLayout = "2006-01-02T15:04:05.000Z"

// msgGone is the not-found text of the Do pipeline, step 1 (Task 6).
const msgGone = "This document no longer exists. It may have been deleted."

// diffOf returns "" when want and got are deeply equal, else both values.
func diffOf(want, got any) string {
	if reflect.DeepEqual(want, got) {
		return ""
	}
	return fmt.Sprintf("want %#v\ngot  %#v", want, got)
}

// loadDoc reads id through doc.LoadDoc in a read transaction.
func loadDoc(t *testing.T, h *fixturetest.H, dt string, id int64) doc.Doc {
	t.Helper()
	var d doc.Doc
	err := h.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		d, err = doc.LoadDoc(t.Context(), tx, dt, id)
		return err
	})
	if err != nil {
		t.Fatalf("doc.LoadDoc(%s, %d): %v", dt, id, err)
	}
	return d
}

// wantDocErr fails unless err is a *doc.Error of kind (and, when msg != "",
// with exactly that Message). It returns the *doc.Error.
func wantDocErr(t *testing.T, err error, kind doc.ErrorKind, msg string) *doc.Error {
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
	return de
}

// orderRows counts fixture_order rows.
func orderRows(t *testing.T, h *fixturetest.H) int {
	t.Helper()
	var n int
	if err := h.DB.SQL().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM fixture_order`).Scan(&n); err != nil {
		t.Fatalf("count fixture_order: %v", err)
	}
	return n
}

// DOC-001
func TestDoc_Create_DraftDefaults(t *testing.T) {
	h := fixturetest.New(t)
	res := h.NewOrder(t, h.Priya, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "1200.000"})

	if res.ID == 0 {
		t.Fatalf("Result.ID = 0, want the new order's id")
	}
	if res.State != doc.StateDraft || res.Version != 1 || res.Number != "" {
		t.Errorf("Result = {State %q, Version %d, Number %q}, want {draft, 1, \"\"}", res.State, res.Version, res.Number)
	}

	d := loadDoc(t, h, fixture.Order, res.ID)
	if d.State != doc.StateDraft {
		t.Errorf("State = %q, want %q", d.State, doc.StateDraft)
	}
	if d.Version != 1 {
		t.Errorf("Version = %d, want 1", d.Version)
	}
	if d.Number != "" {
		t.Errorf("Number = %q, want \"\" (no number before approval, R1)", d.Number)
	}
	if want := fmt.Sprintf("Draft %d", res.ID); h.Display(t, fixture.Order, res.ID) != want {
		t.Errorf("Display = %q, want %q", h.Display(t, fixture.Order, res.ID), want)
	}
	if d.CreatedBy != types.UserID(4) || d.UpdatedBy != types.UserID(4) {
		t.Errorf("CreatedBy, UpdatedBy = %d, %d, want 4, 4 (priya)", d.CreatedBy, d.UpdatedBy)
	}
	const at = "2026-10-24T05:00:00.000Z"
	if got := d.CreatedAt.Format(tsLayout); got != at {
		t.Errorf("CreatedAt = %q, want %q", got, at)
	}
	if got := d.UpdatedAt.Format(tsLayout); got != at {
		t.Errorf("UpdatedAt = %q, want %q", got, at)
	}
	if len(d.Lines) != 1 {
		t.Fatalf("len(Lines) = %d, want 1", len(d.Lines))
	}
	if d.Lines[0].LineNo != 1 {
		t.Errorf("Lines[0].LineNo = %d, want 1", d.Lines[0].LineNo)
	}
	if d.Lines[0].ID == 0 {
		t.Errorf("Lines[0].ID = 0, want a non-zero line id")
	}
	if got, want := d.Lines[0].Values["item_id"], any(h.M.Item["MS-ANGLE-50"]); got != want {
		t.Errorf("Lines[0] item_id = %v (%T), want %v", got, got, want)
	}
	if got, want := d.Lines[0].Values["qty"], any(types.Qty(1200000)); got != want {
		t.Errorf("Lines[0] qty = %v (%T), want %v", got, got, want)
	}
}

// DOC-039
func TestDoc_Lines_NumberedInOrder(t *testing.T) {
	h := fixturetest.New(t)
	codes := []string{"MS-ANGLE-50", "WIRE-18G", "BOLT-M12"}
	res := h.NewOrder(t, h.Priya,
		fixturetest.OL{Item: codes[0], Qty: "10.000"},
		fixturetest.OL{Item: codes[1], Qty: "20.000"},
		fixturetest.OL{Item: codes[2], Qty: "30.000"},
	)

	d := loadDoc(t, h, fixture.Order, res.ID)
	if len(d.Lines) != len(codes) {
		t.Fatalf("len(Lines) = %d, want %d", len(d.Lines), len(codes))
	}
	for i, code := range codes {
		l := d.Lines[i]
		if l.LineNo != i+1 {
			t.Errorf("Lines[%d].LineNo = %d, want %d", i, l.LineNo, i+1)
		}
		if got, want := l.Values["item_id"], any(h.M.Item[code]); got != want {
			t.Errorf("Lines[%d] item_id = %v, want %v (%s)", i, got, want, code)
		}
	}
}

// Review Focus 5
func TestDoc_Timestamps_StoredUTC(t *testing.T) {
	h := fixturetest.New(t)
	h.Clock.SetTime(time.Date(2026, 10, 24, 10, 30, 0, 0, clock.IST))
	res := h.NewOrder(t, h.Priya, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "1200.000"})

	const want = "2026-10-24T05:00:00.000Z"
	var created, updated string
	if err := h.DB.SQL().QueryRowContext(t.Context(),
		`SELECT created_at, updated_at FROM fixture_order WHERE id = ?`, res.ID).Scan(&created, &updated); err != nil {
		t.Fatalf("read fixture_order timestamps: %v", err)
	}
	if created != want || updated != want {
		t.Errorf("raw created_at, updated_at = %q, %q, want %q for both", created, updated, want)
	}

	rows, err := h.DB.SQL().QueryContext(t.Context(),
		`SELECT at FROM audit_log WHERE doc_type = ? AND doc_id = ?`, fixture.Order, res.ID)
	if err != nil {
		t.Fatalf("read audit_log: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var ats []string
	for rows.Next() {
		var at string
		if err := rows.Scan(&at); err != nil {
			t.Fatalf("scan audit at: %v", err)
		}
		ats = append(ats, at)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("audit rows: %v", err)
	}
	if diff := diffOf([]string{want}, ats); diff != "" {
		t.Errorf("raw audit_log.at (want, got):\n%s", diff)
	}
}

func TestDoc_UnknownDocType(t *testing.T) {
	h := fixturetest.New(t)

	_, err := h.Do(t, t.Context(), h.Priya, "fixture.Nope", 1, doc.ActionSubmit, doc.ActionInput{Version: 1})
	_ = wantDocErr(t, err, doc.KindNotFound, msgGone)
	if !errors.Is(err, doctype.ErrNotFound) {
		t.Errorf("Do(fixture.Nope): errors.Is(err, doctype.ErrNotFound) = false, err %v", err)
	}

	shree := h.M.Party["SHREE"]
	_, err = h.Do(t, t.Context(), h.Admin, fixture.Party, shree, doc.ActionSave,
		doc.ActionInput{Version: 1, Values: map[string]string{"code": "SHREE", "name": "Renamed"}})
	_ = wantDocErr(t, err, doc.KindNotFound, msgGone)
	if !errors.Is(err, doctype.ErrNotFound) {
		t.Errorf("Do(fixture.Party): errors.Is(err, doctype.ErrNotFound) = false, err %v", err)
	}
	var name string
	if err := h.DB.SQL().QueryRowContext(t.Context(), `SELECT name FROM fixture_party WHERE id = ?`, shree).Scan(&name); err != nil {
		t.Fatalf("read fixture_party: %v", err)
	}
	if name != "Shree Steel Traders" {
		t.Errorf("SHREE name = %q after a refused save, want unchanged", name)
	}
	if n := h.AuditCount(t); n != 0 {
		t.Errorf("AuditCount = %d, want 0", n)
	}
}

// Director carry-over (no DOC case ID): normalize's unreadable-value errors
// and checkFields' errors are merged by position (header in field order, then
// each line in field order), not appended as two lists.
func TestDoc_Create_FieldErrorsInPositionOrder(t *testing.T) {
	h := fixturetest.New(t)
	in := doc.ActionInput{
		Values: map[string]string{
			"supplier_id": fmt.Sprint(h.M.Party["SHREE"]),
			"credit_days": "3O", // unreadable (normalize); order_date is missing (checkFields)
		},
		Lines: []map[string]string{
			{"item_id": "", "qty": "12.5.3"}, // item required (checkFields), qty unreadable (normalize)
			{"item_id": "", "qty": "5.000"},  // item required (checkFields)
		},
	}
	_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, 0, doc.ActionSave, in)
	de := wantDocErr(t, err, doc.KindInvalid, "")
	want := []doc.FieldError{
		{Field: "order_date", Message: "Order date is required."},
		{Field: "credit_days", Message: "Credit days isn't a valid value. Check it and try again."},
		{Field: "item_id", Line: 1, Message: "Line 1: Item is required."},
		{Field: "qty", Line: 1, Message: "Line 1: Quantity isn't a valid value. Check it and try again."},
		{Field: "item_id", Line: 2, Message: "Line 2: Item is required."},
	}
	if diff := diffOf(want, de.Fields); diff != "" {
		t.Errorf("Fields (want, got):\n%s", diff)
	}
	if diff := diffOf(want, de.FieldErrors()); diff != "" {
		t.Errorf("FieldErrors() (want, got):\n%s", diff)
	}
	if n := orderRows(t, h); n != 0 {
		t.Errorf("fixture_order rows = %d, want 0", n)
	}
	if n := h.AuditCount(t); n != 0 {
		t.Errorf("AuditCount = %d, want 0", n)
	}
}

// Director carry-over (no DOC case ID): a MasterLookup that fails
// (normalized.err) is returned as is, before normalize's and checkFields'
// field errors, and nothing is written.
func TestDoc_Create_LookupErrorFirst(t *testing.T) {
	h := fixturetest.New(t)
	// The stand-in lookup reads fixture_party.warning; without it every lookup fails.
	h.Exec(t, `ALTER TABLE fixture_party RENAME COLUMN warning TO warning_wp1_doc_6`)

	in := doc.ActionInput{
		Values: map[string]string{
			"supplier_id": fmt.Sprint(h.M.Party["SHREE"]),
			"credit_days": "3O", // unreadable; order_date missing
		},
		Lines: []map[string]string{{"item_id": "", "qty": "1.000"}},
	}
	_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, 0, doc.ActionSave, in)
	if err == nil {
		t.Fatalf("Do(create) with a failing supplier lookup: err = nil, want the lookup error")
	}
	if !strings.Contains(err.Error(), "no such column") {
		t.Errorf("err = %q, want the lookup's SQL error (no such column)", err)
	}
	var de *doc.Error
	if errors.As(err, &de) && (de.Kind == doc.KindInvalid || len(de.Fields) > 0) {
		t.Errorf("err = *doc.Error{Kind %q, Fields %v}, want the lookup error, not the field errors", de.Kind, de.Fields)
	}
	if n := orderRows(t, h); n != 0 {
		t.Errorf("fixture_order rows = %d, want 0", n)
	}
	if n := h.AuditCount(t); n != 0 {
		t.Errorf("AuditCount = %d, want 0", n)
	}
}

// envBrokenRegistry makes TestDoc_NewEngine_ChecksRegistry's child process
// register a DocType that fails doctype.CheckRegistry.
const envBrokenRegistry = "WP1_DOC_6_BROKEN_REGISTRY"

// Director carry-over, CD-107 (no DOC case ID): NewEngine panics when
// doctype.CheckRegistry fails, and builds an engine when it passes.
func TestDoc_NewEngine_ChecksRegistry(t *testing.T) {
	if os.Getenv(envBrokenRegistry) == "1" {
		doc.Register(doc.DocType{
			Name: "wp1doc6.Broken", Label: "Broken", Table: "wp1doc6_broken",
			Fields: []doc.Field{{Name: "party_id", Label: "Party", Type: doc.Link, Link: "wp1doc6.Nope"}},
		})
		if doctype.CheckRegistry() == nil {
			t.Fatalf("setup: CheckRegistry() = nil with a link to an unknown DocType")
		}
		defer func() {
			r := recover()
			if r == nil {
				t.Errorf("NewEngine did not panic though CheckRegistry() fails")
				return
			}
			if s := fmt.Sprint(r); !strings.Contains(s, "wp1doc6.Nope") {
				t.Errorf("panic = %q, want it to carry CheckRegistry's error (naming wp1doc6.Nope)", s)
			}
		}()
		doc.NewEngine(doc.Deps{})
		return
	}

	// A valid registry: no panic, and a working engine.
	h := fixturetest.New(t)
	e := doc.NewEngine(doc.Deps{
		DB: h.DB, Clock: h.Clock, Auth: h.Auth, Naming: h.Naming, Numbers: fixturetest.Numbers(),
		Approval: h.Approvals, Files: h.Files,
	})
	err := h.DB.Read(t.Context(), func(tx *db.Tx) error {
		_, err := e.Info(t.Context(), tx, fixture.Order, 1)
		return err
	})
	if !errors.Is(err, doctype.ErrNotFound) {
		t.Errorf("Info(fixture.Order, 1) on an empty table = %v, want doctype.ErrNotFound", err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestDoc_NewEngine_ChecksRegistry$", "-test.count=1")
	cmd.Env = append(os.Environ(), envBrokenRegistry+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("child with a broken registry failed: %v\n%s", err, out)
	}
}

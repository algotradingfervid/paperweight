package doc_test

import (
	"fmt"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/types"
)

// CD-178: Required fields are enforced on submit, not on draft saves. A
// draft save (create, save, line saves) still returns type, format, length,
// Select-option, master-link and line-shape errors.

// rqFixFields is the KindInvalid message (msgFixFields).
const rqFixFields = "Fix the highlighted fields and save again."

// rqSubmitFails submits draft id at version v, wants a KindInvalid error with
// exactly want as Fields, and checks nothing was written: the document and
// the audit count are unchanged.
func rqSubmitFails(t *testing.T, h *fixturetest.H, id int64, v int, want []doc.FieldError) {
	t.Helper()
	before := loadDoc(t, h, fixture.Order, id)
	count := h.AuditCount(t)
	_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, id, doc.ActionSubmit, doc.ActionInput{Version: v})
	de := wantDocErr(t, err, doc.KindInvalid, rqFixFields)
	if diff := diffOf(want, de.Fields); diff != "" {
		t.Errorf("submit Fields (want, got):\n%s", diff)
	}
	after := loadDoc(t, h, fixture.Order, id)
	if after.State != doc.StateDraft || after.Version != v {
		t.Errorf("after a failed submit: State %q, Version %d, want draft, %d", after.State, after.Version, v)
	}
	if diff := diffOf(before, after); diff != "" {
		t.Errorf("document changed by a failed submit (before, after):\n%s", diff)
	}
	if n := h.AuditCount(t); n != count {
		t.Errorf("AuditCount = %d after a failed submit, want %d (no audit row)", n, count)
	}
}

// DOC-019
func TestDoc_Validate_MandatoryCollected(t *testing.T) {
	h := fixturetest.New(t)
	in := doc.ActionInput{
		Values: map[string]string{"supplier_id": "", "order_date": "", "project_id": fmt.Sprint(h.M.Project["PRJ-METRO"])},
		Lines:  []map[string]string{{"item_id": fmt.Sprint(h.M.Item["MS-ANGLE-50"]), "qty": "1200.000"}},
	}
	res, err := h.Do(t, t.Context(), h.Priya, fixture.Order, 0, doc.ActionSave, in)
	if err != nil {
		t.Fatalf("draft create with supplier and order date empty: %v, want ok (CD-178)", err)
	}
	rqSubmitFails(t, h, res.ID, res.Version, []doc.FieldError{
		{Field: "supplier_id", Line: 0, Message: "Supplier is required."},
		{Field: "order_date", Line: 0, Message: "Order date is required."},
	})
}

// CD-178 (1): a draft create and a save with Required header fields empty
// succeed and store the values given.
func TestDoc_Draft_SaveSkipsRequired(t *testing.T) {
	h := fixturetest.New(t)
	project := h.M.Project["PRJ-METRO"]
	res, err := h.Do(t, t.Context(), h.Priya, fixture.Order, 0, doc.ActionSave, doc.ActionInput{Values: map[string]string{
		"supplier_id": "", "order_date": "", "project_id": fmt.Sprint(project),
		"remarks": "Wizard step 1", "supplier_ref": "Q-118",
	}})
	if err != nil {
		t.Fatalf("draft create with Required fields empty: %v, want ok", err)
	}
	if res.State != doc.StateDraft || res.Version != 1 {
		t.Errorf("create Result = {State %q, Version %d}, want {draft, 1}", res.State, res.Version)
	}
	d := loadDoc(t, h, fixture.Order, res.ID)
	if d.Header["supplier_id"] != nil || d.Header["order_date"] != nil {
		t.Errorf("after create: supplier_id, order_date = %#v, %#v, want nil, nil", d.Header["supplier_id"], d.Header["order_date"])
	}
	if got := d.Header["project_id"]; got != any(project) {
		t.Errorf("after create: project_id = %#v, want %d", got, project)
	}
	if got := d.Header["remarks"]; got != any("Wizard step 1") {
		t.Errorf("after create: remarks = %#v, want %q", got, "Wizard step 1")
	}
	if got := d.Header["supplier_ref"]; got != any("Q-118") {
		t.Errorf("after create: supplier_ref = %#v, want %q", got, "Q-118")
	}
	if entries := h.Audit(t, fixture.Order, res.ID); len(entries) != 1 || entries[0].Action != "create" {
		t.Errorf("audit after create = %+v, want one create row", entries)
	}

	// Save: supplier given, order_date still empty.
	shree := h.M.Party["SHREE"]
	in := h.In(d)
	in.Values["supplier_id"] = fmt.Sprint(shree)
	in.Values["order_date"] = ""
	in.Values["remarks"] = "Wizard step 2"
	saved := saveDo(t, h, h.Priya, fixture.Order, res.ID, in)
	if saved.Version != 2 {
		t.Errorf("save Version = %d, want 2", saved.Version)
	}
	d = loadDoc(t, h, fixture.Order, res.ID)
	if got := d.Header["supplier_id"]; got != any(shree) {
		t.Errorf("after save: supplier_id = %#v, want %d", got, shree)
	}
	if d.Header["order_date"] != nil {
		t.Errorf("after save: order_date = %#v, want nil", d.Header["order_date"])
	}
	if got := d.Header["remarks"]; got != any("Wizard step 2") {
		t.Errorf("after save: remarks = %#v, want %q", got, "Wizard step 2")
	}
	_ = saveEntry(t, h, fixture.Order, res.ID)

	// Save: a Required field that was filled is emptied again.
	in = h.In(d)
	in.Values["supplier_id"] = ""
	saved = saveDo(t, h, h.Priya, fixture.Order, res.ID, in)
	if saved.Version != 3 {
		t.Errorf("second save Version = %d, want 3", saved.Version)
	}
	if got := loadDoc(t, h, fixture.Order, res.ID).Header["supplier_id"]; got != nil {
		t.Errorf("after the second save: supplier_id = %#v, want nil", got)
	}
}

// CD-178 (2): a line save succeeds while a Required header field is empty.
func TestDoc_Draft_LineSaveRequiredHeaderEmpty(t *testing.T) {
	h := fixturetest.New(t)
	res := h.NewOrder(t, h.Priya)
	// Setup without the engine, so only the line saves depend on CD-178.
	h.Exec(t, `UPDATE fixture_order SET supplier_id = NULL WHERE id = ?`, res.ID)
	d := loadDoc(t, h, fixture.Order, res.ID)
	if d.Header["supplier_id"] != nil {
		t.Fatalf("setup: supplier_id = %#v, want nil", d.Header["supplier_id"])
	}

	angle, wire := h.M.Item["MS-ANGLE-50"], h.M.Item["WIRE-18G"]
	in := h.In(d)
	in.Lines = []map[string]string{{"item_id": fmt.Sprint(angle), "qty": "12.000", "rate": "62.5000"}}
	saved := saveDo(t, h, h.Priya, fixture.Order, res.ID, in)
	if saved.Version != 2 {
		t.Errorf("line save Version = %d, want 2", saved.Version)
	}
	d = loadDoc(t, h, fixture.Order, res.ID)
	if len(d.Lines) != 1 {
		t.Fatalf("after the line save: %d lines, want 1", len(d.Lines))
	}
	l := d.Lines[0].Values
	if l["item_id"] != any(angle) || l["qty"] != any(types.Qty(12000)) || l["rate"] != any(types.Rate(625000)) {
		t.Errorf("line 1 item_id, qty, rate = %#v, %#v, %#v, want %d, 12.000, 62.5000", l["item_id"], l["qty"], l["rate"], angle)
	}
	if d.Header["supplier_id"] != nil {
		t.Errorf("after the line save: supplier_id = %#v, want nil", d.Header["supplier_id"])
	}

	// A second line save: change line 1, add line 2.
	in = h.In(d)
	in.Lines[0]["qty"] = "15.000"
	in.Lines = append(in.Lines, map[string]string{"item_id": fmt.Sprint(wire), "qty": "3.000"})
	saved = saveDo(t, h, h.Priya, fixture.Order, res.ID, in)
	if saved.Version != 3 {
		t.Errorf("second line save Version = %d, want 3", saved.Version)
	}
	d = loadDoc(t, h, fixture.Order, res.ID)
	if len(d.Lines) != 2 {
		t.Fatalf("after the second line save: %d lines, want 2", len(d.Lines))
	}
	if got := d.Lines[0].Values["qty"]; got != any(types.Qty(15000)) {
		t.Errorf("line 1 qty = %#v, want 15.000", got)
	}
	if got := d.Lines[1].Values["item_id"]; got != any(wire) {
		t.Errorf("line 2 item_id = %#v, want %d", got, wire)
	}
}

// CD-178 (3): submit with Required header and line fields empty returns every
// Required error in field order (header, then each line), and writes nothing.
func TestDoc_Submit_RequiredAllReported(t *testing.T) {
	h := fixturetest.New(t)
	in := doc.ActionInput{
		Values: map[string]string{"supplier_id": "", "order_date": "", "project_id": fmt.Sprint(h.M.Project["PRJ-METRO"])},
		Lines: []map[string]string{
			{"item_id": fmt.Sprint(h.M.Item["MS-ANGLE-50"]), "qty": ""},
			{"item_id": "", "qty": "5.000"},
		},
	}
	res, err := h.Do(t, t.Context(), h.Priya, fixture.Order, 0, doc.ActionSave, in)
	if err != nil {
		t.Fatalf("draft create with Required header and line fields empty: %v, want ok", err)
	}
	if n := len(loadDoc(t, h, fixture.Order, res.ID).Lines); n != 2 {
		t.Fatalf("draft lines = %d, want 2", n)
	}
	rqSubmitFails(t, h, res.ID, res.Version, []doc.FieldError{
		{Field: "supplier_id", Line: 0, Message: "Supplier is required."},
		{Field: "order_date", Line: 0, Message: "Order date is required."},
		{Field: "qty", Line: 1, Message: "Line 1: Quantity is required."},
		{Field: "item_id", Line: 2, Message: "Line 2: Item is required."},
	})
}

// CD-178 (4): a draft save with a bad format, length or master link still
// fails with those field errors while Required fields are empty, and doesn't
// report the Required ones.
func TestDoc_Draft_FormatErrorsStillReported(t *testing.T) {
	h := fixturetest.New(t)
	angle := fmt.Sprint(h.M.Item["MS-ANGLE-50"])

	// Create: a non-numeric Qty, supplier and order_date empty.
	_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, 0, doc.ActionSave, doc.ActionInput{
		Values: map[string]string{"supplier_id": "", "order_date": ""},
		Lines:  []map[string]string{{"item_id": angle, "qty": "abc"}},
	})
	de := wantDocErr(t, err, doc.KindInvalid, rqFixFields)
	want := []doc.FieldError{{Field: "qty", Line: 1, Message: "Line 1: Quantity isn't a valid value. Check it and try again."}}
	if diff := diffOf(want, de.Fields); diff != "" {
		t.Errorf("create Fields (want, got):\n%s", diff)
	}
	if n := orderRows(t, h); n != 0 {
		t.Errorf("fixture_order rows = %d after a refused create, want 0", n)
	}
	if n := h.AuditCount(t); n != 0 {
		t.Errorf("AuditCount = %d after a refused create, want 0", n)
	}

	// Save of a draft whose order_date is empty: an unknown supplier, an
	// out-of-range credit_days, a too-long supplier_ref and a non-numeric Qty.
	res := h.NewOrder(t, h.Priya, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "1.000"})
	h.Exec(t, `UPDATE fixture_order SET order_date = NULL WHERE id = ?`, res.ID)
	d := loadDoc(t, h, fixture.Order, res.ID)
	count := h.AuditCount(t)
	in := h.In(d)
	in.Values["supplier_id"] = "9999"
	in.Values["order_date"] = ""
	in.Values["credit_days"] = "400"
	in.Values["supplier_ref"] = fmt.Sprintf("%0141d", 0)
	in.Lines[0]["qty"] = "abc"
	_, err = h.Do(t, t.Context(), h.Priya, fixture.Order, res.ID, doc.ActionSave, in)
	de = wantDocErr(t, err, doc.KindInvalid, rqFixFields)
	want = []doc.FieldError{
		{Field: "supplier_id", Line: 0, Message: "Supplier not found. Pick one from the list."},
		{Field: "credit_days", Line: 0, Message: "Credit days must be between 0 and 365."},
		{Field: "supplier_ref", Line: 0, Message: "Supplier ref can be at most 140 characters."},
		{Field: "qty", Line: 1, Message: "Line 1: Quantity isn't a valid value. Check it and try again."},
	}
	if diff := diffOf(want, de.Fields); diff != "" {
		t.Errorf("save Fields (want, got):\n%s", diff)
	}
	if diff := diffOf(d, loadDoc(t, h, fixture.Order, res.ID)); diff != "" {
		t.Errorf("draft changed by a refused save (before, after):\n%s", diff)
	}
	if n := h.AuditCount(t); n != count {
		t.Errorf("AuditCount = %d after a refused save, want %d", n, count)
	}
}

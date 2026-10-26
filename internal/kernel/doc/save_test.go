package doc_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/types"
)

// msgStaleFmt is the Do pipeline's stale text (step 9); %s is the display name.
const msgStaleFmt = "Someone else changed %s after you opened it. Reload it and make your changes again."

// saveDo saves draft id with in and fails the test on an error.
func saveDo(t *testing.T, h *fixturetest.H, p auth.Principal, dt string, id int64, in doc.ActionInput) doc.Result {
	t.Helper()
	res, err := h.Do(t, t.Context(), p, dt, id, doc.ActionSave, in)
	if err != nil {
		t.Fatalf("save %s %d: %v", dt, id, err)
	}
	return res
}

// saveEntry returns the document's last audit entry and fails unless it is a
// "save" row.
func saveEntry(t *testing.T, h *fixturetest.H, dt string, id int64) audit.Entry {
	t.Helper()
	entries := h.Audit(t, dt, id)
	if len(entries) == 0 {
		t.Fatalf("no audit entries for %s %d", dt, id)
	}
	e := entries[len(entries)-1]
	if e.Action != "save" {
		t.Fatalf("last audit entry Action = %q, want \"save\" (entries %+v)", e.Action, entries)
	}
	return e
}

// saveCount counts rows of a raw COUNT(*) query.
func saveCount(t *testing.T, h *fixturetest.H, query string, args ...any) int {
	t.Helper()
	var n int
	if err := h.DB.SQL().QueryRowContext(t.Context(), query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// saveLineIDs returns the ids of d's lines in line_no order.
func saveLineIDs(d doc.Doc) []int64 {
	var ids []int64
	for _, l := range d.Lines {
		ids = append(ids, l.ID)
	}
	return ids
}

// DOC-003
func TestDoc_Save_IgnoresSystemFields(t *testing.T) {
	h := fixturetest.New(t)
	res := h.NewOrderWith(t, h.Priya, map[string]string{
		"created_by": "3", "created_at": "2026-04-01T00:00:00.000Z",
	}, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "1200.000"})
	d := loadDoc(t, h, fixture.Order, res.ID)
	const at = "2026-10-24T05:00:00.000Z"
	if d.CreatedBy != types.UserID(4) {
		t.Errorf("after create: CreatedBy = %d, want 4 (priya)", d.CreatedBy)
	}
	if got := d.CreatedAt.Format(tsLayout); got != at {
		t.Errorf("after create: CreatedAt = %q, want %q", got, at)
	}
	if d.Version != 1 {
		t.Fatalf("after create: Version = %d, want 1", d.Version)
	}

	h.Clock.Set("2026-10-24T06:00:00Z")
	in := h.In(d)
	in.Values["state"] = "approved"
	in.Values["created_by"] = "1"
	in.Values["created_at"] = "2026-04-01T00:00:00.000Z"
	in.Values["version"] = "99"
	in.Values["remarks"] = "x"
	in.Lines[0]["received_qty"] = "10"
	saveDo(t, h, h.Priya, fixture.Order, res.ID, in)

	after := loadDoc(t, h, fixture.Order, res.ID)
	if after.State != doc.StateDraft {
		t.Errorf("State = %q, want draft", after.State)
	}
	if after.CreatedBy != types.UserID(4) {
		t.Errorf("CreatedBy = %d, want 4 (unchanged)", after.CreatedBy)
	}
	if got := after.CreatedAt.Format(tsLayout); got != at {
		t.Errorf("CreatedAt = %q, want %q (unchanged)", got, at)
	}
	if after.Version != 2 {
		t.Errorf("Version = %d, want 2", after.Version)
	}
	if got := after.Header["remarks"]; got != any("x") {
		t.Errorf("remarks = %v, want \"x\"", got)
	}
	if len(after.Lines) != 1 {
		t.Fatalf("len(Lines) = %d, want 1", len(after.Lines))
	}
	if n := saveCount(t, h, `SELECT COUNT(*) FROM fixture_order_line WHERE id = ? AND received_qty = 0`, after.Lines[0].ID); n != 1 {
		t.Errorf("received_qty of line %d is not 0 after a save that sent 10", after.Lines[0].ID)
	}
	var fields []string
	for _, c := range saveEntry(t, h, fixture.Order, res.ID).Changes {
		fields = append(fields, c.Field)
	}
	if diff := diffOf([]string{"remarks"}, fields); diff != "" {
		t.Errorf("save audit changed fields (want, got):\n%s", diff)
	}

	// CD-79: a ReadOnly computed field.
	claim := h.NewClaim(t, h.Deepak, "EMP-007",
		fixturetest.CL{Type: "TRAVEL", Amount: "1250.00"}, fixturetest.CL{Type: "FOOD", Amount: "480.50"})
	cd := loadDoc(t, h, fixture.Claim, claim.ID)
	count := h.AuditCount(t)
	bad := h.In(cd)
	bad.Values["total"] = "99999.00"
	_, err := h.Do(t, t.Context(), h.Deepak, fixture.Claim, claim.ID, doc.ActionSave, bad)
	de := wantDocErr(t, err, doc.KindInvalid, "")
	want := []doc.FieldError{{Field: "total", Line: 0, Message: "Total is calculated and can't be entered."}}
	if diff := diffOf(want, de.Fields); diff != "" {
		t.Errorf("total refusal Fields (want, got):\n%s", diff)
	}
	if v := loadDoc(t, h, fixture.Claim, claim.ID).Version; v != cd.Version {
		t.Errorf("claim Version = %d after a refused save, want %d", v, cd.Version)
	}
	if n := h.AuditCount(t); n != count {
		t.Errorf("AuditCount = %d after a refused save, want %d", n, count)
	}
	for _, total := range []string{"1730.50", ""} {
		ok := h.In(loadDoc(t, h, fixture.Claim, claim.ID))
		ok.Values["total"] = total
		if _, err := h.Do(t, t.Context(), h.Deepak, fixture.Claim, claim.ID, doc.ActionSave, ok); err != nil {
			t.Errorf("save with total %q: %v, want ok", total, err)
		}
		if n := saveCount(t, h, `SELECT COUNT(*) FROM fixture_claim WHERE id = ? AND total = 173050`, claim.ID); n != 1 {
			t.Errorf("after save with total %q: total is not 173050", total)
		}
	}
}

// DOC-026. CD-178: a draft save skips Required, so on a save the core error
// merged with the hook's is a non-Required one (credit_days out of range);
// the missing supplier is merged with the hook's error on submit.
func TestDoc_Validate_HookErrorsMerged(t *testing.T) {
	h := fixturetest.New(t)
	res := h.NewOrder(t, h.Priya, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10.000", Rate: "5.0000"})
	before := loadDoc(t, h, fixture.Order, res.ID)
	count := h.AuditCount(t)

	probe := &fixture.Probe{ValidateErrors: []doc.FieldError{{Field: "rate", Line: 1, Message: "Line 1: Rate must be more than 0."}}}
	ctx := fixture.WithProbe(t.Context(), probe)
	in := h.In(before)
	in.Values["supplier_id"] = ""
	in.Values["credit_days"] = "400"
	_, err := h.Do(t, ctx, h.Priya, fixture.Order, res.ID, doc.ActionSave, in)
	de := wantDocErr(t, err, doc.KindInvalid, "Fix the highlighted fields and save again.")
	want := []doc.FieldError{
		{Field: "credit_days", Line: 0, Message: "Credit days must be between 0 and 365."},
		{Field: "rate", Line: 1, Message: "Line 1: Rate must be more than 0."},
	}
	if diff := diffOf(want, de.Fields); diff != "" {
		t.Errorf("save Fields (want, got):\n%s", diff)
	}
	var paths []string
	for _, fe := range de.FieldErrors() {
		paths = append(paths, fe.Path())
	}
	if diff := diffOf([]string{"credit_days", "lines[1].rate"}, paths); diff != "" {
		t.Errorf("save paths (want, got):\n%s", diff)
	}
	if diff := diffOf(before, loadDoc(t, h, fixture.Order, res.ID)); diff != "" {
		t.Errorf("document changed by a failed save (before, after):\n%s", diff)
	}
	if n := h.AuditCount(t); n != count {
		t.Errorf("AuditCount = %d, want %d", n, count)
	}

	// Submit: the draft saved without a supplier (CD-178), then the hook's
	// error comes back together with the Required one.
	blank := h.In(before)
	blank.Values["supplier_id"] = ""
	saved := saveDo(t, h, h.Priya, fixture.Order, res.ID, blank)
	draft := loadDoc(t, h, fixture.Order, res.ID)
	count = h.AuditCount(t)
	_, err = h.Do(t, ctx, h.Priya, fixture.Order, res.ID, doc.ActionSubmit, doc.ActionInput{Version: saved.Version})
	de = wantDocErr(t, err, doc.KindInvalid, "Fix the highlighted fields and save again.")
	want = []doc.FieldError{
		{Field: "supplier_id", Line: 0, Message: "Supplier is required."},
		{Field: "rate", Line: 1, Message: "Line 1: Rate must be more than 0."},
	}
	if diff := diffOf(want, de.Fields); diff != "" {
		t.Errorf("submit Fields (want, got):\n%s", diff)
	}
	paths = nil
	for _, fe := range de.FieldErrors() {
		paths = append(paths, fe.Path())
	}
	if diff := diffOf([]string{"supplier_id", "lines[1].rate"}, paths); diff != "" {
		t.Errorf("submit paths (want, got):\n%s", diff)
	}
	if diff := diffOf(draft, loadDoc(t, h, fixture.Order, res.ID)); diff != "" {
		t.Errorf("document changed by a failed submit (before, after):\n%s", diff)
	}
	if n := h.AuditCount(t); n != count {
		t.Errorf("AuditCount = %d after a failed submit, want %d", n, count)
	}
}

// DOC-028
func TestDoc_Validate_FailureWritesNothing(t *testing.T) {
	h := fixturetest.New(t)
	res := h.NewOrder(t, h.Priya, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10.000"})
	in := h.In(loadDoc(t, h, fixture.Order, res.ID))
	in.Values["remarks"] = "first save"
	saveDo(t, h, h.Priya, fixture.Order, res.ID, in)
	d := loadDoc(t, h, fixture.Order, res.ID)
	if d.Version != 2 {
		t.Fatalf("setup: Version = %d, want 2", d.Version)
	}
	count := h.AuditCount(t)

	in = h.In(d)
	in.Values["supplier_id"] = fmt.Sprint(h.M.Party["BHARAT"])
	in.Values["order_date"] = ""
	// CD-178: the blank order_date alone no longer fails a draft save; the
	// out-of-range credit_days does, and is the only error.
	in.Values["credit_days"] = "400"
	_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, res.ID, doc.ActionSave, in)
	de := wantDocErr(t, err, doc.KindInvalid, "")
	if diff := diffOf([]doc.FieldError{{Field: "credit_days", Message: "Credit days must be between 0 and 365."}}, de.Fields); diff != "" {
		t.Errorf("Fields (want, got):\n%s", diff)
	}

	after := loadDoc(t, h, fixture.Order, res.ID)
	if got := after.Header["supplier_id"]; got != any(h.M.Party["SHREE"]) {
		t.Errorf("supplier_id = %v, want SHREE (%d)", got, h.M.Party["SHREE"])
	}
	if after.Version != 2 {
		t.Errorf("Version = %d, want 2", after.Version)
	}
	if diff := diffOf(d, after); diff != "" {
		t.Errorf("document changed by a failed save (before, after):\n%s", diff)
	}
	if n := h.AuditCount(t); n != count {
		t.Errorf("AuditCount = %d, want %d", n, count)
	}
}

// DOC-030
func TestDoc_Save_OwnerAndModifier(t *testing.T) {
	h := fixturetest.New(t)
	res := h.NewOrder(t, h.Priya, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10.000"})
	h.Clock.Set("2026-10-24T09:15:00Z")
	in := h.In(loadDoc(t, h, fixture.Order, res.ID))
	in.Values["remarks"] = "changed by admin"
	saveDo(t, h, h.Admin, fixture.Order, res.ID, in)

	d := loadDoc(t, h, fixture.Order, res.ID)
	if d.CreatedBy != types.UserID(4) {
		t.Errorf("CreatedBy = %d, want 4 (priya)", d.CreatedBy)
	}
	if got := d.CreatedAt.Format(tsLayout); got != "2026-10-24T05:00:00.000Z" {
		t.Errorf("CreatedAt = %q, want 2026-10-24T05:00:00.000Z", got)
	}
	if d.UpdatedBy != types.UserID(1) {
		t.Errorf("UpdatedBy = %d, want 1 (admin)", d.UpdatedBy)
	}
	if got := d.UpdatedAt.Format(tsLayout); got != "2026-10-24T09:15:00.000Z" {
		t.Errorf("UpdatedAt = %q, want 2026-10-24T09:15:00.000Z", got)
	}
	var updated string
	if err := h.DB.SQL().QueryRowContext(t.Context(), `SELECT updated_at FROM fixture_order WHERE id = ?`, res.ID).Scan(&updated); err != nil {
		t.Fatalf("read updated_at: %v", err)
	}
	if updated != "2026-10-24T09:15:00.000Z" {
		t.Errorf("raw updated_at = %q, want 2026-10-24T09:15:00.000Z", updated)
	}
}

// DOC-040
func TestDoc_Lines_RemoveRenumbers(t *testing.T) {
	h := fixturetest.New(t)
	res := h.NewOrder(t, h.Priya,
		fixturetest.OL{Item: "MS-ANGLE-50", Qty: "1.000"},
		fixturetest.OL{Item: "WIRE-18G", Qty: "2.000"},
		fixturetest.OL{Item: "BOLT-M12", Qty: "3.000"},
		fixturetest.OL{Item: "CEMENT-OPC", Qty: "4.000"},
	)
	d := loadDoc(t, h, fixture.Order, res.ID)
	if len(d.Lines) != 4 {
		t.Fatalf("setup: len(Lines) = %d, want 4", len(d.Lines))
	}
	a, b, c, dd := d.Lines[0].ID, d.Lines[1].ID, d.Lines[2].ID, d.Lines[3].ID

	in := h.In(d)
	in.Lines = append([]map[string]string{in.Lines[0]}, in.Lines[2:]...)
	saveDo(t, h, h.Priya, fixture.Order, res.ID, in)

	after := loadDoc(t, h, fixture.Order, res.ID)
	if diff := diffOf([]int64{a, c, dd}, saveLineIDs(after)); diff != "" {
		t.Errorf("line ids (want, got):\n%s", diff)
	}
	for i, l := range after.Lines {
		if l.LineNo != i+1 {
			t.Errorf("Lines[%d].LineNo = %d, want %d", i, l.LineNo, i+1)
		}
	}
	if n := saveCount(t, h, `SELECT COUNT(*) FROM fixture_order_line WHERE id = ?`, b); n != 0 {
		t.Errorf("line B (%d) rows = %d, want 0", b, n)
	}

	e := saveEntry(t, h, fixture.Order, res.ID)
	if len(e.Changes) != 1 {
		t.Fatalf("save changes = %+v, want exactly the removal (a line_no change alone is not recorded)", e.Changes)
	}
	ch := e.Changes[0]
	if ch.LineNo != 2 || ch.LineID != b || ch.Op != audit.OpRemove {
		t.Errorf("change = %+v, want {LineNo 2, LineID %d, Op remove}", ch, b)
	}
	if !strings.Contains(ch.Before, `"code":"WIRE-18G"`) {
		t.Errorf("remove Before = %s, want it to contain \"code\":\"WIRE-18G\"", ch.Before)
	}
}

// DOC-042
func TestDoc_Lines_StableIDs(t *testing.T) {
	h := fixturetest.New(t)
	res := h.NewOrder(t, h.Priya,
		fixturetest.OL{Item: "MS-ANGLE-50", Qty: "5.000"},
		fixturetest.OL{Item: "WIRE-18G", Qty: "10.000"},
	)
	d := loadDoc(t, h, fixture.Order, res.ID)
	a, b := d.Lines[0].ID, d.Lines[1].ID

	in := h.In(d)
	in.Lines[1]["qty"] = "12.000"
	in.Lines = append(in.Lines, map[string]string{"item_id": fmt.Sprint(h.M.Item["BOLT-M12"]), "qty": "1.000"})
	saveDo(t, h, h.Priya, fixture.Order, res.ID, in)

	after := loadDoc(t, h, fixture.Order, res.ID)
	if len(after.Lines) != 3 {
		t.Fatalf("len(Lines) = %d, want 3", len(after.Lines))
	}
	ids := saveLineIDs(after)
	if ids[0] != a || ids[1] != b {
		t.Errorf("line ids = %v, want a, b kept (%d, %d)", ids, a, b)
	}
	nw := ids[2]
	if nw == 0 || nw == a || nw == b {
		t.Errorf("new line id = %d, want a fresh id", nw)
	}
	if got := after.Lines[1].Values["qty"]; got != any(types.Qty(12000)) {
		t.Errorf("line b qty = %v, want 12000", got)
	}

	e := saveEntry(t, h, fixture.Order, res.ID)
	if len(e.Changes) != 2 {
		t.Fatalf("save changes = %+v, want the qty set and the add", e.Changes)
	}
	var set, add *audit.Change
	for i := range e.Changes {
		switch e.Changes[i].Op {
		case audit.OpSet:
			set = &e.Changes[i]
		case audit.OpAdd:
			add = &e.Changes[i]
		}
	}
	wantSet := audit.Change{Field: "qty", LineNo: 2, LineID: b, Before: "10000", After: "12000", Op: audit.OpSet}
	if set == nil {
		t.Errorf("no set change in %+v", e.Changes)
	} else if diff := diffOf(wantSet, *set); diff != "" {
		t.Errorf("set change (want, got):\n%s", diff)
	}
	if add == nil {
		t.Errorf("no add change in %+v", e.Changes)
	} else if add.LineNo != 3 || add.LineID != nw {
		t.Errorf("add change = %+v, want {LineNo 3, LineID %d, Op add}", *add, nw)
	}
}

// DOC-046
func TestDoc_Lines_ForeignLineIDRefused(t *testing.T) {
	h := fixturetest.New(t)
	ra := h.NewOrder(t, h.Priya, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "1.000"})
	rb := h.NewOrder(t, h.Priya, fixturetest.OL{Item: "WIRE-18G", Qty: "2.000"})
	a, b := loadDoc(t, h, fixture.Order, ra.ID), loadDoc(t, h, fixture.Order, rb.ID)
	count := h.AuditCount(t)

	in := h.In(a)
	in.Lines[0]["id"] = fmt.Sprint(b.Lines[0].ID)
	_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, ra.ID, doc.ActionSave, in)
	_ = wantDocErr(t, err, doc.KindRefused, "Line 1 doesn't belong to this document. Reload and try again.")

	if diff := diffOf(a, loadDoc(t, h, fixture.Order, ra.ID)); diff != "" {
		t.Errorf("order A changed (before, after):\n%s", diff)
	}
	if diff := diffOf(b, loadDoc(t, h, fixture.Order, rb.ID)); diff != "" {
		t.Errorf("order B changed (before, after):\n%s", diff)
	}
	if n := h.AuditCount(t); n != count {
		t.Errorf("AuditCount = %d, want %d", n, count)
	}
}

// DOC-069
func TestDoc_Concurrency_StaleSave(t *testing.T) {
	h := fixturetest.New(t)
	res := h.NewOrder(t, h.Priya, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "1.000"})
	for i := 1; i <= 4; i++ {
		in := h.In(loadDoc(t, h, fixture.Order, res.ID))
		in.Values["remarks"] = fmt.Sprintf("edit %d", i)
		saveDo(t, h, h.Priya, fixture.Order, res.ID, in)
	}
	d := loadDoc(t, h, fixture.Order, res.ID)
	if d.Version != 5 {
		t.Fatalf("setup: Version = %d, want 5", d.Version)
	}
	u1, u2 := h.In(d), h.In(d)

	u1.Values["remarks"] = "U1's change"
	r1 := saveDo(t, h, h.Priya, fixture.Order, res.ID, u1)
	if r1.Version != 6 {
		t.Errorf("U1 Result.Version = %d, want 6", r1.Version)
	}
	count := h.AuditCount(t)

	u2.Values["remarks"] = "U2's change"
	_, err := h.Do(t, t.Context(), h.Meena, fixture.Order, res.ID, doc.ActionSave, u2)
	de := wantDocErr(t, err, doc.KindStale, fmt.Sprintf(msgStaleFmt, fmt.Sprintf("Draft %d", res.ID)))
	if de.Status() != 409 {
		t.Errorf("Status() = %d, want 409", de.Status())
	}

	after := loadDoc(t, h, fixture.Order, res.ID)
	if got := after.Header["remarks"]; got != any("U1's change") {
		t.Errorf("remarks = %v, want U1's value", got)
	}
	if after.Version != 6 {
		t.Errorf("Version = %d, want 6", after.Version)
	}
	if n := h.AuditCount(t); n != count {
		t.Errorf("AuditCount = %d, want %d", n, count)
	}
}

// DOC-100
func TestDoc_Save_NumberGuarded(t *testing.T) {
	h := fixturetest.New(t)
	in := doc.ActionInput{Values: map[string]string{
		"supplier_id": fmt.Sprint(h.M.Party["SHREE"]), "order_date": "2026-10-24", "number": "PO/26-27/00099",
	}}
	_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, 0, doc.ActionSave, in)
	de := wantDocErr(t, err, doc.KindInvalid, "")
	if len(de.Fields) == 0 || de.Fields[0].Field != "number" {
		t.Errorf("create Fields = %+v, want Fields[0].Field \"number\"", de.Fields)
	}
	if n := orderRows(t, h); n != 0 {
		t.Errorf("fixture_order rows = %d, want 0", n)
	}
	if n := h.AuditCount(t); n != 0 {
		t.Errorf("AuditCount = %d, want 0", n)
	}

	res := h.NewOrder(t, h.Priya, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "1.000"})
	d := loadDoc(t, h, fixture.Order, res.ID)
	count := h.AuditCount(t)
	edit := h.In(d)
	edit.Values["number"] = "PO/26-27/00099"
	edit.Values["remarks"] = "with a number"
	_, err = h.Do(t, t.Context(), h.Priya, fixture.Order, res.ID, doc.ActionSave, edit)
	de = wantDocErr(t, err, doc.KindInvalid, "")
	if len(de.Fields) == 0 || de.Fields[0].Field != "number" {
		t.Errorf("save Fields = %+v, want Fields[0].Field \"number\"", de.Fields)
	}
	if diff := diffOf(d, loadDoc(t, h, fixture.Order, res.ID)); diff != "" {
		t.Errorf("draft changed by a refused save (before, after):\n%s", diff)
	}
	if n := h.AuditCount(t); n != count {
		t.Errorf("AuditCount = %d, want %d", n, count)
	}

	edit.Values["number"] = ""
	saveDo(t, h, h.Priya, fixture.Order, res.ID, edit)
	after := loadDoc(t, h, fixture.Order, res.ID)
	if after.Version != 2 || after.Number != "" || after.Header["remarks"] != any("with a number") {
		t.Errorf("after save with number \"\": Version %d, Number %q, remarks %v; want 2, \"\", \"with a number\"",
			after.Version, after.Number, after.Header["remarks"])
	}
}

// DOC-105
func TestDoc_Save_ChecksRestrictions(t *testing.T) {
	h := fixturetest.New(t)
	techpark, metro := h.M.Project["PRJ-TECHPARK"], h.M.Project["PRJ-METRO"]
	h.Auth.DenyValues = func(_ string, values map[string][]int64) error {
		if slices.Contains(values["project_id"], techpark) {
			return auth.ErrForbidden
		}
		return nil
	}
	wantAuthErr := func(what string, err error) {
		t.Helper()
		if !errors.Is(err, auth.ErrForbidden) {
			t.Errorf("%s: err = %v, want auth.ErrForbidden", what, err)
		}
		var de *doc.Error
		if errors.As(err, &de) {
			t.Errorf("%s: err is wrapped in a *doc.Error (%+v), want AUTH's error unchanged", what, de)
		}
	}
	callsSince := func(mark int, call string) int {
		return len(slices.DeleteFunc(slices.Clone(h.Auth.Calls[mark:]), func(c string) bool { return c != call }))
	}

	// Create on PRJ-TECHPARK.
	mark := len(h.Auth.Calls)
	in := doc.ActionInput{
		Values: map[string]string{
			"supplier_id": fmt.Sprint(h.M.Party["SHREE"]), "project_id": fmt.Sprint(techpark), "order_date": "2026-10-24",
		},
		Lines: []map[string]string{{"item_id": fmt.Sprint(h.M.Item["MS-ANGLE-50"]), "qty": "1.000"}},
	}
	_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, 0, doc.ActionSave, in)
	wantAuthErr("create on PRJ-TECHPARK", err)
	if n := callsSince(mark, fmt.Sprintf("AUTH.CheckValues:project_id=%d", techpark)); n != 1 {
		t.Errorf("create: CheckValues(project_id=TECHPARK) calls = %d, want 1 (Calls %v)", n, h.Auth.Calls[mark:])
	}
	if n := orderRows(t, h); n != 0 {
		t.Errorf("fixture_order rows = %d, want 0", n)
	}
	if n := h.AuditCount(t); n != 0 {
		t.Errorf("AuditCount = %d, want 0", n)
	}

	// Create on PRJ-METRO is allowed.
	mark = len(h.Auth.Calls)
	res := h.NewOrder(t, h.Priya, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "1.000"})
	if n := callsSince(mark, fmt.Sprintf("AUTH.CheckValues:project_id=%d", metro)); n != 1 {
		t.Errorf("create on PRJ-METRO: CheckValues(project_id=METRO) calls = %d, want 1 (Calls %v)", n, h.Auth.Calls[mark:])
	}

	// Saving that draft onto PRJ-TECHPARK.
	d := loadDoc(t, h, fixture.Order, res.ID)
	count := h.AuditCount(t)
	edit := h.In(d)
	edit.Values["project_id"] = fmt.Sprint(techpark)
	mark = len(h.Auth.Calls)
	_, err = h.Do(t, t.Context(), h.Priya, fixture.Order, res.ID, doc.ActionSave, edit)
	wantAuthErr("save onto PRJ-TECHPARK", err)
	if n := callsSince(mark, fmt.Sprintf("AUTH.CheckValues:project_id=%d", techpark)); n != 1 {
		t.Errorf("save: CheckValues(project_id=TECHPARK) calls = %d, want 1 (Calls %v)", n, h.Auth.Calls[mark:])
	}
	if diff := diffOf(d, loadDoc(t, h, fixture.Order, res.ID)); diff != "" {
		t.Errorf("draft changed by a refused save (before, after):\n%s", diff)
	}
	if n := h.AuditCount(t); n != count {
		t.Errorf("AuditCount = %d, want %d", n, count)
	}

	// A receipt on an approved order the user may not see.
	o := h.NewOrder(t, h.Priya,
		fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10.000"},
		fixturetest.OL{Item: "WIRE-18G", Qty: "5.000"},
	)
	h.ForceState(t, fixture.Order, o.ID, doc.StateApproved, "PO/26-27/00001")
	od := loadDoc(t, h, fixture.Order, o.ID)
	h.Auth.DenyLink = func(target string, id int64) error {
		if target == fixture.Order && id == o.ID {
			return auth.ErrForbidden
		}
		return nil
	}
	count = h.AuditCount(t)
	mark = len(h.Auth.Calls)
	_, err = h.Do(t, t.Context(), h.Suresh, fixture.Receipt, 0, doc.ActionSave, doc.ActionInput{
		Values: map[string]string{"receipt_date": "2026-10-24"},
		Lines: []map[string]string{
			{"order_line_id": fmt.Sprint(od.Lines[0].ID), "qty": "1.000"},
			{"order_line_id": fmt.Sprint(od.Lines[1].ID), "qty": "1.000"},
		},
	})
	wantAuthErr("receipt on a hidden order", err)
	if n := callsSince(mark, fmt.Sprintf("AUTH.CheckLink:%s:%d", fixture.Order, o.ID)); n != 1 {
		t.Errorf("CheckLink(fixture.Order, %d) calls = %d, want exactly 1 (Calls %v)", o.ID, n, h.Auth.Calls[mark:])
	}
	if n := saveCount(t, h, `SELECT COUNT(*) FROM fixture_receipt`); n != 0 {
		t.Errorf("fixture_receipt rows = %d, want 0", n)
	}
	if n := saveCount(t, h, `SELECT COUNT(*) FROM fixture_receipt_line`); n != 0 {
		t.Errorf("fixture_receipt_line rows = %d, want 0", n)
	}
	if n := h.AuditCount(t); n != count {
		t.Errorf("AuditCount = %d, want %d", n, count)
	}
}

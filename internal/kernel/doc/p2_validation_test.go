package doc_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/types"
)

// p2vID formats an id as a form value.
func p2vID(v int64) string { return fmt.Sprint(v) }

// p2vOrderIn is a new order's input by the 00-fixtures defaults (SHREE,
// PRJ-METRO, 2026-10-24) with lines.
func p2vOrderIn(h *fixturetest.H, lines ...map[string]string) doc.ActionInput {
	return doc.ActionInput{
		Values: map[string]string{
			"supplier_id": p2vID(h.M.Party["SHREE"]), "project_id": p2vID(h.M.Project["PRJ-METRO"]),
			"order_date": "2026-10-24",
		},
		Lines: lines,
	}
}

// p2vLines returns n MS-ANGLE-50 lines of qty 1.
func p2vLines(h *fixturetest.H, n int) []map[string]string {
	lines := make([]map[string]string, n)
	for i := range lines {
		lines[i] = map[string]string{"item_id": p2vID(h.M.Item["MS-ANGLE-50"]), "qty": "1.000"}
	}
	return lines
}

// p2vLineRow is one stored order line, in line_no order.
type p2vLineRow struct {
	ID, ItemID, Qty, Rate int64
	LineNo                int
	UOM                   string
}

// p2vOrderLines reads the stored lines of order id in line_no order.
func p2vOrderLines(t *testing.T, h *fixturetest.H, id int64) []p2vLineRow {
	t.Helper()
	rows, err := h.DB.SQL().QueryContext(t.Context(),
		`SELECT id, line_no, item_id, qty, COALESCE(rate, 0), uom FROM fixture_order_line WHERE parent_id = ? ORDER BY line_no`, id)
	if err != nil {
		t.Fatalf("read lines of order %d: %v", id, err)
	}
	defer func() { _ = rows.Close() }()
	var out []p2vLineRow
	for rows.Next() {
		var r p2vLineRow
		if err := rows.Scan(&r.ID, &r.LineNo, &r.ItemID, &r.Qty, &r.Rate, &r.UOM); err != nil {
			t.Fatalf("scan line of order %d: %v", id, err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("lines of order %d: %v", id, err)
	}
	return out
}

// p2vHasFieldErr reports whether errs holds want exactly.
func p2vHasFieldErr(errs []doc.FieldError, want doc.FieldError) bool {
	return slices.Contains(errs, want)
}

// DOC-002
func TestDoc_Create_DefaultsOnlyFillMissing(t *testing.T) {
	h := fixturetest.New(t)
	in := p2vOrderIn(h,
		map[string]string{"item_id": p2vID(h.M.Item["CEMENT-OPC"]), "qty": "10.000", "uom": "Bag"},
		map[string]string{"item_id": p2vID(h.M.Item["BOLT-M12"]), "qty": "5.000"},
	)
	in.Values["discount_pct"] = "0"
	res := h.MustDo(t, t.Context(), h.Priya, fixture.Order, 0, doc.ActionSave, in)

	if n := saveCount(t, h, `SELECT COUNT(*) FROM fixture_order WHERE id = ? AND priority = 'normal'`, res.ID); n != 1 {
		t.Errorf("stored priority is not \"normal\" for an order created without priority")
	}
	if got := h.Record(t, fixture.Order, res.ID).Values["priority"]; got != "normal" {
		t.Errorf("Load priority = %q, want \"normal\"", got)
	}
	if n := saveCount(t, h, `SELECT COUNT(*) FROM fixture_order WHERE id = ? AND discount_pct IS NOT NULL AND discount_pct = 0`, res.ID); n != 1 {
		t.Errorf("stored discount_pct is not 0 after an explicit \"0\"")
	}
	var uoms []string
	for _, l := range p2vOrderLines(t, h, res.ID) {
		uoms = append(uoms, l.UOM)
	}
	if diff := diffOf([]string{"Bag", "Nos"}, uoms); diff != "" {
		t.Errorf("line uom (want, got):\n%s", diff)
	}
}

// DOC-024
func TestDoc_Validate_MaxLength(t *testing.T) {
	h := fixturetest.New(t)
	res := h.NewOrder(t, h.Priya, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "1.000"})
	d := loadDoc(t, h, fixture.Order, res.ID)
	count := h.AuditCount(t)

	long := h.In(d)
	long.Values["supplier_ref"] = strings.Repeat("a", 141)
	_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, res.ID, doc.ActionSave, long)
	de := wantDocErr(t, err, doc.KindInvalid, "")
	want := doc.FieldError{Field: "supplier_ref", Line: 0, Message: "Supplier ref can be at most 140 characters."}
	if !p2vHasFieldErr(de.Fields, want) {
		t.Errorf("Fields = %+v, want it to hold %+v", de.Fields, want)
	}
	if diff := diffOf(d, loadDoc(t, h, fixture.Order, res.ID)); diff != "" {
		t.Errorf("draft changed by a refused save (before, after):\n%s", diff)
	}
	if n := h.AuditCount(t); n != count {
		t.Errorf("AuditCount = %d after a refused save, want %d", n, count)
	}

	deva := strings.Repeat("क", 140) // 140 characters, 420 bytes
	if len(deva) <= 140 {
		t.Fatalf("setup: Devanagari value is %d bytes, want more than 140", len(deva))
	}
	ok := h.In(d)
	ok.Values["supplier_ref"] = deva
	if _, err := h.Do(t, t.Context(), h.Priya, fixture.Order, res.ID, doc.ActionSave, ok); err != nil {
		t.Fatalf("save with 140 Devanagari characters: %v, want ok", err)
	}
	if n := saveCount(t, h, `SELECT COUNT(*) FROM fixture_order WHERE id = ? AND supplier_ref = ?`, res.ID, deva); n != 1 {
		t.Errorf("stored supplier_ref is not the 140-character Devanagari value")
	}
}

// DOC-025
func TestDoc_ValidateFromToDates(t *testing.T) {
	apr1 := &types.Date{Year: 2026, Month: 4, Day: 1}
	mar31 := &types.Date{Year: 2027, Month: 3, Day: 31}
	for _, c := range []struct {
		name     string
		from, to *types.Date
	}{
		{"both empty", nil, nil},
		{"only from", apr1, nil},
		{"only to", nil, mar31},
		{"from before to", apr1, mar31},
		{"same day", apr1, &types.Date{Year: 2026, Month: 4, Day: 1}},
	} {
		if fe := doc.CheckFromTo(c.from, c.to, "Valid from", "Valid to", "valid_from", 0); fe != nil {
			t.Errorf("%s: CheckFromTo = %+v, want nil", c.name, *fe)
		}
	}

	for _, c := range []struct {
		name               string
		fromLabel, toLabel string
		fromField          string
		line               int
		want               doc.FieldError
	}{
		{"header", "Valid from", "Valid to", "valid_from", 0,
			doc.FieldError{Field: "valid_from", Line: 0, Message: "Valid to can't be before Valid from."}},
		{"line 2", "Valid from", "Valid to", "valid_from", 2,
			doc.FieldError{Field: "valid_from", Line: 2, Message: "Line 2: Valid to can't be before Valid from."}},
		{"other labels", "From date", "To date", "from_date", 0,
			doc.FieldError{Field: "from_date", Line: 0, Message: "To date can't be before From date."}},
	} {
		fe := doc.CheckFromTo(mar31, apr1, c.fromLabel, c.toLabel, c.fromField, c.line)
		if fe == nil {
			t.Errorf("%s: CheckFromTo(2027-03-31, 2026-04-01) = nil, want %+v", c.name, c.want)
			continue
		}
		if diff := diffOf(c.want, *fe); diff != "" {
			t.Errorf("%s: CheckFromTo (want, got):\n%s", c.name, diff)
		}
	}
}

// DOC-029
func TestDoc_Save_NoOp(t *testing.T) {
	h := fixturetest.New(t)
	res := h.NewOrder(t, h.Priya, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10.000", Rate: "62.5000"})
	for i := 1; i <= 3; i++ {
		in := h.In(loadDoc(t, h, fixture.Order, res.ID))
		in.Values["remarks"] = fmt.Sprintf("edit %d", i)
		saveDo(t, h, h.Priya, fixture.Order, res.ID, in)
	}
	d := loadDoc(t, h, fixture.Order, res.ID)
	if d.Version != 4 {
		t.Fatalf("setup: Version = %d, want 4", d.Version)
	}
	count := h.AuditCount(t)
	entries := len(h.Audit(t, fixture.Order, res.ID))

	h.Clock.Set("2026-10-24T09:15:00Z")
	got, err := h.Do(t, t.Context(), h.Priya, fixture.Order, res.ID, doc.ActionSave, h.In(d))
	if err != nil {
		t.Fatalf("identical save: %v, want nil", err)
	}
	if got.Version != 4 {
		t.Errorf("Result.Version = %d, want 4", got.Version)
	}
	after := loadDoc(t, h, fixture.Order, res.ID)
	if after.Version != 4 {
		t.Errorf("stored Version = %d, want 4", after.Version)
	}
	if !after.UpdatedAt.Equal(d.UpdatedAt) {
		t.Errorf("UpdatedAt = %s, want unchanged %s", after.UpdatedAt.Format(tsLayout), d.UpdatedAt.Format(tsLayout))
	}
	if diff := diffOf(d, after); diff != "" {
		t.Errorf("document changed by an identical save (before, after):\n%s", diff)
	}
	if n := h.AuditCount(t); n != count {
		t.Errorf("AuditCount = %d, want %d (no new row)", n, count)
	}
	if n := len(h.Audit(t, fixture.Order, res.ID)); n != entries {
		t.Errorf("order audit entries = %d, want %d", n, entries)
	}
}

// DOC-031
func TestDoc_Save_MissingDoc(t *testing.T) {
	h := fixturetest.New(t)
	res := h.NewOrder(t, h.Priya, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "1.000"})
	d := loadDoc(t, h, fixture.Order, res.ID)
	in := h.In(d)
	in.Values["remarks"] = "saved from the other tab"
	h.MustDo(t, t.Context(), h.Priya, fixture.Order, res.ID, doc.ActionDelete, doc.ActionInput{Version: d.Version})

	orders, lines, count := orderRows(t, h), saveCount(t, h, `SELECT COUNT(*) FROM fixture_order_line`), h.AuditCount(t)
	_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, res.ID, doc.ActionSave, in)
	de := wantDocErr(t, err, doc.KindNotFound, "This document no longer exists. It may have been deleted.")
	if de.Status() != 404 {
		t.Errorf("Status() = %d, want 404", de.Status())
	}
	if !errors.Is(err, doctype.ErrNotFound) {
		t.Errorf("err = %v, want it to wrap doctype.ErrNotFound (CD-54)", err)
	}
	if n := orderRows(t, h); n != orders {
		t.Errorf("fixture_order rows = %d, want %d (nothing created)", n, orders)
	}
	if n := saveCount(t, h, `SELECT COUNT(*) FROM fixture_order_line`); n != lines {
		t.Errorf("fixture_order_line rows = %d, want %d", n, lines)
	}
	if n := h.AuditCount(t); n != count {
		t.Errorf("AuditCount = %d, want %d", n, count)
	}
}

// DOC-041
func TestDoc_Lines_InsertAndMove(t *testing.T) {
	h := fixturetest.New(t)
	res := h.NewOrder(t, h.Priya,
		fixturetest.OL{Item: "MS-ANGLE-50", Qty: "1.000"},
		fixturetest.OL{Item: "WIRE-18G", Qty: "2.000"},
		fixturetest.OL{Item: "CEMENT-OPC", Qty: "3.000"},
	)
	d := loadDoc(t, h, fixture.Order, res.ID)
	if len(d.Lines) != 3 {
		t.Fatalf("setup: len(Lines) = %d, want 3", len(d.Lines))
	}
	a, b, c := d.Lines[0].ID, d.Lines[1].ID, d.Lines[2].ID

	in := h.In(d)
	x := map[string]string{"item_id": p2vID(h.M.Item["BOLT-M12"]), "qty": "7.000"}
	in.Lines = []map[string]string{in.Lines[2], in.Lines[0], x, in.Lines[1]}
	saveDo(t, h, h.Priya, fixture.Order, res.ID, in)

	rows := p2vOrderLines(t, h, res.ID)
	if len(rows) != 4 {
		t.Fatalf("stored lines = %+v, want 4", rows)
	}
	var ids []int64
	for i, r := range rows {
		ids = append(ids, r.ID)
		if r.LineNo != i+1 {
			t.Errorf("line %d: line_no = %d, want %d", i, r.LineNo, i+1)
		}
	}
	xid := ids[2]
	if xid == a || xid == b || xid == c || xid == 0 {
		t.Errorf("X id = %d, want a fresh id", xid)
	}
	if diff := diffOf([]int64{c, a, xid, b}, ids); diff != "" {
		t.Errorf("line ids in line_no order (want C, A, X, B; got):\n%s", diff)
	}
	if rows[2].ItemID != h.M.Item["BOLT-M12"] || rows[2].Qty != 7000 {
		t.Errorf("line 3 = %+v, want X (BOLT-M12, qty 7000)", rows[2])
	}
	if diff := diffOf([]int64{c, a, xid, b}, saveLineIDs(loadDoc(t, h, fixture.Order, res.ID))); diff != "" {
		t.Errorf("LoadDoc line order (want, got):\n%s", diff)
	}
}

// CD-135 (1): a save that only reorders lines is a write.
func TestDoc_Save_ReorderOnlyIsAWrite(t *testing.T) {
	h := fixturetest.New(t)
	res := h.NewOrder(t, h.Priya,
		fixturetest.OL{Item: "MS-ANGLE-50", Qty: "1.000"},
		fixturetest.OL{Item: "WIRE-18G", Qty: "2.000"},
		fixturetest.OL{Item: "CEMENT-OPC", Qty: "3.000"},
	)
	d := loadDoc(t, h, fixture.Order, res.ID)
	a, b, c := d.Lines[0].ID, d.Lines[1].ID, d.Lines[2].ID
	count := h.AuditCount(t)
	entries := len(h.Audit(t, fixture.Order, res.ID))

	in := h.In(d)
	in.Lines = []map[string]string{in.Lines[2], in.Lines[0], in.Lines[1]}
	got := saveDo(t, h, h.Priya, fixture.Order, res.ID, in)
	if got.Version != d.Version+1 {
		t.Errorf("Result.Version = %d, want %d", got.Version, d.Version+1)
	}
	after := loadDoc(t, h, fixture.Order, res.ID)
	if after.Version != d.Version+1 {
		t.Errorf("stored Version = %d, want %d", after.Version, d.Version+1)
	}
	rows := p2vOrderLines(t, h, res.ID)
	var ids []int64
	for i, r := range rows {
		ids = append(ids, r.ID)
		if r.LineNo != i+1 {
			t.Errorf("line %d: line_no = %d, want %d", i, r.LineNo, i+1)
		}
	}
	if diff := diffOf([]int64{c, a, b}, ids); diff != "" {
		t.Errorf("line ids in line_no order (want C, A, B; got):\n%s", diff)
	}
	if n := h.AuditCount(t); n != count+1 {
		t.Errorf("AuditCount = %d, want %d (one save row)", n, count+1)
	}
	all := h.Audit(t, fixture.Order, res.ID)
	if len(all) != entries+1 {
		t.Fatalf("order audit entries = %d, want %d", len(all), entries+1)
	}
	e := saveEntry(t, h, fixture.Order, res.ID)
	if len(e.Changes) != 0 {
		t.Errorf("save Changes = %+v, want none (a line_no change alone is not recorded)", e.Changes)
	}
}

// DOC-044
func TestDoc_Lines_Max500(t *testing.T) {
	h := fixturetest.New(t)
	want := doc.FieldError{Field: "lines", Line: 0, Message: "A document can have at most 500 lines. Split it into two documents."}

	_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, 0, doc.ActionSave, p2vOrderIn(h, p2vLines(h, 501)...))
	de := wantDocErr(t, err, doc.KindInvalid, "")
	if !p2vHasFieldErr(de.Fields, want) {
		t.Errorf("create with 501 lines: Fields = %+v, want it to hold %+v", de.Fields, want)
	}
	if n := orderRows(t, h); n != 0 {
		t.Errorf("fixture_order rows = %d after a refused create, want 0", n)
	}
	if n := h.AuditCount(t); n != 0 {
		t.Errorf("AuditCount = %d after a refused create, want 0", n)
	}

	res, err := h.Do(t, t.Context(), h.Priya, fixture.Order, 0, doc.ActionSave, p2vOrderIn(h, p2vLines(h, 500)...))
	if err != nil {
		t.Fatalf("create with 500 lines: %v, want ok", err)
	}
	if n := len(p2vOrderLines(t, h, res.ID)); n != 500 {
		t.Errorf("stored lines = %d, want 500", n)
	}

	d := loadDoc(t, h, fixture.Order, res.ID)
	count := h.AuditCount(t)
	in := h.In(d)
	in.Lines = append(in.Lines, p2vLines(h, 1)...)
	_, err = h.Do(t, t.Context(), h.Priya, fixture.Order, res.ID, doc.ActionSave, in)
	de = wantDocErr(t, err, doc.KindInvalid, "")
	if !p2vHasFieldErr(de.Fields, want) {
		t.Errorf("save with 501 lines: Fields = %+v, want it to hold %+v", de.Fields, want)
	}
	if n := len(p2vOrderLines(t, h, res.ID)); n != 500 {
		t.Errorf("stored lines = %d after a refused save, want 500", n)
	}
	if n := h.AuditCount(t); n != count {
		t.Errorf("AuditCount = %d after a refused save, want %d", n, count)
	}
}

// DOC-045
func TestDoc_Lines_DuplicateItemAllowed(t *testing.T) {
	h := fixturetest.New(t)
	cement := p2vID(h.M.Item["CEMENT-OPC"])
	res, err := h.Do(t, t.Context(), h.Priya, fixture.Order, 0, doc.ActionSave, p2vOrderIn(h,
		map[string]string{"item_id": cement, "qty": "10", "rate": "395.0000"},
		map[string]string{"item_id": cement, "qty": "5", "rate": "380.0000"},
	))
	if err != nil {
		t.Fatalf("create with two CEMENT-OPC lines: %v, want ok", err)
	}
	rows := p2vOrderLines(t, h, res.ID)
	if len(rows) != 2 {
		t.Fatalf("stored lines = %+v, want 2", rows)
	}
	if rows[0].ID == rows[1].ID {
		t.Errorf("both lines have id %d, want two separate lines", rows[0].ID)
	}
	for i, w := range []struct{ qty, rate int64 }{{10000, 3950000}, {5000, 3800000}} {
		r := rows[i]
		if r.ItemID != h.M.Item["CEMENT-OPC"] || r.Qty != w.qty || r.Rate != w.rate {
			t.Errorf("line %d = %+v, want CEMENT-OPC qty %d rate %d", i+1, r, w.qty, w.rate)
		}
	}
}

// DOC-051
func TestDoc_Schema_NoAllowOnSubmit(t *testing.T) {
	all := doc.All()
	seen := false
	for _, dt := range all {
		if dt.Name == fixture.Order {
			seen = true
		}
		for _, f := range dt.Fields {
			if f.EditableAfterSubmit && !f.RollUp && !f.System {
				t.Errorf("%s.%s is EditableAfterSubmit but neither RollUp nor System", dt.Name, f.Name)
			}
		}
	}
	if !seen {
		t.Errorf("doc.All() (%d DocTypes) does not include %s; the walk covers nothing", len(all), fixture.Order)
	}
}

// DOC-106
func TestDoc_Link_MasterWarning(t *testing.T) {
	h := fixturetest.New(t)
	const partyWarn = "Bharat Tools asks for advance payment on new orders."
	const itemWarn = "Hex Bolt M12x60 is low in stock."
	const itemWarnLine2 = "Line 2: Hex Bolt M12x60 is low in stock."
	fixture.SetWarning(t, h.DB, "fixture_party", "BHARAT", partyWarn)
	fixture.SetWarning(t, h.DB, "fixture_item", "BOLT-M12", itemWarn)

	in := p2vOrderIn(h, map[string]string{"item_id": p2vID(h.M.Item["MS-ANGLE-50"]), "qty": "1.000"})
	in.Values["supplier_id"] = p2vID(h.M.Party["BHARAT"])
	created, err := h.Do(t, t.Context(), h.Priya, fixture.Order, 0, doc.ActionSave, in)
	if err != nil {
		t.Fatalf("create with BHARAT: %v, want ok", err)
	}
	if diff := diffOf([]string{partyWarn}, created.Warnings); diff != "" {
		t.Errorf("create Warnings (want, got):\n%s", diff)
	}

	edit := h.In(loadDoc(t, h, fixture.Order, created.ID))
	edit.Lines = append(edit.Lines, map[string]string{"item_id": p2vID(h.M.Item["BOLT-M12"]), "qty": "2.000"})
	saved, err := h.Do(t, t.Context(), h.Priya, fixture.Order, created.ID, doc.ActionSave, edit)
	if err != nil {
		t.Fatalf("save with BOLT-M12 on line 2: %v, want ok", err)
	}
	for _, w := range []string{partyWarn, itemWarnLine2} {
		if !slices.Contains(saved.Warnings, w) {
			t.Errorf("save Warnings = %q, want it to contain %q", saved.Warnings, w)
		}
	}

	submitted, err := h.Do(t, t.Context(), h.Priya, fixture.Order, created.ID, doc.ActionSubmit, doc.ActionInput{Version: saved.Version})
	if err != nil {
		t.Fatalf("submit: %v, want ok", err)
	}
	for _, w := range []string{partyWarn, itemWarnLine2} {
		if !slices.Contains(submitted.Warnings, w) {
			t.Errorf("submit Warnings = %q, want it to contain %q", submitted.Warnings, w)
		}
	}
	if st := loadDoc(t, h, fixture.Order, created.ID).State; st == doc.StateDraft {
		t.Errorf("State after submit = %q, want it submitted", st)
	}

	plain := h.NewOrder(t, h.Priya, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "1.000"})
	if plain.Warnings != nil {
		t.Errorf("create without warnings: Warnings = %q, want nil", plain.Warnings)
	}
	ps := h.MustDo(t, t.Context(), h.Priya, fixture.Order, plain.ID, doc.ActionSubmit, doc.ActionInput{Version: plain.Version})
	if ps.Warnings != nil {
		t.Errorf("submit without warnings: Warnings = %q, want nil", ps.Warnings)
	}

	trail := h.Audit(t, fixture.Order, created.ID)
	if len(trail) == 0 {
		t.Fatalf("no audit entries for order %d", created.ID)
	}
	for _, e := range trail {
		s := fmt.Sprintf("%+v", e)
		for _, w := range []string{partyWarn, itemWarn} {
			if strings.Contains(s, w) {
				t.Errorf("audit entry %q holds the warning %q: %s", e.Action, w, s)
			}
		}
	}
}

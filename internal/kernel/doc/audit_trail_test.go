package doc_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/types"
)

// DOC-080
func TestAudit_Create(t *testing.T) {
	h := fixturetest.New(t)
	res := h.NewOrder(t, h.Priya, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "1200.000"})
	d := loadDoc(t, h, fixture.Order, res.ID)
	if len(d.Lines) != 1 {
		t.Fatalf("len(Lines) = %d, want 1", len(d.Lines))
	}

	entries := h.Audit(t, fixture.Order, res.ID)
	if len(entries) != 1 {
		t.Fatalf("audit entries = %d, want exactly 1: %+v", len(entries), entries)
	}
	if n := h.AuditCount(t); n != 1 {
		t.Errorf("AuditCount = %d, want 1", n)
	}
	e := entries[0]
	if e.DocType != fixture.Order || e.DocID != res.ID || e.Action != "create" || e.From != "" || e.To != "draft" {
		t.Errorf("entry = {DocType %q, DocID %d, Action %q, From %q, To %q}, want {%q, %d, create, \"\", draft}",
			e.DocType, e.DocID, e.Action, e.From, e.To, fixture.Order, res.ID)
	}
	if e.By != types.UserID(4) {
		t.Errorf("By = %d, want 4 (priya)", e.By)
	}
	if want := time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC); !e.At.Equal(want) {
		t.Errorf("At = %v, want %v", e.At, want)
	}
	if e.CausedBy != nil {
		t.Errorf("CausedBy = %+v, want nil", e.CausedBy)
	}

	byField := map[string]audit.Change{}
	var adds []audit.Change
	for _, c := range e.Changes {
		switch {
		case c.Op == audit.OpAdd:
			adds = append(adds, c)
		case c.LineNo == 0:
			if _, dup := byField[c.Field]; dup {
				t.Errorf("header field %q changed twice: %+v", c.Field, e.Changes)
			}
			byField[c.Field] = c
		}
	}
	for _, want := range []audit.Change{
		{Field: "supplier_id", Before: "null", After: fmt.Sprintf(`{"id":%d,"code":"SHREE"}`, h.M.Party["SHREE"]), Op: audit.OpSet},
		{Field: "order_date", Before: "null", After: `"2026-10-24"`, Op: audit.OpSet},
	} {
		got, ok := byField[want.Field]
		if !ok {
			t.Errorf("no set change for %q in %+v", want.Field, e.Changes)
			continue
		}
		if diff := diffOf(want, got); diff != "" {
			t.Errorf("change %q (want, got):\n%s", want.Field, diff)
		}
	}
	// Every non-empty header value is set; empty ones and system columns are not.
	if c, ok := byField["project_id"]; !ok || c.Op != audit.OpSet || c.Before != "null" ||
		c.After != fmt.Sprintf(`{"id":%d,"code":"PRJ-METRO"}`, h.M.Project["PRJ-METRO"]) {
		t.Errorf("project_id change = %+v (present %v), want a set from null to PRJ-METRO's {id, code}", c, ok)
	}
	for _, f := range []string{"remarks", "credit_days", "supplier_ref", "created_by", "created_at", "updated_by", "updated_at", "version", "state", "number"} {
		if c, ok := byField[f]; ok {
			t.Errorf("unexpected change for %q: %+v", f, c)
		}
	}

	if len(adds) != 1 {
		t.Fatalf("add changes = %d, want 1: %+v", len(adds), e.Changes)
	}
	add := adds[0]
	if add.LineNo != 1 || add.LineID != d.Lines[0].ID {
		t.Errorf("add change LineNo, LineID = %d, %d, want 1, %d", add.LineNo, add.LineID, d.Lines[0].ID)
	}
	if !strings.Contains(add.After, `"qty":1200000`) {
		t.Errorf("add change After = %s, want it to contain \"qty\":1200000 (raw value)", add.After)
	}
}

// DOC-081
func TestAudit_SaveChangedFieldsOnly(t *testing.T) {
	h := fixturetest.New(t)
	res := h.NewOrderWith(t, h.Priya, map[string]string{"remarks": "", "credit_days": "30"},
		fixturetest.OL{Item: "MS-ANGLE-50", Qty: "1.000"})
	in := h.In(loadDoc(t, h, fixture.Order, res.ID))
	in.Values["remarks"] = "Deliver to Pune site"
	in.Values["credit_days"] = "30"
	saveDo(t, h, h.Meena, fixture.Order, res.ID, in)

	entries := h.Audit(t, fixture.Order, res.ID)
	if len(entries) != 2 {
		t.Fatalf("audit entries = %d, want 2 (create, save): %+v", len(entries), entries)
	}
	e := entries[1]
	if e.Action != "save" {
		t.Errorf("Action = %q, want \"save\"", e.Action)
	}
	if e.By != types.UserID(2) {
		t.Errorf("By = %d, want 2 (meena)", e.By)
	}
	want := []audit.Change{{Field: "remarks", Before: `""`, After: `"Deliver to Pune site"`, Op: audit.OpSet}}
	if diff := diffOf(want, e.Changes); diff != "" {
		t.Errorf("Changes (want, got):\n%s", diff)
	}
}

// DOC-082
func TestAudit_LineDiff(t *testing.T) {
	h := fixturetest.New(t)
	res := h.NewOrder(t, h.Priya,
		fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10.000"},
		fixturetest.OL{Item: "WIRE-18G", Qty: "5.000"},
	)
	d := loadDoc(t, h, fixture.Order, res.ID)
	x, y := d.Lines[0].ID, d.Lines[1].ID

	in := h.In(d)
	in.Lines[0]["qty"] = "12.000"
	in.Lines = []map[string]string{in.Lines[0], {"item_id": fmt.Sprint(h.M.Item["BOLT-M12"]), "qty": "3.000"}}
	saveDo(t, h, h.Priya, fixture.Order, res.ID, in)

	after := loadDoc(t, h, fixture.Order, res.ID)
	if len(after.Lines) != 2 || after.Lines[0].ID != x {
		t.Fatalf("lines after save = %+v, want x kept as line 1 and one new line", after.Lines)
	}
	nw := after.Lines[1].ID
	if got := after.Lines[1].Values["item_id"]; got != any(h.M.Item["BOLT-M12"]) {
		t.Errorf("line 2 item_id = %v, want BOLT-M12", got)
	}

	e := saveEntry(t, h, fixture.Order, res.ID)
	if len(e.Changes) != 3 {
		t.Fatalf("save changes = %+v, want 3 (qty set, remove, add)", e.Changes)
	}
	var set, remove, add []audit.Change
	for _, c := range e.Changes {
		switch c.Op {
		case audit.OpSet:
			set = append(set, c)
		case audit.OpRemove:
			remove = append(remove, c)
		case audit.OpAdd:
			add = append(add, c)
		}
	}
	wantSet := []audit.Change{{Field: "qty", LineNo: 1, LineID: x, Before: "10000", After: "12000", Op: audit.OpSet}}
	if diff := diffOf(wantSet, set); diff != "" {
		t.Errorf("set changes (want, got):\n%s", diff)
	}
	if len(remove) != 1 || remove[0].LineNo != 2 || remove[0].LineID != y || !strings.Contains(remove[0].Before, `"qty":5000`) {
		t.Errorf("remove changes = %+v, want one {LineNo 2, LineID %d, Op remove} whose Before contains \"qty\":5000", remove, y)
	}
	if len(add) != 1 || add[0].LineNo != 2 || add[0].LineID != nw ||
		!strings.Contains(add[0].After, `"code":"BOLT-M12"`) || !strings.Contains(add[0].After, `"qty":3000`) {
		t.Errorf("add changes = %+v, want one {LineNo 2, LineID %d, Op add} whose After contains \"code\":\"BOLT-M12\" and \"qty\":3000", add, nw)
	}
}

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

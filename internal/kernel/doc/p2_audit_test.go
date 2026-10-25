package doc_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// p2aHistory runs doc.History for p and fails the test on an error.
func p2aHistory(t *testing.T, h *fixturetest.H, p auth.Principal, dt string, id int64) []audit.Entry {
	t.Helper()
	entries, err := doc.History(t.Context(), h.DB, h.Auth, p, dt, id)
	if err != nil {
		t.Fatalf("doc.History(%s, %d) for user %d: %v", dt, id, p.ID, err)
	}
	return entries
}

// p2aOnly returns the one entry with action, failing unless there is exactly one.
func p2aOnly(t *testing.T, entries []audit.Entry, action string) audit.Entry {
	t.Helper()
	var found []audit.Entry
	for _, e := range entries {
		if e.Action == action {
			found = append(found, e)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d %q entries, want exactly 1: %+v", len(found), action, entries)
	}
	return found[0]
}

// p2aSameEntries fails unless got holds exactly want's entries (any order).
func p2aSameEntries(t *testing.T, got, want []audit.Entry) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d:\ngot  %+v\nwant %+v", len(got), len(want), got, want)
	}
	used := make([]bool, len(got))
	for _, w := range want {
		ok := false
		for i, g := range got {
			if !used[i] && reflect.DeepEqual(g, w) {
				used[i], ok = true, true
				break
			}
		}
		if !ok {
			t.Errorf("entry missing from result: %+v\ngot %+v", w, got)
		}
	}
}

// DOC-087
func TestAudit_RawValues(t *testing.T) {
	h := fixturetest.New(t)
	shree, bharat := h.M.Party["SHREE"], h.M.Party["BHARAT"]
	res := h.NewOrderWith(t, h.Priya, map[string]string{
		"supplier_id": fmt.Sprint(shree), "order_date": "2026-10-01", "amount": "125000.50",
	}, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "1.000"})
	in := h.In(loadDoc(t, h, fixture.Order, res.ID))
	in.Values["amount"] = "130000.00"
	in.Values["order_date"] = "2026-10-24"
	in.Values["supplier_id"] = fmt.Sprint(bharat)
	saveDo(t, h, h.Priya, fixture.Order, res.ID, in)

	entries := p2aHistory(t, h, h.Priya, fixture.Order, res.ID)
	save := p2aOnly(t, entries, "save")
	want := map[string]audit.Change{
		"amount":      {Field: "amount", Before: "12500050", After: "13000000", Op: audit.OpSet},
		"order_date":  {Field: "order_date", Before: `"2026-10-01"`, After: `"2026-10-24"`, Op: audit.OpSet},
		"supplier_id": {Field: "supplier_id", Before: fmt.Sprintf(`{"id":%d,"code":"SHREE"}`, shree), After: fmt.Sprintf(`{"id":%d,"code":"BHARAT"}`, bharat), Op: audit.OpSet},
	}
	if len(save.Changes) != len(want) {
		t.Errorf("save changes = %+v, want exactly %d (amount, order_date, supplier_id)", save.Changes, len(want))
	}
	for _, c := range save.Changes {
		w, ok := want[c.Field]
		if !ok {
			t.Errorf("unexpected change %+v", c)
			continue
		}
		if diff := diffOf(w, c); diff != "" {
			t.Errorf("change %q (want, got):\n%s", c.Field, diff)
		}
		delete(want, c.Field)
	}
	for f := range want {
		t.Errorf("no change for %q in %+v", f, save.Changes)
	}

	// Renaming the supplier's code later leaves the stored history as it was.
	h.Exec(t, `UPDATE fixture_party SET code = 'SHREE-STEEL' WHERE id = ?`, shree)
	after := p2aHistory(t, h, h.Priya, fixture.Order, res.ID)
	fixturetest.Eq(t, after, entries)
	if c := p2aOnly(t, after, "save"); !strings.Contains(fmt.Sprint(c.Changes), `"code":"SHREE"}`) {
		t.Errorf("save changes after rename = %+v, want the code SHREE as of the save", c.Changes)
	}
}

// DOC-088
func TestAudit_ExcludesSystemFields(t *testing.T) {
	h := fixturetest.New(t)
	res := h.NewOrderWith(t, h.Priya, map[string]string{"remarks": ""},
		fixturetest.OL{Item: "MS-ANGLE-50", Qty: "1.000"})
	before := loadDoc(t, h, fixture.Order, res.ID)
	h.Clock.Set("2026-10-24T09:15:00Z")
	in := h.In(before)
	in.Values["remarks"] = "Deliver to Pune site"
	saveDo(t, h, h.Meena, fixture.Order, res.ID, in)

	// The save did change the system columns.
	d := loadDoc(t, h, fixture.Order, res.ID)
	if d.Version != before.Version+1 || d.UpdatedBy != types.UserID(2) || d.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("setup: version %d->%d, updated_by %d, updated_at %v->%v; want version +1, updated_by 2 (meena), updated_at changed",
			before.Version, d.Version, d.UpdatedBy, before.UpdatedAt, d.UpdatedAt)
	}

	save := p2aOnly(t, p2aHistory(t, h, h.Priya, fixture.Order, res.ID), "save")
	var fields []string
	for _, c := range save.Changes {
		fields = append(fields, c.Field)
	}
	fixturetest.Eq(t, fields, []string{"remarks"})
}

// DOC-090
func TestAudit_EscapedAndPermissioned(t *testing.T) {
	const script = "<script>alert(1)</script>"
	h := fixturetest.New(t)
	res := h.NewOrderWith(t, h.Priya, map[string]string{"remarks": ""},
		fixturetest.OL{Item: "MS-ANGLE-50", Qty: "1.000"})
	in := h.In(loadDoc(t, h, fixture.Order, res.ID))
	in.Values["remarks"] = script
	saved := saveDo(t, h, h.Priya, fixture.Order, res.ID, in)
	// Amount 0 needs no approval: submit approves and assigns a number, so the
	// history also holds the number marker (a plain string, not JSON).
	h.MustDo(t, t.Context(), h.Priya, fixture.Order, res.ID, doc.ActionSubmit, doc.ActionInput{Version: saved.Version})

	entries := p2aHistory(t, h, h.Priya, fixture.Order, res.ID)
	p2aSameEntries(t, entries, h.Audit(t, fixture.Order, res.ID))
	save := p2aOnly(t, entries, "save")
	if len(save.Changes) != 1 || save.Changes[0].Field != "remarks" {
		t.Fatalf("save changes = %+v, want one remarks change", save.Changes)
	}
	raw := save.Changes[0].After
	var got string
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("remarks After %q is not a JSON string: %v", raw, err)
	}
	if got != script {
		t.Errorf("remarks After decodes to %q, want %q stored raw", got, script)
	}
	if strings.Contains(raw, "&lt;") || strings.Contains(raw, "&amp;") {
		t.Errorf("remarks After %q is HTML-escaped; values are stored raw", raw)
	}

	// A user denied read gets AUTH's 403, the same as for the document.
	h.Auth.Deny = func(p auth.Principal, _ string, r auth.Right) bool { return p.ID == h.Suresh.ID && r == "read" }
	denied, err := doc.History(t.Context(), h.DB, h.Auth, h.Suresh, fixture.Order, res.ID)
	if !errors.Is(err, auth.ErrForbidden) {
		t.Errorf("History for a user denied read: err = %v, want auth.ErrForbidden", err)
	}
	if len(denied) != 0 {
		t.Errorf("History for a user denied read returned %d entries, want none", len(denied))
	}
	h.Auth.Deny = nil

	// The read check is on the record: a record hidden from the user is not found.
	h.Auth.Hide = func(p auth.Principal, _ string, id int64) bool { return p.ID == h.Suresh.ID && id == res.ID }
	hidden, err := doc.History(t.Context(), h.DB, h.Auth, h.Suresh, fixture.Order, res.ID)
	if !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("History of a record hidden from the user: err = %v, want auth.ErrNotFound", err)
	}
	if len(hidden) != 0 {
		t.Errorf("History of a hidden record returned %d entries, want none", len(hidden))
	}
}

// DOC-091
func TestAudit_DeletedDraftViewable(t *testing.T) {
	h := fixturetest.New(t)
	res := h.NewOrder(t, h.Priya,
		fixturetest.OL{Item: "MS-ANGLE-50", Qty: "1200.000", Rate: "62.5000"},
		fixturetest.OL{Item: "WIRE-18G", Qty: "50.000", Rate: "96.0000"},
	)
	in := h.In(loadDoc(t, h, fixture.Order, res.ID))
	in.Values["remarks"] = "Deliver to Pune site"
	saveDo(t, h, h.Priya, fixture.Order, res.ID, in)
	d := loadDoc(t, h, fixture.Order, res.ID)
	h.Clock.Set("2026-10-24T09:15:00Z")
	h.MustDo(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionDelete, doc.ActionInput{Version: d.Version})
	stored := h.Audit(t, fixture.Order, d.ID)
	if len(stored) != 3 {
		t.Fatalf("setup: %d audit rows, want 3 (create, save, delete): %+v", len(stored), stored)
	}

	// A second draft deleted later, for the Deleted list's order.
	other := h.NewOrder(t, h.Priya, fixturetest.OL{Item: "BOLT-M12", Qty: "3.000"})
	h.Clock.Set("2026-10-24T10:00:00Z")
	h.MustDo(t, t.Context(), h.Priya, fixture.Order, other.ID, doc.ActionDelete, doc.ActionInput{Version: other.Version})

	var deleted []audit.Entry
	if err := h.DB.Read(t.Context(), func(tx *db.Tx) (err error) {
		deleted, err = audit.Deleted(t.Context(), tx)
		return err
	}); err != nil {
		t.Fatalf("audit.Deleted: %v", err)
	}
	if len(deleted) != 2 {
		t.Fatalf("audit.Deleted = %d entries, want 2: %+v", len(deleted), deleted)
	}
	for _, e := range deleted {
		if e.Action != "delete" || e.DocType != fixture.Order {
			t.Errorf("Deleted entry %+v, want Action delete on %s", e, fixture.Order)
		}
	}
	if deleted[0].DocID != other.ID || deleted[1].DocID != d.ID {
		t.Errorf("Deleted DocIDs = [%d %d], want newest first [%d %d]", deleted[0].DocID, deleted[1].DocID, other.ID, d.ID)
	}
	fixturetest.Eq(t, deleted[1], stored[2])

	entries, err := doc.DeletedHistory(t.Context(), h.DB, h.Auth, h.Admin, fixture.Order, d.ID)
	if err != nil {
		t.Fatalf("DeletedHistory for admin: %v", err)
	}
	p2aSameEntries(t, entries, stored)
	del := p2aOnly(t, entries, "delete")
	var snap *audit.Change
	for i := range del.Changes {
		if del.Changes[i].Field == "snapshot" {
			snap = &del.Changes[i]
		}
	}
	if snap == nil {
		t.Fatalf("delete entry has no snapshot change: %+v", del.Changes)
	}
	var got struct {
		ID    int64
		Lines []struct{ ID int64 }
	}
	if err := json.Unmarshal([]byte(snap.Before), &got); err != nil {
		t.Fatalf("snapshot Before is not JSON of the Doc: %v\n%s", err, snap.Before)
	}
	if got.ID != d.ID || len(got.Lines) != 2 {
		t.Errorf("snapshot = {ID %d, %d lines}, want {ID %d, 2 lines}", got.ID, len(got.Lines), d.ID)
	}

	denied, err := doc.DeletedHistory(t.Context(), h.DB, h.Auth, h.Priya, fixture.Order, d.ID)
	if !errors.Is(err, auth.ErrForbidden) {
		t.Errorf("DeletedHistory for priya: err = %v, want auth.ErrForbidden", err)
	}
	if len(denied) != 0 {
		t.Errorf("DeletedHistory for priya returned %d entries, want none", len(denied))
	}
}

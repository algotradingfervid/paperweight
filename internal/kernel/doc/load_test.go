package doc_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// normInfo treats empty id slices as nil, so Info values compare by content.
func normInfo(i doctype.Info) doctype.Info {
	if len(i.ProjectIDs) == 0 {
		i.ProjectIDs = nil
	}
	if len(i.SiteIDs) == 0 {
		i.SiteIDs = nil
	}
	return i
}

// engineInfo calls h.Engine.Info in a read transaction.
func engineInfo(t *testing.T, h *fixturetest.H, dt string, id int64) (doctype.Info, error) {
	t.Helper()
	var info doctype.Info
	err := h.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		info, err = h.Engine.Info(t.Context(), tx, dt, id)
		return err
	})
	return info, err
}

// engineLoad calls h.Engine.Load in a read transaction.
func engineLoad(t *testing.T, h *fixturetest.H, dt string, id int64) (doc.Record, error) {
	t.Helper()
	var rec doc.Record
	err := h.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		rec, err = h.Engine.Load(t.Context(), tx, dt, id)
		return err
	})
	return rec, err
}

// employeeID returns the testkit_employee id of code.
func employeeID(t *testing.T, h *fixturetest.H, code string) int64 {
	t.Helper()
	for _, e := range h.F.Employees {
		if e.Code == code {
			if e.ID == 0 {
				t.Fatalf("employee %s has no seeded id", code)
			}
			return e.ID
		}
	}
	t.Fatalf("no fixture employee %s", code)
	return 0
}

// DOC-097
func TestDoc_Info_FromRegistryFields(t *testing.T) {
	h := fixturetest.New(t)
	var _ doctype.InfoSource = h.Engine

	po := h.RefPO(t)
	want := doctype.Info{
		DocType: fixture.Order, DocLabel: "Purchase Order", ID: po.ID,
		State: doc.StateDraft, Number: "", Display: fmt.Sprintf("Draft %d", po.ID),
		Party: "SHREE Shree Steel Traders", CreatedBy: types.UserID(4),
		ProjectIDs: []int64{h.M.Project["PRJ-METRO"]},
	}
	got, err := engineInfo(t, h, fixture.Order, po.ID)
	if err != nil {
		t.Fatalf("Engine.Info(fixture.Order, %d): %v", po.ID, err)
	}
	if diff := diffOf(normInfo(want), normInfo(got)); diff != "" {
		t.Errorf("order Info (want, got):\n%s", diff)
	}
	if diff := diffOf(normInfo(want), normInfo(h.Info(t, fixture.Order, po.ID))); diff != "" {
		t.Errorf("h.Info for the order (want, got):\n%s", diff)
	}

	claim := h.NewClaim(t, h.Deepak, "EMP-007", fixturetest.CL{Type: "TRAVEL", Amount: "1250.00"})
	wantClaim := doctype.Info{
		DocType: fixture.Claim, DocLabel: "Expense Claim", ID: claim.ID,
		State: doc.StateDraft, Number: "", Display: fmt.Sprintf("Draft %d", claim.ID),
		Party: "", CreatedBy: types.UserID(7), EmployeeID: employeeID(t, h, "EMP-007"),
	}
	gotClaim, err := engineInfo(t, h, fixture.Claim, claim.ID)
	if err != nil {
		t.Fatalf("Engine.Info(fixture.Claim, %d): %v", claim.ID, err)
	}
	if diff := diffOf(normInfo(wantClaim), normInfo(gotClaim)); diff != "" {
		t.Errorf("claim Info (want, got):\n%s", diff)
	}

	for _, c := range []struct {
		name string
		dt   string
		id   int64
	}{
		{"missing order id", fixture.Order, po.ID + 1000},
		{"master DocType", fixture.Party, h.M.Party["SHREE"]},
		{"unknown DocType", "fixture.Nope", 1},
	} {
		if _, err := engineInfo(t, h, c.dt, c.id); !errors.Is(err, doctype.ErrNotFound) {
			t.Errorf("%s: Info(%s, %d) err = %v, want doctype.ErrNotFound", c.name, c.dt, c.id, err)
		}
	}
}

// DOC-102
func TestDoc_Load_PlainValues(t *testing.T) {
	h := fixturetest.New(t)
	po := h.RefPO(t)

	rec, err := engineLoad(t, h, fixture.Order, po.ID)
	if err != nil {
		t.Fatalf("Engine.Load(fixture.Order, %d): %v", po.ID, err)
	}
	info, err := engineInfo(t, h, fixture.Order, po.ID)
	if err != nil {
		t.Fatalf("Engine.Info(fixture.Order, %d): %v", po.ID, err)
	}
	if diff := diffOf(info, rec.Info); diff != "" {
		t.Errorf("Record.Info differs from Engine.Info (info, record):\n%s", diff)
	}
	if rec.Info.ID != po.ID || rec.Info.DocType != fixture.Order {
		t.Errorf("Record.Info = {%s %d}, want {%s %d}", rec.Info.DocType, rec.Info.ID, fixture.Order, po.ID)
	}
	if rec.Version != 1 {
		t.Errorf("Version = %d, want 1", rec.Version)
	}

	wantValues := map[string]string{
		"supplier_id": fmt.Sprint(h.M.Party["SHREE"]),
		"order_date":  "2026-10-24",
		"amount":      "94164.00",
		"remarks":     "",
		"priority":    "normal",
	}
	for k, want := range wantValues {
		got, ok := rec.Values[k]
		if !ok {
			t.Errorf("Values[%q] missing, want %q", k, want)
			continue
		}
		if got != want {
			t.Errorf("Values[%q] = %q, want %q", k, got, want)
		}
	}

	if len(rec.Lines) != 2 {
		t.Fatalf("len(Lines) = %d, want 2", len(rec.Lines))
	}
	d := loadDoc(t, h, fixture.Order, po.ID)
	if len(d.Lines) != 2 {
		t.Fatalf("LoadDoc: len(Lines) = %d, want 2", len(d.Lines))
	}
	wantLine0 := map[string]string{
		"id":           fmt.Sprint(d.Lines[0].ID),
		"item_id":      fmt.Sprint(h.M.Item["MS-ANGLE-50"]),
		"qty":          "1200.000",
		"rate":         "62.5000",
		"uom":          "Nos",
		"description":  "",
		"received_qty": "0.000",
	}
	if diff := diffOf(wantLine0, rec.Lines[0]); diff != "" {
		t.Errorf("Lines[0] (want, got):\n%s", diff)
	}
	if got, want := rec.Lines[1]["item_id"], fmt.Sprint(h.M.Item["WIRE-18G"]); got != want {
		t.Errorf("Lines[1][item_id] = %q, want %q (WIRE-18G)", got, want)
	}
	if got, want := rec.Lines[1]["id"], fmt.Sprint(d.Lines[1].ID); got != want {
		t.Errorf("Lines[1][id] = %q, want %q", got, want)
	}

	for _, c := range []struct {
		name string
		dt   string
		id   int64
	}{
		{"missing order id", fixture.Order, po.ID + 1000},
		{"master DocType", fixture.Party, h.M.Party["SHREE"]},
	} {
		_, err := engineLoad(t, h, c.dt, c.id)
		if !errors.Is(err, doctype.ErrNotFound) {
			t.Errorf("%s: Load(%s, %d) err = %v, want doctype.ErrNotFound", c.name, c.dt, c.id, err)
		}
		var he doctype.HTTPError
		if !errors.As(err, &he) {
			t.Errorf("%s: Load err %v (%T) is not a doctype.HTTPError", c.name, err, err)
		} else if he.Status() != 404 {
			t.Errorf("%s: Status() = %d, want 404", c.name, he.Status())
		}
	}
}

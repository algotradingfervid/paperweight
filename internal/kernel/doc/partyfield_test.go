package doc_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// M0-47 (CD-319 (6)): PartyField is the header field Info.Party is read
// from. For every registered document DocType, and for a few built here,
// partyOf on a document holding one master Link alone shows a party exactly
// when that Link is PartyField's.
func TestDoc_PartyField_AgreesWithPartyOf(t *testing.T) {
	h := fixturetest.New(t)
	party := func(name, column string, required bool) doc.Field {
		return doc.Field{Name: name, Column: column, Label: name, Type: doc.Link, Link: fixture.Party, Required: required}
	}
	built := []struct {
		dt   doc.DocType
		want string // PartyField's name; "" for none
	}{
		{doc.DocType{Name: "m47.TwoParties", Fields: []doc.Field{party("bill_to_id", "bill_to_id", false), party("supplier_id", "supplier_id", true)}}, "supplier_id"},
		{doc.DocType{Name: "m47.Employee", EmployeeField: "employee_id", Fields: []doc.Field{party("supplier_id", "supplier_id", true), party("employee_id", "employee_id", false)}}, "employee_id"},
		{doc.DocType{Name: "m47.FirstRequired", Fields: []doc.Field{party("a_id", "a_id", true), party("b_id", "b_id", true)}}, "a_id"},
		{doc.DocType{Name: "m47.NoneRequired", Fields: []doc.Field{party("a_id", "a_id", false)}}, ""},
		{doc.DocType{Name: "m47.NotMaster", Fields: []doc.Field{{Name: "order_id", Column: "order_id", Label: "Order", Type: doc.Link, Link: fixture.Order, Required: true}}}, ""},
		{doc.DocType{Name: "m47.OnLine", Fields: []doc.Field{{Name: "item_id", Column: "item_id", Label: "Item", Type: doc.Link, Link: fixture.Item, Required: true, OnLine: true}}}, ""},
	}
	var dts []*doc.DocType
	for i := range built {
		dt := &built[i].dt
		f, ok := doc.PartyField(dt)
		if got := map[bool]string{true: f.Name}[ok]; got != built[i].want {
			t.Errorf("PartyField(%s) = %q, %v; want %q", dt.Name, f.Name, ok, built[i].want)
		}
		dts = append(dts, dt)
	}
	order, _ := doc.Lookup(fixture.Order)
	if f, ok := doc.PartyField(order); !ok || f.Name != "supplier_id" {
		t.Errorf("PartyField(fixture.Order) = %q, %v; want supplier_id", f.Name, ok)
	}
	for _, dt := range doc.All() {
		if !dt.IsMaster() {
			dts = append(dts, dt)
		}
	}
	checked := 0
	err := h.DB.Read(t.Context(), func(tx *db.Tx) error {
		for _, dt := range dts {
			pf, hasParty := doc.PartyField(dt)
			for _, f := range dt.HeaderFields() {
				target, ok := doc.Lookup(f.Link)
				if f.Type != doc.Link || !ok || !target.IsMaster() {
					continue
				}
				id, display := m47AnyRow(t, tx, target)
				if id == 0 {
					continue
				}
				got, err := doc.PartyOf(t.Context(), tx, dt, doc.Doc{Header: doc.Values{f.Name: id}})
				if err != nil {
					return err
				}
				want := ""
				if hasParty && pf.Name == f.Name {
					want = display
				}
				if got != want {
					t.Errorf("%s with only %s set: partyOf = %q, want %q (PartyField %q, %v)", dt.Name, f.Name, got, want, pf.Name, hasParty)
				}
				checked++
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if checked < len(built) {
		t.Errorf("checked %d master Links, want at least %d", checked, len(built))
	}
}

// m47AnyRow is the first of the ids 1 to 200 that master dt finds, with its
// "{Code} {Name}" display (the name alone without a code); 0 when none is,
// or when this database has no table for dt (a master another test
// registers).
func m47AnyRow(t *testing.T, tx *db.Tx, dt *doc.DocType) (int64, string) {
	t.Helper()
	for id := int64(1); id <= 200; id++ {
		mi, found, err := dt.Master.Lookup(t.Context(), tx, id)
		if err != nil {
			return 0, ""
		}
		if found {
			if mi.Code == "" {
				return id, mi.Name
			}
			return id, mi.Code + " " + mi.Name
		}
	}
	return 0, ""
}

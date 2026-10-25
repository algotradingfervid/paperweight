package mstkit_test

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// LookupFunc implements doctype.MasterLookup.
var _ doctype.MasterLookup = mstkit.LookupFunc(nil)

// thingLookup is a comparable MasterLookup, so the test can check
// RegisterMaster stores the one it was given.
type thingLookup struct{}

func (*thingLookup) Lookup(context.Context, *db.Tx, int64) (doctype.MasterInfo, bool, error) {
	return doctype.MasterInfo{}, false, nil
}

var (
	thingMaster   = &thingLookup{}
	registerThing sync.Once // doctype.Register panics on a duplicate (-count > 1)
)

func thingFields() []doctype.Field {
	return []doctype.Field{
		{Name: "code", Label: "Code", Column: "code", Type: doctype.Text, Required: true, ListPriority: 4, MaxLen: 20},
		{Name: "name", Label: "Name", Column: "name", Type: doctype.Text, Required: true, ListPriority: 1},
		{Name: "kind", Label: "Kind", Column: "kind", Type: doctype.Select, ListPriority: 5, Options: []string{"Goods", "Service"}, Step: "details"},
		{Name: "disabled", Label: "Disabled", Column: "disabled", Type: doctype.Check, ListPriority: 3},
	}
}

func TestKit_RegisterMasterShape(t *testing.T) {
	registerThing.Do(func() {
		mstkit.RegisterMaster(doctype.DocType{
			Name:        "kit.Thing",
			Label:       "Thing",
			PluralLabel: "Things",
			Table:       "kit_thing",
			Submittable: true, // RegisterMaster forces false (M1)
			Fields:      thingFields(),
		}, thingMaster)
	})
	dt, ok := doctype.Lookup("kit.Thing")
	if !ok {
		t.Fatal(`doctype.Lookup("kit.Thing") not found after RegisterMaster`)
	}
	if dt.Submittable {
		t.Error("Submittable = true; want false")
	}
	if dt.Hooks != nil {
		t.Errorf("Hooks = %v; want nil", dt.Hooks)
	}
	if dt.Master == nil {
		t.Fatal("Master = nil; want the lookup")
	}
	if got, ok := dt.Master.(*thingLookup); !ok || got != thingMaster {
		t.Errorf("Master = %#v; want the lookup passed to RegisterMaster", dt.Master)
	}
	if want := mstkit.DefaultRights("kit.Thing"); !reflect.DeepEqual(dt.DefaultRights, want) {
		t.Errorf("DefaultRights = %v; want DefaultRights(kit.Thing) = %v", dt.DefaultRights, want)
	}
	// CD-42 field extras are kept as given.
	if want := thingFields(); !reflect.DeepEqual(dt.Fields, want) {
		t.Errorf("Fields = %+v; want %+v", dt.Fields, want)
	}

	if got := mstkit.DefaultRights("Party")["Purchase User"]["read"]; got != "all" {
		t.Errorf(`DefaultRights("Party")["Purchase User"]["read"] = %q; want "all"`, got)
	}
	if got := mstkit.DefaultRights("Expense Type")["Employee"]["read"]; got != "all" {
		t.Errorf(`DefaultRights("Expense Type")["Employee"]["read"] = %q; want "all"`, got)
	}
	checkDefaultRightsTable(t)
}

// checkDefaultRightsTable checks the whole M1 table (CD-10): Masters
// Manager rights, read for the roles whose documents link to the master,
// every scope "all", and no Administrator rows.
func checkDefaultRightsTable(t *testing.T) {
	t.Helper()
	all := func(rights ...string) map[string]string {
		m := make(map[string]string, len(rights))
		for _, r := range rights {
			m[r] = "all"
		}
		return m
	}
	full := []string{"read", "create", "write", "delete", "export", "import"}
	readers := func(mm []string, roles ...string) map[string]map[string]string {
		m := map[string]map[string]string{"Masters Manager": all(mm...)}
		for _, r := range roles {
			m[r] = all("read")
		}
		return m
	}
	want := map[string]map[string]map[string]string{
		"Company":       readers([]string{"read", "write", "export"}, "Purchase User", "Stores", "Employee"),
		"Branch":        readers([]string{"read", "create", "write", "delete", "export"}, "Purchase User", "Stores", "Employee"),
		"Party":         readers(full, "Purchase User", "Stores"),
		"Address":       readers(full, "Purchase User", "Stores"),
		"Contact":       readers(full, "Purchase User", "Stores"),
		"Payment Terms": readers([]string{"read", "create", "write", "delete", "export"}, "Purchase User", "Stores"),
		"Item":          readers(full, "Purchase User", "Stores"),
		"UoM":           readers(full, "Purchase User", "Stores"),
		"Tax Code":      readers(full, "Purchase User", "Stores", "Employee"),
		"Project":       readers(full, "Purchase User", "Stores", "Employee"),
		"Site":          readers(full, "Purchase User", "Stores", "Employee"),
		"Employee":      readers(full, "Employee"),
		"Expense Type":  readers(full, "Employee"),
	}
	for dt, w := range want {
		got := mstkit.DefaultRights(dt)
		if _, ok := got["Administrator"]; ok {
			t.Errorf("DefaultRights(%q) has an Administrator key", dt)
		}
		if !reflect.DeepEqual(got, w) {
			t.Errorf("DefaultRights(%q) = %v; want %v", dt, got, w)
		}
	}
}

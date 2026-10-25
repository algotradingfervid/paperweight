package mstkit_test

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// listed is a test master over kit_list_thing, declared the way the masters
// did before MST 1d: DefaultSort {name, id} and a disabled field that isn't
// Filterable.
var listed = mstkit.Kind{DocType: "mstkit.ListThing", Label: "List Thing", Table: "kit_list_thing", HasCode: true}

var registerListed sync.Once // doctype.Register panics on a duplicate (-count > 1)

// kitListThingDDL has the master columns, ids never reused (CD-156).
const kitListThingDDL = `CREATE TABLE kit_list_thing (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	code TEXT NOT NULL UNIQUE CHECK (code = upper(code)),
	name TEXT NOT NULL,
	disabled INTEGER NOT NULL DEFAULT 0 CHECK (disabled IN (0,1)),
	version INTEGER NOT NULL DEFAULT 1,
	created_at TEXT NOT NULL,
	created_by INTEGER,
	updated_at TEXT NOT NULL,
	updated_by INTEGER
)`

// TestKit_RegisterMasterListsThroughKernel has no case ID (MST 1d): a kit
// master's default list and picker run through the real list service and
// return the enabled rows sorted by name, then id.
func TestKit_RegisterMasterListsThroughKernel(t *testing.T) {
	registerListed.Do(func() {
		mstkit.RegisterMaster(doctype.DocType{
			Name:        listed.DocType,
			Label:       listed.Label,
			PluralLabel: "List Things",
			Table:       listed.Table,
			DefaultSort: []doctype.SortKey{{Field: "name"}, {Field: "id"}},
			Fields: []doctype.Field{
				{Name: "code", Label: "Code", Column: "code", Type: doctype.Text, Required: true, ListPriority: 2},
				{Name: "name", Label: "Name", Column: "name", Type: doctype.Text, Required: true, ListPriority: 1},
				{Name: "disabled", Label: "Disabled", Column: "disabled", Type: doctype.Check, ListPriority: 3},
			},
		}, mstkit.StandardLookup(listed))
	})
	env := msttest.New(t)
	ctx := context.Background()
	ts := types.TimestampOf(env.Clock.Now()).String()
	if err := env.DB.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, kitListThingDDL)
		return err
	}); err != nil {
		t.Fatalf("create kit_list_thing: %v", err)
	}
	insert := func(code, name string, disabled bool) int64 {
		t.Helper()
		var id int64
		if err := env.DB.Write(ctx, func(tx *db.Tx) error {
			res, err := tx.SQL().ExecContext(ctx,
				`INSERT INTO kit_list_thing (code, name, disabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
				code, name, disabled, ts, ts)
			if err != nil {
				return err
			}
			id, err = res.LastInsertId()
			return err
		}); err != nil {
			t.Fatalf("insert %s: %v", code, err)
		}
		return id
	}
	// Out of name order; two rows share a name, so id breaks the tie.
	tata := insert("T-TATA", "Tata Projects", false)
	ambika1 := insert("A-AMBIKA", "Ambika Traders", false)
	_ = insert("K-KUMAR", "Kumar Hardware", true)
	ambika2 := insert("A-AMBIKA-2", "Ambika Traders", false)
	bharat := insert("B-BHARAT", "Bharat Electricals", false)
	want := []int64{ambika1, ambika2, bharat, tata}

	dt, ok := doctype.Lookup(listed.DocType)
	if !ok {
		t.Fatalf("doctype.Lookup(%q) not found after RegisterMaster", listed.DocType)
	}
	svc := &list.Service{DB: env.DB, Auth: env.Kit.Auth}
	ids := func(rows []list.Row) []int64 {
		out := make([]int64, len(rows))
		for i, r := range rows {
			out[i] = r.ID
		}
		return out
	}

	picked, err := svc.Picker(ctx, env.Kavya, dt, "", 25)
	if err != nil {
		t.Errorf("Picker(%s) as Kavya = %v; want nil", listed.DocType, err)
	} else {
		if got := ids(picked.Rows); !reflect.DeepEqual(got, want) {
			t.Errorf("Picker(%s) ids = %v; want %v (enabled, name asc then id asc)", listed.DocType, got, want)
		}
		if picked.Total != len(want) {
			t.Errorf("Picker(%s) Total = %d; want %d", listed.DocType, picked.Total, len(want))
		}
	}

	res, err := svc.List(ctx, env.Kavya, dt, list.Query{Filters: list.DefaultFilters(dt), Page: 1})
	if err != nil {
		t.Errorf("List(%s, default filters and sort) as Kavya = %v; want nil", listed.DocType, err)
	} else {
		if got := ids(res.Rows); !reflect.DeepEqual(got, want) {
			t.Errorf("List(%s) ids = %v; want %v (enabled, name asc then id asc)", listed.DocType, got, want)
		}
		if res.Total != len(want) {
			t.Errorf("List(%s) Total = %d; want %d", listed.DocType, res.Total, len(want))
		}
	}
}

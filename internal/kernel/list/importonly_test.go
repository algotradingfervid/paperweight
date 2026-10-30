package list_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/kernel/list/listtest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// Test-only masters whose tables have no column for their ImportOnly fields.
// On ioBare the only field named disabled is ImportOnly, so it has no default
// filter to apply.
const (
	ioThing = "listio.Thing"
	ioBare  = "listio.Bare"
)

func init() {
	doctype.Register(doctype.DocType{
		Name: ioThing, Label: "Thing", PluralLabel: "Things", Table: "numt_io_thing",
		Fields: []doctype.Field{
			{Name: "code", Label: "Code", Type: doctype.Text, Sortable: true, ListPriority: 4},
			{Name: "email", Label: "Email", Type: doctype.Text, Importable: true, ImportOnly: true},
			{Name: "name", Label: "Name", Type: doctype.Text, Sortable: true, ListPriority: 1},
			{Name: "disabled", Label: "Disabled", Type: doctype.Check, Filterable: true},
		},
		SearchFields: []string{"code", "name"},
		DefaultSort:  []doctype.SortKey{{Field: "name"}},
		Master:       listtest.TableLookup("numt_io_thing"),
	})
	doctype.Register(doctype.DocType{
		Name: ioBare, Label: "Bare", PluralLabel: "Bares", Table: "numt_io_bare",
		Fields: []doctype.Field{
			{Name: "code", Label: "Code", Type: doctype.Text, Sortable: true},
			{Name: "disabled", Label: "Disabled", Type: doctype.Check, Importable: true, ImportOnly: true},
		},
		Master: listtest.TableLookup("numt_io_bare"),
	})
}

func TestList_SkipsImportOnly(t *testing.T) {
	ctx := context.Background()
	d := listtest.Setup(t)
	var alpha, zeta int64
	err := d.Write(ctx, func(tx *db.Tx) error {
		for _, q := range []string{
			"CREATE TABLE numt_io_thing (id INTEGER PRIMARY KEY, code TEXT, name TEXT, disabled INTEGER NOT NULL DEFAULT 0)",
			"CREATE TABLE numt_io_bare (id INTEGER PRIMARY KEY, code TEXT)",
			// Rows out of name order, so a missing sort shows.
			"INSERT INTO numt_io_thing (code, name) VALUES ('Z1', 'Zeta')",
			"INSERT INTO numt_io_thing (code, name) VALUES ('A1', 'Alpha')",
			"INSERT INTO numt_io_thing (code, name, disabled) VALUES ('M1', 'Mid', 1)",
			"INSERT INTO numt_io_bare (code) VALUES ('B1')",
		} {
			if _, err := tx.SQL().ExecContext(ctx, q); err != nil {
				return err
			}
		}
		if err := tx.SQL().QueryRowContext(ctx, "SELECT id FROM numt_io_thing WHERE code = 'Z1'").Scan(&zeta); err != nil {
			return err
		}
		return tx.SQL().QueryRowContext(ctx, "SELECT id FROM numt_io_thing WHERE code = 'A1'").Scan(&alpha)
	})
	if err != nil {
		t.Fatal(err)
	}
	dt, ok := doctype.Lookup(ioThing)
	if !ok {
		t.Fatalf("%s not registered", ioThing)
	}
	bare, ok := doctype.Lookup(ioBare)
	if !ok {
		t.Fatalf("%s not registered", ioBare)
	}
	names := func(fs []doctype.Field) []string {
		out := make([]string, len(fs))
		for i, f := range fs {
			out[i] = f.Name
		}
		return out
	}
	if got, want := names(list.HeaderFields(dt)), []string{"code", "name", "disabled"}; !slices.Equal(got, want) {
		t.Errorf("HeaderFields = %q, want %q", got, want)
	}
	if got, want := names(list.Columns(dt)), []string{"name", "code"}; !slices.Equal(got, want) {
		t.Errorf("Columns = %q, want %q", got, want)
	}

	// The default filter and a sort, through the service and its ScanRow.
	s := &list.Service{DB: d, Auth: listtest.NewAuth()}
	p := testkit.Principal(t, "ravi@vardhan.test")
	q := list.Query{Filters: list.DefaultFilters(dt), Sort: []doctype.SortKey{{Field: "name"}}, Page: 1}
	if len(q.Filters) != 1 {
		t.Fatalf("DefaultFilters = %+v, want the disabled filter", q.Filters)
	}
	res, err := s.List(ctx, p, dt, q)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	orderIs(t, "default filter, sort by name", rowIDs(res.Rows), alpha, zeta)
	if res.Total != 2 {
		t.Errorf("Total = %d, want 2", res.Total)
	}
	for _, r := range res.Rows {
		if v, ok := r.Values["email"]; ok {
			t.Errorf("row %d: Values[email] = %v, want no such key", r.ID, v)
		}
	}
	if n, err := s.Count(ctx, p, dt, q); err != nil || n != 2 {
		t.Errorf("Count = %d, %v; want 2, nil", n, err)
	}
	res, err = s.Picker(ctx, p, dt, "zeta", 10)
	if err != nil {
		t.Fatalf("Picker: %v", err)
	}
	orderIs(t, "picker search", rowIDs(res.Rows), zeta)

	// No built query names the field.
	all, _, err := list.BuildAll(dt, q, noScope)
	if err != nil {
		t.Fatal(err)
	}
	page, _, err := list.Build(dt, q, noScope)
	if err != nil {
		t.Fatal(err)
	}
	count, _, err := list.BuildCount(dt, q, noScope)
	if err != nil {
		t.Fatal(err)
	}
	for what, sql := range map[string]string{"BuildAll": all, "Build": page, "BuildCount": count} {
		if strings.Contains(sql, "email") {
			t.Errorf("%s names the ImportOnly field: %s", what, sql)
		}
	}

	// A query can't reach it either.
	_, _, err = list.Build(dt, query(filter("email", list.OpEq, "a@b.test")), noScope)
	wantErr(t, "filter by email", err, "You can't filter Things by 'email'.")
	_, _, err = list.Build(dt, sorted(asc("email")), noScope)
	wantErr(t, "sort by email", err, "You can't sort Things by 'email'.")

	// An ImportOnly field named disabled is no default filter.
	if got := list.DefaultFilters(bare); got != nil {
		t.Errorf("DefaultFilters(bare) = %+v, want nil", got)
	}
	res, err = s.Picker(ctx, p, bare, "", 10)
	if err != nil {
		t.Fatalf("Picker(bare): %v", err)
	}
	if len(res.Rows) != 1 {
		t.Errorf("Picker(bare) = %d rows, want 1", len(res.Rows))
	}
}

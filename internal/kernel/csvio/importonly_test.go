package csvio

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/kernel/list/listtest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// namePerson is a csvio-only master with two ImportOnly fields between its
// stored ones; personTable has no column for either.
const namePerson = "csviotest.Person"

const personTable = "CREATE TABLE numt_csvio_person (id INTEGER PRIMARY KEY, code TEXT, name TEXT, remarks TEXT, " +
	"disabled INTEGER NOT NULL DEFAULT 0, created_at TEXT, updated_at TEXT, created_by INTEGER, updated_by INTEGER)"

func init() {
	doctype.Register(doctype.DocType{
		Name: namePerson, Label: "Person", PluralLabel: "People", Table: "numt_csvio_person",
		Fields: []doctype.Field{
			{Name: "code", Label: "Code", Type: doctype.Text, Required: true, Importable: true},
			{Name: "email", Label: "Email", Type: doctype.Text, Required: true, Importable: true, ImportOnly: true},
			{Name: "name", Label: "Name", Type: doctype.Text, Sortable: true, Importable: true},
			{Name: "mobile", Label: "Mobile", Type: doctype.Text, Importable: true, ImportOnly: true},
			{Name: "remarks", Label: "Remarks", Type: doctype.Text},
			{Name: "disabled", Label: "Disabled", Type: doctype.Check, Filterable: true},
		},
		DefaultSort: []doctype.SortKey{{Field: "name"}},
		Master:      listtest.TableLookup("numt_csvio_person"),
	})
	RegisterImporter(namePerson, personRow)
}

func personRow(ctx context.Context, tx *db.Tx, p auth.Principal, row map[string]string) (int64, []doctype.FieldError, error) {
	record(ctx, namePerson, tx, row)
	id, err := insert(ctx, tx, p, "numt_csvio_person", []string{"code", "name"}, row["code"], row["name"])
	return id, nil, err
}

func person(t *testing.T) *doctype.DocType {
	t.Helper()
	dt, ok := doctype.Lookup(namePerson)
	if !ok {
		t.Fatalf("%s not registered", namePerson)
	}
	return dt
}

func (e *importEnv) personTable(t *testing.T, stmts ...string) {
	t.Helper()
	ctx := context.Background()
	err := e.db.Write(ctx, func(tx *db.Tx) error {
		for _, q := range append([]string{personTable}, stmts...) {
			if _, err := tx.SQL().ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestExport_SkipsImportOnly(t *testing.T) {
	e := newEnv(t)
	e.personTable(t,
		"INSERT INTO numt_csvio_person (code, name) VALUES ('P2', 'Zeta')",
		"INSERT INTO numt_csvio_person (code, name, remarks) VALUES ('P1', 'Alpha', 'first')",
		"INSERT INTO numt_csvio_person (code, name, disabled) VALUES ('P3', 'Mid', 1)",
	)
	dt := person(t)
	q := list.Query{Filters: list.DefaultFilters(dt), Sort: []doctype.SortKey{{Field: "name"}}}

	var buf bytes.Buffer
	n, err := NewExporter(e.db, e.auth).Export(context.Background(), &buf, e.p, dt, q)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if n != 2 {
		t.Errorf("Export returned %d rows, want 2", n)
	}
	want := [][]string{
		{"Code", "Name", "Remarks", "Disabled"},
		{"P1", "Alpha", "first", "No"},
		{"P2", "Zeta", "", "No"},
	}
	if got := readExport(t, buf.Bytes()); !reflect.DeepEqual(got, want) {
		t.Errorf("export = %q, want %q", got, want)
	}
}

func TestImport_ImportOnlyReachesRowFunc(t *testing.T) {
	dt := person(t)

	// Template: Importable fields in declaration order, ImportOnly included.
	if got, want := string(Template(dt)), string(utf8BOM)+"Code,Email,Name,Mobile\r\n"; got != want {
		t.Errorf("Template = %q, want %q", got, want)
	}
	if got, want := RequiredColumns(dt), []string{"Code", "Email"}; !reflect.DeepEqual(got, want) {
		t.Errorf("RequiredColumns = %q, want %q", got, want)
	}

	// MapHeader: by label or name, in file order; a stored field that isn't
	// Importable keeps its warning.
	cols, warns, err := MapHeader(dt, []string{"mobile", "Code", "EMAIL", "Name", "Remarks", "Colour"})
	if err != nil {
		t.Fatalf("MapHeader: %v", err)
	}
	if got, want := colNames(cols), []string{"0:mobile", "1:code", "2:email", "3:name"}; !reflect.DeepEqual(got, want) {
		t.Errorf("cols = %q, want %q", got, want)
	}
	wantWarns := []string{
		"Column 'Remarks' can't be imported and will be ignored.",
		"Column 'Colour' isn't a field of Person and will be ignored.",
	}
	if !reflect.DeepEqual(warns, wantWarns) {
		t.Errorf("warns = %q, want %q", warns, wantWarns)
	}
	_, _, err = MapHeader(dt, []string{"Code", "Name"})
	assertFileError(t, err, "Column 'Email' is missing. Add it to the first row.")
	_, _, err = MapHeader(dt, []string{"Code", "Email", "email"})
	assertFileError(t, err, "Column 'Email' appears twice. Keep one.")

	// The RowFunc sees each cell, keyed by field Name.
	e := newEnv(t)
	e.personTable(t)
	file := strings.TrimPrefix(string(Template(dt)), string(utf8BOM)) +
		"P1,accounts@shreesteel.test,Asha Rao,9876543210\r\n"
	rep, err := e.im.Import(context.Background(), e.p, dt, strings.NewReader(file))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if rep.Imported != 1 || len(rep.Errors) != 0 {
		t.Fatalf("Import: %d imported, errors %q; want 1, none", rep.Imported, errorTexts(rep))
	}
	if len(calls) != 1 {
		t.Fatalf("RowFunc called %d times, want 1", len(calls))
	}
	want := map[string]string{"code": "P1", "email": "accounts@shreesteel.test", "name": "Asha Rao", "mobile": "9876543210"}
	if got := calls[0].Row; !reflect.DeepEqual(got, want) {
		t.Errorf("row = %q, want %q", got, want)
	}
}

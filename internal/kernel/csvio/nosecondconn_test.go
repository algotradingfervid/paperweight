package csvio

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list/listtest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// CD-213: a non-Administrator on the real auth.Service with the pool cut to one
// connection, so the create check inside the import's write must use its tx.

// nameNsImport is Importable, as the real service requires for the import
// right; the listtest masters aren't, since their tests use a fake.
const (
	nameNsImport  = "csviotest.NoSecondConn"
	nsImportTable = "numt_csvio_nosecondconn"
	nsImportFile  = "Code,Name\r\nA1,First\r\nA2,Second\r\n"
)

func init() {
	doctype.Register(doctype.DocType{
		Name: nameNsImport, Label: "Widget", PluralLabel: "Widgets", Table: nsImportTable, Importable: true,
		Fields: []doctype.Field{
			{Name: "code", Label: "Code", Type: doctype.Text, Required: true, Importable: true},
			{Name: "name", Label: "Name", Type: doctype.Text, Importable: true},
		},
		Master: listtest.TableLookup(nsImportTable),
	})
	RegisterImporter(nameNsImport, func(ctx context.Context, tx *db.Tx, p auth.Principal, row map[string]string) (int64, []doctype.FieldError, error) {
		id, err := insert(ctx, tx, p, nsImportTable, []string{"code", "name"}, row["code"], row["name"])
		return id, nil, err
	})
}

// nsImporter returns an env whose Importer is on the real auth.Service and a
// non-Administrator holding exactly rights on nameNsImport.
func nsImporter(t *testing.T, rights map[auth.Right]auth.Scope) (*importEnv, auth.Principal) {
	t.Helper()
	ctx := t.Context()
	e := newEnv(t)
	if err := e.db.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, "CREATE TABLE "+nsImportTable+
			" (id INTEGER PRIMARY KEY, code TEXT, name TEXT, created_at TEXT, updated_at TEXT, created_by INTEGER, updated_by INTEGER)")
		return err
	}); err != nil {
		t.Fatalf("create table: %v", err)
	}
	a := auth.New(auth.Deps{DB: e.db, Clock: testkit.Clock(), Directory: authfake.NewDirectory(), Audit: auditfake.New()})
	id, err := a.CreateFirstAdmin(ctx, "ns-admin@vardhan.test", "No Second Conn Admin", "Correct-Horse-42")
	if err != nil {
		t.Fatalf("CreateFirstAdmin: %v", err)
	}
	admin := auth.Principal{ID: id, Roles: []string{auth.RoleAdministrator}}
	const role = "No Second Conn Importer"
	if err := a.CreateRole(ctx, admin, role); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	if err := a.SetRights(ctx, admin, role, nameNsImport, rights); err != nil {
		t.Fatalf("SetRights: %v", err)
	}
	e.im = NewImporter(e.db, a, testkit.Clock(), e.audit)
	p := testkit.Principal(t, "kavya@vardhan.test")
	p.Roles = []string{role}
	return e, p
}

// nsRun runs Import or Preview on the pool of one.
func nsRun(t *testing.T, e *importEnv, p auth.Principal, run func(*Importer, context.Context, auth.Principal, *doctype.DocType, io.Reader) (Report, error)) Report {
	t.Helper()
	dt, ok := doctype.Lookup(nameNsImport)
	if !ok {
		t.Fatalf("%s is not registered", nameNsImport)
	}
	var rep Report
	err := testkit.NoSecondConn(t, e.db, func(ctx context.Context) (err error) {
		rep, err = run(e.im, ctx, p, dt, strings.NewReader(nsImportFile))
		return err
	})
	if err != nil {
		t.Fatalf("run: %v, want a report", err)
	}
	return rep
}

var nsAllowed = map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightCreate: auth.ScopeAll, auth.RightImport: auth.ScopeAll}

// Import checks create inside its write, then writes every row.
func TestNoSecondConn_CsvioImport(t *testing.T) {
	e, p := nsImporter(t, nsAllowed)
	rep := nsRun(t, e, p, (*Importer).Import)
	if rep.Rows != 2 || rep.Imported != 2 || len(rep.Errors) != 0 {
		t.Errorf("Report = %+v, want 2 rows imported, no errors", rep)
	}
	if n := e.count(t, nsImportTable); n != 2 {
		t.Errorf("rows = %d, want 2", n)
	}
}

// Preview checks create inside its write and rolls everything back.
func TestNoSecondConn_CsvioPreview(t *testing.T) {
	e, p := nsImporter(t, nsAllowed)
	rep := nsRun(t, e, p, (*Importer).Preview)
	if rep.Rows != 2 || rep.Ready != 2 || rep.Imported != 0 || len(rep.Errors) != 0 {
		t.Errorf("Report = %+v, want 2 rows ready, none imported, no errors", rep)
	}
	if n := e.count(t, nsImportTable); n != 0 {
		t.Errorf("rows = %d, want 0", n)
	}
}

// With import but no create, every row reports the refusal and nothing is written.
func TestNoSecondConn_CsvioImportNoCreate(t *testing.T) {
	e, p := nsImporter(t, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightImport: auth.ScopeAll})
	rep := nsRun(t, e, p, (*Importer).Import)
	want := []string{
		"Row 2: you don't have permission to create Widget.",
		"Row 3: you don't have permission to create Widget.",
	}
	if got := errorTexts(rep); !reflect.DeepEqual(got, want) {
		t.Errorf("errors = %q, want %q", got, want)
	}
	if rep.Imported != 0 {
		t.Errorf("Imported = %d, want 0", rep.Imported)
	}
	if n := e.count(t, nsImportTable); n != 0 {
		t.Errorf("rows = %d, want 0", n)
	}
}

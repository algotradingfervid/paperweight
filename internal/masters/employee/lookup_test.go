package employee_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// msgEmpLeft is MST-120's text for EMP-007 (plan Task 19 uses EMP-007).
const msgEmpLeft = "Employee EMP-007 has left. Pick an active employee." // MST-120

// visitDocType is a test-only document with one header link to Employee, so
// the real DOC engine validates it through Employee's Master lookup. Its
// tables are created by visitTables after msttest.New.
const visitDocType = "mst14.Visit"

func init() {
	doc.Register(doc.DocType{
		Name: visitDocType, Label: "Site Visit", PluralLabel: "Site Visits",
		Table: "mst14_visit", LinesTable: "mst14_visit_line",
		OwnerField: "created_by",
		DefaultRights: map[string]map[string]string{
			"Masters Manager": {"read": "all", "create": "all", "write": "all"},
		},
		Fields: []doc.Field{
			{Name: "employee_id", Label: "Employee", Type: doc.Link, Link: employee.DocType, Required: true, ListPriority: 1},
			{Name: "note", Label: "Note", Type: doc.Text, OnLine: true},
		},
		Hooks: nopHooks{},
	})
}

type nopHooks struct{}

func (nopHooks) Validate(context.Context, *db.Tx, int64) []doctype.FieldError { return nil }
func (nopHooks) BeforeSubmit(context.Context, *db.Tx, int64) error            { return nil }
func (nopHooks) ApplyEffects(context.Context, *db.Tx, int64) error            { return nil }
func (nopHooks) ReverseEffects(context.Context, *db.Tx, int64) error          { return nil }
func (nopHooks) Amount(context.Context, *db.Tx, int64) (types.Money, error)   { return 0, nil }
func (nopHooks) Date(context.Context, *db.Tx, int64) (types.Date, error)      { return types.Date{}, nil }

func (w *world) visitTables(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if err := w.env.DB.Write(ctx, func(tx *db.Tx) error {
		for _, stmt := range []string{
			`CREATE TABLE mst14_visit (
				id                INTEGER PRIMARY KEY AUTOINCREMENT,
				number            TEXT UNIQUE,
				state             TEXT NOT NULL DEFAULT 'draft',
				state_reason      TEXT NOT NULL DEFAULT '',
				amended_from_id   INTEGER REFERENCES mst14_visit (id),
				amendment_root_id INTEGER,
				amendment_no      INTEGER NOT NULL DEFAULT 0,
				version           INTEGER NOT NULL DEFAULT 1,
				created_at        TEXT NOT NULL,
				created_by        INTEGER NOT NULL,
				updated_at        TEXT NOT NULL,
				updated_by        INTEGER NOT NULL,
				employee_id       INTEGER REFERENCES employee (id)
			)`,
			`CREATE TABLE mst14_visit_line (
				id        INTEGER PRIMARY KEY AUTOINCREMENT,
				parent_id INTEGER NOT NULL REFERENCES mst14_visit (id) ON DELETE CASCADE,
				line_no   INTEGER NOT NULL,
				note      TEXT NOT NULL DEFAULT ''
			)`,
		} {
			if _, err := tx.SQL().ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("create visit tables: %v", err)
	}
}

func (w *world) lookup(t *testing.T, id int64) (doctype.MasterInfo, bool) {
	t.Helper()
	dt, ok := doctype.Lookup(employee.DocType)
	if !ok || dt.Master == nil {
		t.Fatalf("doctype.Lookup(%q) = (%v, %v); want a master", employee.DocType, dt, ok)
	}
	var info doctype.MasterInfo
	var found bool
	if err := w.env.DB.Read(context.Background(), func(tx *db.Tx) error {
		var err error
		info, found, err = dt.Master.Lookup(context.Background(), tx, id)
		return err
	}); err != nil {
		t.Fatalf("Lookup(%d): %v", id, err)
	}
	return info, found
}

// TestEmployee_LookupLeftIsBlocked: the Master lookup DOC uses reports a
// Left employee as Blocked with MST-120's text and Disabled false, because
// DOC checks Disabled first and would show DOC-034's wording instead.
func TestEmployee_LookupLeftIsBlocked(t *testing.T) { // MST-120
	w := newWorld(t)
	w.create(t, w.input("EMP-007", "Deepak Nair"))
	id := w.ids["EMP-007"]

	if info, found := w.lookup(t, id); !found || info != (doctype.MasterInfo{Code: "EMP-007", Name: "Deepak Nair"}) {
		t.Errorf("Lookup(active EMP-007) = (%+v, %v); want ({Code EMP-007, Name Deepak Nair}, true)", info, found)
	}

	if err := w.update(t, "EMP-007", func(in *employee.Input) {
		in.Status, in.Relieving = employee.Left, date(2026, time.October, 24)
	}); err != nil {
		t.Fatalf("mark EMP-007 Left: %v", err)
	}
	want := doctype.MasterInfo{Code: "EMP-007", Name: "Deepak Nair", Blocked: msgEmpLeft}
	if info, found := w.lookup(t, id); !found || info != want {
		t.Errorf("Lookup(Left EMP-007) = (%+v, %v); want (%+v, true): Disabled false, Blocked MST-120", info, found, want)
	}
}

// TestEmployee_LeftBlocksDraftSave: a draft created while EMP-007 was active
// fails on save once EMP-007 is Left, with MST-120's text and not DOC-034's
// disabled text; a new draft is refused the same way.
func TestEmployee_LeftBlocksDraftSave(t *testing.T) { // MST-120
	w := newWorld(t)
	w.visitTables(t)
	ctx := context.Background()
	w.create(t, w.input("EMP-007", "Deepak Nair"))
	vals := map[string]string{"employee_id": strconv.FormatInt(w.ids["EMP-007"], 10)}
	lines := []map[string]string{{"note": "Site walk"}}

	res, err := w.env.Kit.Engine.Do(ctx, w.env.Kavya, visitDocType, 0, doc.ActionSave, doc.ActionInput{Values: vals, Lines: lines})
	if err != nil {
		t.Fatalf("create draft with active EMP-007: %v", err)
	}

	if err := w.update(t, "EMP-007", func(in *employee.Input) {
		in.Status, in.Relieving = employee.Left, date(2026, time.October, 24)
	}); err != nil {
		t.Fatalf("mark EMP-007 Left: %v", err)
	}

	_, err = w.env.Kit.Engine.Do(ctx, w.env.Kavya, visitDocType, res.ID, doc.ActionSave, doc.ActionInput{Version: res.Version, Values: vals, Lines: lines})
	wantDocFieldError(t, "save draft", err, doctype.FieldError{Field: "employee_id", Message: msgEmpLeft})

	_, err = w.env.Kit.Engine.Do(ctx, w.env.Kavya, visitDocType, 0, doc.ActionSave, doc.ActionInput{Values: vals, Lines: lines})
	wantDocFieldError(t, "new draft", err, doctype.FieldError{Field: "employee_id", Message: msgEmpLeft})
}

func wantDocFieldError(t *testing.T, what string, err error, want doctype.FieldError) {
	t.Helper()
	var de *doc.Error
	if !errors.As(err, &de) {
		t.Fatalf("%s: err = %v (%T); want a *doc.Error with %+v", what, err, err, want)
	}
	if de.Kind != doc.KindInvalid {
		t.Errorf("%s: Kind = %q; want %q (err %v)", what, de.Kind, doc.KindInvalid, err)
	}
	if len(de.Fields) != 1 || de.Fields[0] != want {
		t.Errorf("%s: Fields = %+v; want [%+v]", what, de.Fields, want)
	}
}

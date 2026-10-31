package expense_test

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/masters/allmasters"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// engineRef and directoryRef break the construction cycles as app.New does.
type engineRef struct{ doc.Engine }

type directoryRef struct{ auth.Directory }

// wiringWorld is a seeded database with the real auth, naming, approval,
// files, engine and masters, built as app.New builds them, and the
// expense.Deps for it (not wired).
type wiringWorld struct {
	db     *db.DB
	engine doc.Engine
	deps   expense.Deps
	deepak auth.Principal
	emp007 int64
}

func newWiringWorld(t *testing.T) *wiringWorld {
	t.Helper()
	ctx := context.Background()
	d := testkit.NewDB(t)
	fx := testkit.Seed(t, d)
	clk := testkit.Clock()
	eng, dir := &engineRef{}, &directoryRef{}
	a := auth.New(auth.Deps{DB: d, Clock: clk, Directory: dir, Audit: audit.Default, Refs: eng, Info: eng})
	namer := naming.New(d, clk, a, audit.Default)
	store, err := files.New(files.Config{DataDir: testkit.DataDir(t), DB: d, Auth: a, Info: eng, Clock: clk, Audit: audit.Default})
	if err != nil {
		t.Fatalf("files.New: %v", err)
	}
	apr := approval.New(approval.Deps{DB: d, Clock: clk, Auth: a, Info: eng, Directory: dir, Audit: audit.Default})
	eng.Engine = doc.NewEngine(doc.Deps{DB: d, Clock: clk, Auth: a, Naming: namer, Approval: apr, Files: store, Audit: audit.Default})
	ms := allmasters.New(mstkit.Env{DB: d, Auth: a, Engine: eng.Engine, Auditor: audit.Default, Clock: clk})
	dir.Directory = ms.Directory
	if err := a.SeedDefaults(ctx, auth.SeedOptions{LinkedMasterRead: true}); err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}
	w := &wiringWorld{
		db: d, engine: eng.Engine, deepak: testkit.Principal(t, "deepak@vardhan.test"),
		deps: expense.Deps{Clock: clk, Auth: a, Directory: ms.Directory, Files: store, ExpenseTypes: ms.ExpenseType},
	}
	for _, e := range fx.Employees {
		if e.Code == "EMP-007" {
			w.emp007 = e.ID
		}
	}
	if w.emp007 == 0 {
		t.Fatal("no fixture employee EMP-007")
	}
	return w
}

func (w *wiringWorld) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := w.db.Read(context.Background(), func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM `+table).Scan(&n)
	}); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// save creates a draft as Deepak for EMP-007 and turns a panic that escapes
// the engine into a test failure.
func (w *wiringWorld) save(t *testing.T, dtName string, values map[string]string) (res doc.Result, err error) {
	t.Helper()
	values["employee_id"] = strconv.FormatInt(w.emp007, 10)
	values["purpose"] = "Pune Metro site visit"
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("save %s panicked: %v", dtName, r)
		}
	}()
	return w.engine.Do(context.Background(), w.deepak, dtName, 0, doc.ActionSave, doc.ActionInput{Values: values})
}

// CD-284: every hook reaches its Deps through the package's Wiring. A save on
// a database whose Wiring was never wired is a plain internal error (no user
// message, no panic) and writes nothing; once wired, the same save works.
func TestExp_HooksNeedWiring(t *testing.T) {
	for _, tc := range []struct {
		dt, table, lines string
		values           func() map[string]string
	}{
		{expense.ClaimDocType, "expense_claim", "expense_claim_line", func() map[string]string {
			return map[string]string{"claim_date": "2026-10-24"}
		}},
		{expense.AdvanceDocType, "employee_advance", "employee_advance_line", func() map[string]string {
			return map[string]string{"advance_date": "2026-10-24"}
		}},
	} {
		t.Run(tc.dt, func(t *testing.T) {
			unwired := newWiringWorld(t)
			audits := unwired.count(t, "audit_log")
			_, err := unwired.save(t, tc.dt, tc.values())
			if err == nil {
				t.Fatal("save on an unwired database: err = nil, want a plain internal error")
			}
			var he doctype.HTTPError
			if errors.As(err, &he) {
				t.Errorf("save on an unwired database: err = %v (%T, status %d), want a plain internal error, not a user message", err, err, he.Status())
			}
			if !strings.Contains(err.Error(), "nothing wired") {
				t.Errorf("save on an unwired database: err = %q, want the Wiring's Get error (\"modkit: nothing wired for this database\")", err)
			}
			if n, l, a := unwired.count(t, tc.table), unwired.count(t, tc.lines), unwired.count(t, "audit_log"); n != 0 || l != 0 || a != audits {
				t.Errorf("after the refused save: %d %s rows, %d line rows, audit rows %d → %d; want nothing written", n, tc.table, l, audits, a)
			}

			wired := newWiringWorld(t)
			expense.Wire(wired.db, wired.deps)
			res, err := wired.save(t, tc.dt, tc.values())
			if err != nil {
				t.Fatalf("save on a wired database: %v, want it saved", err)
			}
			if res.State != doc.StateDraft || res.ID == 0 {
				t.Errorf("save on a wired database: Result = %+v, want a draft with an id", res)
			}
			var state string
			var emp int64
			if err := wired.db.Read(context.Background(), func(tx *db.Tx) error {
				return tx.SQL().QueryRowContext(context.Background(),
					fmt.Sprintf(`SELECT state, employee_id FROM %s WHERE id = ?`, tc.table), res.ID).Scan(&state, &emp)
			}); err != nil {
				t.Fatalf("read saved %s %d: %v", tc.table, res.ID, err)
			}
			if state != string(doctype.StateDraft) || emp != wired.emp007 {
				t.Errorf("stored %s %d: state %q, employee_id %d; want draft, %d", tc.table, res.ID, state, emp, wired.emp007)
			}
		})
	}
}

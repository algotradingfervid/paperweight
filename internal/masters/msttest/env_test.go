package msttest_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// thing is a master DocType registered by this test (CD-65 prefix), so
// msttest.New must grant on it like on any registered master.
var thing = mstkit.Kind{DocType: "msttest.Thing", Label: "Thing", Table: "msttest_thing", HasCode: true}

var registerThing sync.Once // doctype.Register panics on a duplicate (-count > 1)

func registerThingMaster() {
	registerThing.Do(func() {
		mstkit.RegisterMaster(doctype.DocType{
			Name:        thing.DocType,
			Label:       thing.Label,
			PluralLabel: "Things",
			Table:       thing.Table,
			Importable:  true,
			Fields: []doctype.Field{
				{Name: "code", Label: "Code", Column: "code", Type: doctype.Text, Required: true, Importable: true},
				{Name: "name", Label: "Name", Column: "name", Type: doctype.Text, Required: true, Importable: true},
				{Name: "disabled", Label: "Disabled", Column: "disabled", Type: doctype.Check},
			},
		}, mstkit.StandardLookup(thing))
	})
}

const docPkg = "github.com/vardhan/internaltools/internal/kernel/doc"

// TestMsttest_NewWiresRealKernel has no case ID (MST Task 1 Produces).
func TestMsttest_NewWiresRealKernel(t *testing.T) {
	registerThingMaster()
	env := msttest.New(t)
	ctx := context.Background()

	// A migrated database, shared with the kit.
	if env.DB == nil {
		t.Fatal("DB = nil")
	}
	if env.Kit.DB != env.DB {
		t.Error("Kit.DB is not DB")
	}
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		var n int
		for _, table := range []string{"audit_log", "gst_state", "goose_db_version", "goose_testkit"} {
			if err := tx.SQL().QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil { //nolint:gosec // G202: fixed table names
				t.Errorf("table %s: %v; want it migrated", table, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("read: %v", err)
	}

	// Real AUTH and DOC services, the production audit writer.
	if _, ok := env.Kit.Auth.(*auth.Auth); !ok {
		t.Errorf("Kit.Auth = %T; want the real *auth.Auth", env.Kit.Auth)
	}
	if env.Kit.Engine == nil {
		t.Fatal("Kit.Engine = nil; want the real doc engine")
	}
	et := reflect.TypeOf(env.Kit.Engine)
	if et.Kind() == reflect.Pointer {
		et = et.Elem()
	}
	if et.PkgPath() != docPkg {
		t.Errorf("Kit.Engine = %T; want the real engine from %s (doc.NewEngine)", env.Kit.Engine, docPkg)
	}
	if env.Kit.Auditor != audit.Default {
		t.Errorf("Kit.Auditor = %T %v; want audit.Default (CD-131)", env.Kit.Auditor, env.Kit.Auditor)
	}

	// A fixed clock at testkit's instant, shared with the kit.
	if env.Clock == nil {
		t.Fatal("Clock = nil")
	}
	if env.Kit.Clock != env.Clock {
		t.Error("Kit.Clock is not Clock")
	}
	if want := testkit.Clock().Now(); !env.Clock.Now().Equal(want) {
		t.Errorf("Clock.Now() = %v; want %v", env.Clock.Now(), want)
	}

	// The canonical principals.
	for _, c := range []struct {
		name  string
		got   auth.Principal
		email string
	}{
		{"Admin", env.Admin, "admin@vardhan.test"},
		{"Kavya", env.Kavya, "kavya@vardhan.test"},
		{"Priya", env.Priya, "priya@vardhan.test"},
		{"Ravi", env.Ravi, "ravi@vardhan.test"},
	} {
		if want := testkit.Principal(t, c.email); !reflect.DeepEqual(c.got, want) {
			t.Errorf("%s = %+v; want %+v", c.name, c.got, want)
		}
	}

	// Kavya holds every applicable right on a registered master; Priya only
	// read (through the real auth.Service.Can).
	for _, r := range []auth.Right{auth.RightRead, auth.RightCreate, auth.RightWrite, auth.RightDelete, auth.RightExport, auth.RightImport} {
		if err := env.Kit.Auth.Can(ctx, env.Kavya, thing.DocType, r, nil); err != nil {
			t.Errorf("Kavya Can(%s, %s) = %v; want nil", thing.DocType, r, err)
		}
	}
	if err := env.Kit.Auth.Can(ctx, env.Priya, thing.DocType, auth.RightRead, nil); err != nil {
		t.Errorf("Priya Can(%s, read) = %v; want nil", thing.DocType, err)
	}
	for _, r := range []auth.Right{auth.RightCreate, auth.RightWrite, auth.RightDelete} {
		if err := env.Kit.Auth.Can(ctx, env.Priya, thing.DocType, r, nil); !errors.Is(err, auth.ErrForbidden) {
			t.Errorf("Priya Can(%s, %s) = %v; want auth.ErrForbidden", thing.DocType, r, err)
		}
	}
}

// TestMsttest_AppliesFixtureRestrictions has no case ID (review finding):
// msttest.New applies the canonical fixtures' user restrictions
// (00-fixtures.md, Users table) as AUTH's own test world does, seen through
// the real AUTH service; users the fixtures don't restrict have none.
func TestMsttest_AppliesFixtureRestrictions(t *testing.T) {
	env := msttest.New(t)
	ctx := context.Background()
	a, ok := env.Kit.Auth.(*auth.Auth)
	if !ok {
		t.Fatalf("Kit.Auth = %T; want the real *auth.Auth", env.Kit.Auth)
	}

	// The fixture values (codes, user ids) from testkit; the database ids of
	// projects and sites from env's own seeded stand-in tables.
	fx := testkit.Seed(t, testkit.NewDB(t))
	if len(fx.Restrictions) == 0 {
		t.Fatal("testkit fixtures have no restrictions; want Priya's and Suresh's (00-fixtures.md)")
	}
	ids := map[string]map[string]int64{"testkit_project": {}, "testkit_site": {}}
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		for table, m := range ids {
			rows, err := tx.SQL().QueryContext(ctx, `SELECT code, id FROM `+table) //nolint:gosec // G202: fixed table names
			if err != nil {
				return err
			}
			for rows.Next() {
				var code string
				var id int64
				if err := rows.Scan(&code, &id); err != nil {
					_ = rows.Close()
					return err
				}
				m[code] = id
			}
			if err := rows.Close(); err != nil {
				return err
			}
			if err := rows.Err(); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("read stand-in ids: %v", err)
	}
	resolve := func(table string, codes []string) []int64 {
		var out []int64
		for _, c := range codes {
			id, ok := ids[table][c]
			if !ok {
				t.Fatalf("%s has no code %q", table, c)
			}
			out = append(out, id)
		}
		slices.Sort(out)
		return out
	}

	want := map[string]map[auth.Dimension][]int64{}
	for _, r := range fx.Restrictions {
		w := map[auth.Dimension][]int64{}
		if len(r.ProjectCodes) > 0 {
			w[auth.DimProject] = resolve("testkit_project", r.ProjectCodes)
		}
		if len(r.SiteCodes) > 0 {
			w[auth.DimSite] = resolve("testkit_site", r.SiteCodes)
		}
		want[r.Email] = w
	}
	if _, ok := want["priya@vardhan.test"]; !ok {
		t.Fatal("fixtures restrict no priya@vardhan.test; want project PRJ-METRO (00-fixtures.md)")
	}

	for _, u := range fx.Users {
		got, err := a.UserRestrictions(ctx, u.ID)
		if err != nil {
			t.Errorf("UserRestrictions(%s) = %v", u.Email, err)
			continue
		}
		w := want[u.Email]
		if len(got) == 0 && len(w) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, w) {
			t.Errorf("UserRestrictions(%s) = %v; want %v (fixture restrictions)", u.Email, got, w)
		}
	}
}

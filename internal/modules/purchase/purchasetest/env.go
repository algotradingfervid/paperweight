// Package purchasetest is purchase's test harness: a testkit database with
// the canonical seed and Arjun, the real kernel services (AUTH, DOC, APR,
// naming), comments and one *purchase.Service, all on one fixed clock, plus
// builders that create documents through Engine.Do (CD-272). Later tasks add
// their helpers in purchasetest/<task>.go.
package purchasetest

import (
	"context"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filesfake"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/modules/modkit"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

// Env is one test's purchase world.
type Env struct {
	DB        *db.DB
	Auth      *auth.Auth
	Engine    doc.Engine
	Clock     *clock.Fixed
	Approvals *approval.Approvals
	Comments  *modkit.Comments
	Service   *purchase.Service
	Files     *files.Store // the engine's store with WithRealFiles, else nil

	Admin, Meena, Ravi, Priya, Suresh, Kavya, Deepak, Arjun auth.Principal
}

// EnvOpt changes how New builds the Env.
type EnvOpt func(*envSpec)

type envSpec struct{ noRules, realFiles bool }

// WithNoRules leaves out the test seed's purchase.Order approval rules, for
// back-entry cases.
func WithNoRules() EnvOpt { return func(s *envSpec) { s.noRules = true } }

// WithRealFiles gives the engine a real files.Store (default policy, data
// under t.TempDir()) on the Env's database and clock instead of filesfake,
// and puts it in Env.Files, so DOC's DeleteAll and CopyAll see attachments
// made through it.
func WithRealFiles() EnvOpt { return func(s *envSpec) { s.realFiles = true } }

// engineRef lets AUTH and APR hold the engine, which needs them, before it
// exists.
type engineRef struct{ doc.Engine }

// directoryRef is MST's directory, which needs AUTH, before it exists.
type directoryRef struct{ auth.Directory }

// New builds the world: testkit.Seed's masters and users, Arjun (CD-257), the
// production DefaultRights through the real AUTH, the fixture restrictions,
// the clock at authtest.T0 and, unless WithNoRules, the test seed's
// purchase.Order rules (Purchase Manager from ₹50,000, Director from
// ₹5,00,000; 00-fixtures.md). The hooks are wired for this database only.
func New(t testing.TB, opts ...EnvOpt) *Env {
	t.Helper()
	var spec envSpec
	for _, o := range opts {
		o(&spec)
	}
	ctx := context.Background()
	d := testkit.NewDB(t)
	fx := testkit.Seed(t, d)
	clk := testkit.Clock()
	arjun := insertArjun(t, d, clk)

	refs, dir := &engineRef{}, &directoryRef{}
	a := auth.New(auth.Deps{DB: d, Clock: clk, Directory: dir, Audit: audit.Default, Refs: refs, Info: refs})
	masters := mstkit.Env{DB: d, Auth: a, Engine: refs, Auditor: audit.Default, Clock: clk}
	dir.Directory = &employee.Directory{Employees: employee.New(masters), Projects: project.New(masters), Sites: site.New(masters)}
	if err := a.SeedDefaults(ctx, auth.SeedOptions{LinkedMasterRead: true}); err != nil {
		t.Fatalf("purchasetest.New: SeedDefaults: %v", err)
	}
	namer := naming.New(d, clk, a, audit.Default)
	approvals := approval.New(approval.Deps{DB: d, Clock: clk, Auth: a, Info: refs, Directory: dir, Audit: audit.Default})
	var store *files.Store
	var fsvc files.Service = filesfake.NewWith(clk, a, refs)
	if spec.realFiles {
		var err error
		if store, err = files.New(files.Config{DataDir: testkit.DataDir(t), DB: d, Auth: a, Info: refs, Clock: clk, Audit: audit.Default}); err != nil {
			t.Fatalf("purchasetest.New: files.New: %v", err)
		}
		fsvc = store
	}
	refs.Engine = doc.NewEngine(doc.Deps{
		DB: d, Clock: clk, Auth: a, Naming: namer, Approval: approvals, Files: fsvc, Audit: audit.Default,
	})
	for _, name := range []string{purchase.DocType, purchase.RequestDocType} {
		dt, _ := doc.Lookup(name)
		if err := namer.EnsureSeries(ctx, dt.Name, dt.Label, dt.Series); err != nil {
			t.Fatalf("purchasetest.New: EnsureSeries(%s): %v", name, err)
		}
	}
	comments := modkit.NewComments(modkit.CommentsDeps{DB: d, Auth: a, Info: refs.Engine, Clock: clk})
	purchase.Wire(d, purchase.HookDeps{Clock: clk, Comments: comments})

	e := &Env{
		DB: d, Auth: a, Engine: refs.Engine, Clock: clk, Approvals: approvals, Comments: comments, Files: store,
		Service: purchase.New(purchase.ServiceDeps{
			DB: d, Engine: refs.Engine, Auth: a, Audit: audit.Default, Approvals: approvals, Comments: comments, Clock: clk,
		}),
		Admin:  testkit.Principal(t, "admin@vardhan.test"),
		Meena:  testkit.Principal(t, "meena@vardhan.test"),
		Ravi:   testkit.Principal(t, "ravi@vardhan.test"),
		Priya:  testkit.Principal(t, "priya@vardhan.test"),
		Suresh: testkit.Principal(t, "suresh@vardhan.test"),
		Kavya:  testkit.Principal(t, "kavya@vardhan.test"),
		Deepak: testkit.Principal(t, "deepak@vardhan.test"),
		Arjun:  arjun,
	}
	restrict(t, e, fx)
	if !spec.noRules {
		for _, r := range []approval.RuleInput{
			{DocType: purchase.DocType, FromAmount: 50_000_00, ApproverType: approval.ApproverRole, Role: "Purchase Manager"},
			{DocType: purchase.DocType, FromAmount: 5_00_000_00, ApproverType: approval.ApproverRole, Role: "Director"},
		} {
			if _, err := approvals.CreateRule(ctx, e.Admin, r); err != nil {
				t.Fatalf("purchasetest.New: approval rule from %s: %v", types.FormatMoney(r.FromAmount), err)
			}
		}
	}
	return e
}

// SetNow moves the shared clock to at and puts it back when t ends. A test
// that calls it never calls t.Parallel.
func (e *Env) SetNow(t testing.TB, at time.Time) {
	t.Helper()
	before := e.Clock.Now()
	e.Clock.Set(at)
	t.Cleanup(func() { e.Clock.Set(before) })
}

// insertArjun adds the module-only fixture user arjun@vardhan.test (Purchase
// User and Employee, EMP-009 reporting to EMP-003, no restrictions; CD-257)
// with plain SQL, as testkit.Seed adds the others, and returns his principal.
func insertArjun(t testing.TB, d *db.DB, clk clock.Clock) auth.Principal {
	t.Helper()
	ctx := context.Background()
	at := types.TimestampOf(clk.Now()).String()
	p := auth.Principal{Name: "Arjun Mehta", Roles: []string{"Employee", "Purchase User"}} // sorted, as AUTH loads them
	err := d.Write(ctx, func(tx *db.Tx) error {
		q := tx.SQL()
		if err := q.QueryRowContext(ctx, `INSERT INTO users (email, name, enabled, created_at, updated_at) VALUES ('arjun@vardhan.test', ?, 1, ?, ?) RETURNING id`,
			p.Name, at, at).Scan(&p.ID); err != nil {
			return err
		}
		for _, role := range p.Roles {
			if _, err := q.ExecContext(ctx, `INSERT INTO user_roles (user_id, role_id) SELECT ?, id FROM roles WHERE name = ?`, p.ID, role); err != nil {
				return err
			}
		}
		_, err := q.ExecContext(ctx, `INSERT INTO employee (code, name, branch_id, user_id, manager_id, joining_date, status, created_at, created_by, updated_at, updated_by)
			VALUES ('EMP-009', ?, (SELECT id FROM branch WHERE code = 'PUNE'), ?, (SELECT id FROM employee WHERE code = 'EMP-003'), '2024-04-01', 'Active', ?, 1, ?, 1)`,
			p.Name, p.ID, at, at)
		return err
	})
	if err != nil {
		t.Fatalf("purchasetest.New: insert arjun: %v", err)
	}
	return p
}

// restrict applies the fixture restrictions (Priya to PRJ-METRO, Suresh to
// PUNE-WH and METRO-SITE) through the real AUTH, as the Administrator.
func restrict(t testing.TB, e *Env, fx testkit.Fixtures) {
	t.Helper()
	users, projects, sites := map[string]auth.UserID{}, map[string]int64{}, map[string]int64{}
	for _, u := range fx.Users {
		users[u.Email] = u.ID
	}
	for _, p := range fx.Projects {
		projects[p.Code] = p.ID
	}
	for _, s := range fx.Sites {
		sites[s.Code] = s.ID
	}
	ids := func(m map[string]int64, codes []string) []int64 {
		var out []int64
		for _, c := range codes {
			out = append(out, m[c])
		}
		return out
	}
	for _, r := range fx.Restrictions {
		for dim, add := range map[auth.Dimension][]int64{
			auth.DimProject: ids(projects, r.ProjectCodes),
			auth.DimSite:    ids(sites, r.SiteCodes),
		} {
			if len(add) == 0 {
				continue
			}
			if err := e.Auth.ChangeRestrictions(context.Background(), e.Admin, users[r.Email], dim, add, nil, nil); err != nil {
				t.Fatalf("purchasetest.New: restrict %s on %s: %v", r.Email, dim, err)
			}
		}
	}
}

package authtest

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// T0 is the default test instant: 24-10-2026 10:30 IST.
var T0 = time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC)

// World is a migrated database with the canonical users, roles,
// restrictions, rights matrix and probe tables, and an Auth over it.
type World struct {
	DB        *db.DB
	Clock     *clock.Fixed
	Auth      *auth.Auth
	Dir       *authfake.Directory
	Audit     *auditfake.Recorder
	Refs      *Refs
	Info      *Info
	Users     map[string]auth.UserID // email → ID
	Projects  map[string]int64       // code → ID
	Sites     map[string]int64       // code → ID
	Employees map[string]int64       // code → ID
}

// ids reads the master IDs by code from the seeded fixtures (the one place
// that knows testkit's field names).
func ids(fx testkit.Fixtures) (projects, sites, employees map[string]int64) {
	projects, sites, employees = map[string]int64{}, map[string]int64{}, map[string]int64{}
	for _, p := range fx.Projects {
		projects[p.Code] = p.ID
	}
	for _, s := range fx.Sites {
		sites[s.Code] = s.ID
	}
	for _, e := range fx.Employees {
		employees[e.Code] = e.ID
	}
	return projects, sites, employees
}

// NewWorld builds the canonical World of 00-fixtures.md. Users have no
// password.
func NewWorld(t testing.TB) *World {
	t.Helper()
	ctx := context.Background()
	d := testkit.NewDB(t)
	fx := testkit.Seed(t, d)
	RegisterDocTypes()
	CreateProbeTables(t, d, fx)
	w := &World{
		DB: d, Clock: clock.NewFixed(T0), Dir: testkit.Directory(fx), Audit: auditfake.New(),
		Refs: &Refs{}, Info: &Info{}, Users: map[string]auth.UserID{},
	}
	w.Projects, w.Sites, w.Employees = ids(fx)
	w.Auth = auth.New(auth.Deps{
		DB: d, Clock: w.Clock, Directory: w.Dir, Audit: w.Audit, Refs: w.Refs, Info: w.Info,
	})
	if err := w.Auth.SeedDefaults(ctx, auth.SeedOptions{}); err != nil {
		t.Fatalf("authtest.NewWorld: SeedDefaults: %v", err)
	}
	for _, u := range fx.Users {
		w.insertUser(t, u.ID, u.Email, u.Name, !u.Disabled, u.Roles)
	}
	testkit.LinkEmployees(t, d, fx)
	for _, r := range fx.Restrictions {
		w.Restrict(t, r.Email, r.ProjectCodes, r.SiteCodes)
	}
	return w
}

// write runs fn in a write transaction, bumps perm_version and fails t on
// error. authtest may not import auth/dbq (CD-85), so the helpers use SQL.
func (w *World) write(t testing.TB, what string, fn func(ctx context.Context, q db.DBTX) error) {
	t.Helper()
	ctx := context.Background()
	if err := w.DB.Write(ctx, func(tx *db.Tx) error {
		q := tx.SQL()
		if err := fn(ctx, q); err != nil {
			return err
		}
		_, err := q.ExecContext(ctx, `UPDATE auth_meta SET perm_version = perm_version + 1 WHERE id = 1`)
		return err
	}); err != nil {
		t.Fatalf("authtest.%s: %v", what, err)
	}
}

// user returns the ID of email, failing t when the World has no such user.
func (w *World) user(t testing.TB, email string) auth.UserID {
	t.Helper()
	u, ok := w.Users[email]
	if !ok {
		t.Fatalf("authtest: no user %q in the World", email)
	}
	return u
}

// insertUser inserts a user (id 0 = next id) with roles and records it in
// w.Users.
func (w *World) insertUser(t testing.TB, id auth.UserID, email, name string, enabled bool, roles []string) auth.UserID {
	t.Helper()
	w.write(t, "AddUser", func(ctx context.Context, q db.DBTX) error {
		var idArg any // NULL = next id
		if id != 0 {
			idArg = int64(id)
		}
		at := auth.FormatTime(w.Clock.Now())
		var got int64
		if err := q.QueryRowContext(ctx,
			`INSERT INTO users (id, email, name, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?) RETURNING id`,
			idArg, email, name, enabled, at, at).Scan(&got); err != nil {
			return err
		}
		id = auth.UserID(got)
		return addRoles(ctx, q, id, roles)
	})
	w.Users[email] = id
	return id
}

// addRoles gives u each role (which must exist); roles it has are kept.
func addRoles(ctx context.Context, q db.DBTX, u auth.UserID, roles []string) error {
	for _, role := range roles {
		var id int64
		err := q.QueryRowContext(ctx, `SELECT id FROM roles WHERE name_key = lower(?)`, role).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("no role " + role)
		}
		if err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx,
			`INSERT OR IGNORE INTO user_roles (user_id, role_id) VALUES (?, ?)`, int64(u), id); err != nil {
			return err
		}
	}
	return nil
}

// roleID returns role's ID, creating the role when it is missing.
func (w *World) roleID(ctx context.Context, q db.DBTX, role string) (int64, error) {
	var id int64
	err := q.QueryRowContext(ctx, `SELECT id FROM roles WHERE name_key = lower(?)`, role).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		at := auth.FormatTime(w.Clock.Now())
		err = q.QueryRowContext(ctx,
			`INSERT INTO roles (name, name_key, created_at, updated_at) VALUES (?, lower(?), ?, ?) RETURNING id`,
			role, role, at, at).Scan(&id)
	}
	return id, err
}

// P returns the principal of email, roles sorted.
func (w *World) P(t testing.TB, email string) auth.Principal {
	t.Helper()
	u := w.user(t, email)
	p := auth.Principal{ID: u}
	ctx := context.Background()
	if err := w.DB.Read(ctx, func(tx *db.Tx) error {
		q := tx.SQL()
		if err := q.QueryRowContext(ctx, `SELECT name FROM users WHERE id = ?`, int64(u)).Scan(&p.Name); err != nil {
			return err
		}
		rows, err := q.QueryContext(ctx,
			`SELECT r.name FROM user_roles ur JOIN roles r ON r.id = ur.role_id WHERE ur.user_id = ?`, int64(u))
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return err
			}
			p.Roles = append(p.Roles, name)
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("authtest.P(%q): %v", email, err)
	}
	slices.Sort(p.Roles)
	return p
}

// AddUser creates an enabled user with no password and the given roles.
func (w *World) AddUser(t testing.TB, email, name string, roles ...string) auth.UserID {
	t.Helper()
	return w.insertUser(t, 0, email, name, true, roles)
}

// Grant gives role scope s for r on doctype, creating the role when it is
// missing; when r isn't read and the role has no read, it also gets read at
// scope s, so the row stays valid. Twin of authfake's Grant (CD-56); the
// wildcard DocType "*" is not taken.
func (w *World) Grant(t testing.TB, role, doctype string, r auth.Right, s auth.Scope) {
	t.Helper()
	w.write(t, "Grant", func(ctx context.Context, q db.DBTX) error {
		rights, err := currentRights(ctx, q, role, doctype)
		if err != nil {
			return err
		}
		rights[r] = s
		if _, ok := rights[auth.RightRead]; !ok {
			rights[auth.RightRead] = s
		}
		return w.replaceRights(ctx, q, role, doctype, rights)
	})
}

// SetRights replaces role's whole row on doctype, creating the role when it
// is missing. Grant and SetRights fail t on a row auth.ValidateRow rejects.
func (w *World) SetRights(t testing.TB, role, doctype string, rights map[auth.Right]auth.Scope) {
	t.Helper()
	w.write(t, "SetRights", func(ctx context.Context, q db.DBTX) error {
		return w.replaceRights(ctx, q, role, doctype, rights)
	})
}

func currentRights(ctx context.Context, q db.DBTX, role, doctype string) (map[auth.Right]auth.Scope, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT rr.right_name, rr.scope FROM role_rights rr JOIN roles r ON r.id = rr.role_id
		 WHERE r.name_key = lower(?) AND rr.doctype = ?`, role, doctype)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[auth.Right]auth.Scope{}
	for rows.Next() {
		var r, s string
		if err := rows.Scan(&r, &s); err != nil {
			return nil, err
		}
		out[auth.Right(r)] = auth.Scope(s)
	}
	return out, rows.Err()
}

// replaceRights validates rights as production does (auth.ValidateRow), then
// replaces role's row on dtName.
func (w *World) replaceRights(ctx context.Context, q db.DBTX, role, dtName string, rights map[auth.Right]auth.Scope) error {
	dt, ok := doctype.Lookup(dtName)
	if !ok {
		return errors.New("no DocType " + dtName)
	}
	if err := auth.ValidateRow(dt, rights); err != nil {
		return err
	}
	id, err := w.roleID(ctx, q, role)
	if err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx,
		`INSERT INTO role_doctypes (role_id, doctype, customised) VALUES (?, ?, 1)
		 ON CONFLICT (role_id, doctype) DO UPDATE SET customised = 1`, id, dtName); err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM role_rights WHERE role_id = ? AND doctype = ?`, id, dtName); err != nil {
		return err
	}
	for _, r := range auth.AllRights {
		if s, ok := rights[r]; ok {
			if _, err := q.ExecContext(ctx,
				`INSERT INTO role_rights (role_id, doctype, right_name, scope) VALUES (?, ?, ?, ?)`,
				id, dtName, string(r), string(s)); err != nil {
				return err
			}
		}
	}
	return nil
}

// AddRole gives email the existing role. Twin of authfake's AddRole.
func (w *World) AddRole(t testing.TB, email, role string) {
	t.Helper()
	u := w.user(t, email)
	w.write(t, "AddRole", func(ctx context.Context, q db.DBTX) error {
		return addRoles(ctx, q, u, []string{role})
	})
}

// RemoveRole takes role from email; no error when the user lacks it. Twin
// of authfake's RemoveRole (CD-86).
func (w *World) RemoveRole(t testing.TB, email, role string) {
	t.Helper()
	u := w.user(t, email)
	w.write(t, "RemoveRole", func(ctx context.Context, q db.DBTX) error {
		_, err := q.ExecContext(ctx,
			`DELETE FROM user_roles WHERE user_id = ? AND role_id IN (SELECT id FROM roles WHERE name_key = lower(?))`,
			int64(u), role)
		return err
	})
}

// SetRoles replaces email's roles.
func (w *World) SetRoles(t testing.TB, email string, roles ...string) {
	t.Helper()
	u := w.user(t, email)
	w.write(t, "SetRoles", func(ctx context.Context, q db.DBTX) error {
		if _, err := q.ExecContext(ctx, `DELETE FROM user_roles WHERE user_id = ?`, int64(u)); err != nil {
			return err
		}
		return addRoles(ctx, q, u, roles)
	})
}

// Restrict replaces email's project and site restrictions, by code; nil
// means no restriction on that dimension. Twin of authfake's Restrict.
func (w *World) Restrict(t testing.TB, email string, projects, sites []string) {
	t.Helper()
	u := w.user(t, email)
	type value struct {
		dim auth.Dimension
		id  int64
	}
	var values []value
	for _, c := range projects {
		id, ok := w.Projects[c]
		if !ok {
			t.Fatalf("authtest.Restrict: no project %q", c)
		}
		values = append(values, value{auth.DimProject, id})
	}
	for _, c := range sites {
		id, ok := w.Sites[c]
		if !ok {
			t.Fatalf("authtest.Restrict: no site %q", c)
		}
		values = append(values, value{auth.DimSite, id})
	}
	w.write(t, "Restrict", func(ctx context.Context, q db.DBTX) error {
		if _, err := q.ExecContext(ctx, `DELETE FROM user_restrictions WHERE user_id = ?`, int64(u)); err != nil {
			return err
		}
		for _, v := range values {
			if _, err := q.ExecContext(ctx,
				`INSERT INTO user_restrictions (user_id, dimension, value_id) VALUES (?, ?, ?)`,
				int64(u), string(v.dim), v.id); err != nil {
				return err
			}
		}
		return nil
	})
}

// SetMasterEnabled enables or disables the probe master row of dt with code.
func (w *World) SetMasterEnabled(t testing.TB, dt, code string, enabled bool) {
	t.Helper()
	var table string
	for _, d := range docTypes() {
		if d.Name == dt && d.Master != nil {
			table = d.Table
		}
	}
	if table == "" {
		t.Fatalf("authtest.SetMasterEnabled: %s is not a probe master", dt)
	}
	ctx := context.Background()
	if err := w.DB.Write(ctx, func(tx *db.Tx) error {
		res, err := tx.SQL().ExecContext(ctx, `UPDATE `+table+` SET enabled = ? WHERE code = ?`, enabled, code) //nolint:gosec // G202: table is a probe constant
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err == nil && n != 1 {
			err = errors.New("no row with code " + code)
		}
		return err
	}); err != nil {
		t.Fatalf("authtest.SetMasterEnabled(%s, %s): %v", dt, code, err)
	}
}

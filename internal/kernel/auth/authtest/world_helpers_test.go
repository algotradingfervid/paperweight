package authtest_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

func permVersion(t *testing.T, w *authtest.World) int64 {
	t.Helper()
	ctx := context.Background()
	var v int64
	if err := w.DB.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, `SELECT perm_version FROM auth_meta WHERE id = 1`).Scan(&v)
	}); err != nil {
		t.Fatalf("read perm_version: %v", err)
	}
	return v
}

// bumps runs fn and checks that it raised perm_version by exactly one.
func bumps(t *testing.T, w *authtest.World, what string, fn func()) {
	t.Helper()
	v := permVersion(t, w)
	fn()
	if got := permVersion(t, w); got != v+1 {
		t.Errorf("perm_version after %s = %d, want %d", what, got, v+1)
	}
}

func roleRights(t *testing.T, w *authtest.World, role, dt string) map[auth.Right]auth.Scope {
	t.Helper()
	got, err := w.Auth.RoleRights(context.Background(), role, dt)
	if err != nil {
		t.Fatalf("RoleRights(%s, %s): %v", role, dt, err)
	}
	return got
}

// TestWorld_GrantAndSetRights checks that Grant creates a missing role, adds
// read at the granted scope only when the role has no read, and that
// SetRights replaces the whole row; each bumps perm_version.
func TestWorld_GrantAndSetRights(t *testing.T) {
	w := authtest.NewWorld(t)

	v := permVersion(t, w)
	w.Grant(t, "Site Reader", authtest.DTSite, auth.RightWrite, auth.ScopeAll)
	if got, want := roleRights(t, w, "Site Reader", authtest.DTSite), (map[auth.Right]auth.Scope{
		auth.RightRead: auth.ScopeAll, auth.RightWrite: auth.ScopeAll,
	}); !reflect.DeepEqual(got, want) {
		t.Errorf("after Grant(write, all) rights = %v, want %v", got, want)
	}
	if got := permVersion(t, w); got != v+1 {
		t.Errorf("perm_version after Grant = %d, want %d", got, v+1)
	}

	w.Grant(t, "Site Reader", authtest.DTSite, auth.RightRead, auth.ScopeOwn)
	if got, want := roleRights(t, w, "Site Reader", authtest.DTSite), (map[auth.Right]auth.Scope{
		auth.RightRead: auth.ScopeOwn, auth.RightWrite: auth.ScopeAll,
	}); !reflect.DeepEqual(got, want) {
		t.Errorf("after Grant(read, own) rights = %v, want %v", got, want)
	}

	// A second non-read grant keeps the existing read scope.
	w.Grant(t, "Site Reader", authtest.DTSite, auth.RightDelete, auth.ScopeAll)
	if got, want := roleRights(t, w, "Site Reader", authtest.DTSite), (map[auth.Right]auth.Scope{
		auth.RightRead: auth.ScopeOwn, auth.RightWrite: auth.ScopeAll, auth.RightDelete: auth.ScopeAll,
	}); !reflect.DeepEqual(got, want) {
		t.Errorf("after Grant(delete, all) rights = %v, want %v", got, want)
	}

	v = permVersion(t, w)
	w.SetRights(t, "Stores", authtest.DTOrder, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeOwn})
	if got, want := roleRights(t, w, "Stores", authtest.DTOrder), (map[auth.Right]auth.Scope{
		auth.RightRead: auth.ScopeOwn,
	}); !reflect.DeepEqual(got, want) {
		t.Errorf("after SetRights Stores on %s = %v, want %v", authtest.DTOrder, got, want)
	}
	if got := permVersion(t, w); got != v+1 {
		t.Errorf("perm_version after SetRights = %d, want %d", got, v+1)
	}

	w.SetRights(t, "Stores", authtest.DTOrder, map[auth.Right]auth.Scope{})
	if got := roleRights(t, w, "Stores", authtest.DTOrder); len(got) != 0 {
		t.Errorf("after SetRights(empty) Stores on %s = %v, want none", authtest.DTOrder, got)
	}
}

// TestWorld_UsersAndRoles checks AddUser, AddRole, RemoveRole and SetRoles.
func TestWorld_UsersAndRoles(t *testing.T) {
	w := authtest.NewWorld(t)

	id := w.AddUser(t, "new@vardhan.test", "New Person", "Employee")
	if id != 9 {
		t.Errorf("AddUser id = %d, want 9", id)
	}
	if w.Users["new@vardhan.test"] != id {
		t.Errorf("w.Users[new@] = %d, want %d", w.Users["new@vardhan.test"], id)
	}
	want := auth.Principal{ID: 9, Name: "New Person", Roles: []string{"Employee"}}
	if got := w.P(t, "new@vardhan.test"); !reflect.DeepEqual(got, want) {
		t.Errorf("P(new@) = %+v, want %+v", got, want)
	}

	w.Grant(t, "Site Reader", authtest.DTSite, auth.RightRead, auth.ScopeAll)
	bumps(t, w, "AddRole", func() { w.AddRole(t, "new@vardhan.test", "Site Reader") })
	w.AddRole(t, "new@vardhan.test", "Site Reader") // already has it: no error
	if got, want := w.P(t, "new@vardhan.test").Roles, []string{"Employee", "Site Reader"}; !reflect.DeepEqual(got, want) {
		t.Errorf("roles after AddRole = %q, want %q", got, want)
	}

	w.RemoveRole(t, "new@vardhan.test", "Director")                                       // lacks it: no error
	bumps(t, w, "RemoveRole", func() { w.RemoveRole(t, "new@vardhan.test", "employee") }) // role names match case-insensitively
	if got, want := w.P(t, "new@vardhan.test").Roles, []string{"Site Reader"}; !reflect.DeepEqual(got, want) {
		t.Errorf("roles after RemoveRole = %q, want %q", got, want)
	}

	bumps(t, w, "SetRoles", func() { w.SetRoles(t, "priya@vardhan.test", "Stores", "Director") })
	if got, want := w.P(t, "priya@vardhan.test"), (auth.Principal{ID: 4, Name: "Priya Shah", Roles: []string{"Director", "Stores"}}); !reflect.DeepEqual(got, want) {
		t.Errorf("P(priya@) after SetRoles = %+v, want %+v", got, want)
	}
}

// TestWorld_Restrict checks that Restrict replaces both dimensions by code.
func TestWorld_Restrict(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)
	type row struct {
		Dim string
		ID  int64
	}
	read := func(email string) []row {
		var out []row
		if err := w.DB.Read(ctx, func(tx *db.Tx) error {
			rows, err := tx.SQL().QueryContext(ctx,
				`SELECT dimension, value_id FROM user_restrictions WHERE user_id = ? ORDER BY dimension, value_id`,
				int64(w.Users[email]))
			if err != nil {
				return err
			}
			defer func() { _ = rows.Close() }()
			for rows.Next() {
				var r row
				if err := rows.Scan(&r.Dim, &r.ID); err != nil {
					return err
				}
				out = append(out, r)
			}
			return rows.Err()
		}); err != nil {
			t.Fatalf("read restrictions: %v", err)
		}
		return out
	}

	if got, want := read("priya@vardhan.test"), []row{{"project", w.Projects["PRJ-METRO"]}}; !reflect.DeepEqual(got, want) {
		t.Errorf("seeded priya restrictions = %v, want %v", got, want)
	}

	bumps(t, w, "Restrict", func() { w.Restrict(t, "suresh@vardhan.test", []string{"PRJ-METRO"}, nil) })
	if got, want := read("suresh@vardhan.test"), []row{{"project", w.Projects["PRJ-METRO"]}}; !reflect.DeepEqual(got, want) {
		t.Errorf("suresh restrictions = %v, want %v (sites cleared)", got, want)
	}

	w.Restrict(t, "suresh@vardhan.test", nil, nil)
	if got := read("suresh@vardhan.test"); len(got) != 0 {
		t.Errorf("suresh restrictions after Restrict(nil, nil) = %v, want none", got)
	}
}

// TestWorld_SetMasterEnabled checks the probe master lookup and the helper.
func TestWorld_SetMasterEnabled(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)
	dt, ok := doctype.Lookup(authtest.DTSite)
	if !ok || dt.Master == nil {
		t.Fatalf("%s not registered as a master", authtest.DTSite)
	}
	lookup := func() (doctype.MasterInfo, bool) {
		var mi doctype.MasterInfo
		var found bool
		if err := w.DB.Read(ctx, func(tx *db.Tx) error {
			var err error
			mi, found, err = dt.Master.Lookup(ctx, tx, w.Sites["PUNE-WH"])
			return err
		}); err != nil {
			t.Fatalf("Master.Lookup: %v", err)
		}
		return mi, found
	}

	if mi, found := lookup(); !found || mi != (doctype.MasterInfo{Code: "PUNE-WH", Name: "PUNE-WH"}) {
		t.Errorf("Lookup(PUNE-WH) = %+v, %v; want enabled PUNE-WH", mi, found)
	}
	w.SetMasterEnabled(t, authtest.DTSite, "PUNE-WH", false)
	if mi, found := lookup(); !found || mi != (doctype.MasterInfo{Code: "PUNE-WH", Name: "PUNE-WH", Disabled: true}) {
		t.Errorf("Lookup(PUNE-WH) after disable = %+v, %v; want disabled", mi, found)
	}
	w.SetMasterEnabled(t, authtest.DTSite, "PUNE-WH", true)
	if mi, _ := lookup(); mi.Disabled {
		t.Errorf("Lookup(PUNE-WH) after enable = %+v, want enabled", mi)
	}

	var missing bool
	if err := w.DB.Read(ctx, func(tx *db.Tx) error {
		_, found, err := dt.Master.Lookup(ctx, tx, 999)
		missing = !found
		return err
	}); err != nil || !missing {
		t.Errorf("Lookup(999) found = %v, err = %v; want not found, nil", !missing, err)
	}
}

// TestWorld_Info checks Info for a numbered order with lines, a claim, the
// Site and Project masters and a missing row.
func TestWorld_Info(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)
	prj, wh, metro, tech := w.Projects["PRJ-METRO"], w.Sites["PUNE-WH"], w.Sites["METRO-SITE"], w.Sites["TECH-SITE"]
	var orderID, claimID int64
	if err := w.DB.Write(ctx, func(tx *db.Tx) error {
		q := tx.SQL()
		res, err := q.ExecContext(ctx,
			`INSERT INTO probe_order (number, state, created_by, created_at, project_id, site_id) VALUES (?, ?, ?, ?, ?, ?)`,
			"PO/26-27/00001", string(doctype.StateApproved), int64(w.Users["priya@vardhan.test"]), auth.FormatTime(authtest.T0), prj, wh)
		if err != nil {
			return err
		}
		if orderID, err = res.LastInsertId(); err != nil {
			return err
		}
		// Lines repeat the header site and add another; Info lists each once, sorted.
		if _, err := q.ExecContext(ctx,
			`INSERT INTO probe_order_line (parent_id, line_site_id) VALUES (?, ?), (?, ?), (?, NULL)`,
			orderID, tech, orderID, wh, orderID); err != nil {
			return err
		}
		res, err = q.ExecContext(ctx,
			`INSERT INTO probe_claim (state, created_by, created_at, employee_id, project_id) VALUES (?, ?, ?, ?, ?)`,
			string(doctype.StateDraft), int64(w.Users["deepak@vardhan.test"]), auth.FormatTime(authtest.T0), w.Employees["EMP-007"], prj)
		if err != nil {
			return err
		}
		claimID, err = res.LastInsertId()
		return err
	}); err != nil {
		t.Fatalf("insert probe rows: %v", err)
	}

	info := func(dt string, id int64) (doctype.Info, error) {
		var out doctype.Info
		err := w.DB.Read(ctx, func(tx *db.Tx) error {
			var err error
			out, err = w.Info.Info(ctx, tx, dt, id)
			return err
		})
		return out, err
	}

	sites := []int64{wh, tech}
	if tech < wh {
		sites = []int64{tech, wh}
	}
	cases := []struct {
		dt   string
		id   int64
		want doctype.Info
	}{
		{authtest.DTOrder, orderID, doctype.Info{
			DocType: authtest.DTOrder, DocLabel: "Purchase Order", ID: orderID, State: doctype.StateApproved,
			Number: "PO/26-27/00001", Display: "PO/26-27/00001", CreatedBy: types.UserID(w.Users["priya@vardhan.test"]),
			ProjectIDs: []int64{prj}, SiteIDs: sites,
		}},
		{authtest.DTClaim, claimID, doctype.Info{
			DocType: authtest.DTClaim, DocLabel: "Expense Claim", ID: claimID, State: doctype.StateDraft,
			Display: fmt.Sprintf("Draft %d", claimID), CreatedBy: types.UserID(w.Users["deepak@vardhan.test"]),
			EmployeeID: w.Employees["EMP-007"], ProjectIDs: []int64{prj},
		}},
		{authtest.DTSite, metro, doctype.Info{
			DocType: authtest.DTSite, DocLabel: "Site", ID: metro, Number: "METRO-SITE", Display: "METRO-SITE",
			SiteIDs: []int64{metro},
		}},
		{authtest.DTProject, prj, doctype.Info{
			DocType: authtest.DTProject, DocLabel: "Project", ID: prj, Number: "PRJ-METRO", Display: "PRJ-METRO",
			ProjectIDs: []int64{prj},
		}},
	}
	for _, c := range cases {
		got, err := info(c.dt, c.id)
		if err != nil {
			t.Errorf("Info(%s, %d): %v", c.dt, c.id, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("Info(%s, %d) = %+v, want %+v", c.dt, c.id, got, c.want)
		}
	}

	for _, dt := range []string{authtest.DTClaim, authtest.DTSite, "authtest.Nope"} {
		if _, err := info(dt, 999); !errors.Is(err, doctype.ErrNotFound) || !errors.Is(err, auth.ErrNotFound) {
			t.Errorf("Info(%s, 999) err = %v, want one wrapping ErrNotFound", dt, err)
		}
	}
}

// TestWorld_Refs checks the ReferenceChecker.
func TestWorld_Refs(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)
	w.Refs.Add("probe_site", 1, "PO/26-27/00001")
	if ref, ok, err := w.Refs.IsReferenced(ctx, nil, "probe_site", 1); ref != "PO/26-27/00001" || !ok || err != nil {
		t.Errorf("IsReferenced(probe_site, 1) = %q, %v, %v; want PO/26-27/00001, true, nil", ref, ok, err)
	}
	for _, k := range []struct {
		table string
		id    int64
	}{{"probe_site", 2}, {"probe_project", 1}} {
		if ref, ok, err := w.Refs.IsReferenced(ctx, nil, k.table, k.id); ref != "" || ok || err != nil {
			t.Errorf("IsReferenced(%s, %d) = %q, %v, %v; want \"\", false, nil", k.table, k.id, ref, ok, err)
		}
	}
}

// TestWorld_LinksEmployees (CD-199 item 4): the World has users, so every
// fixture employee points at its user in the real table.
func TestWorld_LinksEmployees(t *testing.T) {
	w := authtest.NewWorld(t)
	var linked int
	if err := w.DB.SQL().QueryRowContext(t.Context(), `SELECT count(user_id) FROM employee`).Scan(&linked); err != nil {
		t.Fatalf("count linked employees: %v", err)
	}
	if linked != 8 {
		t.Errorf("linked employees = %d; want 8", linked)
	}
}

// TestWorld_DefaultRightsMatchFixtures ties the probe DocTypes' DefaultRights
// to testkit.Fixtures.Rights: fixture.Order → Order, fixture.Receipt →
// Receipt, Expense Claim and fixture.Claim → Claim (the fixture.Claim twin
// has only the team row), "every master" → each master. Administrator ("*")
// has no rows; it holds every right through the role itself.
func TestWorld_DefaultRightsMatchFixtures(t *testing.T) {
	authtest.RegisterDocTypes()
	fx := testkit.Seed(t, testkit.NewDB(t))
	sources := map[string][]string{
		authtest.DTOrder:   {"fixture.Order"},
		authtest.DTReceipt: {"fixture.Receipt"},
		authtest.DTClaim:   {"Expense Claim", "fixture.Claim"},
		authtest.DTItem:    {"every master"},
		authtest.DTParty:   {"every master"},
		authtest.DTSite:    {"every master"},
		authtest.DTProject: {"every master"},
	}
	for name, from := range sources {
		want := map[string]map[string]string{}
		for _, row := range fx.Rights {
			if !slices.Contains(from, row.DocType) {
				continue
			}
			if want[row.Role] == nil {
				want[row.Role] = map[string]string{}
			}
			for _, r := range row.Rights {
				want[row.Role][r] = string(auth.Wider(auth.Scope(want[row.Role][r]), auth.Scope(row.Scope)))
			}
		}
		dt, ok := doctype.Lookup(name)
		if !ok {
			t.Fatalf("%s not registered", name)
		}
		if !reflect.DeepEqual(dt.DefaultRights, want) {
			t.Errorf("%s DefaultRights = %v, want %v (from %q in testkit.Fixtures.Rights)", name, dt.DefaultRights, want, from)
		}
	}
}

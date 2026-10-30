package auth_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// t5InsertPO inserts a probe Purchase Order with the given id, number,
// creator, project and site (0 = NULL).
func t5InsertPO(t *testing.T, w *authtest.World, id int64, number string, createdBy auth.UserID, project, site int64) {
	t.Helper()
	ctx := context.Background()
	nullable := func(v int64) any {
		if v == 0 {
			return nil
		}
		return v
	}
	if err := w.DB.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx,
			`INSERT INTO probe_order (id, number, state, created_by, created_at, project_id, site_id) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, number, string(doctype.StateDraft), int64(createdBy), auth.FormatTime(authtest.T0), nullable(project), nullable(site))
		return err
	}); err != nil {
		t.Fatalf("insert probe PO %d: %v", id, err)
	}
}

// t5WantDenied checks that err is a *auth.DeniedError for r with the given
// own/team flags and exactly msg, and that it is ErrForbidden, not ErrNotFound.
func t5WantDenied(t *testing.T, what string, err error, r auth.Right, ownOnly, teamOnly bool, msg string) {
	t.Helper()
	var de *auth.DeniedError
	if !errors.As(err, &de) {
		t.Errorf("%s: err = %v (%T), want *auth.DeniedError", what, err, err)
		return
	}
	if de.Right != r || de.OwnOnly != ownOnly || de.TeamOnly != teamOnly {
		t.Errorf("%s: DeniedError{Right %q, OwnOnly %v, TeamOnly %v}, want {Right %q, OwnOnly %v, TeamOnly %v}",
			what, de.Right, de.OwnOnly, de.TeamOnly, r, ownOnly, teamOnly)
	}
	if !errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrNotFound) {
		t.Errorf("%s: err = %v, want errors.Is ErrForbidden and not ErrNotFound", what, err)
	}
	if got := err.Error(); got != msg {
		t.Errorf("%s: message = %q, want %q", what, got, msg)
	}
}

// t5WantNotFound checks that err is ErrNotFound (404), not ErrForbidden.
func t5WantNotFound(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, auth.ErrNotFound) || errors.Is(err, auth.ErrForbidden) {
		t.Errorf("%s: err = %v, want auth.ErrNotFound (404), not ErrForbidden", what, err)
	}
}

// AUTH-063
func TestRights_UnionAcrossRoles(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)
	suresh := "suresh@vardhan.test"
	po := &auth.RecordRef{
		DocType: authtest.DTOrder, ID: 1, CreatedBy: w.Users["priya@vardhan.test"],
		ProjectIDs: []int64{w.Projects["PRJ-METRO"]}, SiteIDs: []int64{w.Sites["METRO-SITE"]},
	}

	w.SetRoles(t, suresh, "Stores", "Employee", "Purchase Manager")
	if err := w.Auth.Can(ctx, w.P(t, suresh), authtest.DTOrder, auth.RightApprove, po); err != nil {
		t.Errorf("Stores + Employee + Purchase Manager: Can(approve PO) = %v, want nil", err)
	}

	w.SetRoles(t, suresh, "Stores", "Employee")
	err := w.Auth.Can(ctx, w.P(t, suresh), authtest.DTOrder, auth.RightApprove, po)
	if !errors.Is(err, auth.ErrForbidden) {
		t.Errorf("Stores + Employee: Can(approve PO) = %v, want auth.ErrForbidden", err)
	}
}

// AUTH-069
func TestRights_AdminAll(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)
	admin := w.P(t, "admin@vardhan.test")
	creator := w.Users["priya@vardhan.test"]

	var checked int
	for _, dt := range doctype.All() {
		for _, r := range auth.AllRights {
			if !auth.Applies(dt, r) {
				continue
			}
			for prjCode, prj := range w.Projects {
				for siteCode, site := range w.Sites {
					rec := &auth.RecordRef{
						DocType: dt.Name, ID: 1, CreatedBy: creator,
						EmployeeID: w.Employees["EMP-004"],
						ProjectIDs: []int64{prj}, SiteIDs: []int64{site},
					}
					if err := w.Auth.Can(ctx, admin, dt.Name, r, rec); err != nil {
						t.Errorf("admin Can(%s, %s, record on %s/%s) = %v, want nil", dt.Name, r, prjCode, siteCode, err)
					}
					checked++
				}
			}
			if err := w.Auth.Can(ctx, admin, dt.Name, r, nil); err != nil {
				t.Errorf("admin Can(%s, %s, nil) = %v, want nil", dt.Name, r, err)
			}
			if pred, err := w.Auth.Scope(ctx, admin, dt.Name, r); err != nil || pred.SQL != "" || len(pred.Args) != 0 {
				t.Errorf("admin Scope(%s, %s) = ({SQL %q, Args %v}, %v), want empty Predicate and nil", dt.Name, r, pred.SQL, pred.Args, err)
			}
		}
	}
	if checked == 0 || w.Sites["TECH-SITE"] == 0 || w.Projects["PRJ-TECHPARK"] == 0 {
		t.Fatalf("no DocType × right × site × project combinations checked (sites %v, projects %v)", w.Sites, w.Projects)
	}

	// Inapplicable rights are refused even for the Administrator.
	item := &auth.RecordRef{DocType: authtest.DTItem, ID: 1, CreatedBy: creator}
	if err := w.Auth.Can(ctx, admin, authtest.DTItem, auth.RightSubmit, item); !errors.Is(err, auth.ErrForbidden) {
		t.Errorf("admin Can(Item, submit) = %v, want auth.ErrForbidden (submit does not apply to a master)", err)
	}
}

// AUTH-096
func TestRestriction_DoesNotGrant(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)
	deepak := "deepak@vardhan.test"
	w.Restrict(t, deepak, nil, []string{"PUNE-WH"})
	po := &auth.RecordRef{
		DocType: authtest.DTOrder, ID: 1, CreatedBy: w.Users["priya@vardhan.test"],
		ProjectIDs: []int64{w.Projects["PRJ-METRO"]}, SiteIDs: []int64{w.Sites["PUNE-WH"]},
	}
	err := w.Auth.Can(ctx, w.P(t, deepak), authtest.DTOrder, auth.RightRead, po)
	t5WantDenied(t, "deepak read PO at PUNE-WH", err, auth.RightRead, false, false,
		"You don't have permission to read Purchase Order. Ask your administrator for a role that allows it.")
	var he doctype.HTTPError
	if !errors.As(err, &he) || he.Status() != 403 {
		t.Errorf("deepak read PO at PUNE-WH: err = %v, want an HTTPError with status 403 (not 404)", err)
	}
}

func TestCan_403vs404(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)
	priya, ravi := w.P(t, "priya@vardhan.test"), w.P(t, "ravi@vardhan.test")
	metro, techpark := w.Projects["PRJ-METRO"], w.Projects["PRJ-TECHPARK"]

	techPO := &auth.RecordRef{
		DocType: authtest.DTOrder, ID: 1, CreatedBy: ravi.ID,
		ProjectIDs: []int64{techpark}, SiteIDs: []int64{w.Sites["TECH-SITE"]},
	}
	t5WantNotFound(t, "priya (PRJ-METRO) read PO on PRJ-TECHPARK",
		w.Auth.Can(ctx, priya, authtest.DTOrder, auth.RightRead, techPO))

	metroPO := &auth.RecordRef{
		DocType: authtest.DTOrder, ID: 2, CreatedBy: ravi.ID,
		ProjectIDs: []int64{metro}, SiteIDs: []int64{w.Sites["METRO-SITE"]},
	}
	if err := w.Auth.Can(ctx, ravi, authtest.DTOrder, auth.RightApprove, metroPO); err != nil {
		t.Errorf("ravi approve PO = %v, want nil", err)
	}
	t5WantDenied(t, "priya approve PO", w.Auth.Can(ctx, priya, authtest.DTOrder, auth.RightApprove, metroPO),
		auth.RightApprove, false, false,
		"You don't have permission to approve Purchase Order. Ask your administrator for a role that allows it.")
	t5WantDenied(t, "priya delete PO created by ravi", w.Auth.Can(ctx, priya, authtest.DTOrder, auth.RightDelete, metroPO),
		auth.RightDelete, true, false, "You can only delete Purchase Orders you created.")
}

func TestCan_TeamScope(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)
	priya, ravi := w.P(t, "priya@vardhan.test"), w.P(t, "ravi@vardhan.test")
	metro := []int64{w.Projects["PRJ-METRO"]}

	deepakClaim := &auth.RecordRef{
		DocType: authtest.DTClaim, ID: 1, CreatedBy: w.Users["deepak@vardhan.test"],
		EmployeeID: w.Employees["EMP-007"], ProjectIDs: metro,
	}
	for _, r := range []auth.Right{auth.RightRead, auth.RightApprove} {
		if err := w.Auth.Can(ctx, priya, authtest.DTClaim, r, deepakClaim); err != nil {
			t.Errorf("priya %s Deepak's claim (team) = %v, want nil", r, err)
		}
	}
	t5WantNotFound(t, "ravi read Deepak's claim (not a direct report)",
		w.Auth.Can(ctx, ravi, authtest.DTClaim, auth.RightRead, deepakClaim))

	priyaClaim := &auth.RecordRef{
		DocType: authtest.DTClaim, ID: 2, CreatedBy: w.Users["admin@vardhan.test"],
		EmployeeID: w.Employees["EMP-004"], ProjectIDs: metro,
	}
	if err := w.Auth.Can(ctx, priya, authtest.DTClaim, auth.RightApprove, priyaClaim); err != nil {
		t.Errorf("priya approve her own claim (team includes own) = %v, want nil", err)
	}

	t5WantDenied(t, "priya write Deepak's claim (write is own)",
		w.Auth.Can(ctx, priya, authtest.DTClaim, auth.RightWrite, deepakClaim),
		auth.RightWrite, true, false, "You can only change Expense Claims you created.")

	w.SetRights(t, "Employee", authtest.DTClaim, map[auth.Right]auth.Scope{
		auth.RightRead: auth.ScopeAll, auth.RightApprove: auth.ScopeTeam,
		auth.RightCreate: auth.ScopeOwn, auth.RightWrite: auth.ScopeOwn,
		auth.RightSubmit: auth.ScopeOwn, auth.RightPrint: auth.ScopeOwn,
	})
	sureshClaim := &auth.RecordRef{
		DocType: authtest.DTClaim, ID: 3, CreatedBy: w.Users["suresh@vardhan.test"],
		EmployeeID: w.Employees["EMP-005"], ProjectIDs: metro,
	}
	t5WantDenied(t, "priya approve Suresh's claim (read all, approve team)",
		w.Auth.Can(ctx, priya, authtest.DTClaim, auth.RightApprove, sureshClaim),
		auth.RightApprove, false, true,
		"You can only approve your own Expense Claims and those of people who report to you.")
}

func TestUsersWithRight_RespectsRestrictionsAndDisabled(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)
	// Approvers that must not be listed: Priya is restricted to PRJ-METRO,
	// old@ is disabled.
	w.AddRole(t, "priya@vardhan.test", "Purchase Manager")
	w.AddRole(t, "old@vardhan.test", "Purchase Manager")
	po := &auth.RecordRef{
		DocType: authtest.DTOrder, ID: 1, CreatedBy: w.Users["priya@vardhan.test"],
		ProjectIDs: []int64{w.Projects["PRJ-TECHPARK"]}, SiteIDs: []int64{w.Sites["TECH-SITE"]},
	}
	got, err := w.Auth.UsersWithRight(ctx, authtest.DTOrder, auth.RightApprove, po)
	if err != nil {
		t.Fatalf("UsersWithRight: %v", err)
	}
	want := []auth.UserID{
		w.Users["admin@vardhan.test"], w.Users["meena@vardhan.test"], w.Users["ravi@vardhan.test"],
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("UsersWithRight(PO approve, PRJ-TECHPARK) = %v, want %v (admin, meena, ravi)", got, want)
	}
}

func TestCheckValuesAndLink(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)
	email := "suresh@vardhan.test"
	w.Restrict(t, email, nil, []string{"PUNE-WH"})
	suresh := w.P(t, email)
	tech, wh := w.Sites["TECH-SITE"], w.Sites["PUNE-WH"]

	const siteMsg = "You can only use Site PUNE-WH. Choose one of your allowed sites."
	for _, key := range []string{"site_id", "lines.line_site_id"} {
		err := w.Auth.CheckValues(ctx, suresh, authtest.DTReceipt, map[string][]int64{key: {tech}})
		var ve *auth.ValueError
		if !errors.As(err, &ve) || !errors.Is(err, auth.ErrForbidden) {
			t.Errorf("CheckValues(%s: TECH-SITE) = %v (%T), want *auth.ValueError (ErrForbidden)", key, err, err)
			continue
		}
		if got := err.Error(); got != siteMsg {
			t.Errorf("CheckValues(%s: TECH-SITE) message = %q, want %q", key, got, siteMsg)
		}
	}
	if err := w.Auth.CheckValues(ctx, suresh, authtest.DTReceipt, map[string][]int64{"other": {tech}}); err != nil {
		t.Errorf("CheckValues(other: TECH-SITE) = %v, want nil (key is not a site field)", err)
	}
	if err := w.Auth.CheckValues(ctx, suresh, authtest.DTReceipt, map[string][]int64{"site_id": {wh}}); err != nil {
		t.Errorf("CheckValues(site_id: PUNE-WH) = %v, want nil (allowed site)", err)
	}

	t5InsertPO(t, w, 30, "PO/26-27/00030", w.Users["priya@vardhan.test"], w.Projects["PRJ-TECHPARK"], tech)
	t5InsertPO(t, w, 31, "PO/26-27/00031", w.Users["priya@vardhan.test"], w.Projects["PRJ-METRO"], wh)

	err := w.Auth.CheckLink(ctx, suresh, authtest.DTOrder, 30)
	var le *auth.LinkError
	if !errors.As(err, &le) || !errors.Is(err, auth.ErrForbidden) {
		t.Errorf("CheckLink(PO-30 at TECH-SITE) = %v (%T), want *auth.LinkError (ErrForbidden)", err, err)
	} else if got, want := err.Error(), "You can't use Purchase Order PO/26-27/00030. It isn't available to you."; got != want {
		t.Errorf("CheckLink(PO-30) message = %q, want %q", got, want)
	}
	if err := w.Auth.CheckLink(ctx, suresh, authtest.DTOrder, 31); err != nil {
		t.Errorf("CheckLink(PO-31 at PUNE-WH) = %v, want nil (visible)", err)
	}
	if err := w.Auth.CheckLink(ctx, suresh, authtest.DTOrder, 9999); !errors.Is(err, doctype.ErrNotFound) {
		t.Errorf("CheckLink(PO 9999) = %v, want doctype.ErrNotFound", err)
	}
}

func TestService_User(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)
	user := func(u auth.UserID) (auth.UserInfo, bool, error) {
		var info auth.UserInfo
		var ok bool
		err := w.DB.Read(ctx, func(tx *db.Tx) error {
			var err error
			info, ok, err = w.Auth.User(ctx, tx, u)
			return err
		})
		return info, ok, err
	}

	info, ok, err := user(4)
	want := auth.UserInfo{ID: 4, Email: "priya@vardhan.test", Name: "Priya Shah", Enabled: true, Version: 1}
	if err != nil || !ok || !reflect.DeepEqual(info, want) {
		t.Errorf("User(4) = %+v, %v, %v; want %+v, true, nil", info, ok, err, want)
	}

	info, ok, err = user(8)
	want = auth.UserInfo{ID: 8, Email: "old@vardhan.test", Name: "Former Staff", Enabled: false, Version: 1}
	if err != nil || !ok || !reflect.DeepEqual(info, want) {
		t.Errorf("User(8) = %+v, %v, %v; want %+v, true, nil", info, ok, err, want)
	}

	if info, ok, err = user(999); err != nil || ok {
		t.Errorf("User(999) = %+v, %v, %v; want ok false and a nil error", info, ok, err)
	}
}

// t5Kind classifies a Service result for the parity test.
func t5Kind(err error) string {
	switch {
	case err == nil:
		return "nil"
	case errors.Is(err, auth.ErrNotFound):
		return "notfound"
	case errors.Is(err, auth.ErrForbidden):
		return "forbidden"
	}
	return "other: " + err.Error()
}

// CD-56: other WPs test against authfake, so pin that it answers like the real service
func TestAuthfake_MatchesReal(t *testing.T) {
	const clerk = "clerk@vardhan.test"
	const poID = 30
	clerkGrants := func(t *testing.T, w *authtest.World, f *authfake.Fake, rights ...auth.Right) {
		for _, r := range rights {
			w.Grant(t, "Clerk", authtest.DTOrder, r, auth.ScopeAll)
			f.Grant("Clerk", authtest.DTOrder, r, auth.ScopeAll)
		}
	}
	cases := []struct {
		name  string
		email string
		setup func(t *testing.T, w *authtest.World, f *authfake.Fake)
		edit  func(po *auth.RecordRef) // changes the record Can is asked about
	}{
		{"read", clerk, func(t *testing.T, w *authtest.World, f *authfake.Fake) {
			clerkGrants(t, w, f, auth.RightRead)
			w.AddRole(t, clerk, "Clerk")
			f.AddRole(w.Users[clerk], "Clerk")
		}, nil},
		{"read and approve", clerk, func(t *testing.T, w *authtest.World, f *authfake.Fake) {
			clerkGrants(t, w, f, auth.RightRead, auth.RightApprove)
			w.AddRole(t, clerk, "Clerk")
			f.AddRole(w.Users[clerk], "Clerk")
		}, nil},
		{"restricted to PUNE-WH, PO at TECH-SITE", clerk, func(t *testing.T, w *authtest.World, f *authfake.Fake) {
			clerkGrants(t, w, f, auth.RightRead, auth.RightApprove)
			w.AddRole(t, clerk, "Clerk")
			f.AddRole(w.Users[clerk], "Clerk")
			w.Restrict(t, clerk, nil, []string{"PUNE-WH"})
			f.Restrict(w.Users[clerk], nil, []int64{w.Sites["PUNE-WH"]})
			f.Hide(authtest.DTOrder, poID)
		}, nil},
		{"restricted to PRJ-TECHPARK, record with no project (AUTH-093)", clerk, func(t *testing.T, w *authtest.World, f *authfake.Fake) {
			clerkGrants(t, w, f, auth.RightRead, auth.RightApprove)
			w.AddRole(t, clerk, "Clerk")
			f.AddRole(w.Users[clerk], "Clerk")
			w.Restrict(t, clerk, []string{"PRJ-TECHPARK"}, nil)
			f.Restrict(w.Users[clerk], []int64{w.Projects["PRJ-TECHPARK"]}, nil)
		}, func(po *auth.RecordRef) { po.ProjectIDs = nil }},
		{"restricted to TECH-SITE, record with no site (AUTH-093)", clerk, func(t *testing.T, w *authtest.World, f *authfake.Fake) {
			clerkGrants(t, w, f, auth.RightRead, auth.RightApprove)
			w.AddRole(t, clerk, "Clerk")
			f.AddRole(w.Users[clerk], "Clerk")
			w.Restrict(t, clerk, nil, []string{"TECH-SITE"})
			f.Restrict(w.Users[clerk], nil, []int64{w.Sites["TECH-SITE"]})
		}, func(po *auth.RecordRef) { po.SiteIDs = nil }},
		{"role added then removed (CD-86)", clerk, func(t *testing.T, w *authtest.World, f *authfake.Fake) {
			clerkGrants(t, w, f, auth.RightRead, auth.RightApprove)
			w.AddRole(t, clerk, "Clerk")
			f.AddRole(w.Users[clerk], "Clerk")
			w.RemoveRole(t, clerk, "Clerk")
			f.RemoveRole(w.Users[clerk], "Clerk")
		}, nil},
		{"Administrator (CD-86 wildcard)", "admin@vardhan.test", func(t *testing.T, w *authtest.World, f *authfake.Fake) {
			for _, r := range auth.AllRights {
				f.Grant(auth.RoleAdministrator, "*", r, auth.ScopeAll)
			}
			f.AddRole(w.Users["admin@vardhan.test"], auth.RoleAdministrator)
		}, nil},
		{"Administrator role alone, no grants (CD-182)", "admin@vardhan.test", func(t *testing.T, w *authtest.World, f *authfake.Fake) {
			f.AddRole(w.Users["admin@vardhan.test"], auth.RoleAdministrator)
		}, nil},
		{"disabled user (CD-86)", "old@vardhan.test", func(t *testing.T, w *authtest.World, f *authfake.Fake) {
			clerkGrants(t, w, f, auth.RightRead, auth.RightApprove)
			u := w.Users["old@vardhan.test"]
			w.AddRole(t, "old@vardhan.test", "Clerk")
			f.AddUser(auth.UserInfo{ID: u, Email: "old@vardhan.test", Name: "Former Staff", Enabled: false})
			f.AddRole(u, "Clerk")
		}, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			w := authtest.NewWorld(t)
			w.AddUser(t, clerk, "Clerk User")
			tech := w.Sites["TECH-SITE"]
			t5InsertPO(t, w, poID, "PO/26-27/00030", w.Users["admin@vardhan.test"], w.Projects["PRJ-TECHPARK"], tech)
			f := authfake.New()
			tc.setup(t, w, f)
			p := w.P(t, tc.email)
			po := &auth.RecordRef{
				DocType: authtest.DTOrder, ID: poID, CreatedBy: w.Users["admin@vardhan.test"],
				ProjectIDs: []int64{w.Projects["PRJ-TECHPARK"]}, SiteIDs: []int64{tech},
			}
			if tc.edit != nil {
				tc.edit(po)
			}

			compare := func(what string, real, fake error) {
				t.Helper()
				if r, fk := t5Kind(real), t5Kind(fake); r != fk {
					t.Errorf("%s: real = %s (%v), authfake = %s (%v)", what, r, real, fk, fake)
				}
			}
			rights := []auth.Right{auth.RightRead, auth.RightApprove}
			if tc.email == "admin@vardhan.test" {
				dt, _ := doctype.Lookup(authtest.DTOrder)
				rights = nil
				for _, r := range auth.AllRights {
					if auth.Applies(dt, r) {
						rights = append(rights, r)
					}
				}
			}
			for _, r := range rights {
				compare("Can("+string(r)+", nil)",
					w.Auth.Can(ctx, p, authtest.DTOrder, r, nil), f.Can(ctx, p, authtest.DTOrder, r, nil))
				compare("Can("+string(r)+", PO at TECH-SITE)",
					w.Auth.Can(ctx, p, authtest.DTOrder, r, po), f.Can(ctx, p, authtest.DTOrder, r, po))
			}
			values := map[string][]int64{"site_id": {tech}}
			compare("CheckValues(site_id: TECH-SITE)",
				w.Auth.CheckValues(ctx, p, authtest.DTOrder, values), f.CheckValues(ctx, p, authtest.DTOrder, values))
			compare("CheckLink(PO at TECH-SITE)",
				w.Auth.CheckLink(ctx, p, authtest.DTOrder, poID), f.CheckLink(ctx, p, authtest.DTOrder, poID))

			// The World also lists the seeded approvers; compare whether this
			// case's user is listed.
			realUsers, err := w.Auth.UsersWithRight(ctx, authtest.DTOrder, auth.RightApprove, nil)
			if err != nil {
				t.Fatalf("real UsersWithRight: %v", err)
			}
			fakeUsers, err := f.UsersWithRight(ctx, authtest.DTOrder, auth.RightApprove, nil)
			if err != nil {
				t.Fatalf("authfake UsersWithRight: %v", err)
			}
			if r, fk := slices.Contains(realUsers, p.ID), slices.Contains(fakeUsers, p.ID); r != fk {
				t.Errorf("UsersWithRight(PO approve) lists user %d: real %v (%v), authfake %v (%v)", p.ID, r, realUsers, fk, fakeUsers)
			}
		})
	}
}

// CD-173: authfake applies auth.Applies like the real service. For every
// registered DocType × every right in AllRights plus an unknown right, the
// fake is granted the right (it accepts any grant) and the real service is
// granted it where it applies; Can, Scope and UsersWithRight must agree.
func TestAuthfake_AppliesMatchesReal(t *testing.T) {
	const unknown auth.Right = "foo"
	rights := append(slices.Clone(auth.AllRights), unknown)

	authtest.RegisterDocTypes()
	dts := doctype.All()
	if len(dts) == 0 {
		t.Fatal("no DocTypes registered")
	}
	// The rows the CD names must be among the ones checked.
	item, okItem := doctype.Lookup(authtest.DTItem)
	receipt, okReceipt := doctype.Lookup(authtest.DTReceipt)
	if !okItem || item.Submittable || !okReceipt || receipt.Approvable {
		t.Fatalf("need %s registered as a master (not submittable) and %s registered as not approvable", authtest.DTItem, authtest.DTReceipt)
	}

	var inapplicable int
	for _, dt := range dts {
		t.Run(dt.Name, func(t *testing.T) {
			for _, who := range []string{"Clerk", auth.RoleAdministrator} {
				t.Run(who, func(t *testing.T) { appliesParity(t, dt, rights, who, &inapplicable) })
			}
		})
	}
	if inapplicable == 0 {
		t.Error("no inapplicable DocType × right rows were checked")
	}
}

// appliesParity runs the CD-173 comparison for one DocType as a Clerk
// granted every right on it, or as the Administrator (the real service's
// bypass; the fake's "*" grant of every right), whose inapplicable rights
// must be refused as well.
func appliesParity(t *testing.T, dt *doctype.DocType, rights []auth.Right, who string, inapplicable *int) {
	const clerk = "clerk@vardhan.test"
	ctx := context.Background()
	w := authtest.NewWorld(t)
	email := clerk
	var u auth.UserID
	f := authfake.New()
	if who == auth.RoleAdministrator {
		email = "admin@vardhan.test"
		u = w.Users[email]
		f.AddUser(auth.UserInfo{ID: u, Email: email, Name: "Admin", Enabled: true})
		for _, r := range rights {
			f.Grant(auth.RoleAdministrator, "*", r, auth.ScopeAll)
		}
		f.AddRole(u, auth.RoleAdministrator)
	} else {
		u = w.AddUser(t, clerk, "Clerk User")
		f.AddUser(auth.UserInfo{ID: u, Email: clerk, Name: "Clerk User", Enabled: true})
		for _, r := range rights {
			f.Grant("Clerk", dt.Name, r, auth.ScopeAll)
			if auth.Applies(dt, r) {
				w.Grant(t, "Clerk", dt.Name, r, auth.ScopeAll)
			}
		}
		w.AddRole(t, clerk, "Clerk")
		f.AddRole(u, "Clerk")
	}
	p := w.P(t, email)
	rec := &auth.RecordRef{DocType: dt.Name, ID: 1, CreatedBy: u}

	compare := func(what string, applies bool, real, fake error) {
		t.Helper()
		want := "forbidden"
		if applies {
			want = "nil"
		}
		r, fk := t5Kind(real), t5Kind(fake)
		if r != want || fk != want {
			t.Errorf("%s: real = %s (%v), authfake = %s (%v), want both %s", what, r, real, fk, fake, want)
			return
		}
		if real != nil && real.Error() != fake.Error() {
			t.Errorf("%s: real message %q, authfake message %q, want the same", what, real.Error(), fake.Error())
		}
	}
	for _, r := range rights {
		applies := auth.Applies(dt, r)
		if !applies {
			*inapplicable++
		}
		compare(fmt.Sprintf("Can(%s, %s, nil)", dt.Name, r), applies,
			w.Auth.Can(ctx, p, dt.Name, r, nil), f.Can(ctx, p, dt.Name, r, nil))
		compare(fmt.Sprintf("Can(%s, %s, own record)", dt.Name, r), applies,
			w.Auth.Can(ctx, p, dt.Name, r, rec), f.Can(ctx, p, dt.Name, r, rec))
		_, realErr := w.Auth.Scope(ctx, p, dt.Name, r)
		_, fakeErr := f.Scope(ctx, p, dt.Name, r)
		compare(fmt.Sprintf("Scope(%s, %s)", dt.Name, r), applies, realErr, fakeErr)

		realUsers, err := w.Auth.UsersWithRight(ctx, dt.Name, r, nil)
		if err != nil {
			t.Fatalf("real UsersWithRight(%s, %s): %v", dt.Name, r, err)
		}
		fakeUsers, err := f.UsersWithRight(ctx, dt.Name, r, nil)
		if err != nil {
			t.Fatalf("authfake UsersWithRight(%s, %s): %v", dt.Name, r, err)
		}
		inReal, inFake := slices.Contains(realUsers, u), slices.Contains(fakeUsers, u)
		if inReal != applies || inFake != applies {
			t.Errorf("UsersWithRight(%s, %s) lists the clerk: real %v (%v), authfake %v (%v), want both %v",
				dt.Name, r, inReal, realUsers, inFake, fakeUsers, applies)
		}
	}
}

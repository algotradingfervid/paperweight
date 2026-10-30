package auth_test

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
)

const (
	t17Priya  = "priya@vardhan.test"
	t17Ravi   = "ravi@vardhan.test"
	t17Suresh = "suresh@vardhan.test"
	t17Old    = "old@vardhan.test"
	t17Store1 = "store1@vardhan.test"
	t17Store2 = "store2@vardhan.test"
	t17Mobile = "+91 98200 12345"
	t17Pass   = "Granite-River-4417" // passes the policy
)

// t17Sorted returns ids sorted.
func t17Sorted(ids []auth.UserID) []auth.UserID {
	out := slices.Clone(ids)
	slices.Sort(out)
	return out
}

// AUTH-052
func TestUser_DefaultRoleAndNoRoleWarning(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	admin := w.P(t, t7Admin)

	id, warnings, err := w.Auth.CreateUser(ctx, admin, auth.NewUser{Email: t7Kiran, Name: "Kiran Joshi", Password: t17Pass})
	if err != nil {
		t.Fatalf("CreateUser(kiran) = %v, want nil", err)
	}
	if len(warnings) != 0 {
		t.Errorf("CreateUser(kiran) warnings = %q, want none", warnings)
	}
	w.Users[t7Kiran] = id
	if got := w.P(t, t7Kiran).Roles; !reflect.DeepEqual(got, []string{auth.RoleEmployee}) {
		t.Fatalf("kiran roles after create = %q, want [Employee]", got)
	}

	warnings, err = w.Auth.UpdateUser(ctx, admin, id, auth.UserPatch{Roles: &[]string{}})
	if err != nil {
		t.Fatalf("UpdateUser(kiran, Roles: []) = %v, want nil", err)
	}
	if want := []string{"Kiran Joshi has no roles and will see nothing after signing in."}; !reflect.DeepEqual(warnings, want) {
		t.Errorf("UpdateUser(kiran, Roles: []) warnings = %q, want %q", warnings, want)
	}
	if got := w.P(t, t7Kiran).Roles; len(got) != 0 {
		t.Errorf("kiran roles after removing all = %q, want none", got)
	}
}

// AUTH-056
func TestUser_DisabledStillShownOnHistory(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	admin := w.P(t, t7Admin)
	priya, old, ravi := w.Users[t17Priya], w.Users[t17Old], w.Users[t17Ravi]
	// Priya and old@ get approve, so the approver list could include them.
	w.AddRole(t, t17Priya, "Purchase Manager")
	w.AddRole(t, t17Old, "Purchase Manager")

	got, err := w.Auth.DisplayName(ctx, priya)
	if err != nil || got != "Priya Shah" {
		t.Fatalf("DisplayName(priya) while enabled = (%q, %v), want (\"Priya Shah\", nil)", got, err)
	}
	approvers, err := w.Auth.UsersWithRight(ctx, authtest.DTOrder, auth.RightApprove, nil)
	if err != nil || !slices.Contains(approvers, priya) {
		t.Fatalf("UsersWithRight(PO, approve) while priya enabled = (%v, %v), want it to contain priya %d", approvers, err, priya)
	}

	enabled := false
	if _, err := w.Auth.UpdateUser(ctx, admin, priya, auth.UserPatch{Enabled: &enabled}); err != nil {
		t.Fatalf("disable priya: %v", err)
	}

	for _, tc := range []struct {
		who  string
		id   auth.UserID
		want string
	}{
		{"priya", priya, "Priya Shah (disabled)"},
		{"old@", old, "Former Staff (disabled)"},
		{"ravi", ravi, "Ravi Kulkarni"},
	} {
		got, err := w.Auth.DisplayName(ctx, tc.id)
		if err != nil || got != tc.want {
			t.Errorf("DisplayName(%s) = (%q, %v), want (%q, nil)", tc.who, got, err, tc.want)
		}
	}

	var want []auth.UserID
	for _, email := range []string{t7Admin, "meena@vardhan.test", t17Ravi, t17Suresh, "kavya@vardhan.test", t7Deepak} {
		want = append(want, w.Users[email])
	}
	users, err := w.Auth.EnabledUsers(ctx)
	if err != nil {
		t.Fatalf("EnabledUsers = %v", err)
	}
	if got := t17Sorted(users); !reflect.DeepEqual(got, t17Sorted(want)) {
		t.Errorf("EnabledUsers = %v, want %v (no priya, no old@)", got, t17Sorted(want))
	}

	approvers, err = w.Auth.UsersWithRight(ctx, authtest.DTOrder, auth.RightApprove, nil)
	if err != nil {
		t.Fatalf("UsersWithRight(PO, approve) = %v", err)
	}
	if slices.Contains(approvers, priya) || slices.Contains(approvers, old) {
		t.Errorf("UsersWithRight(PO, approve) = %v, must not contain priya %d or old@ %d", approvers, priya, old)
	}
	if !slices.Contains(approvers, ravi) {
		t.Errorf("UsersWithRight(PO, approve) = %v, want it to contain ravi %d", approvers, ravi)
	}
}

// AUTH-061
func TestRoles_DeleteBlockedWhileAssigned(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	admin := w.P(t, t7Admin)
	w.AddUser(t, t17Store1, "Store One", "Stores")
	w.AddUser(t, t17Store2, "Store Two", "Stores")
	stores := t3RoleID(t, w.DB, "Stores")
	rowsBefore := t7Count(t, w, `SELECT COUNT(*) FROM role_doctypes WHERE role_id = ?`, stores)
	if rowsBefore == 0 {
		t.Fatal("Stores has no role_doctypes rows after seeding")
	}

	err := w.Auth.DeleteRole(ctx, admin, "Stores")
	t7WantText(t, "DeleteRole(Stores) while assigned", err,
		"Role Stores is assigned to 3 users. Remove it from them before deleting the role.")
	if n := t7Count(t, w, `SELECT COUNT(*) FROM roles WHERE id = ?`, stores); n != 1 {
		t.Fatalf("roles rows for Stores after refused delete = %d, want 1", n)
	}
	if n := t7Count(t, w, `SELECT COUNT(*) FROM role_doctypes WHERE role_id = ?`, stores); n != rowsBefore {
		t.Fatalf("role_doctypes rows for Stores after refused delete = %d, want %d", n, rowsBefore)
	}

	for _, email := range []string{t17Suresh, t17Store1, t17Store2} {
		w.RemoveRole(t, email, "Stores")
	}
	if err := w.Auth.DeleteRole(ctx, admin, "Stores"); err != nil {
		t.Fatalf("DeleteRole(Stores) once unassigned = %v, want nil", err)
	}
	for _, q := range []string{
		`SELECT COUNT(*) FROM roles WHERE id = ?`,
		`SELECT COUNT(*) FROM role_doctypes WHERE role_id = ?`,
		`SELECT COUNT(*) FROM role_rights WHERE role_id = ?`,
	} {
		if n := t7Count(t, w, q, stores); n != 0 {
			t.Errorf("%s [Stores %d] = %d after delete, want 0", q, stores, n)
		}
	}
}

// AUTH-062
func TestRoles_UniqueAndAdminFixed(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	admin := w.P(t, t7Admin)

	err := w.Auth.CreateRole(ctx, admin, "stores")
	t7WantText(t, `CreateRole("stores")`, err, "A role named Stores already exists.")
	if n := t7Count(t, w, `SELECT COUNT(*) FROM roles WHERE name_key = 'stores'`); n != 1 {
		t.Errorf("roles named stores = %d, want 1", n)
	}

	if err := w.Auth.CreateRole(ctx, admin, "Quality"); err != nil {
		t.Fatalf(`CreateRole("Quality") = %v, want nil`, err)
	}
	if n := t7Count(t, w, `SELECT COUNT(*) FROM roles WHERE name = 'Quality' AND name_key = 'quality'`); n != 1 {
		t.Errorf("roles named Quality = %d, want 1", n)
	}

	adminRole := t3RoleID(t, w.DB, auth.RoleAdministrator)
	t7WantProblem(t, "RenameRole(Administrator)", w.Auth.RenameRole(ctx, admin, auth.RoleAdministrator, "Admins"), auth.ErrAdminRoleFixed)
	t7WantProblem(t, "DeleteRole(Administrator)", w.Auth.DeleteRole(ctx, admin, auth.RoleAdministrator), auth.ErrAdminRoleFixed)
	t7WantProblem(t, "SetRights(Administrator, PO)", w.Auth.SetRights(ctx, admin, auth.RoleAdministrator, authtest.DTOrder,
		map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll}), auth.ErrAdminRoleFixed)

	if n := t7Count(t, w, `SELECT COUNT(*) FROM roles WHERE id = ? AND name = 'Administrator' AND is_admin = 1`, adminRole); n != 1 {
		t.Errorf("Administrator role row after refusals: %d rows, want 1 unchanged", n)
	}
	// testkit.Seed stores the fixtures' Administrator "*" row; nothing else may exist.
	if n := t7Count(t, w, `SELECT COUNT(*) FROM role_rights WHERE role_id = ? AND doctype <> '*'`, adminRole); n != 0 {
		t.Errorf("role_rights rows for Administrator = %d, want 0", n)
	}
}

// Carry-over (UpdateUser rules 1–2): a non-admin may change only their own
// name and mobile; anything else is ErrAdminOnlyField and changes nothing.
func TestUser_UpdateAdminOnlyRules(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	priya := w.P(t, t17Priya)
	ravi := w.Users[t17Ravi]
	before := t7Snapshot(t, w)
	mobile, email, enabled := t17Mobile, "x@vardhan.test", true
	roles := []string{"Employee", "Purchase User"}
	ids := []int64{w.Projects["PRJ-METRO"]}

	_, err := w.Auth.UpdateUser(ctx, priya, ravi, auth.UserPatch{Mobile: &mobile})
	t7WantProblem(t, "priya edits ravi's mobile", err, auth.ErrAdminOnlyField)
	for _, tc := range []struct {
		name  string
		patch auth.UserPatch
	}{
		{"email", auth.UserPatch{Email: &email}},
		{"enabled", auth.UserPatch{Enabled: &enabled}},
		{"roles", auth.UserPatch{Roles: &roles}},
		{"projects", auth.UserPatch{Projects: &ids}},
		{"sites", auth.UserPatch{Sites: &ids}},
	} {
		_, err := w.Auth.UpdateUser(ctx, priya, priya.ID, tc.patch)
		t7WantProblem(t, "priya sets own "+tc.name, err, auth.ErrAdminOnlyField)
	}
	if after := t7Snapshot(t, w); !reflect.DeepEqual(after, before) {
		t.Fatalf("refused updates changed rows:\n got %q\nwant %q", after, before)
	}

	if _, err := w.Auth.UpdateUser(ctx, priya, priya.ID, auth.UserPatch{Mobile: &mobile}); err != nil {
		t.Fatalf("priya sets own mobile = %v, want nil", err)
	}
	if got := t7Strings(t, w, `SELECT mobile FROM users WHERE id = ?`, int64(priya.ID)); !reflect.DeepEqual(got, []string{mobile}) {
		t.Errorf("priya mobile = %q, want %q", got, mobile)
	}
}

// Carry-over: changing a user's email deletes their sessions; a change that
// leaves the email alone does not.
func TestUser_UpdateEmailEndsSessions(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	admin := w.P(t, t7Admin)
	priya := w.Users[t17Priya]
	t7AddSession(t, w, priya, "auth-17-a")
	t7AddSession(t, w, priya, "auth-17-b")

	mobile := t17Mobile
	if _, err := w.Auth.UpdateUser(ctx, admin, priya, auth.UserPatch{Mobile: &mobile}); err != nil {
		t.Fatalf("admin sets priya's mobile = %v", err)
	}
	if n := t7Sessions(t, w, priya); n != 2 {
		t.Fatalf("priya sessions after a mobile change = %d, want 2", n)
	}

	email := "priya.shah@vardhan.test"
	if _, err := w.Auth.UpdateUser(ctx, admin, priya, auth.UserPatch{Email: &email}); err != nil {
		t.Fatalf("admin changes priya's email = %v", err)
	}
	if n := t7Sessions(t, w, priya); n != 0 {
		t.Errorf("priya sessions after the email change = %d, want 0", n)
	}
	if got := t7Strings(t, w, `SELECT email FROM users WHERE id = ?`, int64(priya)); !reflect.DeepEqual(got, []string{email}) {
		t.Errorf("priya email = %q, want %q", got, email)
	}
}

// Carry-over: a role or restriction change bumps perm_version; a profile
// change does not.
func TestUser_UpdatePermChangeBumpsPermVersion(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	admin := w.P(t, t7Admin)
	suresh := w.Users[t17Suresh]

	v := t3PermVersion(t, w.DB)
	mobile := t17Mobile
	if _, err := w.Auth.UpdateUser(ctx, admin, suresh, auth.UserPatch{Mobile: &mobile}); err != nil {
		t.Fatalf("mobile change: %v", err)
	}
	if got := t3PermVersion(t, w.DB); got != v {
		t.Errorf("perm_version after a mobile change = %d, want %d", got, v)
	}

	roles := []string{"Employee", "Purchase User", "Stores"}
	sites := []int64{w.Sites["PUNE-WH"], w.Sites["METRO-SITE"], w.Sites["TECH-SITE"]}
	projects := []int64{w.Projects["PRJ-METRO"]}
	for _, tc := range []struct {
		name  string
		patch auth.UserPatch
	}{
		{"roles", auth.UserPatch{Roles: &roles}},
		{"sites", auth.UserPatch{Sites: &sites}},
		{"projects", auth.UserPatch{Projects: &projects}},
	} {
		v = t3PermVersion(t, w.DB)
		if _, err := w.Auth.UpdateUser(ctx, admin, suresh, tc.patch); err != nil {
			t.Fatalf("%s change: %v", tc.name, err)
		}
		if got := t3PermVersion(t, w.DB); got <= v {
			t.Errorf("perm_version after a %s change = %d, want > %d", tc.name, got, v)
		}
	}
}

// Carry-over: UpdateUser writes one audit entry with one set change per
// changed field; restriction values are shown as codes (AUTH-068's format).
func TestUser_UpdateAuditChanges(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	admin := w.P(t, t7Admin)
	suresh := w.Users[t17Suresh]
	n0 := len(w.Audit.Entries())

	name, mobile := "Suresh Patil", t17Mobile // name unchanged: no change recorded
	roles := []string{"Employee", "Purchase User", "Stores"}
	sites := []int64{w.Sites["PUNE-WH"], w.Sites["METRO-SITE"], w.Sites["TECH-SITE"]}
	if _, err := w.Auth.UpdateUser(ctx, admin, suresh, auth.UserPatch{Name: &name, Mobile: &mobile, Roles: &roles, Sites: &sites}); err != nil {
		t.Fatalf("UpdateUser(suresh) = %v", err)
	}
	es := w.Audit.Entries()[n0:]
	if len(es) != 1 {
		t.Fatalf("audit entries written = %d (%+v), want 1", len(es), es)
	}
	e := es[0]
	if !e.At.Equal(authtest.T0) {
		t.Errorf("audit At = %v, want %v", e.At, authtest.T0)
	}
	e.At = authtest.T0
	slices.SortFunc(e.Changes, func(a, b audit.Change) int { return strings.Compare(a.Field, b.Field) })
	want := audit.Entry{
		DocType: "User", DocID: int64(suresh), Action: "update", By: admin.ID, At: authtest.T0,
		Changes: []audit.Change{
			{Field: "mobile", Before: "", After: t17Mobile, Op: "set"},
			{Field: "roles", Before: "Employee, Stores", After: "Employee, Purchase User, Stores", Op: "set"},
			{Field: "sites", Before: "[PUNE-WH, METRO-SITE]", After: "[PUNE-WH, METRO-SITE, TECH-SITE]", Op: "set"},
		},
	}
	if !reflect.DeepEqual(e, want) {
		t.Errorf("audit entry =\n %+v\nwant\n %+v", e, want)
	}
}

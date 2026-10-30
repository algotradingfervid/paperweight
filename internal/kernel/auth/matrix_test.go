package auth_test

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// t3Master makes a DocType a master (CD-21).
type t3Master struct{}

func (t3Master) Lookup(context.Context, *db.Tx, int64) (doctype.MasterInfo, bool, error) {
	return doctype.MasterInfo{}, false, nil
}

// t3Register registers dt unless an earlier run of the same test in this
// process did: the registry is global and panics on a repeat (-count=2).
func t3Register(dt doctype.DocType) {
	if _, ok := doctype.Lookup(dt.Name); !ok {
		doctype.Register(dt)
	}
}

// t3Auth builds an Auth over a fresh migrated database with a fixed clock and
// an audit recorder.
func t3Auth(t *testing.T) (*auth.Auth, *db.DB, *clock.Fixed, *auditfake.Recorder) {
	t.Helper()
	d := testkit.NewDB(t)
	clk := testkit.Clock()
	rec := auditfake.New()
	a := auth.New(auth.Deps{DB: d, Clock: clk, Audit: rec})
	return a, d, clk, rec
}

// t3WantProblem checks that err is a 400 with exactly msg.
func t3WantProblem(t *testing.T, what string, err error, msg string) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: err = nil, want %q", what, msg)
		return
	}
	if err.Error() != msg {
		t.Errorf("%s: err = %q, want %q", what, err.Error(), msg)
	}
	var he doctype.HTTPError
	if !errors.As(err, &he) || he.Status() != 400 {
		t.Errorf("%s: err %v is not a doctype.HTTPError with status 400", what, err)
	}
}

// t3Rows reads role_rights for (role, doctype) straight from the table.
func t3Rows(t *testing.T, d *db.DB, role, dt string) map[string]string {
	t.Helper()
	ctx := context.Background()
	got := map[string]string{}
	err := d.Read(ctx, func(tx *db.Tx) error {
		rows, err := tx.SQL().QueryContext(ctx, `
			SELECT rr.right_name, rr.scope FROM role_rights rr
			JOIN roles r ON r.id = rr.role_id
			WHERE r.name = ? AND rr.doctype = ?`, role, dt)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var rn, sc string
			if err := rows.Scan(&rn, &sc); err != nil {
				return err
			}
			got[rn] = sc
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("read role_rights(%s, %s): %v", role, dt, err)
	}
	return got
}

// t3RoleDocType reads the role_doctypes row for (role, doctype); ok is false
// when there is none.
func t3RoleDocType(t *testing.T, d *db.DB, role, dt string) (customised int, ok bool) {
	t.Helper()
	ctx := context.Background()
	var n int
	err := d.Read(ctx, func(tx *db.Tx) error {
		if err := tx.SQL().QueryRowContext(ctx, `
			SELECT count(*) FROM role_doctypes rd JOIN roles r ON r.id = rd.role_id
			WHERE r.name = ? AND rd.doctype = ?`, role, dt).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return nil
		}
		return tx.SQL().QueryRowContext(ctx, `
			SELECT rd.customised FROM role_doctypes rd JOIN roles r ON r.id = rd.role_id
			WHERE r.name = ? AND rd.doctype = ?`, role, dt).Scan(&customised)
	})
	if err != nil {
		t.Fatalf("read role_doctypes(%s, %s): %v", role, dt, err)
	}
	return customised, n > 0
}

func t3PermVersion(t *testing.T, d *db.DB) int64 {
	t.Helper()
	ctx := context.Background()
	var v int64
	err := d.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, `SELECT perm_version FROM auth_meta WHERE id = 1`).Scan(&v)
	})
	if err != nil {
		t.Fatalf("read perm_version: %v", err)
	}
	return v
}

func t3RoleID(t *testing.T, d *db.DB, role string) int64 {
	t.Helper()
	ctx := context.Background()
	var id int64
	err := d.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, `SELECT id FROM roles WHERE name = ?`, role).Scan(&id)
	})
	if err != nil {
		t.Fatalf("read id of role %q: %v", role, err)
	}
	return id
}

func t3Equal(got map[auth.Right]auth.Scope, want map[auth.Right]auth.Scope) bool {
	return maps.Equal(got, want)
}

var t3PO = &doctype.DocType{
	Name: "t3.PO", Label: "Purchase Order", PluralLabel: "Purchase Orders",
	Submittable: true, Approvable: true,
}

var t3Item = &doctype.DocType{
	Name: "t3.Item", Label: "Item", PluralLabel: "Items",
	Master: t3Master{}, Importable: true,
}

var t3Party = &doctype.DocType{
	Name: "t3.Party", Label: "Party", PluralLabel: "Parties",
	Master: t3Master{}, Importable: true,
}

var t3Claim = &doctype.DocType{
	Name: "t3.Claim", Label: "Expense Claim", PluralLabel: "Expense Claims",
	Submittable: true, Approvable: true, EmployeeField: "employee_id",
}

// AUTH-065
func TestMatrix_ReadRequired(t *testing.T) {
	// Stores on Purchase Order keeps print but clears read.
	err := auth.ValidateRow(t3PO, map[auth.Right]auth.Scope{auth.RightPrint: auth.ScopeAll})
	t3WantProblem(t, "ValidateRow(PO, {print: all})", err,
		"Read is needed for any other right on Purchase Order. Tick Read or clear the other rights.")

	if err := auth.ValidateRow(t3PO, map[auth.Right]auth.Scope{
		auth.RightRead: auth.ScopeAll, auth.RightPrint: auth.ScopeAll,
	}); err != nil {
		t.Errorf("ValidateRow(PO, {read: all, print: all}) = %v, want nil", err)
	}
}

// AUTH-066
func TestMatrix_InapplicableRightsRejected(t *testing.T) {
	for _, dt := range []*doctype.DocType{t3Item, t3Party} {
		for _, r := range []auth.Right{auth.RightSubmit, auth.RightCancel, auth.RightAmend, auth.RightApprove} {
			err := auth.ValidateRow(dt, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, r: auth.ScopeAll})
			name := strings.ToUpper(string(r)[:1]) + string(r)[1:]
			t3WantProblem(t, "ValidateRow("+dt.Label+", {read, "+string(r)+"})", err,
				name+" does not apply to "+dt.Label+". Clear it.")
		}
	}
	// The literal wording for the case's example.
	err := auth.ValidateRow(t3Item, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightSubmit: auth.ScopeAll})
	t3WantProblem(t, "ValidateRow(Item, {read, submit})", err, "Submit does not apply to Item. Clear it.")

	// Purchase Order is not importable.
	err = auth.ValidateRow(t3PO, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightImport: auth.ScopeAll})
	t3WantProblem(t, "ValidateRow(PO, {read, import})", err, "Import does not apply to Purchase Order. Clear it.")

	// Order of checks: the first inapplicable right in AllRights order wins,
	// and comes before the read-required check.
	err = auth.ValidateRow(t3Item, map[auth.Right]auth.Scope{auth.RightApprove: auth.ScopeAll, auth.RightSubmit: auth.ScopeAll})
	t3WantProblem(t, "ValidateRow(Item, {approve, submit}) without read", err, "Submit does not apply to Item. Clear it.")

	// Rights that do apply to a master are accepted.
	if err := auth.ValidateRow(t3Item, map[auth.Right]auth.Scope{
		auth.RightRead: auth.ScopeAll, auth.RightCreate: auth.ScopeAll, auth.RightWrite: auth.ScopeAll,
		auth.RightDelete: auth.ScopeAll, auth.RightExport: auth.ScopeAll, auth.RightImport: auth.ScopeAll,
	}); err != nil {
		t.Errorf("ValidateRow(Item, {read, create, write, delete, export, import}) = %v, want nil", err)
	}
	// Can(admin, Item, submit) → ErrForbidden is asserted in Task 5 (Can does not exist yet).
}

// AUTH-119
func TestMatrix_TeamOnlyWithEmployeeField(t *testing.T) {
	const teamMsg = "Team does not apply to Purchase Order. Choose Own or All."

	err := auth.ValidateRow(t3PO, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeTeam})
	t3WantProblem(t, "ValidateRow(PO, {read: team})", err, teamMsg)

	// Team is checked before read-required.
	err = auth.ValidateRow(t3PO, map[auth.Right]auth.Scope{auth.RightPrint: auth.ScopeTeam})
	t3WantProblem(t, "ValidateRow(PO, {print: team}) without read", err, teamMsg)

	if err := auth.ValidateRow(t3Claim, map[auth.Right]auth.Scope{
		auth.RightRead: auth.ScopeTeam, auth.RightApprove: auth.ScopeTeam,
	}); err != nil {
		t.Errorf("ValidateRow(Claim, {read: team, approve: team}) = %v, want nil", err)
	}

	if got, want := auth.ScopeOptions(t3PO), []auth.Scope{auth.ScopeOwn, auth.ScopeAll}; !slices.Equal(got, want) {
		t.Errorf("ScopeOptions(PO) = %v, want %v", got, want)
	}
	if got, want := auth.ScopeOptions(t3Claim), []auth.Scope{auth.ScopeOwn, auth.ScopeTeam, auth.ScopeAll}; !slices.Equal(got, want) {
		t.Errorf("ScopeOptions(Claim) = %v, want %v", got, want)
	}

	// Through SetRights: the save fails and nothing changes.
	t3Register(doctype.DocType{
		Name: "t3.TeamPO", Label: "Purchase Order", PluralLabel: "Purchase Orders",
		Submittable: true, Approvable: true,
	})
	t3Register(doctype.DocType{
		Name: "t3.TeamClaim", Label: "Expense Claim", PluralLabel: "Expense Claims",
		Submittable: true, Approvable: true, EmployeeField: "employee_id",
	})
	a, d, _, _ := t3Auth(t)
	ctx := context.Background()
	admin := testkit.Principal(t, "admin@vardhan.test")

	before := t3Rows(t, d, auth.RoleEmployee, "t3.TeamPO")
	err = a.SetRights(ctx, admin, auth.RoleEmployee, "t3.TeamPO", map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeTeam})
	t3WantProblem(t, "SetRights(Employee, PO, {read: team})", err, teamMsg)
	if after := t3Rows(t, d, auth.RoleEmployee, "t3.TeamPO"); !maps.Equal(before, after) {
		t.Errorf("role_rights(Employee, PO) after refused SetRights = %v, want unchanged %v", after, before)
	}

	want := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeTeam, auth.RightApprove: auth.ScopeTeam}
	if err := a.SetRights(ctx, admin, auth.RoleEmployee, "t3.TeamClaim", want); err != nil {
		t.Fatalf("SetRights(Employee, Claim, {read: team, approve: team}) = %v, want nil", err)
	}
	got, err := a.RoleRights(ctx, auth.RoleEmployee, "t3.TeamClaim")
	if err != nil || !t3Equal(got, want) {
		t.Errorf("RoleRights(Employee, Claim) = (%v, %v), want (%v, nil)", got, err, want)
	}
}

func TestMatrix_SeedDefaultsIdempotentAndCacheBumps(t *testing.T) {
	const dtName = "t3.Seed"
	const newRole = "t3 Seed Clerk"
	t3Register(doctype.DocType{
		Name: dtName, Label: "Seed Order", PluralLabel: "Seed Orders",
		Submittable: true, Approvable: true,
		DefaultRights: map[string]map[string]string{
			"Purchase User": {"read": "all", "create": "all", "export": "all", "delete": "own"},
			newRole:         {"read": "all"},
		},
	})
	a, d, clk, rec := t3Auth(t)
	ctx := context.Background()
	admin := testkit.Principal(t, "admin@vardhan.test")

	wantPU := map[string]string{"read": "all", "create": "all", "export": "all", "delete": "own"}
	wantNew := map[string]string{"read": "all"}

	v0 := t3PermVersion(t, d)
	if err := a.SeedDefaults(ctx, auth.SeedOptions{}); err != nil {
		t.Fatalf("SeedDefaults (first) = %v", err)
	}
	v1 := t3PermVersion(t, d)
	if v1 <= v0 {
		t.Errorf("perm_version after SeedDefaults = %d, want > %d", v1, v0)
	}
	if got := t3Rows(t, d, "Purchase User", dtName); !maps.Equal(got, wantPU) {
		t.Errorf("seeded role_rights(Purchase User, %s) = %v, want %v", dtName, got, wantPU)
	}
	if got := t3Rows(t, d, newRole, dtName); !maps.Equal(got, wantNew) {
		t.Errorf("seeded role_rights(%s, %s) = %v, want %v (missing role created)", newRole, dtName, got, wantNew)
	}
	if c, ok := t3RoleDocType(t, d, "Purchase User", dtName); !ok || c != 0 {
		t.Errorf("role_doctypes(Purchase User, %s) = (customised %d, exists %v), want (0, true)", dtName, c, ok)
	}

	// Second seed: same rows.
	if err := a.SeedDefaults(ctx, auth.SeedOptions{}); err != nil {
		t.Fatalf("SeedDefaults (second) = %v", err)
	}
	if got := t3Rows(t, d, "Purchase User", dtName); !maps.Equal(got, wantPU) {
		t.Errorf("role_rights(Purchase User, %s) after second seed = %v, want %v", dtName, got, wantPU)
	}
	if got := t3Rows(t, d, newRole, dtName); !maps.Equal(got, wantNew) {
		t.Errorf("role_rights(%s, %s) after second seed = %v, want %v", newRole, dtName, got, wantNew)
	}

	// SetRights replaces the row, marks it customised and bumps perm_version.
	vBefore := t3PermVersion(t, d)
	edited := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightCreate: auth.ScopeAll, auth.RightDelete: auth.ScopeOwn}
	if err := a.SetRights(ctx, admin, "Purchase User", dtName, edited); err != nil {
		t.Fatalf("SetRights(Purchase User, %s, without export) = %v", dtName, err)
	}
	if v := t3PermVersion(t, d); v <= vBefore {
		t.Errorf("perm_version after SetRights = %d, want > %d", v, vBefore)
	}
	got, err := a.RoleRights(ctx, "Purchase User", dtName)
	if err != nil || !t3Equal(got, edited) {
		t.Errorf("RoleRights(Purchase User, %s) right after SetRights = (%v, %v), want (%v, nil)", dtName, got, err, edited)
	}
	if c, ok := t3RoleDocType(t, d, "Purchase User", dtName); !ok || c != 1 {
		t.Errorf("role_doctypes(Purchase User, %s) after SetRights = (customised %d, exists %v), want (1, true)", dtName, c, ok)
	}

	// The audit entry for the change.
	entries := rec.Entries()
	if len(entries) != 1 {
		t.Fatalf("audit entries after one SetRights = %d (%+v), want 1", len(entries), entries)
	}
	e := entries[0]
	if e.DocType != "Role" || e.DocID != t3RoleID(t, d, "Purchase User") || e.Action != "rights" ||
		e.By != admin.ID || !e.At.Equal(clk.Now()) {
		t.Errorf("audit entry = {DocType %q, DocID %d, Action %q, By %d, At %v}, want {\"Role\", %d, \"rights\", %d, %v}",
			e.DocType, e.DocID, e.Action, e.By, e.At, t3RoleID(t, d, "Purchase User"), admin.ID, clk.Now())
	}
	wantChange := audit.Change{Field: dtName + ".export", Before: "all", After: "none", Op: "set"}
	if !slices.Contains(e.Changes, wantChange) {
		t.Errorf("audit changes = %+v, want them to contain %+v", e.Changes, wantChange)
	}

	// Re-seeding leaves the admin's edited row alone.
	if err := a.SeedDefaults(ctx, auth.SeedOptions{}); err != nil {
		t.Fatalf("SeedDefaults (after edit) = %v", err)
	}
	got, err = a.RoleRights(ctx, "Purchase User", dtName)
	if err != nil || !t3Equal(got, edited) {
		t.Errorf("RoleRights(Purchase User, %s) after re-seed = (%v, %v), want edited row %v", dtName, got, err, edited)
	}

	// An unknown scope string in DefaultRights is an error naming the DocType and role.
	t3Register(doctype.DocType{
		Name: "t3.BadScope", Label: "Bad Scope",
		DefaultRights: map[string]map[string]string{"t3 Bad Role": {"read": "some"}},
	})
	bad, _ := doctype.Lookup("t3.BadScope")
	bad.DefaultRights = map[string]map[string]string{"t3 Bad Role": {"read": "some"}} // a repeat run finds it neutralised
	// The registry is global: neutralise the bad row so later SeedDefaults calls work.
	t.Cleanup(func() { bad.DefaultRights = nil })
	a2, _, _, _ := t3Auth(t)
	err = a2.SeedDefaults(ctx, auth.SeedOptions{})
	if err == nil || !strings.Contains(err.Error(), "t3.BadScope") || !strings.Contains(err.Error(), "t3 Bad Role") {
		t.Errorf("SeedDefaults with scope \"some\" = %v, want an error naming \"t3.BadScope\" and \"t3 Bad Role\"", err)
	}
	bad.DefaultRights = nil

	// grant (unexported) reflecting the change immediately is not reachable from
	// package auth_test; Task 5's Can tests cover it.
}

func TestMatrix_LinkedMasterReadInProduction(t *testing.T) {
	t3Register(doctype.DocType{Name: "t3.Site", Label: "Site", PluralLabel: "Sites", Master: t3Master{}})
	t3Register(doctype.DocType{Name: "t3.LParty", Label: "Party", PluralLabel: "Parties", Master: t3Master{}})
	t3Register(doctype.DocType{Name: "t3.LOther", Label: "Other", PluralLabel: "Others"})
	t3Register(doctype.DocType{
		Name: "t3.Doc", Label: "Doc", PluralLabel: "Docs", Submittable: true,
		Fields: []doctype.Field{
			{Name: "site_id", Label: "Site", Column: "site_id", Type: doctype.Link, Link: "t3.Site"},
			{Name: "other_id", Label: "Other", Column: "other_id", Type: doctype.Link, Link: "t3.LOther"},
		},
		Links: []doctype.LinkSpec{{Field: "party_id", Target: "t3.LParty"}},
		DefaultRights: map[string]map[string]string{
			"Clerk":     {"create": "all", "read": "all"},
			"t3 Viewer": {"read": "all"},
		},
	})
	ctx := context.Background()

	// Test seed: no derived master reads.
	a, d, _, _ := t3Auth(t)
	if err := a.SeedDefaults(ctx, auth.SeedOptions{}); err != nil {
		t.Fatalf("SeedDefaults(SeedOptions{}) = %v", err)
	}
	for _, m := range []string{"t3.Site", "t3.LParty"} {
		if _, ok := t3RoleDocType(t, d, "Clerk", m); ok {
			t.Errorf("SeedOptions{}: role_doctypes has a Clerk row on %s, want none", m)
		}
		if got := t3Rows(t, d, "Clerk", m); len(got) != 0 {
			t.Errorf("SeedOptions{}: role_rights(Clerk, %s) = %v, want none", m, got)
		}
	}
	if got := t3Rows(t, d, "Clerk", "t3.Doc"); !maps.Equal(got, map[string]string{"create": "all", "read": "all"}) {
		t.Errorf("SeedOptions{}: role_rights(Clerk, t3.Doc) = %v, want {create: all, read: all}", got)
	}

	// Production seed on a fresh database.
	a2, d2, _, _ := t3Auth(t)
	if err := a2.SeedDefaults(ctx, auth.SeedOptions{LinkedMasterRead: true}); err != nil {
		t.Fatalf("SeedDefaults(LinkedMasterRead) = %v", err)
	}
	want := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll}
	for _, m := range []string{"t3.Site", "t3.LParty"} {
		got, err := a2.RoleRights(ctx, "Clerk", m)
		if err != nil || !t3Equal(got, want) {
			t.Errorf("LinkedMasterRead: RoleRights(Clerk, %s) = (%v, %v), want (%v, nil)", m, got, err, want)
		}
	}
	// Not a master: no derived read.
	if got := t3Rows(t, d2, "Clerk", "t3.LOther"); len(got) != 0 {
		t.Errorf("LinkedMasterRead: role_rights(Clerk, t3.LOther) = %v, want none (not a master)", got)
	}
	// A role without create gets no derived read.
	if got := t3Rows(t, d2, "t3 Viewer", "t3.Site"); len(got) != 0 {
		t.Errorf("LinkedMasterRead: role_rights(t3 Viewer, t3.Site) = %v, want none (no create)", got)
	}
}

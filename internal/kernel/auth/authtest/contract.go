package authtest

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// Setup prepares the state a contract row needs, through the real admin API
// or the fake's seeding helpers (WP3 Task 1, CD-182).
type Setup interface {
	// Admin returns an enabled Administrator (the same one on every call).
	Admin(t *testing.T) auth.Principal
	// User creates an enabled user holding roles (none: no roles at all;
	// missing roles are created) and returns its principal.
	User(t *testing.T, email string, roles ...string) auth.Principal
	// Grant adds r at scope s to role's row on doctype. Rows are granted
	// read first, as the matrix requires (AUTH-065).
	Grant(t *testing.T, role, doctype string, r auth.Right, s auth.Scope)
	// Restrict sets u's allowed projects and sites (nil: that dimension is
	// unrestricted). Called at most once per user.
	Restrict(t *testing.T, u auth.UserID, projects, sites []int64)
	// DB is a database with the probe tables (CreateProbeTables): rows for
	// evaluating Scope predicates, transactions for the tx methods, and the
	// probe_project and probe_site ids.
	DB() *db.DB
}

// Run runs the auth.Service contract rows against the service newSvc
// returns; each row gets a fresh service and Setup.
func Run(t *testing.T, newSvc func(t *testing.T) (auth.Service, Setup)) {
	t.Helper()
	RegisterDocTypes()
	for _, row := range contractRows {
		t.Run(row.name, func(t *testing.T) {
			svc, s := newSvc(t)
			row.run(t, svc, s)
		})
	}
	for _, row := range refusalRows {
		t.Run(row.name, func(t *testing.T) {
			svc, s := newSvc(t)
			row.run(t, svc, s.Admin(t), s.DB())
		})
	}
}

// RunRefusals runs only the refusal rows that hold whatever is granted:
// inapplicable rights, an unknown dimension and Forbidden. It is for
// doubles that allow everything by design (fixturetest).
func RunRefusals(t *testing.T, svc auth.Service) {
	t.Helper()
	RegisterDocTypes()
	admin := auth.Principal{ID: 1, Name: "Anil Kapoor", Roles: []string{auth.RoleAdministrator}}
	d := testkit.NewDB(t)
	for _, row := range refusalRows {
		t.Run(row.name, func(t *testing.T) { row.run(t, svc, admin, d) })
	}
}

// Roles the rows create; none of them is in the seeded matrix.
const (
	roleReader   = "Contract Reader"
	roleApprover = "Contract Approver"
	roleOwner    = "Contract Owner"
	roleOther    = "Contract Other"
)

// unknownRight is not in auth.AllRights (CD-171).
const unknownRight auth.Right = "foo"

var contractRows = []struct {
	name string
	run  func(t *testing.T, svc auth.Service, s Setup)
}{
	{"AUTH-064 deny by default", rowDenyByDefault},
	{"AUTH-071 granted right allowed", rowGrantedAllowed},
	{"AUTH-073 read but no right is forbidden", rowReadNoRight},
	{"AUTH-063 rights combine across roles", rowUnionAcrossRoles},
	{"AUTH-086 own scope other's record not found", rowOwnScope},
	{"AUTH-087 own-only delete is forbidden", rowOwnOnlyDelete},
	{"AUTH-085 own scope predicate", rowOwnScopePredicate},
	{"AUTH-092 project restriction is strict", rowProjectRestriction},
	{"AUTH-092 restriction predicate", rowRestrictionPredicate},
	{"AUTH-096 restriction never grants", rowRestrictionNoGrant},
	{"AUTH-069 Administrator allowed and unrestricted", rowAdminAll},
	{"AUTH-042 disabled user refused", rowDisabledUser},
	{"AUTH-116 UsersWithRight", rowUsersWithRight},
	{"CD-174 Restricted", rowRestricted},
}

var refusalRows = []struct {
	name string
	run  func(t *testing.T, svc auth.Service, admin auth.Principal, d *db.DB)
}{
	{"AUTH-066 inapplicable rights refused", rowInapplicable},
	{"CD-174 unknown dimension", rowUnknownDimension},
	{"CD-174 Forbidden", rowForbidden},
}

// order returns a probe Purchase Order record created by u.
func order(id int64, u auth.UserID, projects ...int64) *auth.RecordRef {
	return &auth.RecordRef{DocType: DTOrder, ID: id, CreatedBy: u, ProjectIDs: projects}
}

// masterID returns the id of code in a probe master table of d.
func masterID(t *testing.T, d *db.DB, table, code string) int64 {
	t.Helper()
	ctx := context.Background()
	var id int64
	if err := d.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, `SELECT id FROM `+table+` WHERE code = ?`, code).Scan(&id) //nolint:gosec // G202: table is a probe constant
	}); err != nil {
		t.Fatalf("authtest: %s %s: %v", table, code, err)
	}
	return id
}

// projects returns the ids of PRJ-METRO and PRJ-TECHPARK.
func projects(t *testing.T, s Setup) (metro, techpark int64) {
	t.Helper()
	return masterID(t, s.DB(), "probe_project", "PRJ-METRO"), masterID(t, s.DB(), "probe_project", "PRJ-TECHPARK")
}

// insertOrders inserts probe_order rows; project 0 is NULL.
func insertOrders(t *testing.T, s Setup, recs ...*auth.RecordRef) {
	t.Helper()
	ctx := context.Background()
	if err := s.DB().Write(ctx, func(tx *db.Tx) error {
		for _, r := range recs {
			var project any
			if len(r.ProjectIDs) > 0 {
				project = r.ProjectIDs[0]
			}
			if _, err := tx.SQL().ExecContext(ctx,
				`INSERT INTO probe_order (id, state, created_by, created_at, project_id) VALUES (?, ?, ?, ?, ?)`,
				r.ID, string(doctype.StateDraft), int64(r.CreatedBy), auth.FormatTime(T0), project); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("authtest: insert probe orders: %v", err)
	}
}

// visible returns the probe_order ids pred keeps, ascending.
func visible(t *testing.T, s Setup, pred auth.Predicate) []int64 {
	t.Helper()
	ctx := context.Background()
	q := `SELECT id FROM probe_order`
	if pred.SQL != "" {
		q += ` WHERE ` + pred.SQL
	}
	var ids []int64
	if err := s.DB().Read(ctx, func(tx *db.Tx) error {
		rows, err := tx.SQL().QueryContext(ctx, q+` ORDER BY id`, pred.Args...)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("authtest: evaluate predicate %q %v: %v", pred.SQL, pred.Args, err)
	}
	return ids
}

// wantDenied checks that err is the real service's 403: a *auth.DeniedError
// for r with the given own-only flag, ErrForbidden and not ErrNotFound.
func wantDenied(t *testing.T, what string, err error, r auth.Right, ownOnly bool) {
	t.Helper()
	var de *auth.DeniedError
	if !errors.As(err, &de) {
		t.Errorf("%s: err = %v (%T), want *auth.DeniedError (403)", what, err, err)
		return
	}
	if de.Right != r || de.OwnOnly != ownOnly || de.TeamOnly {
		t.Errorf("%s: DeniedError{Right %q, OwnOnly %v, TeamOnly %v}, want {Right %q, OwnOnly %v, TeamOnly false}",
			what, de.Right, de.OwnOnly, de.TeamOnly, r, ownOnly)
	}
	if !errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrNotFound) {
		t.Errorf("%s: err = %v, want errors.Is ErrForbidden and not ErrNotFound", what, err)
	}
}

// wantNotFound checks that err is ErrNotFound (404) and not ErrForbidden.
func wantNotFound(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, auth.ErrNotFound) || errors.Is(err, auth.ErrForbidden) {
		t.Errorf("%s: err = %v, want auth.ErrNotFound (404), not ErrForbidden", what, err)
	}
}

func wantNil(t *testing.T, what string, err error) {
	t.Helper()
	if err != nil {
		t.Errorf("%s: err = %v, want nil", what, err)
	}
}

// AUTH-064: no role, or a role with rights only elsewhere, gets a 403 on
// the DocType, with or without a record.
func rowDenyByDefault(t *testing.T, svc auth.Service, s Setup) {
	ctx := context.Background()
	s.Grant(t, roleOther, DTReceipt, auth.RightRead, auth.ScopeAll)
	admin := s.Admin(t)
	for _, p := range []auth.Principal{s.User(t, "none@contract.test"), s.User(t, "other@contract.test", roleOther)} {
		for _, r := range []auth.Right{auth.RightRead, auth.RightCreate, auth.RightApprove} {
			wantDenied(t, fmt.Sprintf("%s Can(Order, %s, nil)", p.Name, r), svc.Can(ctx, p, DTOrder, r, nil), r, false)
			wantDenied(t, fmt.Sprintf("%s Can(Order, %s, record)", p.Name, r), svc.Can(ctx, p, DTOrder, r, order(1, admin.ID)), r, false)
		}
		_, err := svc.Scope(ctx, p, DTOrder, auth.RightRead)
		wantDenied(t, p.Name+" Scope(Order, read)", err, auth.RightRead, false)
	}
}

// AUTH-071: a granted right at scope all is allowed on the DocType and on a
// record, and its list has no filter.
func rowGrantedAllowed(t *testing.T, svc auth.Service, s Setup) {
	ctx := context.Background()
	s.Grant(t, roleReader, DTOrder, auth.RightRead, auth.ScopeAll)
	s.Grant(t, roleReader, DTOrder, auth.RightPrint, auth.ScopeAll)
	p := s.User(t, "reader@contract.test", roleReader)
	rec := order(1, s.Admin(t).ID)
	for _, r := range []auth.Right{auth.RightRead, auth.RightPrint} {
		wantNil(t, "Can(Order, "+string(r)+", nil)", svc.Can(ctx, p, DTOrder, r, nil))
		wantNil(t, "Can(Order, "+string(r)+", other's record)", svc.Can(ctx, p, DTOrder, r, rec))
	}
	pred, err := svc.Scope(ctx, p, DTOrder, auth.RightRead)
	if err != nil || pred.SQL != "" || len(pred.Args) != 0 {
		t.Errorf("Scope(Order, read) = ({%q, %v}, %v), want an empty Predicate and nil", pred.SQL, pred.Args, err)
	}
	// Control: the grant is what allows it.
	none := s.User(t, "none@contract.test")
	wantDenied(t, "no role Can(Order, read, other's record)", svc.Can(ctx, none, DTOrder, auth.RightRead, rec), auth.RightRead, false)
}

// AUTH-073: read without write on a visible record is a 403, not a 404.
func rowReadNoRight(t *testing.T, svc auth.Service, s Setup) {
	ctx := context.Background()
	s.Grant(t, roleReader, DTOrder, auth.RightRead, auth.ScopeAll)
	p := s.User(t, "reader@contract.test", roleReader)
	rec := order(1, s.Admin(t).ID)
	for _, r := range []auth.Right{auth.RightWrite, auth.RightSubmit, auth.RightApprove, auth.RightDelete} {
		wantDenied(t, "Can(Order, "+string(r)+", visible record)", svc.Can(ctx, p, DTOrder, r, rec), r, false)
		wantDenied(t, "Can(Order, "+string(r)+", nil)", svc.Can(ctx, p, DTOrder, r, nil), r, false)
		_, err := svc.Scope(ctx, p, DTOrder, r)
		wantDenied(t, "Scope(Order, "+string(r)+")", err, r, false)
	}
}

// AUTH-063: rights from several roles combine with OR.
func rowUnionAcrossRoles(t *testing.T, svc auth.Service, s Setup) {
	ctx := context.Background()
	s.Grant(t, roleReader, DTOrder, auth.RightRead, auth.ScopeAll)
	s.Grant(t, roleApprover, DTOrder, auth.RightRead, auth.ScopeAll)
	s.Grant(t, roleApprover, DTOrder, auth.RightApprove, auth.ScopeAll)
	rec := order(1, s.Admin(t).ID)
	both := s.User(t, "both@contract.test", roleReader, roleApprover)
	wantNil(t, "Reader + Approver Can(Order, approve, record)", svc.Can(ctx, both, DTOrder, auth.RightApprove, rec))
	reader := s.User(t, "reader@contract.test", roleReader)
	wantDenied(t, "Reader Can(Order, approve, record)", svc.Can(ctx, reader, DTOrder, auth.RightApprove, rec), auth.RightApprove, false)
}

// AUTH-086: with read and write at scope own, my record is allowed and
// another person's is not found.
func rowOwnScope(t *testing.T, svc auth.Service, s Setup) {
	ctx := context.Background()
	s.Grant(t, roleOwner, DTOrder, auth.RightRead, auth.ScopeOwn)
	s.Grant(t, roleOwner, DTOrder, auth.RightWrite, auth.ScopeOwn)
	p := s.User(t, "owner@contract.test", roleOwner)
	mine, theirs := order(1, p.ID), order(2, s.Admin(t).ID)
	for _, r := range []auth.Right{auth.RightRead, auth.RightWrite} {
		wantNil(t, "Can(Order, "+string(r)+", own record)", svc.Can(ctx, p, DTOrder, r, mine))
		wantNotFound(t, "Can(Order, "+string(r)+", other's record)", svc.Can(ctx, p, DTOrder, r, theirs))
	}
}

// AUTH-087: read all with delete own: deleting a visible record of someone
// else is a 403 naming own scope; my own is allowed; create is unaffected.
func rowOwnOnlyDelete(t *testing.T, svc auth.Service, s Setup) {
	ctx := context.Background()
	s.Grant(t, roleOwner, DTOrder, auth.RightRead, auth.ScopeAll)
	s.Grant(t, roleOwner, DTOrder, auth.RightCreate, auth.ScopeAll)
	s.Grant(t, roleOwner, DTOrder, auth.RightDelete, auth.ScopeOwn)
	p := s.User(t, "owner@contract.test", roleOwner)
	wantDenied(t, "Can(Order, delete, other's record)",
		svc.Can(ctx, p, DTOrder, auth.RightDelete, order(2, s.Admin(t).ID)), auth.RightDelete, true)
	wantNil(t, "Can(Order, delete, own record)", svc.Can(ctx, p, DTOrder, auth.RightDelete, order(1, p.ID)))
	wantNil(t, "Can(Order, create, nil)", svc.Can(ctx, p, DTOrder, auth.RightCreate, nil))
}

// AUTH-085: the read Scope of an own-scope user keeps only their rows.
func rowOwnScopePredicate(t *testing.T, svc auth.Service, s Setup) {
	ctx := context.Background()
	s.Grant(t, roleOwner, DTOrder, auth.RightRead, auth.ScopeOwn)
	p := s.User(t, "owner@contract.test", roleOwner)
	insertOrders(t, s, order(1, p.ID), order(2, s.Admin(t).ID))
	pred, err := svc.Scope(ctx, p, DTOrder, auth.RightRead)
	if err != nil {
		t.Fatalf("Scope(Order, read) = %v, want nil", err)
	}
	if got := visible(t, s, pred); !slices.Equal(got, []int64{1}) {
		t.Errorf("Scope(Order, read) {%q, %v} keeps orders %v, want [1] (own only)", pred.SQL, pred.Args, got)
	}
}

// AUTH-092, AUTH-093: restricted to PRJ-METRO, a record on PRJ-TECHPARK, on
// both, or with no project is not found; one on PRJ-METRO is allowed.
func rowProjectRestriction(t *testing.T, svc auth.Service, s Setup) {
	ctx := context.Background()
	metro, techpark := projects(t, s)
	s.Grant(t, roleReader, DTOrder, auth.RightRead, auth.ScopeAll)
	p := s.User(t, "restricted@contract.test", roleReader)
	s.Restrict(t, p.ID, []int64{metro}, nil)
	admin := s.Admin(t).ID
	wantNil(t, "Can(Order, read, PRJ-METRO)", svc.Can(ctx, p, DTOrder, auth.RightRead, order(1, admin, metro)))
	wantNotFound(t, "Can(Order, read, PRJ-TECHPARK)", svc.Can(ctx, p, DTOrder, auth.RightRead, order(2, admin, techpark)))
	wantNotFound(t, "Can(Order, read, PRJ-METRO and PRJ-TECHPARK)", svc.Can(ctx, p, DTOrder, auth.RightRead, order(3, admin, metro, techpark)))
	wantNotFound(t, "Can(Order, read, no project) (strict, AUTH-093)", svc.Can(ctx, p, DTOrder, auth.RightRead, order(4, admin)))
}

// AUTH-092, AUTH-093: the restricted user's read Scope keeps the PRJ-METRO
// order only, not the PRJ-TECHPARK one or the one with no project.
func rowRestrictionPredicate(t *testing.T, svc auth.Service, s Setup) {
	ctx := context.Background()
	metro, techpark := projects(t, s)
	s.Grant(t, roleReader, DTOrder, auth.RightRead, auth.ScopeAll)
	p := s.User(t, "restricted@contract.test", roleReader)
	s.Restrict(t, p.ID, []int64{metro}, nil)
	admin := s.Admin(t).ID
	insertOrders(t, s, order(11, admin, metro), order(12, admin, techpark), order(13, admin))
	pred, err := svc.Scope(ctx, p, DTOrder, auth.RightRead)
	if err != nil {
		t.Fatalf("Scope(Order, read) = %v, want nil", err)
	}
	if got := visible(t, s, pred); !slices.Equal(got, []int64{11}) {
		t.Errorf("Scope(Order, read) {%q, %v} keeps orders %v, want [11] (PRJ-METRO only)", pred.SQL, pred.Args, got)
	}
}

// AUTH-096: a restriction on a user with no Order rights still gives a 403.
func rowRestrictionNoGrant(t *testing.T, svc auth.Service, s Setup) {
	ctx := context.Background()
	metro, _ := projects(t, s)
	p := s.User(t, "restricted@contract.test")
	s.Restrict(t, p.ID, []int64{metro}, nil)
	wantDenied(t, "Can(Order, read, PRJ-METRO)",
		svc.Can(ctx, p, DTOrder, auth.RightRead, order(1, s.Admin(t).ID, metro)), auth.RightRead, false)
}

// AUTH-069: the Administrator, even with restriction rows, may use every
// applicable Order right on a record outside them, and its list has no
// filter.
func rowAdminAll(t *testing.T, svc auth.Service, s Setup) {
	ctx := context.Background()
	metro, techpark := projects(t, s)
	admin := s.Admin(t)
	s.Restrict(t, admin.ID, []int64{metro}, nil)
	dt, _ := doctype.Lookup(DTOrder)
	rec := order(1, 9999, techpark)
	for _, r := range auth.AllRights {
		if !auth.Applies(dt, r) {
			continue
		}
		wantNil(t, "admin Can(Order, "+string(r)+", PRJ-TECHPARK record)", svc.Can(ctx, admin, DTOrder, r, rec))
		pred, err := svc.Scope(ctx, admin, DTOrder, r)
		if err != nil || pred.SQL != "" || len(pred.Args) != 0 {
			t.Errorf("admin Scope(Order, %s) = ({%q, %v}, %v), want an empty Predicate and nil", r, pred.SQL, pred.Args, err)
		}
	}
	// Control: the same restriction hides the record from a reader.
	s.Grant(t, roleReader, DTOrder, auth.RightRead, auth.ScopeAll)
	reader := s.User(t, "reader@contract.test", roleReader)
	s.Restrict(t, reader.ID, []int64{metro}, nil)
	wantNotFound(t, "restricted reader Can(Order, read, PRJ-TECHPARK record)", svc.Can(ctx, reader, DTOrder, auth.RightRead, rec))
}

// AUTH-042, AUTH-120: a disabled user reads as disabled and drops out of
// UsersWithRight until enabled again; disabling a missing user is not found.
func rowDisabledUser(t *testing.T, svc auth.Service, s Setup) {
	ctx := context.Background()
	s.Grant(t, roleApprover, DTOrder, auth.RightRead, auth.ScopeAll)
	s.Grant(t, roleApprover, DTOrder, auth.RightApprove, auth.ScopeAll)
	p := s.User(t, "approver@contract.test", roleApprover)
	listed := func(what string, want bool) {
		t.Helper()
		got, err := svc.UsersWithRight(ctx, DTOrder, auth.RightApprove, nil)
		if err != nil {
			t.Fatalf("%s: UsersWithRight: %v", what, err)
		}
		if slices.Contains(got, p.ID) != want {
			t.Errorf("%s: UsersWithRight(Order, approve) = %v, lists user %d: %v, want %v", what, got, p.ID, !want, want)
		}
	}
	enabled := func(what string, want bool) {
		t.Helper()
		var info auth.UserInfo
		var ok bool
		if err := s.DB().Read(ctx, func(tx *db.Tx) error {
			var err error
			info, ok, err = svc.User(ctx, tx, p.ID)
			return err
		}); err != nil || !ok || info.Enabled != want {
			t.Errorf("%s: User(%d) = (%+v, %v, %v), want Enabled %v, true, nil", what, p.ID, info, ok, err, want)
		}
	}
	write := func(fn func(tx *db.Tx) error) error { return s.DB().Write(ctx, fn) }

	enabled("before", true)
	listed("before", true)
	if err := write(func(tx *db.Tx) error { return svc.DisableUser(ctx, tx, p.ID, "left") }); err != nil {
		t.Fatalf("DisableUser: %v", err)
	}
	enabled("disabled", false)
	listed("disabled", false)
	if err := write(func(tx *db.Tx) error { return svc.EnableUser(ctx, tx, p.ID) }); err != nil {
		t.Fatalf("EnableUser: %v", err)
	}
	enabled("enabled again", true)
	listed("enabled again", true)

	err := write(func(tx *db.Tx) error { return svc.DisableUser(ctx, tx, 99999, "left") })
	wantNotFound(t, "DisableUser(99999)", err)
}

// AUTH-116, AUTH-120: UsersWithRight lists the enabled users Can allows,
// ascending: the Administrator and the approvers, not a user without the
// right, nor a restricted approver for a record outside the restriction;
// nobody for a right that doesn't apply.
func rowUsersWithRight(t *testing.T, svc auth.Service, s Setup) {
	ctx := context.Background()
	metro, techpark := projects(t, s)
	s.Grant(t, roleApprover, DTOrder, auth.RightRead, auth.ScopeAll)
	s.Grant(t, roleApprover, DTOrder, auth.RightApprove, auth.ScopeAll)
	admin := s.Admin(t)
	approver := s.User(t, "approver@contract.test", roleApprover)
	none := s.User(t, "none@contract.test")
	restricted := s.User(t, "restricted@contract.test", roleApprover)
	s.Restrict(t, restricted.ID, []int64{metro}, nil)

	check := func(what string, rec *auth.RecordRef, in, out []auth.Principal) {
		t.Helper()
		got, err := svc.UsersWithRight(ctx, DTOrder, auth.RightApprove, rec)
		if err != nil {
			t.Fatalf("%s: UsersWithRight: %v", what, err)
		}
		if !slices.IsSorted(got) {
			t.Errorf("%s: UsersWithRight = %v, want ascending ids", what, got)
		}
		for _, p := range in {
			if !slices.Contains(got, p.ID) {
				t.Errorf("%s: UsersWithRight = %v, want it to list %s (%d)", what, got, p.Name, p.ID)
			}
		}
		for _, p := range out {
			if slices.Contains(got, p.ID) {
				t.Errorf("%s: UsersWithRight = %v, want it not to list %s (%d)", what, got, p.Name, p.ID)
			}
		}
	}
	check("no record", nil, []auth.Principal{admin, approver, restricted}, []auth.Principal{none})
	check("PRJ-METRO record", order(1, admin.ID, metro), []auth.Principal{admin, approver, restricted}, []auth.Principal{none})
	check("PRJ-TECHPARK record", order(2, admin.ID, techpark), []auth.Principal{admin, approver}, []auth.Principal{none, restricted})

	got, err := svc.UsersWithRight(ctx, DTItem, auth.RightApprove, nil)
	if err != nil || len(got) != 0 {
		t.Errorf("UsersWithRight(Item, approve) = (%v, %v), want nobody and nil (approve doesn't apply to a master)", got, err)
	}
}

// CD-174: Restricted is true only on a dimension the user has a restriction
// on, and false for the Administrator even with restriction rows.
func rowRestricted(t *testing.T, svc auth.Service, s Setup) {
	d := s.DB()
	metro, _ := projects(t, s)
	site := masterID(t, d, "probe_site", "PUNE-WH")
	restricted := s.User(t, "restricted@contract.test")
	s.Restrict(t, restricted.ID, []int64{metro}, nil)
	open := s.User(t, "open@contract.test")
	admin := s.Admin(t)
	s.Restrict(t, admin.ID, []int64{metro}, []int64{site})
	cases := []struct {
		p    auth.Principal
		dim  auth.Dimension
		want bool
	}{
		{restricted, auth.DimProject, true},
		{restricted, auth.DimSite, false},
		{open, auth.DimProject, false},
		{open, auth.DimSite, false},
		{admin, auth.DimProject, false},
		{admin, auth.DimSite, false},
	}
	for _, c := range cases {
		got, err := restrictedIn(t, svc, d, c.p, string(c.dim))
		if err != nil || got != c.want {
			t.Errorf("Restricted(%s, %s) = (%v, %v), want (%v, nil)", c.p.Name, c.dim, got, err, c.want)
		}
	}
}

// restrictedIn calls svc.Restricted inside a read transaction of d.
func restrictedIn(t *testing.T, svc auth.Service, d *db.DB, p auth.Principal, dim string) (bool, error) {
	t.Helper()
	ctx := context.Background()
	var got bool
	var callErr error
	if err := d.Read(ctx, func(tx *db.Tx) error {
		got, callErr = svc.Restricted(ctx, tx, p, dim)
		return nil
	}); err != nil {
		t.Fatalf("authtest: Read: %v", err)
	}
	return got, callErr
}

// AUTH-066, CD-171, CD-173: an unknown right, submit on a master and approve
// on a DocType that isn't approvable are a 403 in Can and Scope and list
// nobody in UsersWithRight, for the Administrator too.
func rowInapplicable(t *testing.T, svc auth.Service, admin auth.Principal, _ *db.DB) {
	ctx := context.Background()
	cases := []struct {
		dt string
		r  auth.Right
	}{
		{DTOrder, unknownRight},
		{DTItem, auth.RightSubmit},
		{DTItem, auth.RightApprove},
		{DTReceipt, auth.RightApprove},
	}
	for _, c := range cases {
		what := fmt.Sprintf("admin %%s(%s, %s)", c.dt, c.r)
		wantDenied(t, fmt.Sprintf(what, "Can")+" nil", svc.Can(ctx, admin, c.dt, c.r, nil), c.r, false)
		rec := &auth.RecordRef{DocType: c.dt, ID: 1, CreatedBy: admin.ID}
		wantDenied(t, fmt.Sprintf(what, "Can")+" own record", svc.Can(ctx, admin, c.dt, c.r, rec), c.r, false)
		_, err := svc.Scope(ctx, admin, c.dt, c.r)
		wantDenied(t, fmt.Sprintf(what, "Scope"), err, c.r, false)
		users, err := svc.UsersWithRight(ctx, c.dt, c.r, nil)
		if err != nil || len(users) != 0 {
			t.Errorf("UsersWithRight(%s, %s) = (%v, %v), want nobody and nil", c.dt, c.r, users, err)
		}
	}
}

// CD-174: an unknown dimension is a caller bug: a plain error naming it,
// not a 403 or 404, for the Administrator too.
func rowUnknownDimension(t *testing.T, svc auth.Service, admin auth.Principal, d *db.DB) {
	plain := auth.Principal{ID: admin.ID + 1000, Name: "Plain User"}
	for _, p := range []auth.Principal{plain, admin} {
		for _, dim := range []string{"region", ""} {
			got, err := restrictedIn(t, svc, d, p, dim)
			what := fmt.Sprintf("Restricted(%s, %q)", p.Name, dim)
			if got {
				t.Errorf("%s = true, want false with the error", what)
			}
			if err == nil {
				t.Errorf("%s: err = nil, want an error (unknown dimension)", what)
				continue
			}
			var he doctype.HTTPError
			if errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrNotFound) || errors.As(err, &he) {
				t.Errorf("%s: err = %v (%T), want a plain internal error, not a 403, 404 or HTTPError", what, err, err)
			}
			if q := fmt.Sprintf("%q", dim); !strings.Contains(err.Error(), q) {
				t.Errorf("%s: message %q, want it to name the dimension %s", what, err.Error(), q)
			}
		}
	}
}

// CD-174: Forbidden(msg) is a 403 showing exactly msg that errors.Is
// ErrForbidden, also when wrapped.
func rowForbidden(t *testing.T, _ auth.Service, _ auth.Principal, _ *db.DB) {
	const msg = "You can only work with your assigned projects. Ask your administrator to add this Project."
	err := auth.Forbidden(msg)
	if !errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrNotFound) {
		t.Errorf("Forbidden(msg) = %v, want errors.Is ErrForbidden and not ErrNotFound", err)
	}
	if !errors.Is(fmt.Errorf("save: %w", err), auth.ErrForbidden) {
		t.Errorf("wrapped Forbidden(msg) is not ErrForbidden")
	}
	if err == nil || err.Error() != msg {
		t.Errorf("Forbidden(msg) = %v, want the message %q", err, msg)
	}
	var he doctype.HTTPError
	if !errors.As(err, &he) || he.Status() != http.StatusForbidden {
		t.Errorf("Forbidden(msg) = %v (%T), want a doctype.HTTPError with status 403", err, err)
	}
}

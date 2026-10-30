package authtest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
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
	// Directory is the auth.Directory the service reads employees and
	// reporting lines from; rows link users and set managers on it.
	Directory() *authfake.Directory
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

// RunRefusals runs only the rows that hold whatever is granted: inapplicable
// rights, an unknown dimension and CD-199's UserByEmail, whose users add
// creates. It is for doubles that allow everything by design (fixturetest).
func RunRefusals(t *testing.T, svc auth.Service, add func(email string) auth.Principal) {
	t.Helper()
	RegisterDocTypes()
	admin := auth.Principal{ID: 1, Name: "Anil Kapoor", Roles: []string{auth.RoleAdministrator}}
	d := testkit.NewDB(t)
	for _, row := range refusalRows {
		t.Run(row.name, func(t *testing.T) { row.run(t, svc, admin, d) })
	}
	t.Run("CD-199 UserByEmail", func(t *testing.T) { checkUserByEmail(t, svc, d, add) })
	RunUnknownDocType(t, svc, admin)
}

// RunUnknownDocType runs the CD-171 unknown-DocType row. It is not in Run:
// authfake answers an unregistered DocType with a 403 by design (CD-157).
func RunUnknownDocType(t *testing.T, svc auth.Service, admin auth.Principal) {
	t.Helper()
	RegisterDocTypes()
	t.Run("CD-171 unknown DocType", func(t *testing.T) { rowUnknownDocType(t, svc, admin, nil) })
	t.Run("CD-213 unknown DocType in a tx", func(t *testing.T) { rowUnknownDocTypeTx(t, svc, admin, testkit.NewDB(t)) })
}

// Roles the rows create; none of them is in the seeded matrix.
const (
	roleReader   = "Contract Reader"
	roleApprover = "Contract Approver"
	roleOwner    = "Contract Owner"
	roleOther    = "Contract Other"
	roleTeam     = "Contract Team"
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
	{"AUTH-116 team scope: direct reports only", rowTeamScope},
	{"D-AUTH-12 own via employee", rowOwnViaEmployee},
	{"AUTH-116 team scope predicate", rowTeamScopePredicate},
	{"AUTH-087 own-only write on a teammate's claim", rowOwnOnlyWriteTeammate},
	{"AUTH-087 team-only write outside the team", rowTeamOnlyWrite},
	{"AUTH-092 project restriction is strict", rowProjectRestriction},
	{"AUTH-092 restriction predicate", rowRestrictionPredicate},
	{"AUTH-093 site restriction is strict", rowSiteRestriction},
	{"AUTH-096 restriction never grants", rowRestrictionNoGrant},
	{"AUTH-069 Administrator allowed and unrestricted", rowAdminAll},
	{"AUTH-042 disabled user refused", rowDisabledUser},
	{"AUTH-116 UsersWithRight", rowUsersWithRight},
	{"CD-174 Restricted", rowRestricted},
	{"CD-199 UserByEmail", rowUserByEmail},
	{"CD-213 Tx methods match their twins", rowTxParity},
}

var refusalRows = []struct {
	name string
	run  func(t *testing.T, svc auth.Service, admin auth.Principal, d *db.DB)
}{
	{"AUTH-066 inapplicable rights refused", rowInapplicable},
	{"CD-174 unknown dimension", rowUnknownDimension},
	{"CD-213 inapplicable rights refused in a tx", rowInapplicableTx},
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

// insertClaims inserts probe_claim rows; employee 0 is NULL.
func insertClaims(t *testing.T, s Setup, recs ...*auth.RecordRef) {
	t.Helper()
	ctx := context.Background()
	if err := s.DB().Write(ctx, func(tx *db.Tx) error {
		for _, r := range recs {
			var employee any
			if r.EmployeeID != 0 {
				employee = r.EmployeeID
			}
			if _, err := tx.SQL().ExecContext(ctx,
				`INSERT INTO probe_claim (id, state, created_by, created_at, employee_id) VALUES (?, ?, ?, ?, ?)`,
				r.ID, string(doctype.StateDraft), int64(r.CreatedBy), auth.FormatTime(T0), employee); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("authtest: insert probe claims: %v", err)
	}
}

// visible returns the probe_order ids pred keeps, ascending.
func visible(t *testing.T, s Setup, pred auth.Predicate) []int64 {
	t.Helper()
	return visibleIn(t, s, "probe_order", pred)
}

// visibleIn returns the ids of the probe table rows pred keeps, ascending.
func visibleIn(t *testing.T, s Setup, table string, pred auth.Predicate) []int64 {
	t.Helper()
	ctx := context.Background()
	q := `SELECT id FROM ` + table
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
// for r, ErrForbidden and not ErrNotFound, naming scope only (own, team or
// "" for neither) as the scope the right is limited to.
func wantDenied(t *testing.T, what string, err error, r auth.Right, only auth.Scope) {
	t.Helper()
	var de *auth.DeniedError
	if !errors.As(err, &de) {
		t.Errorf("%s: err = %v (%T), want *auth.DeniedError (403)", what, err, err)
		return
	}
	ownOnly, teamOnly := only == auth.ScopeOwn, only == auth.ScopeTeam
	if de.Right != r || de.OwnOnly != ownOnly || de.TeamOnly != teamOnly {
		t.Errorf("%s: DeniedError{Right %q, OwnOnly %v, TeamOnly %v}, want {Right %q, OwnOnly %v, TeamOnly %v}",
			what, de.Right, de.OwnOnly, de.TeamOnly, r, ownOnly, teamOnly)
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
			wantDenied(t, fmt.Sprintf("%s Can(Order, %s, nil)", p.Name, r), svc.Can(ctx, p, DTOrder, r, nil), r, "")
			wantDenied(t, fmt.Sprintf("%s Can(Order, %s, record)", p.Name, r), svc.Can(ctx, p, DTOrder, r, order(1, admin.ID)), r, "")
		}
		_, err := svc.Scope(ctx, p, DTOrder, auth.RightRead)
		wantDenied(t, p.Name+" Scope(Order, read)", err, auth.RightRead, "")
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
	wantDenied(t, "no role Can(Order, read, other's record)", svc.Can(ctx, none, DTOrder, auth.RightRead, rec), auth.RightRead, "")
}

// AUTH-073: read without write on a visible record is a 403, not a 404.
func rowReadNoRight(t *testing.T, svc auth.Service, s Setup) {
	ctx := context.Background()
	s.Grant(t, roleReader, DTOrder, auth.RightRead, auth.ScopeAll)
	p := s.User(t, "reader@contract.test", roleReader)
	rec := order(1, s.Admin(t).ID)
	for _, r := range []auth.Right{auth.RightWrite, auth.RightSubmit, auth.RightApprove, auth.RightDelete} {
		wantDenied(t, "Can(Order, "+string(r)+", visible record)", svc.Can(ctx, p, DTOrder, r, rec), r, "")
		wantDenied(t, "Can(Order, "+string(r)+", nil)", svc.Can(ctx, p, DTOrder, r, nil), r, "")
		_, err := svc.Scope(ctx, p, DTOrder, r)
		wantDenied(t, "Scope(Order, "+string(r)+")", err, r, "")
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
	wantDenied(t, "Reader Can(Order, approve, record)", svc.Can(ctx, reader, DTOrder, auth.RightApprove, rec), auth.RightApprove, "")
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
		svc.Can(ctx, p, DTOrder, auth.RightDelete, order(2, s.Admin(t).ID)), auth.RightDelete, auth.ScopeOwn)
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

// Employees the team rows link in the Setup's Directory: report and
// indirect report up to manager; stranger reports to nobody.
const (
	empManager  int64 = 9001
	empReport   int64 = 9002
	empStranger int64 = 9003
	empIndirect int64 = 9004
)

// team creates the manager, the report and the stranger, each holding
// role, and links them to their employees: report and indirect report up
// to the manager.
func team(t *testing.T, s Setup, role string) (manager, report, stranger auth.Principal) {
	t.Helper()
	manager = s.User(t, "manager@contract.test", role)
	report = s.User(t, "report@contract.test", role)
	stranger = s.User(t, "stranger@contract.test", role)
	d := s.Directory()
	d.Link(manager.ID, empManager)
	d.Link(report.ID, empReport)
	d.Link(stranger.ID, empStranger)
	d.SetManager(empReport, empManager)
	d.SetManager(empIndirect, empReport)
	return manager, report, stranger
}

// claim returns a probe Expense Claim record created by u for employee
// (0: none).
func claim(id int64, u auth.UserID, employee int64) *auth.RecordRef {
	return &auth.RecordRef{DocType: DTClaim, ID: id, CreatedBy: u, EmployeeID: employee}
}

// AUTH-116, AUTH-117: with read at scope team, a direct report's claim is
// allowed; a stranger's and an indirect report's are not found.
func rowTeamScope(t *testing.T, svc auth.Service, s Setup) {
	ctx := context.Background()
	s.Grant(t, roleTeam, DTClaim, auth.RightRead, auth.ScopeTeam)
	manager, report, stranger := team(t, s, roleTeam)
	admin := s.Admin(t).ID
	wantNil(t, "Can(Claim, read, direct report's claim)", svc.Can(ctx, manager, DTClaim, auth.RightRead, claim(1, report.ID, empReport)))
	wantNotFound(t, "Can(Claim, read, stranger's claim)", svc.Can(ctx, manager, DTClaim, auth.RightRead, claim(2, stranger.ID, empStranger)))
	wantNotFound(t, "Can(Claim, read, indirect report's claim)", svc.Can(ctx, manager, DTClaim, auth.RightRead, claim(3, admin, empIndirect)))
}

// D-AUTH-12: with read at scope own, a claim someone else keyed in for my
// employee is mine; my report's is not found.
func rowOwnViaEmployee(t *testing.T, svc auth.Service, s Setup) {
	ctx := context.Background()
	s.Grant(t, roleOwner, DTClaim, auth.RightRead, auth.ScopeOwn)
	manager, _, _ := team(t, s, roleOwner)
	admin := s.Admin(t).ID
	wantNil(t, "Can(Claim, read, claim for my employee)", svc.Can(ctx, manager, DTClaim, auth.RightRead, claim(1, admin, empManager)))
	wantNotFound(t, "Can(Claim, read, report's claim) at scope own", svc.Can(ctx, manager, DTClaim, auth.RightRead, claim(2, admin, empReport)))
}

// AUTH-116: the read Scope of a team-scope manager keeps the claims they
// created, those for their employee and those for direct reports.
func rowTeamScopePredicate(t *testing.T, svc auth.Service, s Setup) {
	ctx := context.Background()
	s.Grant(t, roleTeam, DTClaim, auth.RightRead, auth.ScopeTeam)
	manager, _, _ := team(t, s, roleTeam)
	admin := s.Admin(t).ID
	insertClaims(t, s,
		claim(1, admin, empManager), claim(2, admin, empReport), claim(3, admin, empStranger),
		claim(4, manager.ID, 0), claim(5, admin, empIndirect), claim(6, admin, 0))
	pred, err := svc.Scope(ctx, manager, DTClaim, auth.RightRead)
	if err != nil {
		t.Fatalf("Scope(Claim, read) = %v, want nil", err)
	}
	if got := visibleIn(t, s, "probe_claim", pred); !slices.Equal(got, []int64{1, 2, 4}) {
		t.Errorf("Scope(Claim, read) {%q, %v} keeps claims %v, want [1 2 4] (own and direct report's)", pred.SQL, pred.Args, got)
	}
}

// AUTH-087: read all with write own: writing a direct report's claim is a
// 403 naming own scope; a claim for my employee is allowed.
func rowOwnOnlyWriteTeammate(t *testing.T, svc auth.Service, s Setup) {
	ctx := context.Background()
	s.Grant(t, roleOwner, DTClaim, auth.RightRead, auth.ScopeAll)
	s.Grant(t, roleOwner, DTClaim, auth.RightWrite, auth.ScopeOwn)
	manager, report, _ := team(t, s, roleOwner)
	wantDenied(t, "Can(Claim, write, report's claim)",
		svc.Can(ctx, manager, DTClaim, auth.RightWrite, claim(1, report.ID, empReport)), auth.RightWrite, auth.ScopeOwn)
	wantNil(t, "Can(Claim, write, claim for my employee)", svc.Can(ctx, manager, DTClaim, auth.RightWrite, claim(2, s.Admin(t).ID, empManager)))
}

// AUTH-087: read all with write team: writing a direct report's claim is
// allowed; a stranger's is a 403 naming team scope.
func rowTeamOnlyWrite(t *testing.T, svc auth.Service, s Setup) {
	ctx := context.Background()
	s.Grant(t, roleTeam, DTClaim, auth.RightRead, auth.ScopeAll)
	s.Grant(t, roleTeam, DTClaim, auth.RightWrite, auth.ScopeTeam)
	manager, report, stranger := team(t, s, roleTeam)
	wantNil(t, "Can(Claim, write, report's claim)", svc.Can(ctx, manager, DTClaim, auth.RightWrite, claim(1, report.ID, empReport)))
	wantDenied(t, "Can(Claim, write, stranger's claim)",
		svc.Can(ctx, manager, DTClaim, auth.RightWrite, claim(2, stranger.ID, empStranger)), auth.RightWrite, auth.ScopeTeam)
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

// AUTH-092, AUTH-093: restricted to PUNE-WH, a record on PUNE-WH is
// allowed; one on TECH-SITE or with no site is not found.
func rowSiteRestriction(t *testing.T, svc auth.Service, s Setup) {
	ctx := context.Background()
	pune, tech := masterID(t, s.DB(), "probe_site", "PUNE-WH"), masterID(t, s.DB(), "probe_site", "TECH-SITE")
	s.Grant(t, roleReader, DTOrder, auth.RightRead, auth.ScopeAll)
	p := s.User(t, "restricted@contract.test", roleReader)
	s.Restrict(t, p.ID, nil, []int64{pune})
	at := func(id int64, sites ...int64) *auth.RecordRef {
		return &auth.RecordRef{DocType: DTOrder, ID: id, CreatedBy: s.Admin(t).ID, SiteIDs: sites}
	}
	wantNil(t, "Can(Order, read, PUNE-WH)", svc.Can(ctx, p, DTOrder, auth.RightRead, at(1, pune)))
	wantNotFound(t, "Can(Order, read, TECH-SITE)", svc.Can(ctx, p, DTOrder, auth.RightRead, at(2, tech)))
	wantNotFound(t, "Can(Order, read, no site) (strict, AUTH-093)", svc.Can(ctx, p, DTOrder, auth.RightRead, at(3)))
}

// AUTH-096: a restriction on a user with no Order rights still gives a 403.
func rowRestrictionNoGrant(t *testing.T, svc auth.Service, s Setup) {
	ctx := context.Background()
	metro, _ := projects(t, s)
	p := s.User(t, "restricted@contract.test")
	s.Restrict(t, p.ID, []int64{metro}, nil)
	wantDenied(t, "Can(Order, read, PRJ-METRO)",
		svc.Can(ctx, p, DTOrder, auth.RightRead, order(1, s.Admin(t).ID, metro)), auth.RightRead, "")
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

// CD-199: UserByEmail finds a user by normalised email in the caller's tx,
// as User does by id. Doubles run this with a blank-email user seeded, which
// the real service can't hold, and must not match it.
func rowUserByEmail(t *testing.T, svc auth.Service, s Setup) {
	checkUserByEmail(t, svc, s.DB(), func(email string) auth.Principal { return s.User(t, email) })
}

func checkUserByEmail(t *testing.T, svc auth.Service, d *db.DB, add func(email string) auth.Principal) {
	ctx := context.Background()
	const email = "kiran@contract.test"
	kiran := add(email)
	wrongCase := add("Wrong.Case@Contract.Test") // only doubles keep the case

	byEmail := func(email string) (auth.UserInfo, bool) {
		t.Helper()
		var info auth.UserInfo
		var ok bool
		if err := d.Read(ctx, func(tx *db.Tx) error {
			var err error
			info, ok, err = svc.UserByEmail(ctx, tx, email)
			return err
		}); err != nil {
			t.Fatalf("UserByEmail(%q): %v", email, err)
		}
		return info, ok
	}
	byID := func(u auth.UserID) auth.UserInfo {
		t.Helper()
		var info auth.UserInfo
		var ok bool
		if err := d.Read(ctx, func(tx *db.Tx) error {
			var err error
			info, ok, err = svc.User(ctx, tx, u)
			return err
		}); err != nil || !ok {
			t.Fatalf("User(%d) = (_, %v, %v), want found", u, ok, err)
		}
		return info
	}
	found := func(email string, want auth.UserInfo) {
		t.Helper()
		if got, ok := byEmail(email); !ok || got != want {
			t.Errorf("UserByEmail(%q) = (%+v, %v), want (%+v, true)", email, got, ok, want)
		}
	}

	want := auth.UserInfo{ID: kiran.ID, Email: email, Name: email, Enabled: true, Version: byID(kiran.ID).Version}
	found(email, want)
	found("  KIRAN@Contract.Test\t", want)
	found("wrong.case@contract.test", byID(wrongCase.ID))
	found(" WRONG.CASE@contract.test ", byID(wrongCase.ID))

	for _, none := range []string{"nobody@contract.test", "", "   "} {
		if got, ok := byEmail(none); ok || got != (auth.UserInfo{}) {
			t.Errorf("UserByEmail(%q) = (%+v, %v), want (zero, false)", none, got, ok)
		}
	}

	want.Enabled = false
	want.Version++
	if err := d.Write(ctx, func(tx *db.Tx) error {
		if err := svc.DisableUser(ctx, tx, kiran.ID, "left"); err != nil {
			return err
		}
		got, ok, err := svc.UserByEmail(ctx, tx, email)
		if err == nil && (!ok || got != want) {
			t.Errorf("UserByEmail in the disabling tx = (%+v, %v), want (%+v, true)", got, ok, want)
		}
		return err
	}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	found(email, want)
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
		wantDenied(t, fmt.Sprintf(what, "Can")+" nil", svc.Can(ctx, admin, c.dt, c.r, nil), c.r, "")
		rec := &auth.RecordRef{DocType: c.dt, ID: 1, CreatedBy: admin.ID}
		wantDenied(t, fmt.Sprintf(what, "Can")+" own record", svc.Can(ctx, admin, c.dt, c.r, rec), c.r, "")
		_, err := svc.Scope(ctx, admin, c.dt, c.r)
		wantDenied(t, fmt.Sprintf(what, "Scope"), err, c.r, "")
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

// CD-171: an unknown DocType is a caller bug: the real service's plain
// error, not a 403 or 404, from Can, Scope, CheckValues and UsersWithRight,
// for the Administrator too.
func rowUnknownDocType(t *testing.T, svc auth.Service, admin auth.Principal, _ *db.DB) {
	ctx := context.Background()
	const dt = "authtest.Unknown"
	want := fmt.Sprintf("auth: unknown DocType %q", dt)
	plain := func(what string, err error) {
		t.Helper()
		if err == nil || err.Error() != want || errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrNotFound) {
			t.Errorf("%s(%s) = %v, want the plain error %q", what, dt, err, want)
		}
	}
	plain("Can", svc.Can(ctx, admin, dt, auth.RightRead, nil))
	plain("Can record", svc.Can(ctx, admin, dt, auth.RightRead, &auth.RecordRef{DocType: dt, ID: 1, CreatedBy: admin.ID}))
	_, err := svc.Scope(ctx, admin, dt, auth.RightRead)
	plain("Scope", err)
	plain("CheckValues", svc.CheckValues(ctx, admin, dt, map[string][]int64{"project": {1}}))
	_, err = svc.UsersWithRight(ctx, dt, auth.RightRead, nil)
	plain("UsersWithRight", err)
}

// errRollback makes inTxs' write transaction roll back.
var errRollback = errors.New("authtest: roll back")

// inTxs runs fn inside a read transaction of d and inside a write transaction
// that is rolled back, as a subtest each (CD-213).
func inTxs(t *testing.T, d *db.DB, fn func(t *testing.T, tx *db.Tx)) {
	t.Helper()
	ctx := context.Background()
	t.Run("read", func(t *testing.T) {
		if err := d.Read(ctx, func(tx *db.Tx) error { fn(t, tx); return nil }); err != nil {
			t.Fatalf("Read: %v", err)
		}
	})
	t.Run("write", func(t *testing.T) {
		if err := d.Write(ctx, func(tx *db.Tx) error { fn(t, tx); return errRollback }); !errors.Is(err, errRollback) {
			t.Fatalf("Write: %v, want the rollback", err)
		}
	})
}

// outcome is what the parity rows compare: an error's type, kind and exact text.
func outcome(err error) string {
	if err == nil {
		return "nil"
	}
	return fmt.Sprintf("%T forbidden=%v notfound=%v %q", err, errors.Is(err, auth.ErrForbidden), errors.Is(err, auth.ErrNotFound), err.Error())
}

// parityDocTypes is every DocType the suite registers.
func parityDocTypes() []*doctype.DocType {
	var out []*doctype.DocType
	for _, d := range docTypes() {
		dt, _ := doctype.Lookup(d.Name)
		out = append(out, dt)
	}
	return out
}

// parityRights is every right plus one that doesn't exist.
func parityRights() []auth.Right { return append(slices.Clone(auth.AllRights), unknownRight) }

// parity is the state rowTxParity compares on: users of every kind of access.
type parity struct {
	people               []auth.Principal // those below, and one who reads all but writes less
	owner, restricted    auth.Principal
	manager, none, admin auth.Principal
	metro, techpark      int64
	pune, tech           int64
}

// newParity gives roleOwner every applicable right at scope own and roleReader
// at scope all, on every DocType, and roleTeam team scope on the Claim. The
// restricted user holds roleReader on PRJ-METRO and PUNE-WH only; a user with
// roleOther and roleApprover reads all but writes only own Orders and team Claims.
func newParity(t *testing.T, s Setup) parity {
	t.Helper()
	x := parity{pune: masterID(t, s.DB(), "probe_site", "PUNE-WH"), tech: masterID(t, s.DB(), "probe_site", "TECH-SITE")}
	x.metro, x.techpark = projects(t, s)
	for _, dt := range parityDocTypes() {
		for _, r := range auth.AllRights {
			if auth.Applies(dt, r) {
				s.Grant(t, roleOwner, dt.Name, r, auth.ScopeOwn)
				s.Grant(t, roleReader, dt.Name, r, auth.ScopeAll)
			}
		}
	}
	for _, r := range []auth.Right{auth.RightRead, auth.RightWrite, auth.RightApprove} {
		s.Grant(t, roleTeam, DTClaim, r, auth.ScopeTeam)
	}
	s.Grant(t, roleOther, DTOrder, auth.RightRead, auth.ScopeAll)
	s.Grant(t, roleOther, DTOrder, auth.RightWrite, auth.ScopeOwn)
	s.Grant(t, roleApprover, DTClaim, auth.RightRead, auth.ScopeAll)
	s.Grant(t, roleApprover, DTClaim, auth.RightWrite, auth.ScopeTeam)
	x.owner = s.User(t, "owner@contract.test", roleOwner)
	x.restricted = s.User(t, "restricted@contract.test", roleReader)
	s.Restrict(t, x.restricted.ID, []int64{x.metro}, []int64{x.pune})
	x.manager, _, _ = team(t, s, roleTeam)
	x.none = s.User(t, "none@contract.test")
	x.admin = s.Admin(t)
	partial := s.User(t, "partial@contract.test", roleOther, roleApprover)
	x.people = []auth.Principal{x.owner, x.restricted, x.manager, x.none, x.admin, partial}
	return x
}

// records are p's view of dt: nil, its own, another's, a report's, one for its
// employee, one outside the restrictions and one with empty ids.
func (x parity) records(dt string, p auth.Principal) map[string]*auth.RecordRef {
	rec := func(id int64, by auth.UserID, emp int64, projects, sites []int64) *auth.RecordRef {
		return &auth.RecordRef{DocType: dt, ID: id, CreatedBy: by, EmployeeID: emp, ProjectIDs: projects, SiteIDs: sites}
	}
	in, inSite := []int64{x.metro}, []int64{x.pune}
	return map[string]*auth.RecordRef{
		"nil":                 nil,
		"own":                 rec(1, p.ID, 0, in, inSite),
		"another's":           rec(2, x.admin.ID, empStranger, in, inSite),
		"a report's":          rec(3, x.admin.ID, empReport, in, inSite),
		"for my employee":     rec(4, x.admin.ID, empManager, in, inSite),
		"outside restriction": rec(5, p.ID, 0, []int64{x.techpark}, []int64{x.tech}),
		"empty ids":           rec(6, p.ID, 0, nil, nil),
	}
}

// CD-213: each Tx method answers exactly as its twin, error kind and text,
// inside a read and inside a rolled-back write. Each comparison first checks
// the twin gave the different answers it must, so an empty table can't pass.
func rowTxParity(t *testing.T, svc auth.Service, s Setup) {
	x := newParity(t, s)
	t.Run("CanTx", func(t *testing.T) { parityCan(t, svc, s, x) })
	t.Run("CheckValuesTx", func(t *testing.T) { parityCheckValues(t, svc, s, x) })
	t.Run("CheckLinkTx", func(t *testing.T) { parityCheckLink(t, svc, s, x) })
	t.Run("UsersWithRightTx", func(t *testing.T) { parityUsersWithRight(t, svc, s, x) })
	t.Run("HasRoleTx", func(t *testing.T) { parityHasRole(t, svc, s, x) })
}

// wantSeen fails unless every outcome class in want occurred.
func wantSeen(t *testing.T, seen map[string]bool, want ...string) {
	t.Helper()
	for _, k := range want {
		if !seen[k] {
			t.Errorf("the twin never answered %q, so the comparison proves nothing (saw %v)", k, seen)
		}
	}
}

// parityCan compares Can and CanTx over every DocType × right × record × user.
func parityCan(t *testing.T, svc auth.Service, s Setup, x parity) {
	ctx := context.Background()
	type call struct {
		what string
		p    auth.Principal
		dt   string
		r    auth.Right
		rec  *auth.RecordRef
		want string
	}
	var calls []call
	seen := map[string]bool{}
	for _, dt := range parityDocTypes() {
		for _, r := range parityRights() {
			for _, p := range x.people {
				for name, rec := range x.records(dt.Name, p) {
					err := svc.Can(ctx, p, dt.Name, r, rec)
					var de *auth.DeniedError
					switch {
					case err == nil:
						seen["allowed"] = true
					case errors.Is(err, auth.ErrNotFound):
						seen["not found"] = true
					case errors.As(err, &de) && de.OwnOnly:
						seen["own only"] = true
					case errors.As(err, &de) && de.TeamOnly:
						seen["team only"] = true
					case errors.As(err, &de):
						seen["denied"] = true
					}
					calls = append(calls, call{fmt.Sprintf("%s %s(%s, %s, %s)", "CanTx", p.Name, dt.Name, r, name), p, dt.Name, r, rec, outcome(err)})
				}
			}
		}
	}
	wantSeen(t, seen, "allowed", "not found", "own only", "team only", "denied")
	inTxs(t, s.DB(), func(t *testing.T, tx *db.Tx) {
		for _, c := range calls {
			if got := outcome(svc.CanTx(ctx, tx, c.p, c.dt, c.r, c.rec)); got != c.want {
				t.Errorf("%s = %s, want what Can gave: %s", c.what, got, c.want)
			}
		}
	})
}

// parityCheckValues compares CheckValues and CheckValuesTx.
func parityCheckValues(t *testing.T, svc auth.Service, s Setup, x parity) {
	ctx := context.Background()
	sets := []map[string][]int64{
		nil,
		{"project_id": {x.metro}},
		{"project_id": {x.techpark}},
		{"project_id": {x.metro, x.techpark}},
		{"site_id": {x.pune}},
		{"site_id": {x.tech}},
		{"lines.line_site_id": {x.tech}},
		{"project_id": {x.metro}, "site_id": {x.tech}},
	}
	type call struct {
		p      auth.Principal
		dt     string
		values map[string][]int64
		want   string
	}
	var calls []call
	seen := map[string]bool{}
	for _, dt := range parityDocTypes() {
		for _, p := range x.people {
			for _, values := range sets {
				err := svc.CheckValues(ctx, p, dt.Name, values)
				var ve *auth.ValueError
				seen["allowed"] = seen["allowed"] || err == nil
				seen["value error"] = seen["value error"] || errors.As(err, &ve)
				calls = append(calls, call{p, dt.Name, values, outcome(err)})
			}
		}
	}
	wantSeen(t, seen, "allowed", "value error")
	inTxs(t, s.DB(), func(t *testing.T, tx *db.Tx) {
		for _, c := range calls {
			if got := outcome(svc.CheckValuesTx(ctx, tx, c.p, c.dt, c.values)); got != c.want {
				t.Errorf("CheckValuesTx(%s, %s, %v) = %s, want what CheckValues gave: %s", c.p.Name, c.dt, c.values, got, c.want)
			}
		}
	})
}

// parityCheckLink compares CheckLink and CheckLinkTx on stored records and a
// missing one.
func parityCheckLink(t *testing.T, svc auth.Service, s Setup, x parity) {
	ctx := context.Background()
	insertOrders(t, s, order(1, x.owner.ID), order(2, x.admin.ID, x.metro), order(3, x.admin.ID, x.techpark))
	insertClaims(t, s, claim(4, x.admin.ID, empReport), claim(5, x.admin.ID, empStranger))
	targets := []struct {
		dt string
		id int64
	}{{DTOrder, 1}, {DTOrder, 2}, {DTOrder, 3}, {DTOrder, 999}, {DTClaim, 4}, {DTClaim, 5}, {DTProject, x.metro}}
	type call struct {
		p    auth.Principal
		dt   string
		id   int64
		want string
	}
	var calls []call
	seen := map[string]bool{}
	for _, tg := range targets {
		for _, p := range x.people {
			err := svc.CheckLink(ctx, p, tg.dt, tg.id)
			var le *auth.LinkError
			seen["allowed"] = seen["allowed"] || err == nil
			seen["link error"] = seen["link error"] || errors.As(err, &le)
			calls = append(calls, call{p, tg.dt, tg.id, outcome(err)})
		}
	}
	wantSeen(t, seen, "allowed", "link error")
	inTxs(t, s.DB(), func(t *testing.T, tx *db.Tx) {
		for _, c := range calls {
			if got := outcome(svc.CheckLinkTx(ctx, tx, c.p, c.dt, c.id)); got != c.want {
				t.Errorf("CheckLinkTx(%s, %s, %d) = %s, want what CheckLink gave: %s", c.p.Name, c.dt, c.id, got, c.want)
			}
		}
	})
}

// parityUsersWithRight compares UsersWithRight and UsersWithRightTx on fewer
// DocTypes and records than parityCan: each call runs Can for every user.
func parityUsersWithRight(t *testing.T, svc auth.Service, s Setup, x parity) {
	ctx := context.Background()
	type call struct {
		dt    string
		r     auth.Right
		name  string
		rec   *auth.RecordRef
		users []auth.UserID
		want  string
	}
	var calls []call
	seen := map[string]bool{}
	for _, dt := range []string{DTOrder, DTClaim, DTItem} {
		for _, r := range parityRights() {
			recs := x.records(dt, x.owner)
			for _, name := range []string{"nil", "own", "outside restriction"} {
				rec := recs[name]
				users, err := svc.UsersWithRight(ctx, dt, r, rec)
				seen["nobody"] = seen["nobody"] || len(users) == 0
				seen["somebody"] = seen["somebody"] || len(users) > 1
				calls = append(calls, call{dt, r, name, rec, users, outcome(err)})
			}
		}
	}
	wantSeen(t, seen, "nobody", "somebody")
	inTxs(t, s.DB(), func(t *testing.T, tx *db.Tx) {
		for _, c := range calls {
			users, err := svc.UsersWithRightTx(ctx, tx, c.dt, c.r, c.rec)
			if got := outcome(err); got != c.want || !slices.Equal(users, c.users) {
				t.Errorf("UsersWithRightTx(%s, %s, %s) = (%v, %s), want what UsersWithRight gave: (%v, %s)", c.dt, c.r, c.name, users, got, c.users, c.want)
			}
		}
	})
}

// parityHasRole compares HasRole and HasRoleTx, with a missing user and role.
func parityHasRole(t *testing.T, svc auth.Service, s Setup, x parity) {
	ctx := context.Background()
	type call struct {
		u    auth.UserID
		role string
		has  bool
		want string
	}
	var calls []call
	seen := map[bool]bool{}
	for _, u := range []auth.UserID{x.owner.ID, x.restricted.ID, x.manager.ID, x.none.ID, 99999} {
		for _, role := range []string{roleOwner, roleReader, roleTeam, "No Such Role"} {
			has, err := svc.HasRole(ctx, u, role)
			seen[has] = true
			calls = append(calls, call{u, role, has, outcome(err)})
		}
	}
	if !seen[true] || !seen[false] {
		t.Errorf("HasRole answered only %v, so the comparison proves nothing", seen)
	}
	inTxs(t, s.DB(), func(t *testing.T, tx *db.Tx) {
		for _, c := range calls {
			has, err := svc.HasRoleTx(ctx, tx, c.u, c.role)
			if got := outcome(err); got != c.want || has != c.has {
				t.Errorf("HasRoleTx(%d, %q) = (%v, %s), want what HasRole gave: (%v, %s)", c.u, c.role, has, got, c.has, c.want)
			}
		}
	})
}

// CD-213, CD-171, CD-173: rowInapplicable through the Tx methods.
func rowInapplicableTx(t *testing.T, svc auth.Service, admin auth.Principal, d *db.DB) {
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
	inTxs(t, d, func(t *testing.T, tx *db.Tx) {
		for _, c := range cases {
			what := fmt.Sprintf("admin CanTx(%s, %s)", c.dt, c.r)
			wantDenied(t, what+" nil", svc.CanTx(ctx, tx, admin, c.dt, c.r, nil), c.r, "")
			rec := &auth.RecordRef{DocType: c.dt, ID: 1, CreatedBy: admin.ID}
			wantDenied(t, what+" own record", svc.CanTx(ctx, tx, admin, c.dt, c.r, rec), c.r, "")
			users, err := svc.UsersWithRightTx(ctx, tx, c.dt, c.r, nil)
			if err != nil || len(users) != 0 {
				t.Errorf("UsersWithRightTx(%s, %s) = (%v, %v), want nobody and nil", c.dt, c.r, users, err)
			}
		}
	})
}

// CD-213, CD-171: rowUnknownDocType through the Tx methods.
func rowUnknownDocTypeTx(t *testing.T, svc auth.Service, admin auth.Principal, d *db.DB) {
	ctx := context.Background()
	const dt = "authtest.Unknown"
	want := fmt.Sprintf("auth: unknown DocType %q", dt)
	inTxs(t, d, func(t *testing.T, tx *db.Tx) {
		plain := func(what string, err error) {
			t.Helper()
			if err == nil || err.Error() != want || errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrNotFound) {
				t.Errorf("%s(%s) = %v, want the plain error %q", what, dt, err, want)
			}
		}
		plain("CanTx", svc.CanTx(ctx, tx, admin, dt, auth.RightRead, nil))
		plain("CanTx record", svc.CanTx(ctx, tx, admin, dt, auth.RightRead, &auth.RecordRef{DocType: dt, ID: 1, CreatedBy: admin.ID}))
		plain("CheckValuesTx", svc.CheckValuesTx(ctx, tx, admin, dt, map[string][]int64{"project": {1}}))
		_, err := svc.UsersWithRightTx(ctx, tx, dt, auth.RightRead, nil)
		plain("UsersWithRightTx", err)
	})
}

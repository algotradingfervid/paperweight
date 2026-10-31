package expense_test

import (
	"context"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// The implementer's E2 tests (review round 1): rules of validate_claim.go,
// validate_advance.go and defaults.go that the committed E2 cases don't
// reach.

// e2Exec runs a fixture statement in a write tx.
func e2Exec(t *testing.T, e *expensetest.Env, q string, args ...any) {
	t.Helper()
	ctx := context.Background()
	if err := e.DB.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, q, args...)
		return err
	}); err != nil {
		t.Fatalf("%s %v: %v", q, args, err)
	}
}

// e2NewUser creates a user with roles and no employee, and loads it.
func e2NewUser(t *testing.T, e *expensetest.Env, email string, roles ...string) auth.Principal {
	t.Helper()
	ctx := context.Background()
	uid, _, err := e.Auth.CreateUser(ctx, e.P(t, e2Admin), auth.NewUser{
		Email: email, Name: "No Employee", Password: "Correct-Horse-Battery-42", Roles: roles,
	})
	if err != nil {
		t.Fatalf("create %s: %v", email, err)
	}
	p, _, err := e.Auth.LoadPrincipal(ctx, uid)
	if err != nil {
		t.Fatalf("load %s: %v", email, err)
	}
	return p
}

// TestExpImpl_RoleChangeAfterSubmit: an Administrator who isn't Deepak
// enters and submits a claim and an advance for him, then loses the
// Administrator role; both are still approved (spec §3.1: pending and later
// states skip the employee check), and the snapshots taken in draft stay as
// they were though the employee and the type are renamed.
func TestExpImpl_RoleChangeAfterSubmit(t *testing.T) {
	e := expensetest.New(t)
	e.AddRoles(t, e2Kavya, auth.RoleAdministrator)
	kavya, priya := e.P(t, e2Kavya), e.P(t, e2Priya)
	emp7 := e2Employee(t, e, "EMP-007")
	forDeepak := expensetest.Values{"employee_id": e2ID(emp7)}

	claim := e.DraftClaim(t, kavya, forDeepak)
	advance := e.DraftAdvance(t, kavya, forDeepak)
	for _, d := range []struct {
		dt string
		id int64
	}{{expense.ClaimDocType, claim.ID}, {expense.AdvanceDocType, advance.ID}} {
		res := e2Must(t, "Kavya submits "+d.dt)(e.Act(t, kavya, d.dt, d.id, doc.ActionSubmit, ""))
		if res.State != doc.StatePending {
			t.Fatalf("%s after submit is %q, want pending", d.dt, res.State)
		}
	}
	before := e2ReadLine(t, e, claim.ID, 1)

	e2Exec(t, e, `DELETE FROM user_roles WHERE user_id = ? AND role_id = (SELECT id FROM roles WHERE name_key = lower(?))`,
		int64(kavya.ID), auth.RoleAdministrator)
	e2Exec(t, e, `UPDATE employee SET name = 'Deepak Renamed' WHERE id = ?`, emp7)
	e2Exec(t, e, `UPDATE expense_type SET name = 'Travel Renamed' WHERE code = 'TRAVEL'`)

	for _, d := range []struct {
		dt string
		id int64
	}{{expense.ClaimDocType, claim.ID}, {expense.AdvanceDocType, advance.ID}} {
		res := e2Must(t, "Priya approves "+d.dt)(e.Act(t, priya, d.dt, d.id, doc.ActionApprove, ""))
		if res.State != doc.StateApproved {
			t.Errorf("%s after approve is %q, want approved", d.dt, res.State)
		}
	}
	if c := e2ReadClaim(t, e, claim.ID); c.snapName != "Deepak Nair" {
		t.Errorf("claim's employee snapshot after approve = %q, want \"Deepak Nair\"", c.snapName)
	}
	if a := e2ReadAdvance(t, e, advance.ID); a.snapName != "Deepak Nair" {
		t.Errorf("advance's employee snapshot after approve = %q, want \"Deepak Nair\"", a.snapName)
	}
	if l := e2ReadLine(t, e, claim.ID, 1); l.typeName != before.typeName {
		t.Errorf("claim line's type snapshot after approve = %q, want %q", l.typeName, before.typeName)
	}
}

// TestExpImpl_BilledToDefaultBranch (CD-281 (c)): with BLR disabled, PUNE
// (head office) and a later AAA share one GSTIN. BranchGSTINs counts it once,
// so a company bill gets Billed to and submits; the head office is chosen
// though AAA has the lower code, and with no head office holding the GSTIN
// the lowest code is chosen.
func TestExpImpl_BilledToDefaultBranch(t *testing.T) {
	ctx := context.Background()
	e := expensetest.New(t)
	deepak, admin := e.P(t, e2Deepak), e.P(t, e2Admin)
	pune := e2Branch(t, e, "PUNE")

	e2Exec(t, e, `UPDATE branch SET disabled = 1 WHERE code = 'BLR'`)
	saved, err := e.Masters.Company.Create(ctx, admin, company.BranchInput{Code: "AAA", Name: "Akurdi Office", StateCode: "27", GSTIN: gstinPUNE})
	if err != nil {
		t.Fatalf("add branch AAA: %v", err)
	}
	aaa := saved.ID
	if n := e2BranchGSTINs(t, e); n != 1 {
		t.Fatalf("BranchGSTINs with PUNE and AAA on one GSTIN = %d, want 1", n)
	}

	c := e.DraftClaim(t, deepak, nil, e2CompanyBill(nil))
	if l := e2ReadLine(t, e, c.ID, 1); e2Int(l.billedToID) != pune {
		t.Errorf("head office PUNE holds the GSTIN: Billed to = %d, want PUNE (%d), not AAA (%d)", e2Int(l.billedToID), pune, aaa)
	}
	res := e2Must(t, "submit with one shared GSTIN")(e.Act(t, deepak, expense.ClaimDocType, c.ID, doc.ActionSubmit, ""))
	if res.State != doc.StatePending {
		t.Errorf("after submit the claim is %q, want pending", res.State)
	}

	e2Exec(t, e, `UPDATE branch SET is_head_office = 0 WHERE code = 'PUNE'`)
	c = e.DraftClaim(t, deepak, nil, e2CompanyBill(nil))
	if l := e2ReadLine(t, e, c.ID, 1); e2Int(l.billedToID) != aaa {
		t.Errorf("no head office holds the GSTIN: Billed to = %d, want AAA (%d), the lowest code", e2Int(l.billedToID), aaa)
	}
}

// TestExpImpl_BilledToCompanyBillOnly (spec §3.2): a defaulted Billed to is
// cleared when the tick is removed, and a line without the tick naming a
// branch with no GSTIN saves with none.
func TestExpImpl_BilledToCompanyBillOnly(t *testing.T) {
	ctx := context.Background()
	e := expensetest.New(t)
	deepak, admin := e.P(t, e2Deepak), e.P(t, e2Admin)
	type V = expensetest.Values
	e2Exec(t, e, `UPDATE branch SET disabled = 1 WHERE code = 'BLR'`)

	c := e.DraftClaim(t, deepak, nil, e2CompanyBill(nil))
	if l := e2ReadLine(t, e, c.ID, 1); l.billedToID == nil || l.billedGSTIN != gstinPUNE {
		t.Fatalf("ticked line: Billed to = %d, snap_billed_gstin %q; want PUNE, %s", e2Int(l.billedToID), l.billedGSTIN, gstinPUNE)
	}
	e2Must(t, "untick the line")(e.Edit(t, deepak, expense.ClaimDocType, c.ID, nil, V{"company_bill": "false"}))
	if l := e2ReadLine(t, e, c.ID, 1); l.billedToID != nil || l.billedGSTIN != "" {
		t.Errorf("unticked line: Billed to = %d, snap_billed_gstin %q; want none", e2Int(l.billedToID), l.billedGSTIN)
	}

	saved, err := e.Masters.Company.Create(ctx, admin, company.BranchInput{Code: "SITE-OFF", Name: "Site Office", StateCode: "27"})
	if err != nil {
		t.Fatalf("add branch SITE-OFF: %v", err)
	}
	res := e2Must(t, "an unticked line naming SITE-OFF")(e.Create(deepak, expense.ClaimDocType,
		e.ClaimInput(t, deepak, nil, V{"billed_to_id": e2ID(saved.ID)})))
	if l := e2ReadLine(t, e, res.ID, 1); l.billedToID != nil {
		t.Errorf("unticked line naming SITE-OFF: Billed to = %d, want none", e2Int(l.billedToID))
	}
}

// TestExpImpl_LineSnapshots: a claim line snapshots its project and site
// codes, and an advance line its type code and name, project and site codes.
func TestExpImpl_LineSnapshots(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, e2Deepak)
	type V = expensetest.Values
	var project, site int64
	for _, p := range e.F.Projects {
		if p.Code == "PRJ-METRO" {
			project = p.ID
		}
	}
	for _, s := range e.F.Sites {
		if s.Code == "METRO-SITE" {
			site = s.ID
		}
	}
	tags := V{"expense_type_id": e2Type(t, e, "LODGING"), "project_id": e2ID(project), "site_id": e2ID(site)}

	c := e.DraftClaim(t, deepak, nil, tags)
	if l := e2ReadLine(t, e, c.ID, 1); l.projectCode != "PRJ-METRO" || l.siteCode != "METRO-SITE" {
		t.Errorf("claim line snapshot = project %q, site %q; want PRJ-METRO, METRO-SITE", l.projectCode, l.siteCode)
	}

	a := e.DraftAdvance(t, deepak, nil, tags)
	var typeCode, typeName, projectCode, siteCode string
	e2Scan(t, e, `SELECT snap_type_code, snap_type_name, snap_project_code, snap_site_code
		FROM employee_advance_line WHERE parent_id = ? AND line_no = 1`, []any{a.ID},
		&typeCode, &typeName, &projectCode, &siteCode)
	if typeCode != "LODGING" || typeName != "Lodging" || projectCode != "PRJ-METRO" || siteCode != "METRO-SITE" {
		t.Errorf("advance line snapshot = %q %q, project %q, site %q; want LODGING Lodging, PRJ-METRO, METRO-SITE",
			typeCode, typeName, projectCode, siteCode)
	}
}

// TestExpImpl_NoEmployeeNamesAnother: an Administrator with no employee
// back-enters a claim for Deepak; a non-admin with no employee naming him is
// refused with the claims-for-yourself text.
func TestExpImpl_NoEmployeeNamesAnother(t *testing.T) {
	e := expensetest.New(t)
	emp7 := e2Employee(t, e, "EMP-007")
	forDeepak := expensetest.Values{"employee_id": e2ID(emp7)}

	admin := e2NewUser(t, e, "noemp-admin@vardhan.test", auth.RoleAdministrator)
	res := e2Must(t, "an Administrator with no employee enters Deepak's claim")(e.Create(admin, expense.ClaimDocType,
		e.ClaimInput(t, admin, forDeepak)))
	if c := e2ReadClaim(t, e, res.ID); c.employeeID != emp7 {
		t.Errorf("back-entered claim's employee = %d, want EMP-007 (%d)", c.employeeID, emp7)
	}

	plain := e2NewUser(t, e, "noemp@vardhan.test")
	e.WantRefused(t, msgOnlyOwnClaims, func() error {
		_, err := e.Create(plain, expense.ClaimDocType, e.ClaimInput(t, plain, forDeepak))
		return err
	})
}

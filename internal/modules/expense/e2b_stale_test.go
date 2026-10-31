package expense_test

import (
	"context"
	"maps"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/expensetype"
	"github.com/vardhan/internaltools/internal/masters/taxcode"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// CD-302: a step save (CD-205, CD-207) keeps Validate's writes when the
// field errors fall outside ActionInput.Only, so each derived field is
// written from the current inputs or cleared, never left from an earlier
// save. Every case saves with Only naming purpose alone, sends a changed
// input that fails a check, and expects that check's text in
// Result.Problems: the proof the save took the path that keeps writes.

// e2bStep saves stored draft id of dt as p, scoped to only: the header
// values in header, and every stored line in full with lines[n] merged over
// line n (1-based), except line drop (0 drops none).
func e2bStep(t *testing.T, e *expensetest.Env, p auth.Principal, dt string, id int64,
	header expensetest.Values, lines map[int]expensetest.Values, drop int, only ...string) (doc.Result, error) {
	t.Helper()
	rec := e.Load(t, dt, id)
	in := doc.ActionInput{Version: rec.Version, Values: maps.Clone(header), Only: only}
	for i, l := range rec.Lines {
		if i+1 == drop {
			continue
		}
		m := maps.Clone(l)
		maps.Copy(m, lines[i+1])
		in.Lines = append(in.Lines, m)
	}
	return e.Engine.Do(context.Background(), p, dt, id, doc.ActionSave, in)
}

// e2bKept fails the test unless the step save was written with problem
// among its Result.Problems.
func e2bKept(t *testing.T, what string, problem string) func(doc.Result, error) {
	t.Helper()
	return func(res doc.Result, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v (messages %q); want written with problem %q", what, err, e2Messages(err), problem)
		}
		if !slices.ContainsFunc(res.Problems, func(f doc.FieldError) bool { return f.Message == problem }) {
			t.Fatalf("%s: problems %+v; want %q (the save must keep Validate's writes)", what, res.Problems, problem)
		}
	}
}

// TestExp_StaleEmployeeSnapshot: the employee snapshot is the stored
// employee's, and blank when the employee is blank, on a claim and an
// advance.
func TestExp_StaleEmployeeSnapshot(t *testing.T) {
	ctx := context.Background()
	e := expensetest.New(t)
	admin, deepak := e.P(t, e2Admin), e.P(t, e2Deepak)
	emp7, emp6 := e2Employee(t, e, "EMP-007"), e2Employee(t, e, "EMP-006")

	// An Administrator with no employee: they may enter for others, and a
	// blank employee can't default.
	uid, _, err := e.Auth.CreateUser(ctx, admin, auth.NewUser{
		Email: "backoffice@vardhan.test", Name: "Back Office", Password: "Correct-Horse-Battery-42",
		Roles: []string{auth.RoleAdministrator},
	})
	if err != nil {
		t.Fatalf("create an Administrator with no employee: %v", err)
	}
	office, _, err := e.Auth.LoadPrincipal(ctx, uid)
	if err != nil {
		t.Fatalf("load the new user: %v", err)
	}

	type kind struct {
		dt, onlyOwn string
		draft       func(p auth.Principal, h expensetest.Values) doc.Result
		read        func(id int64) (employeeID int64, name, code string)
	}
	kinds := []kind{
		{
			dt: expense.ClaimDocType, onlyOwn: msgOnlyOwnClaims,
			draft: func(p auth.Principal, h expensetest.Values) doc.Result { return e.DraftClaim(t, p, h) },
			read: func(id int64) (int64, string, string) {
				c := e2ReadClaim(t, e, id)
				return c.employeeID, c.snapName, c.snapCode
			},
		},
		{
			dt: expense.AdvanceDocType, onlyOwn: msgOnlyOwnAdvances,
			draft: func(p auth.Principal, h expensetest.Values) doc.Result { return e.DraftAdvance(t, p, h) },
			read: func(id int64) (int64, string, string) {
				a := e2ReadAdvance(t, e, id)
				return a.employeeID, a.snapName, a.snapCode
			},
		},
	}
	for _, k := range kinds {
		t.Run(k.dt, func(t *testing.T) {
			// Blank employee, nothing to default to: no snapshot.
			c := k.draft(office, expensetest.Values{"employee_id": e2ID(emp7)})
			e2bKept(t, "clear the employee", msgNoEmployee)(e2bStep(t, e, office, k.dt, c.ID,
				expensetest.Values{"employee_id": "", "purpose": "Pune Metro site visit, day 2"}, nil, 0, "purpose"))
			if id, name, code := k.read(c.ID); id != 0 || name != "" || code != "" {
				t.Errorf("blank employee kept: employee %d, snapshot %q %q; want none, \"\" \"\"", id, name, code)
			}

			// Another employee, refused for Deepak: the snapshot follows the
			// stored employee.
			d := k.draft(admin, expensetest.Values{"employee_id": e2ID(emp7)})
			e2bKept(t, "Deepak names EMP-006", k.onlyOwn)(e2bStep(t, e, deepak, k.dt, d.ID,
				expensetest.Values{"employee_id": e2ID(emp6), "purpose": "Pune Metro site visit, day 2"}, nil, 0, "purpose"))
			if id, name, code := k.read(d.ID); id != emp6 || name != "Kavya Rao" || code != "EMP-006" {
				t.Errorf("EMP-006 kept: employee %d, snapshot %q %q; want %d, \"Kavya Rao\" \"EMP-006\"", id, name, code, emp6)
			}
		})
	}
}

// TestExp_StaleLineDefaults (AD ruling on CD-302): HSN/SAC and tax code are
// user inputs that Validate fills only when blank (spec §3.2). When a line's
// type changes, a filled value is kept and a blank one is filled from the new
// type. The type, project and site snapshots follow the line.
func TestExp_StaleLineDefaults(t *testing.T) {
	ctx := context.Background()
	e := expensetest.New(t)
	deepak, admin := e.P(t, e2Deepak), e.P(t, e2Admin)
	type V = expensetest.Values

	var gst18, gst5 taxcode.TaxCode
	types := map[string]expensetype.ExpenseType{}
	if err := e.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		if gst18, _, err = e.Masters.TaxCode.ByCode(ctx, tx, "GST18"); err != nil {
			return err
		}
		if gst5, _, err = e.Masters.TaxCode.ByCode(ctx, tx, "GST5"); err != nil {
			return err
		}
		for _, code := range []string{"LODGING", "FOOD"} {
			if types[code], err = e.Masters.ExpenseType.Get(ctx, tx, e.F.ExpenseTypeIDs[code]); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("read the masters: %v", err)
	}
	defaults := map[string]struct {
		hsn string
		tax int64
	}{"LODGING": {"996311", gst18.ID}, "FOOD": {"996331", gst5.ID}}
	for code, d := range defaults {
		et := types[code]
		if _, err := e.Masters.ExpenseType.Update(ctx, admin, et.ID, et.Version, expensetype.Input{
			Code: et.Code, Name: et.Name, DefaultHSNSAC: d.hsn, DefaultTaxCodeID: d.tax,
		}); err != nil {
			t.Fatalf("set %s's defaults: %v", code, err)
		}
	}

	lodging := V{"expense_type_id": e2Type(t, e, "LODGING"), "hsn_sac": "", "tax_code_id": ""}
	c := e.DraftClaim(t, deepak, nil, lodging, lodging, lodging)
	if l := e2ReadLine(t, e, c.ID, 1); l.hsnSAC != "996311" || e2Int(l.taxCodeID) != gst18.ID {
		t.Fatalf("setup: LODGING line = SAC %q, tax code %d; want 996311, %d", l.hsnSAC, e2Int(l.taxCodeID), gst18.ID)
	}

	// FOOD disabled: picking it is a link error, kept as a problem.
	food := types["FOOD"]
	food = e2bTypeAfterUpdate(t, e, food.ID)
	if err := e.Masters.ExpenseType.SetDisabled(ctx, admin, food.ID, food.Version, true); err != nil {
		t.Fatalf("disable FOOD: %v", err)
	}
	metro, techSite := e.E8Project(t, "PRJ-METRO"), e.E8Site(t, "TECH-SITE")
	foodLine := V{"expense_type_id": e2Type(t, e, "FOOD"), "project_id": e2ID(metro), "site_id": e2ID(techSite)}
	res, err := e2bStep(t, e, deepak, expense.ClaimDocType, c.ID, V{"purpose": "Pune Metro site visit, day 2"},
		map[int]V{1: foodLine, 2: {"expense_type_id": e2Type(t, e, "FOOD"), "hsn_sac": "", "tax_code_id": ""},
			3: {"expense_type_id": ""}}, 0, "purpose")
	if err != nil {
		t.Fatalf("step save with a disabled type: %v (messages %q)", err, e2Messages(err))
	}
	if len(res.Problems) == 0 {
		t.Fatalf("step save with a disabled type: no problems; want FOOD's link error kept as a problem")
	}

	if l := e2ReadLine(t, e, c.ID, 1); l.hsnSAC != "996311" || e2Int(l.taxCodeID) != gst18.ID {
		t.Errorf("line 1 LODGING to FOOD, values filled: SAC %q, tax code %d; want the kept 996311, GST18 (%d)", l.hsnSAC, e2Int(l.taxCodeID), gst18.ID)
	} else if l.typeCode != "FOOD" || l.typeName != "Food" || l.projectCode != "PRJ-METRO" || l.siteCode != "TECH-SITE" {
		t.Errorf("line 1 snapshots = %q %q %q %q; want FOOD Food PRJ-METRO TECH-SITE", l.typeCode, l.typeName, l.projectCode, l.siteCode)
	}
	if l := e2ReadLine(t, e, c.ID, 2); l.hsnSAC != "996331" || e2Int(l.taxCodeID) != gst5.ID {
		t.Errorf("line 2 LODGING to FOOD, values cleared: SAC %q, tax code %d; want FOOD's 996331, GST5 (%d)", l.hsnSAC, e2Int(l.taxCodeID), gst5.ID)
	}
	if l := e2ReadLine(t, e, c.ID, 3); l.hsnSAC != "996311" || e2Int(l.taxCodeID) != gst18.ID || l.typeCode != "" || l.typeName != "" {
		t.Errorf("line 3 type cleared: SAC %q, tax code %d, snapshot %q %q; want the kept 996311, GST18 (%d), no snapshot",
			l.hsnSAC, e2Int(l.taxCodeID), l.typeCode, l.typeName, gst18.ID)
	}
}

// e2bTypeAfterUpdate reads expense type id again, for its new version.
func e2bTypeAfterUpdate(t *testing.T, e *expensetest.Env, id int64) expensetype.ExpenseType {
	t.Helper()
	ctx := context.Background()
	var et expensetype.ExpenseType
	if err := e.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		et, err = e.Masters.ExpenseType.Get(ctx, tx, id)
		return err
	}); err != nil {
		t.Fatalf("read expense type %d: %v", id, err)
	}
	return et
}

// TestExp_StaleBilledTo: Billed to on a branch with no GSTIN snapshots no
// GSTIN, and untoggling the company bill clears Billed to and its snapshot.
func TestExp_StaleBilledTo(t *testing.T) {
	ctx := context.Background()
	e := expensetest.New(t)
	deepak, admin := e.P(t, e2Deepak), e.P(t, e2Admin)
	type V = expensetest.Values
	pune := e2Branch(t, e, "PUNE")
	saved, err := e.Masters.Company.Create(ctx, admin, company.BranchInput{Code: "SITE-OFF", Name: "Site Office", StateCode: "27"})
	if err != nil {
		t.Fatalf("add branch SITE-OFF: %v", err)
	}

	bill := e2CompanyBill(V{"billed_to_id": e2ID(pune)})
	c := e.DraftClaim(t, deepak, nil, bill, bill)
	if l := e2ReadLine(t, e, c.ID, 1); l.billedGSTIN != gstinPUNE {
		t.Fatalf("setup: Billed to PUNE snapshot %q, want %q", l.billedGSTIN, gstinPUNE)
	}

	e2bKept(t, "Billed to SITE-OFF", msgBranchNoGSTIN)(e2bStep(t, e, deepak, expense.ClaimDocType, c.ID,
		V{"purpose": "Pune Metro site visit, day 2"},
		map[int]V{1: {"billed_to_id": e2ID(saved.ID)}, 2: {"company_bill": "false"}}, 0, "purpose"))
	if l := e2ReadLine(t, e, c.ID, 1); e2Int(l.billedToID) != saved.ID || l.billedGSTIN != "" {
		t.Errorf("line 1 Billed to SITE-OFF: branch %d, GSTIN snapshot %q; want %d, none", e2Int(l.billedToID), l.billedGSTIN, saved.ID)
	}
	if l := e2ReadLine(t, e, c.ID, 2); l.billedToID != nil || l.billedGSTIN != "" {
		t.Errorf("line 2 no longer a company bill: branch %d, GSTIN snapshot %q; want none", e2Int(l.billedToID), l.billedGSTIN)
	}
}

// TestExp_StaleTotals: total, net payable and the period are the stored
// lines' after a line becomes invalid and another is removed; an advance's
// amount likewise.
func TestExp_StaleTotals(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, e2Deepak)
	type V = expensetest.Values
	const (
		msgAmountPositive = "Line 1: Amount must be more than 0."
		msgDateFuture     = "Line 2: Date can't be in the future."
	)

	c := e.DraftClaim(t, deepak, nil,
		V{"expense_date": "2026-10-22", "amount": "1000.00"},
		V{"expense_date": "2026-10-23", "amount": "2000.00"},
		V{"expense_date": "2026-10-20", "amount": "3000.00"})
	// Line 1 at 0 (the engine's error), line 2 dated tomorrow (Validate's),
	// line 3 removed.
	step := func() (doc.Result, error) {
		return e2bStep(t, e, deepak, expense.ClaimDocType, c.ID, V{"purpose": "Pune Metro site visit, day 2"},
			map[int]V{1: {"amount": "0.00"}, 2: {"expense_date": "2026-10-25"}}, 3, "purpose")
	}
	res, err := step()
	e2bKept(t, "claim line 1 at 0", msgAmountPositive)(res, err)
	e2bKept(t, "claim line 2 tomorrow", msgDateFuture)(res, err)
	if got := e2ReadClaim(t, e, c.ID); e2Int(got.total) != 200000 || e2Int(got.net) != 200000 ||
		e2Str(got.periodFrom) != "2026-10-22" || e2Str(got.periodTo) != "2026-10-25" {
		t.Errorf("claim total %d, net %d, period %s to %s; want 200000, 200000, 2026-10-22 to 2026-10-25",
			e2Int(got.total), e2Int(got.net), e2Str(got.periodFrom), e2Str(got.periodTo))
	}

	a := e.DraftAdvance(t, deepak, nil, V{"line_amount": "5000.00"}, V{"line_amount": "3000.00"})
	e2bKept(t, "advance line 1 at 0, line 2 removed", msgAmountPositive)(e2bStep(t, e, deepak, expense.AdvanceDocType, a.ID,
		V{"purpose": "Pune Metro site visit, day 2"}, map[int]V{1: {"line_amount": "0.00"}}, 2, "purpose"))
	if got := e2ReadAdvance(t, e, a.ID); e2Int(got.amount) != 0 {
		t.Errorf("advance amount %d; want 0", e2Int(got.amount))
	}
}

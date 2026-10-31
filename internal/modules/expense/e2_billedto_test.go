package expense_test

import (
	"context"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// CD-281's Billed to texts.
const (
	msgBilledToRequired = "Line 1: Billed to is required for a bill in the company GSTIN."
	msgBranchNoGSTIN    = "Line 1: Choose a branch with a GSTIN."
)

// The fixture branch GSTINs (00-fixtures F1).
const (
	gstinPUNE = "27AAFCV2449G1Z7"
	gstinBLR  = "29AAFCV2449G1Z3"
)

// e2CompanyBill is a company-bill line with a valid supplier GSTIN and
// invoice no., so only Billed to is in question.
func e2CompanyBill(extra expensetest.Values) expensetest.Values {
	v := expensetest.Values{"company_bill": "true", "supplier_gstin": "27AABCS1429B1ZU", "invoice_no": "INV-778"}
	for k, x := range extra {
		v[k] = x
	}
	return v
}

// e2BranchGSTINs is expense.BranchGSTINs read in its own transaction.
func e2BranchGSTINs(t *testing.T, e *expensetest.Env) int {
	t.Helper()
	ctx := context.Background()
	var n int
	if err := e.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		n, err = expense.BranchGSTINs(ctx, tx)
		return err
	}); err != nil {
		t.Errorf("expense.BranchGSTINs: %v", err)
		return -1
	}
	return n
}

// e2GetBranch reads the branch with code.
func e2GetBranch(t *testing.T, e *expensetest.Env, code string) company.Branch {
	t.Helper()
	ctx := context.Background()
	var b company.Branch
	if err := e.DB.Read(ctx, func(tx *db.Tx) error {
		var ok bool
		var err error
		b, ok, err = e.Masters.Company.ByCode(ctx, tx, code)
		if err == nil && !ok {
			t.Fatalf("no branch %s", code)
		}
		return err
	}); err != nil {
		t.Fatalf("read branch %s: %v", code, err)
	}
	return b
}

// TestExp_BilledTo (CD-271 G4, CD-281 (c)): with two branch GSTINs a
// company-bill line needs Billed to at submit and snapshots its GSTIN; a
// branch with no GSTIN is refused on save; with one branch GSTIN Validate
// sets the branch holding it and submit needs nothing more; a line without
// the tick has no Billed to.
func TestExp_BilledTo(t *testing.T) { // EXP-058
	ctx := context.Background()
	e := expensetest.New(t)
	deepak, admin, priya := e.P(t, e2Deepak), e.P(t, e2Admin), e.P(t, e2Priya)
	type V = expensetest.Values
	pune, blr := e2Branch(t, e, "PUNE"), e2Branch(t, e, "BLR")

	// SITE-OFF: enabled, no GSTIN. Never counted, never accepted.
	saved, err := e.Masters.Company.Create(ctx, admin, company.BranchInput{Code: "SITE-OFF", Name: "Site Office", StateCode: "27"})
	if err != nil {
		t.Fatalf("add branch SITE-OFF: %v", err)
	}
	siteOff := saved.ID

	if n := e2BranchGSTINs(t, e); n != 2 {
		t.Errorf("BranchGSTINs with PUNE, BLR and SITE-OFF (no GSTIN) = %d, want 2", n)
	}

	t.Run("two GSTINs", func(t *testing.T) {
		c := e.DraftClaim(t, deepak, nil, e2CompanyBill(nil))
		if l := e2ReadLine(t, e, c.ID, 1); l.billedToID != nil {
			t.Errorf("with two GSTINs a blank Billed to was set to %d; want none (no default)", e2Int(l.billedToID))
		}
		e.WantRefused(t, msgBilledToRequired, func() error {
			_, err := e.Act(t, deepak, expense.ClaimDocType, c.ID, doc.ActionSubmit, "")
			return err
		})

		// A crafted post naming SITE-OFF, on a draft and on a new claim.
		crafted := e.DraftClaim(t, deepak, nil, e2CompanyBill(nil))
		e.WantRefused(t, msgBranchNoGSTIN, func() error {
			_, err := e.Edit(t, deepak, expense.ClaimDocType, crafted.ID, nil, V{"billed_to_id": e2ID(siteOff)})
			return err
		})
		e.WantRefused(t, msgBranchNoGSTIN, func() error {
			_, err := e.Create(deepak, expense.ClaimDocType, e.ClaimInput(t, deepak, nil, e2CompanyBill(V{"billed_to_id": e2ID(siteOff)})))
			return err
		})

		// BLR: submits, and the GSTIN is snapshotted.
		c = e.DraftClaim(t, deepak, nil, e2CompanyBill(V{"billed_to_id": e2ID(blr)}))
		res := e2Must(t, "submit Billed to BLR")(e.Act(t, deepak, expense.ClaimDocType, c.ID, doc.ActionSubmit, ""))
		if res.State != doc.StatePending {
			t.Errorf("after submit the claim is %q, want pending", res.State)
		}
		if l := e2ReadLine(t, e, c.ID, 1); e2Int(l.billedToID) != blr || l.billedGSTIN != gstinBLR {
			t.Errorf("Billed to = %d, snap_billed_gstin %q; want BLR (%d), %s", e2Int(l.billedToID), l.billedGSTIN, blr, gstinBLR)
		}

		// BLR's GSTIN is edited; approving the claim (Validate runs again)
		// keeps the snapshot.
		b := e2GetBranch(t, e, "BLR")
		next := e2OtherGSTIN(t, "29AAFCV2449G2Z")
		if _, err := e.Masters.Company.Update(ctx, admin, blr, b.Version, company.BranchInput{
			Code: b.Code, Name: b.Name, StateCode: b.StateCode, GSTIN: next, IsHeadOffice: b.IsHeadOffice,
		}); err != nil {
			t.Fatalf("edit BLR's GSTIN to %s: %v", next, err)
		}
		e2Must(t, "approve as Priya")(e.Act(t, priya, expense.ClaimDocType, c.ID, doc.ActionApprove, ""))
		if l := e2ReadLine(t, e, c.ID, 1); l.billedGSTIN != gstinBLR {
			t.Errorf("after BLR's GSTIN became %s, snap_billed_gstin = %q; want %s unchanged", next, l.billedGSTIN, gstinBLR)
		}
	})

	t.Run("one GSTIN", func(t *testing.T) {
		// BLR's site TECH-SITE goes first: a branch with an enabled site
		// can't be disabled.
		var tech site.Site
		if err := e.DB.Read(ctx, func(tx *db.Tx) error {
			var err error
			tech, _, err = e.Masters.Site.ByCode(ctx, tx, "TECH-SITE")
			return err
		}); err != nil {
			t.Fatalf("read TECH-SITE: %v", err)
		}
		if err := e.Masters.Site.SetDisabled(ctx, admin, tech.ID, tech.Version, true); err != nil {
			t.Fatalf("disable TECH-SITE: %v", err)
		}
		b := e2GetBranch(t, e, "BLR")
		if err := e.Masters.Company.SetDisabled(ctx, admin, blr, b.Version, true); err != nil {
			t.Fatalf("disable BLR: %v", err)
		}
		if n := e2BranchGSTINs(t, e); n != 1 {
			t.Errorf("BranchGSTINs with BLR disabled = %d, want 1 (Billed to is hidden)", n)
		}

		c := e.DraftClaim(t, deepak, nil, e2CompanyBill(nil), V{"amount": "200.00"})
		if l := e2ReadLine(t, e, c.ID, 1); e2Int(l.billedToID) != pune || l.billedGSTIN != gstinPUNE {
			t.Errorf("one GSTIN: Billed to = %d, snap_billed_gstin %q; want PUNE (%d), %s", e2Int(l.billedToID), l.billedGSTIN, pune, gstinPUNE)
		}
		if l := e2ReadLine(t, e, c.ID, 2); l.billedToID != nil || l.billedGSTIN != "" {
			t.Errorf("line without the tick: Billed to = %d, snap_billed_gstin %q; want none", e2Int(l.billedToID), l.billedGSTIN)
		}
		res := e2Must(t, "submit with one GSTIN")(e.Act(t, deepak, expense.ClaimDocType, c.ID, doc.ActionSubmit, ""))
		if res.State != doc.StatePending {
			t.Errorf("after submit the claim is %q, want pending", res.State)
		}
	})
}

// e2OtherGSTIN is prefix plus the check character that makes a valid GSTIN.
func e2OtherGSTIN(t *testing.T, prefix string) string {
	t.Helper()
	for _, c := range "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ" {
		if g := prefix + string(c); gstin.ValidateGSTIN(g) == nil {
			return g
		}
	}
	t.Fatalf("no valid GSTIN starts %s", prefix)
	return ""
}

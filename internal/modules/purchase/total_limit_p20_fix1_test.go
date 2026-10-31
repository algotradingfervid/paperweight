package purchase_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/masters/paymentterms"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// MOD-PO P20 fix round 1 (CD-308, CD-309 (4) with AD-13's ruling).

// p20GrandOnlyOver is one 18% line of 100 kg at ₹8,47,45,762.7120: the net
// is ₹8,47,45,76,271.20; intra-state each half of the GST is
// ₹76,27,11,864.408, rounded to ₹76,27,11,864.41, so the grand total is
// ₹10,00,00,00,000.02, while the net, each GST part and the rounded total
// (₹10,00,00,00,000.00, exactly the maximum) are not over. Only the grand
// total is over.
func p20GrandOnlyOver(t *testing.T, env *purchasetest.Env) []map[string]string {
	t.Helper()
	return []map[string]string{p2Line(t, env, "MS-ANGLE-50", "KGS", "100", "84745762.712")}
}

// Test 1, grand total only (CD-308): the save is refused with exactly the
// CD-308 error, though the rounded total is at the maximum.
func TestPO_TotalLimitGrandOnlySaveRefused(t *testing.T) {
	env := purchasetest.New(t)
	before := p2CountAll(t, env)
	_, err := p2Create(t, env, env.Priya, p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO"), p20GrandOnlyOver(t, env))
	p2WantInvalid(t, "create, grand ₹10,00,00,00,000.02", err, p20Err)
	p2SameCounts(t, "create, grand ₹10,00,00,00,000.02", before, env)
}

// Test 1, grand total only, step save (CD-308, CD-302): a remarks-only step
// save posting the line commits with the CD-308 error as a problem and the
// header totals cleared.
func TestPO_TotalLimitGrandOnlyStepSave(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p2RefLines(t, env)...))
	before := p2Snap(t, env, po.ID, p20TotalCols...)

	res, err := p2Step(t, env, env.Priya, po.ID, []string{"remarks"}, map[string]string{"remarks": "Big order"}, p20GrandOnlyOver(t, env))
	p2WantProblems(t, "grand ₹10,00,00,00,000.02", res, err, p20Err)
	p20WantCleared(t, "grand ₹10,00,00,00,000.02", env, po.ID, before)
	p20WantAmounts(t, "grand ₹10,00,00,00,000.02", env, po.ID, "847457627120")
}

// Test 2, inter-state (CD-308, CD-302): a BHARAT draft (inter-state, all
// IGST) with the reference lines stores a non-zero IGST total; a step save
// posting lines over the maximum clears every total, the IGST total too.
func TestPO_TotalLimitStepSaveInterState(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(),
		purchasetest.OrderHeader(map[string]string{"vendor_id": masterID(t, env.DB, "party", "BHARAT")}),
		purchasetest.OrderLines(p2RefLines(t, env)...))
	before := p2Snap(t, env, po.ID, p20TotalCols...)
	if p2Cleared(before["igst_total"]) || !p2Cleared(before["cgst_total"]) {
		t.Fatalf("BHARAT draft: igst_total %q cgst_total %q, want a non-zero IGST total and no CGST (inter-state)", before["igst_total"], before["cgst_total"])
	}

	res, err := p2Step(t, env, env.Priya, po.ID, []string{"remarks"}, map[string]string{"remarks": "Big order"}, p20Over(t, env))
	p2WantProblems(t, "inter-state lines over the maximum", res, err, p20Err)
	p20WantCleared(t, "inter-state lines over the maximum", env, po.ID, before)
	p20WantAmounts(t, "inter-state lines over the maximum", env, po.ID, "600000000000", "600000000000")
}

// Test 3 (CD-309 (4), AD-13 ruling): the old default is the old vendor's
// raw default terms, even when those terms are now disabled. A SHREE draft
// holding NET30 (SHREE's default) whose NET30 is then disabled, switched to
// BHARAT (default NET45), takes NET45.
func TestPO_VendorChangeResetsDisabledDefaultTerms(t *testing.T) {
	for _, p := range p20SavePaths {
		t.Run(p.name, func(t *testing.T) {
			env := purchasetest.New(t)
			net30, net45 := p20Terms(t, env, "NET30", "Net 30 days"), p20Terms(t, env, "NET45", "Net 45 days")
			p20SetDefaultTerms(t, env, "SHREE", net30)
			p20SetDefaultTerms(t, env, "BHARAT", net45)
			po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderHeader(map[string]string{"payment_terms_id": ""}))
			p20WantTerms(t, "SHREE draft", env, po.ID, net30, "Net 30 days")

			if err := paymentterms.New(p2Kit(env)).SetDisabled(t.Context(), env.Admin, net30, 1, true); err != nil {
				t.Fatalf("disable NET30: %v", err)
			}
			p20SwitchVendor(t, env, po.ID, "BHARAT", p.step)
			p20WantTerms(t, "switched to BHARAT after NET30 was disabled", env, po.ID, net45, "Net 45 days")
		})
	}
}

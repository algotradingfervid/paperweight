package expenseweb

import (
	"slices"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// S1 (Claim wizard): the step labels, the Claim details step, the hidden
// fields, the line cards and the check page (UX §5.3, §5.4, §5.6).

// claimStepLabels are the claim's step labels (spec §12).
func claimStepLabels(_ Deps) map[string]string {
	return map[string]string{"items": "Expenses"}
}

// claimSteps are the claim's custom steps: Claim details (UX §5.3).
func claimSteps(d Deps) map[string]docscreen.Step {
	return map[string]docscreen.Step{"details": {Body: d.detailsBody, Save: d.detailsSave}}
}

// claimLineNoun names the claim's lines (UX §5.4, CD-270 Q1).
func claimLineNoun() docscreen.LineNoun {
	return docscreen.LineNoun{Add: "Add an expense", Title: "Expense", Empty: "No expenses yet. Add one for each bill."}
}

// claimHide hides what a viewer doesn't type or read (UX §5 intro): the
// employee as an input from everyone but an Administrator (CD-327 (2)), the
// receipt hash (the line
// page posts it itself), the period pair, and Billed to while the company
// has one branch GSTIN, which Validate then fills.
func claimHide(_ Deps) func(docscreen.Ctx, doctype.Field) bool {
	return func(c docscreen.Ctx, f doctype.Field) bool {
		switch f.Name {
		case "employee_id":
			return !isAdmin(c.Principal) && asInput(c)
		case "receipt_sha256": // the line page and the card draw it themselves
			return !asPair(c)
		case "period_from", "period_to": // the page's Period pair shows them
			return !asPair(c)
		case "billed_to_id":
			return oneBranchGSTIN(c)
		}
		return false
	}
}

// asInput reports whether the employee would be an input: on a new
// document (no View) or a draft, never on a listing (the list, search,
// sort, filter and CSV, where View is nil too). Past draft and on listings,
// approvers and readers see the claimant (CD-327 (2)).
func asInput(c docscreen.Ctx) bool {
	return !c.Listing && (c.View == nil || c.View.Info.State == doctype.StateDraft)
}

// isAdmin reports whether p holds the Administrator role.
func isAdmin(p auth.Principal) bool { return slices.Contains(p.Roles, auth.RoleAdministrator) }

// oneBranchGSTIN reports whether the company's enabled branches carry one
// distinct GSTIN. Hide can't return an error: a failed read shows the field,
// and Validate still enforces the rule.
func oneBranchGSTIN(c docscreen.Ctx) bool {
	var n int
	err := c.Deps.DB.Read(c.Req.Context(), func(tx *db.Tx) error {
		var err error
		n, err = expense.BranchGSTINs(c.Req.Context(), tx)
		return err
	})
	return err == nil && n == 1
}

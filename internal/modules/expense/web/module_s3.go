package expenseweb

import (
	"fmt"
	"slices"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// S3 (Pages): the claim's and the advance's document page and reason page
// additions, and the advance's hidden fields (UX §5.7 to §5.12).

// The hints and notes of the reason pages (UX §5.9, §5.12; spec §4, §6).
const (
	hintHowPaid     = "For example how it was paid"
	hintReturned    = `For example "₹2,000 returned by NEFT 1234 on 30-11-2026"`
	noteLeftOnClose = "%s is left on this advance. Closing treats it as returned."
)

// claimPage is the claim's document page additions (UX §5.7, §5.8): the
// warnings while it can still change, the draft notice for a reader, the
// Period and Closed pairs, and only the files no line holds (UX-E7).
func claimPage(d Deps) func(docscreen.Ctx) (docscreen.PageExtra, error) {
	return func(c docscreen.Ctx) (docscreen.PageExtra, error) {
		x := docscreen.PageExtra{Pairs: employeePair(c)}
		v := c.View
		switch v.Info.State {
		case doctype.StateDraft, doctype.StatePending, doctype.StateRejected:
			ws, err := d.Service.ClaimWarnings(c.Req.Context(), c.Principal, v.Info.ID)
			if err != nil {
				return x, err
			}
			x.Warnings = ws
		}
		if v.Info.State == doctype.StateDraft {
			x.Notice = &components.BannerProps{Message: v.Info.Party + " hasn't submitted this claim yet."}
		}
		if from, to := v.Display["period_from"], v.Display["period_to"]; from != "" && to != "" {
			x.Pairs = append(x.Pairs, components.Pair{Label: "Period", Value: from + " to " + to})
		}
		x.Pairs = append(x.Pairs, closedPair(v)...)
		files, err := otherFiles(c, d)
		if err != nil {
			return x, err
		}
		x.Files = &docscreen.FilesCard{Title: "Other files", Items: files}
		return x, nil
	}
}

// claimReason adds the close hint (spec §6).
func claimReason(_ Deps) func(docscreen.Ctx, doctype.Action) (docscreen.ReasonExtra, error) {
	return func(_ docscreen.Ctx, a doctype.Action) (docscreen.ReasonExtra, error) {
		if a != doctype.ActionClose {
			return docscreen.ReasonExtra{}, nil
		}
		return docscreen.ReasonExtra{Hint: hintHowPaid}, nil
	}
}

// advancePage is the advance's document page additions (UX §5.11).
func advancePage(d Deps) func(docscreen.Ctx) (docscreen.PageExtra, error) {
	return func(c docscreen.Ctx) (docscreen.PageExtra, error) { return advanceExtra(c, d) }
}

// advanceReason adds, on Close, the note on what's left (EXP-059) and the
// hint for how it was settled; with nothing left, the claims' hint.
func advanceReason(d Deps) func(docscreen.Ctx, doctype.Action) (docscreen.ReasonExtra, error) {
	return func(c docscreen.Ctx, a doctype.Action) (docscreen.ReasonExtra, error) {
		if a != doctype.ActionClose {
			return docscreen.ReasonExtra{}, nil
		}
		left, err := d.Service.LeftOn(c.Req.Context(), c.Principal, c.View.Info.ID)
		if err != nil {
			return docscreen.ReasonExtra{}, err
		}
		if left <= 0 {
			return docscreen.ReasonExtra{Hint: hintHowPaid}, nil
		}
		return docscreen.ReasonExtra{Note: fmt.Sprintf(noteLeftOnClose, types.FormatINR(left)), Hint: hintReturned}, nil
	}
}

// advanceHide hides used_amount and paid_on, which the page shows as pairs,
// and the employee from everyone but an Administrator (UX §5 intro).
func advanceHide(_ Deps) func(docscreen.Ctx, doctype.Field) bool {
	return func(c docscreen.Ctx, f doctype.Field) bool {
		switch f.Name {
		case "used_amount", "paid_on":
			return true
		case "employee_id":
			return !slices.Contains(c.Principal.Roles, auth.RoleAdministrator)
		}
		return false
	}
}

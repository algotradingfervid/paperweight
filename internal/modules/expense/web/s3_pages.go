package expenseweb

import (
	"context"
	"errors"
	"strconv"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// closedPair is the pair Closed with the close reason on a closed document,
// so how it was settled shows without opening History (UX §5.7, §5.11).
func closedPair(v *docscreen.DocView) []components.Pair {
	if v.Info.State != doctype.StateClosed || v.Origin.StateReason == "" {
		return nil
	}
	return []components.Pair{{Label: "Closed", Value: v.Origin.StateReason}}
}

// employeePair names the employee in Details when the module hides the
// employee field from the viewer (UX §5 intro): docscreen then leaves out
// the party too (CD-319 (4)), and the page must still say whose it is.
func employeePair(c docscreen.Ctx) []components.Pair {
	f, ok := c.DT.Field("employee_id")
	if hide := c.Deps.Modules[c.DT.Name].Hide; !ok || hide == nil || !hide(c, f) || c.View.Info.Party == "" {
		return nil
	}
	return []components.Pair{{Label: "Employee", Value: c.View.Info.Party}}
}

// otherFiles are the claim's files that no line holds as its receipt
// (UX-E7, EUX-031): the receipts show beside their lines.
func otherFiles(c docscreen.Ctx, d Deps) ([]components.Attachment, error) {
	v := c.View
	if len(v.Attachments) == 0 {
		return nil, nil
	}
	held := map[string]bool{}
	for _, l := range v.Lines {
		if sha := l.Inputs["receipt_sha256"]; sha != "" {
			held[sha] = true
		}
	}
	metas, err := d.Files.List(c.Req.Context(), c.Principal, v.Record)
	if err != nil {
		return nil, err
	}
	receipt := map[int64]bool{}
	for _, m := range metas {
		receipt[m.ID] = held[m.SHA256]
	}
	var out []components.Attachment
	for _, a := range v.Attachments {
		if !receipt[a.ID] {
			out = append(out, a)
		}
	}
	return out, nil
}

// The advance page's claims card (UX §5.11, CD-248 (1)).
const (
	claimsTitle   = "Claims set against this advance"
	claimsNone    = "No claims set against this advance yet."
	claimsOther   = "Other claims"
	setAgainst    = " set against"
	countsOnceApp = " (counts once approved)"
)

var claimsColumns = []components.Column{
	{Key: "claim", Label: "Claim", Priority: components.PTitle, Code: true, NoWrap: true},
	{Key: "amount", Label: "Set against", Priority: components.PFigure, Num: true},
	{Key: "date", Label: "Claim date", Priority: components.PMeta, NoWrap: true},
	{Key: "state", Label: "State", Priority: components.PState},
}

// claimsView is the claims card: the claims the viewer can read, and what
// the others set against the advance (blank when nothing).
type claimsView struct {
	rows  []components.Row
	other string
}

// advanceExtra is the advance page's additions: the G7 warnings while it
// can still change; once approved, the Paid on, Used and Left pairs, the
// claims card, and Mark as paid or Undo paid for a user holding cancel.
func advanceExtra(c docscreen.Ctx, d Deps) (docscreen.PageExtra, error) {
	x := docscreen.PageExtra{Pairs: employeePair(c)}
	v := c.View
	ctx := c.Req.Context()
	switch v.Info.State {
	case doctype.StateDraft, doctype.StatePending:
		ws, err := d.Service.AdvanceWarnings(ctx, c.Principal, v.Info.ID)
		if err != nil {
			return x, err
		}
		x.Warnings = ws
		return x, nil
	case doctype.StateApproved, doctype.StateClosed:
	default:
		return x, nil
	}
	var (
		amount, used types.Money
		paidOn       string
		blockers     []doc.Blocker
		card         claimsView
	)
	err := c.Deps.DB.Read(ctx, func(tx *db.Tx) error {
		rec, err := c.Deps.Engine.Load(ctx, tx, expense.AdvanceDocType, v.Info.ID)
		if err != nil {
			return err
		}
		if amount, err = money(rec.Values["amount"]); err != nil {
			return err
		}
		if used, err = money(rec.Values["used_amount"]); err != nil {
			return err
		}
		paidOn = rec.Values["paid_on"]
		if blockers, err = doc.LinkedBlockers(ctx, tx, expense.AdvanceDocType, v.Info.ID); err != nil {
			return err
		}
		card, err = claimsAgainst(ctx, tx, c, d, blockers, used)
		return err
	})
	if err != nil {
		return x, err
	}
	paid := "Not paid yet"
	if paidOn != "" {
		on, err := types.DateFromISO(paidOn)
		if err != nil {
			return x, err
		}
		paid = types.FormatDate(on)
	}
	x.Pairs = append(x.Pairs, []components.Pair{
		{Label: "Paid on", Value: paid},
		{Label: "Used", Value: types.FormatINR(used), Num: true},
		{Label: "Left", Value: types.FormatINR(amount - used), Num: true},
	}...)
	x.Pairs = append(x.Pairs, closedPair(v)...)
	x.Cards = append(x.Cards, claimsCard(card))
	if v.Info.State != doctype.StateApproved {
		return x, nil
	}
	switch err := d.Auth.Can(ctx, c.Principal, expense.AdvanceDocType, auth.RightCancel, &v.Record); {
	case errors.Is(err, auth.ErrForbidden), errors.Is(err, auth.ErrNotFound):
		return x, nil
	case err != nil:
		return x, err
	}
	base := "/modules/expense/advances/" + strconv.FormatInt(v.Info.ID, 10)
	switch {
	case paidOn == "":
		x.Primary = &components.ButtonProps{Label: "Mark as paid", Href: base + "/paid"}
	case len(blockers) == 0:
		x.More = []components.MoreAction{{Label: "Undo paid", Href: base + "/unpaid"}}
	}
	return x, nil
}

// claimsAgainst lists each live claim naming the advance that the viewer
// can read (CheckLinkTx, CD-312), in LinkedBlockers' order. The claims the
// viewer can't read are never named: they show only as what they set
// against the advance, the stored Used less the listed approved and closed
// claims (CD-248 (1), CD-233, CD-240); hidden pending claims add nothing.
func claimsAgainst(ctx context.Context, tx *db.Tx, c docscreen.Ctx, d Deps, bs []doc.Blocker, used types.Money) (claimsView, error) {
	var out claimsView
	listed := types.Money(0)
	for _, b := range bs {
		switch err := d.Auth.CheckLinkTx(ctx, tx, c.Principal, b.DocType, b.ID); {
		case errors.Is(err, auth.ErrForbidden), errors.Is(err, auth.ErrNotFound):
			continue
		case err != nil:
			return out, err
		}
		rec, err := c.Deps.Engine.Load(ctx, tx, b.DocType, b.ID)
		if err != nil {
			return out, err
		}
		less, err := money(rec.Values["less_advance"])
		if err != nil {
			return out, err
		}
		date, err := types.DateFromISO(rec.Values["claim_date"])
		if err != nil {
			return out, err
		}
		amount := types.FormatINR(less) + setAgainst
		if b.State == doctype.StatePending {
			amount += countsOnceApp
		} else {
			listed += less
		}
		out.rows = append(out.rows, components.Row{
			Href:   "/doc/" + b.DocType + "/" + strconv.FormatInt(b.ID, 10),
			Values: map[string]string{"claim": rec.Info.Display, "amount": amount, "date": types.FormatDate(date)},
			State:  b.State,
		})
	}
	if other := used - listed; other > 0 {
		out.other = types.FormatINR(other) + setAgainst
	}
	return out, nil
}

// money parses a stored money value: an approved or closed advance always
// has its amount, and a live claim its less advance.
func money(plain string) (types.Money, error) { return types.ParseMoney(plain) }

package docscreen

import (
	"context"
	"strconv"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// Place is where an action shows.
type Place int

// Places.
const (
	BarPrimary Place = iota
	BarSecondary
	More
)

// ActionButton is one action the user may take. Href is the GET page for a
// reason or confirm step (or Print's URL); "" is a direct POST.
type ActionButton struct {
	Action          doctype.Action
	Label           string
	Place           Place
	Danger, Confirm bool
	Href            string
}

// actionPrint is the Print item's action name; it is FILE's page, not a
// DOC action.
const actionPrint doctype.Action = "print"

// pageRights are the rights the document page asks about.
var pageRights = []auth.Right{"write", "submit", "approve", "cancel", "amend", "delete", "print"}

// Rights asks Can once per right the document page uses.
func Rights(ctx context.Context, a auth.Service, p auth.Principal, name string, rec *auth.RecordRef) map[auth.Right]bool {
	m := make(map[auth.Right]bool, len(pageRights))
	for _, r := range pageRights {
		m[r] = a.Can(ctx, p, name, r, rec) == nil
	}
	return m
}

// defaultPrintURL is Print's URL when Deps.PrintURL is nil.
func defaultPrintURL(name string, id int64) string {
	return "/print/" + name + "/" + strconv.FormatInt(id, 10)
}

func (d Deps) printURL(name string, id int64) string {
	if d.PrintURL != nil {
		return d.PrintURL(name, id)
	}
	return defaultPrintURL(name, id)
}

// Actions is the action bar and "More actions" list for v's state and the
// user's rights (spec R4, DOC-094, design-system §8.4, §8.5). Print goes to
// v.PrintURL, which View sets from Deps.PrintURL, or else to the default.
// A POST item carries v's version (CD-94).
func Actions(v DocView, printable bool, r map[auth.Right]bool) (bar components.ActionBarProps, more []components.MoreAction) {
	base := docURL(v.Info.DocType, v.Info.ID)
	for _, b := range buttons(v, printable, r) {
		switch b.Place {
		case BarPrimary, BarSecondary:
			bp := &components.ButtonProps{Label: b.Label, Href: b.Href, Variant: components.Secondary}
			if b.Place == BarPrimary {
				bp.Variant = components.Primary
			}
			if b.Href == "" {
				bp.Busy = b.Label + "…"
				bp.Attrs = map[string]any{"form": actionFormID(b.Action)}
			}
			if b.Place == BarPrimary {
				bar.Primary = bp
			} else {
				bar.Secondary = bp
			}
		case More:
			m := components.MoreAction{Label: b.Label, Href: b.Href, Danger: b.Danger, NewTab: b.Action == actionPrint}
			if b.Href == "" {
				m.Href, m.Post = base+"/"+string(b.Action), true
				m.Hidden = []components.Param{{Name: "version", Value: strconv.Itoa(v.Version)}}
			}
			more = append(more, m)
		}
	}
	if v.Info.State == doctype.StatePending && r["approve"] && v.ApproveBlocked != "" {
		bar.Note = v.ApproveBlocked
	}
	return bar, more
}

// actionFormID is the id of the page's hidden form a bar button posts.
func actionFormID(a doctype.Action) string { return "act-" + string(a) }

// buttons lists the allowed actions in display order.
func buttons(v DocView, printable bool, r map[auth.Right]bool) []ActionButton {
	base := docURL(v.Info.DocType, v.Info.ID)
	var out []ActionButton
	add := func(b ActionButton) { out = append(out, b) }
	switch v.Info.State {
	case doctype.StateDraft:
		if r["submit"] {
			label := "Submit"
			if v.Approval.Needed {
				label = "Submit for approval"
			}
			add(ActionButton{Action: doctype.ActionSubmit, Label: label, Place: BarPrimary})
		}
		if r["delete"] {
			add(ActionButton{Action: doctype.ActionDelete, Label: "Delete draft", Place: More, Danger: true, Confirm: true, Href: base + "/delete"})
		}
	case doctype.StatePending:
		approver := r["approve"] && v.ApproveBlocked == ""
		if approver {
			add(ActionButton{Action: doctype.ActionApprove, Label: "Approve", Place: BarPrimary})
			add(ActionButton{Action: doctype.ActionReject, Label: "Reject", Place: BarSecondary, Confirm: true, Href: base + "/reject"})
		}
		if r["submit"] {
			// The bar holds Withdraw only when it shows no approve buttons
			// and no note (a note replaces the bar's buttons).
			place := BarSecondary
			if approver || (r["approve"] && v.ApproveBlocked != "") {
				place = More
			}
			add(ActionButton{Action: doctype.ActionWithdraw, Label: "Withdraw", Place: place})
		}
	case doctype.StateRejected:
		if r["write"] {
			add(ActionButton{Action: doctype.ActionRevise, Label: "Revise", Place: BarPrimary})
		}
	case doctype.StateApproved:
		if r["submit"] {
			add(ActionButton{Action: doctype.ActionClose, Label: "Close", Place: More, Confirm: true, Href: base + "/close"})
		}
		if r["cancel"] {
			add(ActionButton{Action: doctype.ActionCancel, Label: "Cancel", Place: More, Danger: true, Confirm: true, Href: base + "/cancel"})
		}
	case doctype.StateCancelled:
		if r["amend"] {
			add(ActionButton{Action: doctype.ActionAmend, Label: "Amend", Place: BarPrimary})
		}
	}
	if r["print"] && printable {
		href := v.PrintURL
		if href == "" {
			href = defaultPrintURL(v.Info.DocType, v.Info.ID)
		}
		add(ActionButton{Action: actionPrint, Label: "Print", Place: More, Href: href})
	}
	return out
}

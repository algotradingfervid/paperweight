package docscreen

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/layout"
)

// reasonPage is a reason page (reject, cancel, close; design-system §8.5)
// or, with no label, the delete confirm page.
type reasonPage struct {
	action    doctype.Action
	right     auth.Right // the GET also needs it (CD-46)
	verb      string     // title: "{verb} {Display}"
	label     string     // the reason field's label; "" = no reason
	maxLen    int
	button    string // "{button}" or, with withLabel, "{button} {label, lower}"
	withLabel bool
	danger    bool
}

var reasonPages = map[string]reasonPage{
	"reject": {action: doctype.ActionReject, right: "approve", verb: "Reject", label: "Reason for rejecting", maxLen: 1000, button: "Reject and send back"},
	"cancel": {action: doctype.ActionCancel, right: "cancel", verb: "Cancel", label: "Reason for cancelling", maxLen: 500, button: "Cancel", withLabel: true, danger: true},
	"close":  {action: doctype.ActionClose, right: "submit", verb: "Close", label: "Reason for closing", maxLen: 500, button: "Close", withLabel: true},
	"delete": {action: doctype.ActionDelete, right: "delete", button: "Delete draft", danger: true},
}

// reasonView is everything a reason or confirm page renders.
type reasonView struct {
	Opts     layout.PageOpts
	Summary  string
	Action   string
	Version  int
	Field    components.FieldProps // the reason; unused on the delete page
	MaxLen   int
	Question string // the delete page's question
	Button   components.ButtonProps
	Danger   string
	Link     *components.Link // the document that refused a cancel
	Errors   []components.FieldErr
}

func trimReason(s string) string { return strings.TrimSpace(s) }

// reasonGet shows the page to a user who may read the document and holds
// the page's right; anyone else gets AUTH's error (CD-46).
func (d Deps) reasonGet(w http.ResponseWriter, r *http.Request, dt *doctype.DocType, id int64, name string, rp reasonPage) {
	p := d.principal(r)
	v, err := d.View(r.Context(), p, dt.Name, id)
	if err != nil {
		problem(w, r, err)
		return
	}
	if err := d.Auth.Can(r.Context(), p, dt.Name, rp.right, &v.Record); err != nil {
		problem(w, r, err)
		return
	}
	rv := reasonViewOf(dt, v, name, rp, v.Version, "", Problem{})
	ui.Render(w, r, http.StatusOK, reasonPageFull(rv), reasonPageMain(rv))
}

// reasonPost re-renders the page after a refused POST, keeping the typed
// reason and the posted version (CD-94), with pr's status.
func (d Deps) reasonPost(w http.ResponseWriter, r *http.Request, dt *doctype.DocType, id int64, name string, rp reasonPage, version int, pr Problem) {
	v, err := d.View(r.Context(), d.principal(r), dt.Name, id)
	if err != nil {
		problem(w, r, err)
		return
	}
	rv := reasonViewOf(dt, v, name, rp, version, r.PostFormValue("reason"), pr)
	if rp.action == doctype.ActionCancel && rv.Danger != "" {
		rv.Link = d.blockerLink(r.Context(), dt.Name, id)
	}
	ui.Render(w, r, pr.Status, reasonPageFull(rv), reasonPageMain(rv))
}

// blockerLink links the one document that blocks a cancel (DOC-092). Several
// are only listed, in the message. The message stands alone, so a failed read
// just leaves the link out.
func (d Deps) blockerLink(ctx context.Context, name string, id int64) *components.Link {
	var bs []doc.Blocker
	err := d.DB.Read(ctx, func(tx *db.Tx) (err error) {
		bs, err = doc.LinkedBlockers(ctx, tx, name, id)
		return err
	})
	if err != nil || len(bs) != 1 {
		return nil
	}
	return &components.Link{Label: naming.Label(bs[0].Number, bs[0].ID), Href: docURL(bs[0].DocType, bs[0].ID)}
}

func reasonViewOf(dt *doctype.DocType, v DocView, name string, rp reasonPage, version int, typed string, pr Problem) reasonView {
	base := docURL(dt.Name, v.Info.ID)
	rv := reasonView{
		Opts:    layout.PageOpts{Title: rp.verb + " " + v.Info.Display, Width: layout.Form, Back: &components.Link{Label: v.Info.Display, Href: base}},
		Action:  base + "/" + name,
		Version: version,
		MaxLen:  rp.maxLen,
		Button:  components.ButtonProps{Label: rp.button, Variant: components.Primary},
	}
	var parts []string
	for _, s := range []string{v.Info.Display, v.Info.Party, v.GrandTotal} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	rv.Summary = strings.Join(parts, " · ")
	if rp.withLabel {
		rv.Button.Label += " " + strings.ToLower(dt.Label)
	}
	if rp.danger {
		rv.Button.Variant = components.Danger
	}
	rv.Button.Busy = rv.Button.Label + "…"
	if rp.label == "" {
		rv.Opts.Title = rp.button
		rv.Question = "Delete " + v.Info.Display + "? This can't be undone."
	} else {
		rv.Field = components.FieldProps{ID: components.FieldID("reason"), Name: "reason", Label: rp.label, Value: typed}
	}
	i := slices.IndexFunc(pr.Fields, func(f doctype.FieldError) bool { return f.Field == "reason" })
	if i >= 0 && rp.label != "" {
		rv.Field.Error = pr.Fields[i].Message
		rv.Errors = []components.FieldErr{{ID: rv.Field.ID, Message: rv.Field.Error}}
	} else {
		rv.Danger = pr.Message
		rv.Errors = fieldErrs(pr.Fields)
	}
	rv.Opts.Error = rv.Danger != "" || len(rv.Errors) > 0
	return rv
}

// info reads the document's Info in a read transaction.
func (d Deps) info(ctx context.Context, name string, id int64) (doctype.Info, error) {
	var info doctype.Info
	err := d.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		info, err = d.Engine.Info(ctx, tx, name, id)
		return err
	})
	return info, err
}

func reasonOpts(rv reasonView) layout.PageOpts {
	o := rv.Opts
	o.BannerSlot = reasonBanners(rv)
	return o
}

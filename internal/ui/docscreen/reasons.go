package docscreen

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/layout"
)

// reasonPage is a reason page (reject, cancel, close; design-system §8.5)
// or, with no label, the delete confirm page.
type reasonPage struct {
	action    doctype.Action
	right     auth.Right // the GET also needs it (CD-46); close's is closeRight's
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
	"close":  {action: doctype.ActionClose, verb: "Close", label: "Reason for closing", maxLen: 500, button: "Close", withLabel: true},
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
	Note     string // the module's note above the field (hooks spec §4.4)
	NoteWarn bool   // the note is a warning (CD-326 (7))
}

func trimReason(s string) string { return strings.TrimSpace(s) }

// reasonGet shows the page to a user who may read the document and holds
// the page's right; anyone else gets AUTH's error (CD-46). The close page
// needs the DocType's CloseRight, and is a 404 when it is CloseNone
// (CD-255, CD-277).
func (d Deps) reasonGet(w http.ResponseWriter, r *http.Request, dt *doctype.DocType, id int64, name string, rp reasonPage) {
	right := rp.right
	if rp.action == doctype.ActionClose {
		var ok bool
		if right, ok = closeRight(dt.Name); !ok {
			problem(w, r, doctype.ErrNotFound)
			return
		}
	}
	p := d.principal(r)
	v, err := d.View(r.Context(), p, dt.Name, id)
	if err != nil {
		problem(w, r, err)
		return
	}
	if err := d.Auth.Can(r.Context(), p, dt.Name, right, &v.Record); err != nil {
		problem(w, r, err)
		return
	}
	rv, err := d.reasonViewWith(r, dt, v, name, rp, v.Version, "", Problem{})
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	ui.Render(w, r, http.StatusOK, reasonPageFull(rv), reasonPageMain(rv))
}

// reasonMissing is pr, the engine's refusal of a reason post sent with no
// reason, with its error on the reason field (DOC's for close and cancel,
// APR's for reject) shown as "Enter a reason." (422). The engine checks the
// right, the approver, the state and the version first, so every other
// refusal is pr unchanged (CD-354).
func reasonMissing(pr Problem) Problem {
	if slices.ContainsFunc(pr.Fields, func(e doctype.FieldError) bool { return e.Field == "reason" }) {
		return Problem{Status: http.StatusUnprocessableEntity, Fields: []doctype.FieldError{{Field: "reason", Message: "Enter a reason."}}}
	}
	return pr
}

// reasonPost re-renders the page after a refused POST, keeping the typed
// reason and the posted version (CD-94), with pr's status.
func (d Deps) reasonPost(w http.ResponseWriter, r *http.Request, dt *doctype.DocType, id int64, name string, rp reasonPage, version int, pr Problem) {
	p := d.principal(r)
	v, err := d.View(r.Context(), p, dt.Name, id)
	if err != nil {
		problem(w, r, err)
		return
	}
	rv, err := d.reasonViewWith(r, dt, v, name, rp, version, r.PostFormValue("reason"), pr)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	if pr.Blocked {
		rv.Link = d.blockerLink(r.Context(), p, dt.Name, id)
	}
	notice(w, r, pr)
	ui.Render(w, r, pr.Status, reasonPageFull(rv), reasonPageMain(rv))
}

// blockerLink links the one document that blocks a cancel, for a user who may
// read it: no dead links (DOC-092). Several are only listed, in the message.
// The message stands alone, so a failed check is logged and loses only the link.
func (d Deps) blockerLink(ctx context.Context, p auth.Principal, name string, id int64) *components.Link {
	var link *components.Link
	err := d.DB.Read(ctx, func(tx *db.Tx) error {
		bs, err := doc.LinkedBlockers(ctx, tx, name, id)
		if err != nil || len(bs) != 1 {
			return err
		}
		b := bs[0]
		info, err := d.Engine.Info(ctx, tx, b.DocType, b.ID)
		if err != nil {
			return err
		}
		rec := recordRef(info)
		switch err := d.Auth.CanTx(ctx, tx, p, b.DocType, auth.RightRead, &rec); {
		case errors.Is(err, auth.ErrForbidden), errors.Is(err, auth.ErrNotFound):
			return nil
		case err != nil:
			return err
		}
		link = &components.Link{Label: naming.Label(b.Number, b.ID), Href: docURL(b.DocType, b.ID)}
		return nil
	})
	if err != nil {
		slog.ErrorContext(ctx, "docscreen: blocker link", "doctype", name, "id", id, "user", p.ID, "err", err)
		return nil
	}
	return link
}

// reasonViewWith is reasonViewOf with the module's Reason additions: its
// note above the field and its hint as the field's (hooks spec §4.4). An
// error on a field the module hides is a plain message, and a hidden party
// or figure field leaves the party or the grand total out of the summary
// (CD-319).
func (d Deps) reasonViewWith(r *http.Request, dt *doctype.DocType, v DocView, name string, rp reasonPage, version int, typed string, pr Problem) (reasonView, error) {
	c := d.ctxOf(r, dt, &v)
	extra, err := reasonExtra(c, rp.action)
	if err != nil {
		return reasonView{}, err
	}
	pr.Fields = unhiddenAll(c, pr.Fields)
	hideSummary(c, &v)
	rv := reasonViewOf(dt, v, name, rp, version, typed, pr)
	rv.Note, rv.NoteWarn, rv.Field.Hint = extra.Note, extra.NoteWarn, extra.Hint
	return rv, nil
}

// reasonField is the reason field, with the module's note above it as an
// info banner when there is one, or a warning banner with NoteWarn.
func reasonField(rv reasonView) templ.Component {
	field := components.Textarea(rv.Field, rv.MaxLen)
	if rv.Note == "" {
		return field
	}
	kind := components.Info
	if rv.NoteWarn {
		kind = components.Warning
	}
	return templ.Join(components.Banner(components.BannerProps{Kind: kind, Message: rv.Note}), field)
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
		rv.Errors = pageErrs(pr.Fields)
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

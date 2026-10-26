package docscreen

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/layout"
)

// directActions are the actions posted straight from the document page.
var directActions = map[string]doctype.Action{
	"submit":   doctype.ActionSubmit,
	"withdraw": doctype.ActionWithdraw,
	"approve":  doctype.ActionApprove,
	"revise":   doctype.ActionRevise,
	"amend":    doctype.ActionAmend,
}

// badVersionText answers a POST without a usable version (CD-146's
// errBadForm wording, CD-148).
const badVersionText = "The form could not be read. Reload the page and try again."

// pageView is everything the document page renders.
type pageView struct {
	DT      *doctype.DocType
	V       DocView
	Bar     components.ActionBarProps
	More    []components.MoreAction
	Forms   []actionForm
	Danger  string // a refused action's message, unchanged
	Errors  []components.FieldErr
	Success string // an htmx action's banner, or the flash's beside "Next request" (a 303 uses the flash)
	Next    string // "Next request" link after an approve
}

// actionForm is the hidden form a bar button posts through.
type actionForm struct {
	ID, Action string
	Version    int
}

// target reads {doctype} and {id}; an unknown DocType or a bad id is 404.
func target(w http.ResponseWriter, r *http.Request) (*doctype.DocType, int64, bool) {
	dt, ok := doctype.Lookup(chi.URLParam(r, "doctype"))
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if !ok || err != nil {
		problem(w, r, doctype.ErrNotFound)
		return nil, 0, false
	}
	return dt, id, true
}

// page serves GET /doc/{doctype}/{id}. A draft's document page is its
// check page (design-system §8.3).
func (d Deps) page(w http.ResponseWriter, r *http.Request) {
	dt, id, ok := target(w, r)
	if !ok {
		return
	}
	p := d.principal(r)
	v, err := d.View(r.Context(), p, dt.Name, id)
	if err != nil {
		problem(w, r, err)
		return
	}
	if v.Info.State == doctype.StateDraft {
		http.Redirect(w, r, docURL(dt.Name, id)+"/edit/check", http.StatusSeeOther)
		return
	}
	pv := d.pageViewOf(r.Context(), p, dt, v)
	if r.URL.Query().Get("next") == "1" {
		pv.Next = d.InboxURL
		pv.Success = takeSuccess(r.Context())
	}
	ui.Render(w, r, http.StatusOK, docPage(pv), docMain(pv))
}

// takeSuccess takes the flash's first success notice, so the page shows it
// with the "Next request" link straight under it; the other notices go back
// for the layout.
func takeSuccess(ctx context.Context) string {
	var text string
	for _, n := range web.TakeFlash(ctx) {
		if n.Level == "success" && text == "" {
			text = n.Text
			continue
		}
		web.Flash(ctx, n.Level, n.Text)
	}
	return text
}

func (d Deps) pageViewOf(ctx context.Context, p auth.Principal, dt *doctype.DocType, v DocView) pageView {
	rights := Rights(ctx, d.Auth, p, dt.Name, &v.Record)
	pv := pageView{DT: dt, V: v}
	pv.Bar, pv.More = Actions(v, dt.Printable, rights)
	if pv.Bar.Note == "" {
		for _, b := range buttons(v, dt.Printable, rights) {
			if b.Place != More && b.Href == "" {
				pv.Forms = append(pv.Forms, actionForm{ID: actionFormID(b.Action), Action: docURL(dt.Name, v.Info.ID) + "/" + string(b.Action), Version: v.Version})
			}
		}
	}
	return pv
}

// actionGet serves GET /doc/{doctype}/{id}/{action}: only the reason and
// confirm pages have one.
func (d Deps) actionGet(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "action")
	rp, ok := reasonPages[name]
	if !ok {
		problem(w, r, doctype.ErrNotFound)
		return
	}
	dt, id, ok := target(w, r)
	if !ok {
		return
	}
	d.reasonGet(w, r, dt, id, name, rp)
}

// actionPost serves POST /doc/{doctype}/{id}/{action}. DOC checks the right
// of every action (CD-52), so the handler adds none; the posted version is
// passed as is (CD-94), and a missing or non-numeric one is a 400 before
// DOC is called.
func (d Deps) actionPost(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "action")
	a, direct := directActions[name]
	rp, confirm := reasonPages[name]
	if !direct && !confirm {
		problem(w, r, doctype.ErrNotFound)
		return
	}
	dt, id, ok := target(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	p := d.principal(r)
	version, err := strconv.Atoi(r.PostFormValue("version"))
	if err != nil {
		web.Error(w, r, http.StatusBadRequest, badVersionText)
		return
	}
	in := doc.ActionInput{Version: version}
	if confirm {
		a = rp.action
		if rp.label != "" {
			in.Reason = trimReason(r.PostFormValue("reason"))
			if in.Reason == "" {
				d.reasonPost(w, r, dt, id, name, rp, version, Problem{
					Status: http.StatusUnprocessableEntity,
					Fields: []doctype.FieldError{{Field: "reason", Message: "Enter a reason."}},
				})
				return
			}
		}
	}
	var display string
	if a == doctype.ActionDelete {
		info, err := d.info(ctx, dt.Name, id)
		if err != nil {
			problem(w, r, err)
			return
		}
		display = info.Display
	}
	res, err := d.Engine.Do(ctx, p, dt.Name, id, a, in)
	if err != nil {
		pr, ok := Classify(err)
		if !ok {
			web.ErrorFrom(w, r, err)
			return
		}
		if confirm {
			d.reasonPost(w, r, dt, id, name, rp, version, pr)
			return
		}
		if a == doctype.ActionSubmit && pr.Status == http.StatusUnprocessableEntity {
			d.failingStep(w, r, dt, id, pr)
			return
		}
		d.refused(w, r, dt, id, pr)
		return
	}
	d.succeeded(w, r, dt, id, a, res, display)
}

// refused renders the document page with the kernel's message in a danger
// banner and its status (409, 403, 422; CD-44).
func (d Deps) refused(w http.ResponseWriter, r *http.Request, dt *doctype.DocType, id int64, pr Problem) {
	p := d.principal(r)
	v, err := d.View(r.Context(), p, dt.Name, id)
	if err != nil {
		ui.Render(w, r, pr.Status, problemPage(pr), problemMain(pr))
		return
	}
	pv := d.pageViewOf(r.Context(), p, dt, v)
	pv.Danger = pr.Message
	pv.Errors = fieldErrs(pr.Fields)
	ui.Render(w, r, pr.Status, docPage(pv), docMain(pv))
}

// succeeded sends the user on (CD-80): htmx gets the page the action leads
// to (the document page, a check page or the list) with the banners
// rendered and HX-Push-Url; otherwise the banners go in the flash and a 303
// follows.
func (d Deps) succeeded(w http.ResponseWriter, r *http.Request, dt *doctype.DocType, id int64, a doctype.Action, res doc.Result, display string) {
	ctx := r.Context()
	p := d.principal(r)
	text := d.successText(ctx, p, dt.Name, id, a, res, display)
	to, onPage := docURL(dt.Name, id), true
	switch a {
	case doctype.ActionRevise, doctype.ActionWithdraw:
		to, onPage = docURL(dt.Name, id)+"/edit/check", false
	case doctype.ActionAmend:
		to, onPage = docURL(dt.Name, res.ID)+"/edit/check", false
	case doctype.ActionDelete:
		to, onPage = "/doc/"+dt.Name, false
	}
	if ui.IsHTMX(r) && !onPage {
		switch a {
		case doctype.ActionDelete:
			w.Header().Set("HX-Push-Url", to)
			d.renderList(w, r, dt, text, res.Warnings)
		case doctype.ActionAmend:
			d.checkPage(w, r, dt, res.ID, text, res.Warnings)
		default:
			d.checkPage(w, r, dt, id, text, res.Warnings)
		}
		return
	}
	if ui.IsHTMX(r) {
		v, err := d.View(ctx, p, dt.Name, id)
		if err != nil {
			problem(w, r, err)
			return
		}
		pv := d.pageViewOf(ctx, p, dt, v)
		pv.Success = text
		pv.V.Warnings = res.Warnings
		if a == doctype.ActionApprove {
			pv.Next = d.InboxURL
		}
		w.Header().Set("HX-Push-Url", to)
		ui.Render(w, r, http.StatusOK, docPage(pv), docMain(pv))
		return
	}
	if text != "" {
		web.Flash(ctx, "success", text)
	}
	for _, msg := range res.Warnings {
		web.Flash(ctx, "warning", msg)
	}
	if a == doctype.ActionApprove {
		to += "?next=1"
	}
	http.Redirect(w, r, to, http.StatusSeeOther) //nolint:gosec // G710: a local /doc/ path from a registered DocType name and numeric ids
}

// successText is the banner after action a (design-system §8.3, §8.5).
func (d Deps) successText(ctx context.Context, p auth.Principal, name string, id int64, a doctype.Action, res doc.Result, display string) string {
	numbered := "Approved. " + res.Number + " has been numbered."
	switch a {
	case doctype.ActionSubmit:
		switch res.State {
		case doctype.StatePending:
			if av, err := d.Extras.Approval(ctx, p, name, id); err == nil && av.WaitingOn != "" {
				return "Submitted for approval to " + av.WaitingOn + "."
			}
		case doctype.StateApproved:
			return numbered
		}
	case doctype.ActionApprove:
		return numbered
	case doctype.ActionReject:
		return "Rejected and sent back."
	case doctype.ActionWithdraw:
		return "Withdrawn. You can change the draft and submit it again."
	case doctype.ActionAmend:
		if o, err := d.Extras.Origin(ctx, p, name, res.ID); err == nil && o.AmendedFrom != "" {
			return "Amending " + o.AmendedFrom
		}
	case doctype.ActionCancel:
		return "Cancelled."
	case doctype.ActionClose:
		return "Closed."
	case doctype.ActionDelete:
		return "Deleted " + display + "."
	}
	return ""
}

// fieldErrs links each field error to its input's id.
func fieldErrs(fs []doctype.FieldError) []components.FieldErr {
	out := make([]components.FieldErr, len(fs))
	for i, f := range fs {
		path := f.Field
		if f.Line > 0 {
			path = "lines[" + strconv.Itoa(f.Line) + "]." + f.Field
		}
		out[i] = components.FieldErr{ID: components.FieldID(path), Message: f.Message}
	}
	return out
}

func docOpts(pv pageView) layout.PageOpts {
	return layout.PageOpts{
		Title:      pv.V.Info.Display,
		Error:      pv.Danger != "" || len(pv.Errors) > 0,
		BannerSlot: docBanners(pv),
		BarSlot:    docBar(pv),
	}
}

// waitingLine is who or why a pending document waits on.
func waitingLine(pv pageView) string {
	a := pv.V.Approval
	if pv.V.Info.State != doctype.StatePending {
		return ""
	}
	if pv.Bar.Primary != nil && pv.Bar.Primary.Label == "Approve" && a.Preview != "" {
		return "Needs your approval: " + a.Preview
	}
	if a.WaitingOn != "" {
		return "Waiting for approval from " + a.WaitingOn
	}
	return a.Preview
}

func lineCards(lines []LineView) []components.LineCard {
	out := make([]components.LineCard, len(lines))
	for i, l := range lines {
		out[i] = l.Card
	}
	return out
}

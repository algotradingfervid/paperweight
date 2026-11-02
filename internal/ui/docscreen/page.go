package docscreen

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

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
	Danger  string           // a refused action's message, unchanged
	Reload  *components.Link // a stale version's document and page: Danger shows as the ConflictBanner
	Errors  []components.FieldErr
	Success string // an htmx action's banner, or the flash's beside "Next request" (a 303 uses the flash)
	Next    string // "Next request" link after an approve
	Attach  *attachView
	Extra   PageExtra                 // the module's additions (hooks spec §4.4)
	Lines   *components.LineListProps // the line cards and display grid (CD-313)
	Facts   []components.Pair         // the module's KeyFacts the viewer may see (UI v2 §5.13)
	Party   string                    // the party line; "" when KeyFacts names the party field
	Steps   []components.LifeStep     // the lifecycle with dates and the module's extra steps (UI v2 §5.13 item 4)
	Block   bool                      // Success shows as the success block: it follows a Submit (A-25)
	Follow  []components.Link         // the success block's follow-ups the viewer may use
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
// check page for a user who may change it, and the read-only document page
// for every other reader (design-system §8.3, CD-263 G1).
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
	if v.Info.State == doctype.StateDraft && d.canWrite(r, dt, v) {
		http.Redirect(w, r, docURL(dt.Name, id)+"/edit/check", http.StatusSeeOther)
		return
	}
	pv, err := d.pageViewOf(r, p, dt, v)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	next, submitted := r.URL.Query().Get("next") == "1", r.URL.Query().Get("submitted") == "1"
	if next || submitted {
		pv.Success = takeSuccess(r.Context()) // once: a second take finds the flash empty
	}
	if next {
		pv.Next = d.InboxURL
	}
	if submitted {
		d.successBlock(r, p, &pv)
	}
	ui.Render(w, r, http.StatusOK, docPage(pv), docMain(pv))
}

// successBlock shows pv's Success, when there is one, as the success block
// after a Submit (UI v2 §5.13 item 2, A-25), with "New {label}" for a viewer
// who may create and "Go to {label}" when the module declares a Home.
func (d Deps) successBlock(r *http.Request, p auth.Principal, pv *pageView) {
	if pv.Success == "" {
		return
	}
	pv.Block = true
	if d.can(r, p, pv.DT.Name, auth.RightCreate) {
		pv.Follow = append(pv.Follow, newDocLink(pv.DT))
	}
	if h := d.modOf(pv.DT).Home; h.Href != "" {
		pv.Follow = append(pv.Follow, components.Link{Label: "Go to " + h.Label, Href: h.Href})
	}
}

// newDocLink is "New {label}" to dt's new-document page.
func newDocLink(dt *doctype.DocType) components.Link {
	return components.Link{Label: "New " + strings.ToLower(dt.Label), Href: "/doc/" + dt.Name + "/new"}
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

// pageViewOf is document v's page: its Details, display values, party and
// grand total without the fields the module hides, Header and HeaderNames kept parallel, and the
// module's line summaries (CD-256 (1)), and the module's Page additions,
// whose error it returns (hooks spec §4.4).
func (d Deps) pageViewOf(r *http.Request, p auth.Principal, dt *doctype.DocType, v DocView) (pageView, error) {
	rights := Rights(r.Context(), d.Auth, p, dt.Name, &v.Record)
	c := d.ctxOf(r, dt, &v)
	shown := v
	shown.Header, shown.HeaderNames, shown.Display = nil, nil, map[string]string{}
	for n, name := range v.HeaderNames {
		if f, ok := dt.Field(name); ok && hidden(c, f) {
			continue
		}
		shown.Header = append(shown.Header, v.Header[n])
		shown.HeaderNames = append(shown.HeaderNames, name)
	}
	for name, s := range v.Display {
		if f, ok := dt.Field(name); !ok || !hidden(c, f) {
			shown.Display[name] = s
		}
	}
	hideSummary(c, &shown)
	applyLineCards(c, &shown)
	extra, err := pageExtra(c)
	if err != nil {
		return pageView{}, err
	}
	noteLines(shown.Lines, extra.LineNotes)
	facts := d.modOf(dt).KeyFacts
	steps, err := pageSteps(dt, shown, d.modOf(dt).LifeSteps, extra.Life)
	if err != nil {
		return pageView{}, err
	}
	pv := pageView{DT: dt, V: shown, Extra: extra, Attach: attachOf(dt, v, rights), Lines: d.lineList(r, dt, shown), Facts: keyFacts(facts, shown), Party: shown.Info.Party, Steps: steps}
	if pf, ok := doc.PartyField(dt); ok && slices.Contains(facts, pf.Name) {
		pv.Party = "" // the facts show it
	}
	pv.Bar, pv.More = Actions(v, dt.Printable, rights)
	if err := applyPageExtra(&pv); err != nil {
		return pageView{}, err
	}
	if pv.Bar.Note == "" {
		for _, b := range buttons(v, dt.Printable, rights) {
			if b.Place != More && b.Href == "" {
				pv.Forms = append(pv.Forms, actionForm{ID: actionFormID(b.Action), Action: docURL(dt.Name, v.Info.ID) + "/" + string(b.Action), Version: v.Version})
			}
		}
	}
	return pv, nil
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
	if _, closable := closeRight(dt.Name); confirm && rp.action == doctype.ActionClose && !closable {
		problem(w, r, doctype.ErrNotFound) // CloseNone: no close page to post to (CD-277)
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
			in.Reason = trimReason(r.PostFormValue("reason")) // an empty one is the engine's to refuse (CD-354)
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
			if in.Reason == "" {
				pr = reasonMissing(pr)
			}
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

// refused renders the document page with the kernel's message and its status
// (409, 403, 422; CD-44), in the ConflictBanner on a stale version, else in a
// danger banner. An error on a field the module hides is a plain message, linked to
// #main (CD-319).
func (d Deps) refused(w http.ResponseWriter, r *http.Request, dt *doctype.DocType, id int64, pr Problem) {
	p := d.principal(r)
	v, err := d.View(r.Context(), p, dt.Name, id)
	if err != nil {
		notice(w, r, pr)
		ui.Render(w, r, pr.Status, problemPage(pr), problemMain(pr))
		return
	}
	pv, err := d.pageViewOf(r, p, dt, v)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	pv.Danger, pv.Reload = pr.Message, conflictReload(pr, v.Info.Display, docURL(dt.Name, id))
	pv.Errors = pageErrs(unhiddenAll(d.ctxOf(r, dt, &v), pr.Fields))
	notice(w, r, pr)
	ui.Render(w, r, pr.Status, docPage(pv), docMain(pv))
}

// conflictReload is the ConflictBanner's Reload for pr, naming the document
// and going to href, or nil unless pr is a stale version (CD-362). A refused
// or blocked 409 stays a danger banner: reloading changes nothing.
func conflictReload(pr Problem, label, href string) *components.Link {
	if !pr.Stale {
		return nil
	}
	return &components.Link{Label: label, Href: href}
}

// succeeded sends the user on (CD-80): htmx gets the page the action leads
// to (the document page, a check page or the list) with the banners
// rendered and HX-Push-Url; otherwise the banners go in the flash and a 303
// follows.
func (d Deps) succeeded(w http.ResponseWriter, r *http.Request, dt *doctype.DocType, id int64, a doctype.Action, res doc.Result, display string) {
	ctx := r.Context()
	p := d.principal(r)
	text := d.successText(ctx, p, dt, id, a, res, display)
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
		pv, err := d.pageViewOf(r, p, dt, v)
		if err != nil {
			web.ErrorFrom(w, r, err)
			return
		}
		pv.Success = text
		pv.V.Warnings = res.Warnings
		if a == doctype.ActionApprove {
			pv.Next = d.InboxURL
		}
		if a == doctype.ActionSubmit {
			d.successBlock(r, p, &pv)
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
	switch a {
	case doctype.ActionApprove:
		to += "?next=1"
	case doctype.ActionSubmit:
		to += "?submitted=1"
	}
	http.Redirect(w, r, to, http.StatusSeeOther) //nolint:gosec // G710: a local /doc/ path from a registered DocType name and numeric ids
}

// successText is the banner after action a (design-system §8.3, §8.5).
func (d Deps) successText(ctx context.Context, p auth.Principal, dt *doctype.DocType, id int64, a doctype.Action, res doc.Result, display string) string {
	numbered := doctype.StateLabel(*dt, doctype.StateApproved) + ". " + res.Number + " has been numbered."
	switch a {
	case doctype.ActionSubmit:
		switch res.State {
		case doctype.StatePending:
			if av, err := d.Extras.Approval(ctx, p, dt.Name, id); err == nil && av.WaitingOn != "" {
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
		if o, err := d.Extras.Origin(ctx, p, dt.Name, res.ID); err == nil && o.AmendedFrom != "" {
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
		id := components.FieldID(f.Field)
		if f.Line > 0 {
			id = LineFieldID(f.Line, f.Field)
		}
		out[i] = components.FieldErr{ID: id, Message: f.Message}
	}
	return out
}

// pageErrs is fieldErrs for a page with no inputs to link to by field: an
// error with no field links to #main.
func pageErrs(fs []doctype.FieldError) []components.FieldErr {
	out := fieldErrs(fs)
	for i, f := range fs {
		if f.Field == "" {
			out[i].ID = "main" // no field to link to
		}
	}
	return out
}

func docOpts(pv pageView) layout.PageOpts {
	return layout.PageOpts{
		Title:      pv.V.Info.Display,
		Focus:      true, // the action bar is the page's bottom bar (UI v2 §5.2)
		NoHeading:  true, // the header card has the h1 (UI v2 §5.13)
		Back:       &components.Link{Label: pv.DT.PluralLabel, Href: listURL(pv.DT.Name, ListQuery{})},
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
	approver := pv.Bar.Primary != nil && pv.Bar.Primary.Label == "Approve"
	if approver && a.Roles != "" {
		return "Needs your approval: " + a.Roles + ", " + a.Why
	}
	if approver && a.Preview != "" {
		return "Needs your approval: " + a.Preview
	}
	if a.WaitingOn != "" {
		return "Waiting for approval from " + a.WaitingOn
	}
	return a.Preview
}

// lineList is v's line cards with no actions, as the check and document
// pages show them, and the module's display grid (CD-313).
func (d Deps) lineList(r *http.Request, dt *doctype.DocType, v DocView) *components.LineListProps {
	cards := lineCards(v.Lines)
	return &components.LineListProps{Cards: cards, Total: len(v.Lines), Grid: d.displayRows(r, dt, v, cards), Display: true}
}

func lineCards(lines []LineView) []components.LineCard {
	out := make([]components.LineCard, len(lines))
	for i, l := range lines {
		out[i] = l.Card
	}
	return out
}

// keyFacts is the Details pair of each field names lists, in that order,
// from v after the module's Hide (pageViewOf), so a hidden field has none.
func keyFacts(names []string, v DocView) []components.Pair {
	var out []components.Pair
	for _, name := range names {
		if i := slices.Index(v.HeaderNames, name); i >= 0 {
			out = append(out, v.Header[i])
		}
	}
	return out
}

// CheckKeyFacts reports the first Module.KeyFacts name that isn't a header
// field its DocType declares (a line field, an unknown name, or any name on
// an unregistered DocType), so a typo fails when the routes are built, not
// by showing nothing (UI v2 §5.13).
func CheckKeyFacts(mods map[string]Module) error {
	for name, m := range mods {
		dt, _ := doctype.Lookup(name) // unregistered: no fields, so every name fails
		for _, f := range m.KeyFacts {
			if fd, found := dt.Field(f); !found || fd.OnLine {
				return fmt.Errorf("docscreen: %s KeyFacts names %q, which isn't one of its header fields", name, f)
			}
		}
	}
	return nil
}

// CheckHome reports the first Module.Home that is set but has no label or a
// URL that isn't a local path starting with "/" (UI v2 §5.13 item 2).
func CheckHome(mods map[string]Module) error {
	for name, m := range mods {
		h := m.Home
		if h == (components.Link{}) {
			continue
		}
		if h.Label == "" || !strings.HasPrefix(h.Href, "/") {
			return fmt.Errorf("docscreen: %s Home %+v needs a label and a URL starting with /", name, h)
		}
	}
	return nil
}

// CheckFooterTotal reports the first Module.FooterTotal that isn't a header
// Money field its DocType declares, so a typo fails when the routes are
// built, not by showing no total (UI v2 A-22, CD-376).
func CheckFooterTotal(mods map[string]Module) error {
	for name, m := range mods {
		if m.FooterTotal == "" {
			continue
		}
		dt, _ := doctype.Lookup(name) // unregistered: no fields
		if f, found := dt.Field(m.FooterTotal); !found || f.OnLine || f.Type != doctype.Money {
			return fmt.Errorf("docscreen: %s FooterTotal names %q, which isn't one of its header Money fields", name, m.FooterTotal)
		}
	}
	return nil
}

// CheckLifeSteps reports the first Module.LifeSteps declaration that can't
// show: on a DocType that isn't registered or submittable, a blank or
// repeated word, or one of the DocType's own lifecycle words (UI v2 §5.13
// item 4).
func CheckLifeSteps(mods map[string]Module) error {
	for name, m := range mods {
		if len(m.LifeSteps) == 0 {
			continue
		}
		dt, ok := doctype.Lookup(name)
		if !ok || !dt.Submittable {
			return fmt.Errorf("docscreen: %s declares LifeSteps but has no lifecycle", name)
		}
		seen := map[string]bool{}
		for _, s := range lifeStates(dt, doctype.StateDraft) {
			seen[stateWord(dt, s)] = true
		}
		for _, s := range []doctype.State{doctype.StateRejected, doctype.StateClosed, doctype.StateCancelled} {
			seen[stateWord(dt, s)] = true
		}
		for _, w := range m.LifeSteps {
			if w == "" || seen[w] {
				return fmt.Errorf("docscreen: %s LifeSteps names %q, which is blank or already a step", name, w)
			}
			seen[w] = true
		}
	}
	return nil
}

// pageSteps is v's lifecycle (lifeSteps) with each reached step's date
// (dd-mm) from the history the page shows, and the module's extra steps
// after the approved one, marked by life. An extra step marked current
// takes over from the approved step, which becomes done; with none current
// the state's own step stays current, so exactly one step is (A-26). A life
// word the module doesn't declare, a rejected or ended mark, or a current
// mark while the approved step isn't current (another current extra step,
// or a document past or before Approved) is an error.
func pageSteps(dt *doctype.DocType, v DocView, extra []string, life map[string]components.LifeStep) ([]components.LifeStep, error) {
	for w, x := range life {
		if !slices.Contains(extra, w) {
			return nil, fmt.Errorf("docscreen: PageExtra.Life names %q, which %s doesn't declare in LifeSteps", w, dt.Name)
		}
		if x.Mark == components.LifeRejected || x.Mark == components.LifeEnded {
			return nil, fmt.Errorf("docscreen: PageExtra.Life marks %q rejected or ended; extra steps are later, done or current", w)
		}
	}
	states, steps := lifeStates(dt, v.Info.State), lifeSteps(dt, v.Info.State)
	var out []components.LifeStep
	for i, st := range states {
		s := steps[i]
		if s.Mark != components.LifeTodo {
			s.Date = stepDate(st, v)
		}
		out = append(out, s)
		if st != doctype.StateApproved {
			continue
		}
		at := len(out) - 1
		for _, w := range extra {
			x := life[w]
			x.Label = w
			if x.Mark == components.LifeCurrent {
				if out[at].Mark != components.LifeCurrent {
					return nil, fmt.Errorf("docscreen: PageExtra.Life marks %q current, but %s isn't at its approved step or another step is current", w, v.Info.Display)
				}
				out[at].Mark = components.LifeDone
			}
			out = append(out, x)
		}
	}
	return out, nil
}

// stepActions are the history words of the entry that reaches each state;
// "Approved on submit" and "Rejected: {reason}" start with theirs.
var stepActions = map[doctype.State]string{
	doctype.StateDraft:     "Created",
	doctype.StatePending:   "Submitted",
	doctype.StateApproved:  "Approved",
	doctype.StateRejected:  "Rejected",
	doctype.StateClosed:    "Closed",
	doctype.StateCancelled: "Cancelled",
}

// stepDate is the day (dd-mm) of the newest audit or approval entry that
// reached state s, "" when there is none. Both histories are already on the
// page for this viewer, newest first.
func stepDate(s doctype.State, v DocView) string {
	for _, h := range slices.Concat(v.History, v.Approval.History) {
		if strings.HasPrefix(h.Action, stepActions[s]) {
			return h.At[:min(len(h.At), len("02-01"))]
		}
	}
	return ""
}

// lifeStates are the states lifeSteps shows for a document in state s.
func lifeStates(dt *doctype.DocType, s doctype.State) []doctype.State {
	if !dt.Submittable {
		return nil
	}
	states := []doctype.State{doctype.StateDraft}
	switch {
	case dt.Approvable && s == doctype.StateRejected:
		states = append(states, s)
	case dt.Approvable:
		states = append(states, doctype.StatePending)
	}
	states = append(states, doctype.StateApproved)
	switch s {
	case doctype.StateClosed, doctype.StateCancelled:
		states = append(states, s)
	}
	return states
}

// lifeSteps is a submittable DocType's lifecycle with the document's state s
// marked (UI v2 §5.13 item 4, A-26): Draft, Pending approval when dt is
// Approvable, then its approved word; Rejected marks the approval step with
// its own word, and Closed or Cancelled adds a final step. Words follow the
// state badge.
func lifeSteps(dt *doctype.DocType, s doctype.State) []components.LifeStep {
	states := lifeStates(dt, s)
	mark := components.LifeCurrent
	switch {
	case dt.Approvable && s == doctype.StateRejected:
		mark = components.LifeRejected
	case s == doctype.StateClosed, s == doctype.StateCancelled:
		mark = components.LifeEnded
	}
	at := slices.Index(states, s)
	out := make([]components.LifeStep, len(states))
	for i, st := range states {
		out[i] = components.LifeStep{Label: stateWord(dt, st)}
		switch {
		case i < at:
			out[i].Mark = components.LifeDone
		case i == at:
			out[i].Mark = mark
		}
	}
	return out
}

// stateWord is the state badge's word for s on dt.
func stateWord(dt *doctype.DocType, s doctype.State) string {
	return cmp.Or(dt.StateLabels[s], components.StateLabel(s))
}

package docscreen_test

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/alexedwards/scs/v2"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// successTexts are the success banners' messages in sel, then each success
// block's text (A-25), outside the toast templates.
func successTexts(sel *goquery.Selection) []string {
	out := bannerTexts(sel, "success")
	sel.Find(".success-block").Each(func(_ int, b *goquery.Selection) {
		if b.ParentsFiltered("template").Length() == 0 {
			out = append(out, norm(b.Find(".success-block__text").Text()))
		}
	})
	return out
}

// e5bFollow is the success block's follow-ups as "label|href", links or
// buttons alike (a follow-up with no URL would render as a button).
func e5bFollow(page *goquery.Document) []string {
	var out []string
	page.Find("#main .success-block .btn").Each(func(_ int, s *goquery.Selection) {
		out = append(out, norm(s.Text())+"|"+s.AttrOr("href", ""))
	})
	return out
}

// UI v2 E5b (§5.13 item 2, A-25, CD-372): after Submit the document page
// shows the success block (role="status") in place of the success banner,
// through htmx and through the flash after a 303, with who it went to under
// its heading. R9: so does Approve, heading Approved, with "Next request".
// "New sample order" shows only with the create right; docscreen has no
// module home, so there is no "Go to" follow-up. ?submitted=1 with nothing
// in the flash shows nothing.
func TestDocPage_SubmitSuccessBlock(t *testing.T) {
	const pending = "It went to Ravi Kulkarni (Purchase Manager). You'll see it in My requests until it's decided."
	const numbered = "SO/26-27/00001 has been numbered."
	newLink := []string{"New sample order|" + samplePath + "/new"}
	nextLink := []string{"Next request|" + inboxURL}
	for _, c := range []struct {
		name   string
		action string
		htmx   bool
		create bool
		block  []string
		title  string
		follow []string
	}{
		{"htmx submit, may create", "submit", true, true, []string{pending}, "Submitted for approval", newLink},
		{"htmx submit, may not create", "submit", true, false, []string{pending}, "Submitted for approval", nil},
		{"submit then 303", "submit", false, true, []string{pending}, "Submitted for approval", newLink},
		{"submit then 303, may not create", "submit", false, false, []string{pending}, "Submitted for approval", nil},
		{"htmx approve", "approve", true, true, []string{numbered}, "Approved", slices.Concat(nextLink, newLink)},
		{"htmx approve, may not create", "approve", true, false, []string{numbered}, "Approved", nextLink},
		{"approve then 303", "approve", false, true, []string{numbered}, "Approved", slices.Concat(nextLink, newLink)},
		{"approve then 303, may not create", "approve", false, false, []string{numbered}, "Approved", nextLink},
	} {
		t.Run(c.name, func(t *testing.T) {
			rights := allRights
			if c.create {
				rights = append(slices.Clone(allRights), "create")
			}
			e := newDocEnv(t, rights...)
			before, result := doctype.StateDraft, doc.Result{ID: 42, State: doctype.StatePending, Version: 4}
			if c.action == "approve" {
				before, result = doctype.StatePending, doc.Result{ID: 42, State: doctype.StateApproved, Number: "SO/26-27/00001", Version: 4}
			}
			e.seed(before)
			e.extras.SetApproval(docscreentest.Sample, 42, docscreen.ApprovalView{Needed: true, WaitingOn: "Ravi Kulkarni (Purchase Manager)"})
			e.docs.SetResult(docscreentest.Sample, 42, doctype.Action(c.action), result, nil)
			var page *goquery.Document
			if c.htmx {
				rec := e.post(t, doc42+"/"+c.action, nil, "HX-Request", "true")
				if rec.Code != http.StatusOK {
					t.Fatalf("status = %d; body %.300s", rec.Code, rec.Body)
				}
				page = uitest.Doc(t, rec)
			} else {
				rec := e.post(t, doc42+"/"+c.action, nil)
				location(t, rec)
				e.seed(result.State)
				page = uitest.Doc(t, e.get(t, rec.Header().Get("Location")))
			}
			var blocks []string
			page.Find(`#main .success-block[role="status"]`).Each(func(_ int, b *goquery.Selection) {
				blocks = append(blocks, norm(b.Find(".success-block__text").Text()))
			})
			if !slices.Equal(blocks, c.block) {
				t.Errorf("success blocks = %q, want %q", blocks, c.block)
			}
			if got := norm(page.Find("#main .success-block h2").Text()); got != c.title {
				t.Errorf("heading = %q, want %q", got, c.title)
			}
			if got := bannerTexts(page.Find("#main"), "success"); len(got) != 0 {
				t.Errorf("success banners = %q, want none beside the block", got)
			}
			if n := page.Find(`#main a[href="` + inboxURL + `"]`).Length(); c.action == "approve" && n != 1 {
				t.Errorf("%d Next request links, want 1", n)
			}
			if got := e5bFollow(page); !slices.Equal(got, c.follow) {
				t.Errorf("follow-ups = %q, want %q", got, c.follow)
			}
		})
	}
	t.Run("submitted=1 with an empty flash", func(t *testing.T) {
		e := newDocEnv(t, append(slices.Clone(allRights), "create")...)
		e.seed(doctype.StatePending)
		page := uitest.Doc(t, e.get(t, doc42+"?submitted=1"))
		if got := successTexts(page.Find("#main")); len(got) != 0 {
			t.Errorf("success = %q, want none", got)
		}
		if n := page.Find(".success-block").Length(); n != 0 {
			t.Errorf("%d success blocks, want none", n)
		}
	})
}

// E5b back link (§5.13 item 1): the document page starts with a link to its
// DocType's list, named by its plural label, also on a refused action's
// redraw.
func TestDocPage_BackLink(t *testing.T) {
	e := newDocEnv(t, allRights...)
	e.seed(doctype.StatePending)
	e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionApprove, doc.Result{}, statusErr{status: http.StatusConflict, msg: "changed"})
	for _, c := range []struct {
		name string
		rec  func() *goquery.Document
	}{
		{"GET", func() *goquery.Document { return uitest.Doc(t, e.get(t, doc42)) }},
		{"409 redraw", func() *goquery.Document { return uitest.Doc(t, e.post(t, doc42+"/approve", url.Values{})) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			back := c.rec().Find("#main a.back-link")
			if back.Length() != 1 || norm(back.Text()) != "Sample Orders" || back.AttrOr("href", "") != samplePath {
				t.Errorf("back links = %d, %q to %q; want one \"Sample Orders\" to %s", back.Length(), norm(back.Text()), back.AttrOr("href", ""), samplePath)
			}
		})
	}
}

// E5b Actions card (§5.13 item 7, CD-372): the document's actions are one
// copy, in an Actions card last in the side column's tree (on phones its
// contents are the sticky footer; from 80rem CSS moves it to the top). A page with buttons, with only More actions, or
// with only the bar's note has it; a page with none has no card.
func TestDocPage_ActionsCard(t *testing.T) {
	for _, c := range []struct {
		name    string
		state   doctype.State
		rights  []auth.Right
		block   error
		card    bool
		bar     string
		more    int
		noteBar string
	}{
		{"approver on a pending order", doctype.StatePending, allRights, nil, true, "Approve", 2, ""},
		{"only print", doctype.StateApproved, []auth.Right{"read", "print"}, nil, true, "", 1, ""},
		{"only the note", doctype.StatePending, []auth.Right{"read", "approve"}, errors.New("You approved an earlier step."), true, "", 0, "You approved an earlier step."},
		{"read only", doctype.StateApproved, []auth.Right{"read"}, nil, false, "", 0, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newDocEnv(t, c.rights...)
			e.apr.block = c.block
			e.seed(c.state)
			page := uitest.Doc(t, e.get(t, doc42))
			card := page.Find("#main .doc-side > .doc-actions")
			if got := card.Length() == 1; got != c.card {
				t.Fatalf("Actions card = %v, want %v", got, c.card)
			}
			if !c.card {
				if n := page.Find(".actionbar, .more-actions, .doc-actions").Length(); n != 0 {
					t.Errorf("%d action regions, want none", n)
				}
				return
			}
			if !page.Find("#main .doc-side").Children().Last().Is(".doc-actions") {
				t.Error("the Actions card is not last in .doc-side (phone order; moved up from 80rem)")
			}
			if got := norm(card.Find(".card-head").Text()); got != "Actions" {
				t.Errorf("card title = %q, want Actions", got)
			}
			if n, in := page.Find(".actionbar").Length(), card.Find(".actionbar").Length(); n > 1 || n != in {
				t.Errorf("%d action bars, %d in the card; want at most one, in the card", n, in)
			}
			if got := norm(card.Find(".actionbar .btn-primary").Text()); got != c.bar {
				t.Errorf("primary = %q, want %q", got, c.bar)
			}
			if got := card.Find(".more-actions li").Length(); got != c.more {
				t.Errorf("More actions = %d, want %d", got, c.more)
			}
			if got := norm(card.Find(".actionbar-note").Text()); got != c.noteBar {
				t.Errorf("note = %q, want %q", got, c.noteBar)
			}
		})
	}
}

// e5bSteps are the timeline's steps as "text|aria-current".
func e5bSteps(page *goquery.Document) []string {
	var out []string
	page.Find(".doc-head ol.lifecycle > li").Each(func(_ int, s *goquery.Selection) {
		out = append(out, norm(s.Text())+"|"+s.AttrOr("aria-current", ""))
	})
	return out
}

// E5b step dates (§5.13 item 4): a reached step shows the day (dd-mm) of
// the newest audit or approval entry that reached it ("Approved on submit"
// and "Rejected: {reason}" count); a later step shows none even when the
// history names it, and a step with no entry has none.
func TestDocPage_StepDates(t *testing.T) {
	for _, c := range []struct {
		name       string
		state      doctype.State
		audit, apr []docscreen.HistoryRow
		want       []string
	}{
		{"pending, resubmitted", doctype.StatePending,
			[]docscreen.HistoryRow{{At: "26-10-2026 09:15", Action: "Submitted"}, {At: "24-10-2026 18:00", Action: "Approved"}, {At: "23-10-2026 10:00", Action: "Submitted"}, {At: "19-10-2026 08:00", Action: "Created"}}, nil,
			[]string{"Draft (done) 19-10|", "Pending approval 26-10|step", "Approved|"}},
		{"approved on submit", doctype.StateApproved,
			[]docscreen.HistoryRow{{At: "20-10-2026 11:00", Action: "Created"}}, []docscreen.HistoryRow{{At: "21-10-2026 12:00", Action: "Approved on submit (no approval needed)"}, {At: "21-10-2026 12:00", Action: "Submitted"}},
			[]string{"Draft (done) 20-10|", "Approved 21-10|step"}},
		{"rejected", doctype.StateRejected,
			nil, []docscreen.HistoryRow{{At: "25-10-2026 16:40", Action: "Rejected: Wrong GSTIN"}, {At: "24-10-2026 09:00", Action: "Submitted"}},
			[]string{"Draft (done)|", "Rejected 25-10|step", "Approved|"}},
		{"cancelled", doctype.StateCancelled,
			[]docscreen.HistoryRow{{At: "02-11-2026 00:10", Action: "Cancelled: Supplier closed"}, {At: "28-10-2026 10:00", Action: "Approved"}}, nil,
			[]string{"Draft (done)|", "Pending approval (done)|", "Approved (done) 28-10|", "Cancelled 02-11|step"}},
		{"closed, no history", doctype.StateClosed, nil, nil,
			[]string{"Draft (done)|", "Pending approval (done)|", "Approved (done)|", "Closed|step"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newDocEnv(t, "read")
			e.seed(c.state)
			e.extras.SetAudit(docscreentest.Sample, 42, c.audit)
			e.extras.SetApproval(docscreentest.Sample, 42, docscreen.ApprovalView{Needed: true, History: c.apr})
			if got := e5bSteps(uitest.Doc(t, e.get(t, doc42))); !slices.Equal(got, c.want) {
				t.Errorf("steps = %q, want %q", got, c.want)
			}
		})
	}
}

// E5b extra steps (§5.13 item 4): Module.LifeSteps adds steps after the
// approved one, marked by PageExtra.Life. Only an extra step marked current
// moves Approved from current to done, so exactly one step is current
// (A-26). A Life word the module doesn't declare, a rejected or ended mark,
// or two current steps are a 500.
func TestDocPage_ExtraLifeSteps(t *testing.T) {
	extra := []string{"Sent", "Received"}
	for _, c := range []struct {
		name string
		life map[string]components.LifeStep
		code int
		want []string
	}{
		{"none reached", nil, http.StatusOK, []string{"Draft (done)|", "Approved|step", "Sent|", "Received|"}},
		{"sent", map[string]components.LifeStep{"Sent": {Mark: components.LifeDone, Date: "05-11"}}, http.StatusOK,
			[]string{"Draft (done)|", "Approved|step", "Sent (done) 05-11|", "Received|"}},
		{"sent and current", map[string]components.LifeStep{"Sent": {Mark: components.LifeCurrent, Date: "05-11"}}, http.StatusOK,
			[]string{"Draft (done)|", "Approved (done)|", "Sent 05-11|step", "Received|"}},
		{"part received", map[string]components.LifeStep{"Sent": {Mark: components.LifeDone}, "Received": {Mark: components.LifeCurrent, Note: "Part"}}, http.StatusOK,
			[]string{"Draft (done)|", "Approved (done)|", "Sent (done)|", "Received Part|step"}},
		{"a later step marked", map[string]components.LifeStep{"Received": {Mark: components.LifeTodo, Note: "Soon"}}, http.StatusOK,
			[]string{"Draft (done)|", "Approved|step", "Sent|", "Received Soon|"}},
		{"undeclared word", map[string]components.LifeStep{"Paid": {Mark: components.LifeDone}}, http.StatusInternalServerError, nil},
		{"rejected mark", map[string]components.LifeStep{"Sent": {Mark: components.LifeRejected}}, http.StatusInternalServerError, nil},
		{"ended mark", map[string]components.LifeStep{"Received": {Mark: components.LifeEnded}}, http.StatusInternalServerError, nil},
		{"two current", map[string]components.LifeStep{"Sent": {Mark: components.LifeCurrent}, "Received": {Mark: components.LifeCurrent}}, http.StatusInternalServerError, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			e, id := m01bHooked(t, docscreen.Module{LifeSteps: extra, Page: func(docscreen.Ctx) (docscreen.PageExtra, error) {
				return docscreen.PageExtra{Life: c.life}, nil
			}})
			m01bSubmit(t, e, id)
			rec := e.get(t, m01bURL(id))
			if rec.Code != c.code {
				t.Fatalf("status = %d, want %d", rec.Code, c.code)
			}
			if c.code != http.StatusOK {
				return
			}
			page := uitest.Doc(t, rec)
			if got := e5bSteps(page); !slices.Equal(got, c.want) {
				t.Errorf("steps = %q, want %q", got, c.want)
			}
			if n := page.Find(`.doc-head li[aria-current="step"]`).Length(); n != 1 {
				t.Errorf("%d current steps, want exactly one", n)
			}
		})
	}
}

// LifeSteps are checked when the routes are built: a DocType that isn't
// registered or submittable, a blank or repeated word, or one of its own
// lifecycle words (Hooked: Draft, Approved, Closed, Cancelled, Rejected)
// panics. New words pass, and so does a module with none.
func TestRoutes_LifeStepsChecked(t *testing.T) {
	docscreentest.RegisterHooked()
	docscreentest.RegisterSample()
	for _, c := range []struct {
		name  string
		mods  map[string]docscreen.Module
		panic bool
	}{
		{"unregistered DocType", map[string]docscreen.Module{"e5b.Nowhere": {LifeSteps: []string{"Sent"}}}, true},
		{"not submittable", map[string]docscreen.Module{docscreentest.Supplier: {LifeSteps: []string{"Sent"}}}, true},
		{"blank word", map[string]docscreen.Module{docscreentest.Hooked: {LifeSteps: []string{"Sent", ""}}}, true},
		{"repeated word", map[string]docscreen.Module{docscreentest.Hooked: {LifeSteps: []string{"Sent", "Sent"}}}, true},
		{"its approved word", map[string]docscreen.Module{docscreentest.Hooked: {LifeSteps: []string{"Approved"}}}, true},
		{"its draft word", map[string]docscreen.Module{docscreentest.Hooked: {LifeSteps: []string{"Draft"}}}, true},
		{"its closed word", map[string]docscreen.Module{docscreentest.Hooked: {LifeSteps: []string{"Closed"}}}, true},
		{"its cancelled word", map[string]docscreen.Module{docscreentest.Hooked: {LifeSteps: []string{"Cancelled"}}}, true},
		{"its rejected word", map[string]docscreen.Module{docscreentest.Hooked: {LifeSteps: []string{"Rejected"}}}, true},
		{"pending word on an approvable DocType", map[string]docscreen.Module{docscreentest.Sample: {LifeSteps: []string{"Pending approval"}}}, true},
		{"new words", map[string]docscreen.Module{docscreentest.Hooked: {LifeSteps: []string{"Sent", "Received"}}, docscreentest.Sample: {LifeSteps: []string{"Paid"}}, "e5b.Nowhere": {}}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if got := recover() != nil; got != c.panic {
					t.Errorf("Routes panicked = %v, want %v", got, c.panic)
				}
			}()
			docscreen.Routes(docscreen.Deps{Modules: c.mods})
		})
	}
}

// e5bMount gives e the modules mods and rebuilds its router as newDocEnv
// does.
func e5bMount(e *docEnv, mods map[string]docscreen.Module) {
	e.d.Modules = mods
	r := web.NewRouter(web.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Sessions: scs.New()})
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(ui.WithShell(r.Context(), ui.Shell{CSRF: csrfToken})))
		})
	})
	docscreen.Routes(e.d)(r)
	e.h = r
}

// "Go to {label}" (head ruling on the E5b review): a module with a Home adds
// it after "New {label}"; with no Home there is none, also without the
// create right.
func TestDocPage_SuccessBlockHome(t *testing.T) {
	home := components.Link{Label: "Samples", Href: "/modules/samples"}
	for _, c := range []struct {
		name   string
		create bool
		home   components.Link
		want   []string
	}{
		{"home and create", true, home, []string{"New sample order|" + samplePath + "/new", "Go to Samples|/modules/samples"}},
		{"home without create", false, home, []string{"Go to Samples|/modules/samples"}},
		{"no home", true, components.Link{}, []string{"New sample order|" + samplePath + "/new"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			rights := allRights
			if c.create {
				rights = append(slices.Clone(allRights), "create")
			}
			e := newDocEnv(t, rights...)
			e5bMount(e, map[string]docscreen.Module{docscreentest.Sample: {Home: c.home}})
			e.seed(doctype.StateDraft)
			e.extras.SetApproval(docscreentest.Sample, 42, docscreen.ApprovalView{Needed: true, WaitingOn: "Ravi Kulkarni (Purchase Manager)"})
			e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSubmit, doc.Result{ID: 42, State: doctype.StatePending, Version: 4}, nil)
			page := uitest.Doc(t, e.post(t, doc42+"/submit", nil, "HX-Request", "true"))
			if got := e5bFollow(page); !slices.Equal(got, c.want) {
				t.Errorf("follow-ups = %q, want %q", got, c.want)
			}
		})
	}
}

// Module.Home is checked when the routes are built: set, it needs a label
// and a URL starting with "/".
func TestRoutes_HomeChecked(t *testing.T) {
	docscreentest.RegisterSample()
	for _, c := range []struct {
		name  string
		home  components.Link
		panic bool
	}{
		{"no label", components.Link{Href: "/modules/samples"}, true},
		{"no URL", components.Link{Label: "Samples"}, true},
		{"not a local path", components.Link{Label: "Samples", Href: "https://example.com/"}, true},
		{"label and path", components.Link{Label: "Samples", Href: "/modules/samples"}, false},
		{"none", components.Link{}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if got := recover() != nil; got != c.panic {
					t.Errorf("Routes panicked = %v, want %v", got, c.panic)
				}
			}()
			docscreen.Routes(docscreen.Deps{Modules: map[string]docscreen.Module{docscreentest.Sample: {Home: c.home}}})
		})
	}
}

// ?next=1&submitted=1 takes the flash once: the block keeps the text and
// the "Next request" link is there too.
func TestDocPage_NextAndSubmitted(t *testing.T) {
	const pending = "It went to Ravi Kulkarni (Purchase Manager). You'll see it in My requests until it's decided."
	e := newDocEnv(t, allRights...)
	e.seed(doctype.StateDraft)
	e.extras.SetApproval(docscreentest.Sample, 42, docscreen.ApprovalView{Needed: true, WaitingOn: "Ravi Kulkarni (Purchase Manager)"})
	e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSubmit, doc.Result{ID: 42, State: doctype.StatePending, Version: 4}, nil)
	location(t, e.post(t, doc42+"/submit", nil))
	e.seed(doctype.StatePending)
	page := uitest.Doc(t, e.get(t, doc42+"?next=1&submitted=1"))
	if got := successTexts(page.Find("#main")); !slices.Equal(got, []string{pending}) {
		t.Errorf("success = %q, want [%q]", got, pending)
	}
	if page.Find("#main .success-block").Length() != 1 || norm(page.Find(`#main a[href="`+inboxURL+`"]`).Text()) != "Next request" {
		t.Error("want the success block and the Next request link")
	}
}

// Only ?next=1 or ?submitted=1 take the flash's success into the page: an
// htmx GET of the page leaves it for the next full page, which shows it as
// the plain banner, in the text as flashed.
func TestDocPage_FlashLeftWithoutParams(t *testing.T) {
	const pending = "Submitted for approval to Ravi Kulkarni (Purchase Manager)."
	e := newDocEnv(t, allRights...)
	e.seed(doctype.StateDraft)
	e.extras.SetApproval(docscreentest.Sample, 42, docscreen.ApprovalView{Needed: true, WaitingOn: "Ravi Kulkarni (Purchase Manager)"})
	e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSubmit, doc.Result{ID: 42, State: doctype.StatePending, Version: 4}, nil)
	location(t, e.post(t, doc42+"/submit", nil))
	e.seed(doctype.StatePending)
	if got := successTexts(uitest.Doc(t, e.get(t, doc42, "HX-Request", "true")).Selection); len(got) != 0 {
		t.Errorf("htmx GET shows %q, want nothing", got)
	}
	if got := successTexts(uitest.Doc(t, e.get(t, doc42)).Find("#main")); !slices.Equal(got, []string{pending}) {
		t.Errorf("next full GET shows %q, want [%q]", got, pending)
	}
}

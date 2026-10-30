package docscreen_test

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/screens"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// checkSection is the check page's card titled title.
func checkSection(sel *goquery.Selection, title string) *goquery.Selection {
	return sel.Find("section.card").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return norm(s.Find(".card-title").First().Text()) == title
	})
}

// postTarget is where a submit control posts: its formaction, else its
// form's action.
func postTarget(doc *goquery.Document, el *goquery.Selection) (action string, form *goquery.Selection) {
	form = formOf(doc, el)
	if fa, ok := el.Attr("formaction"); ok && fa != "" {
		return fa, form
	}
	action, _ = form.Attr("action")
	return action, form
}

func TestCheckPage_SectionsStatusAndPreview(t *testing.T) {
	const (
		needs = "This order needs approval from a Purchase Manager because it is ₹50,000 or more."
		none  = "No approval needed. It will be approved and numbered when you submit."
	)
	for _, c := range []struct {
		needed  bool
		preview string
		primary string
	}{
		{true, needs, "Submit for approval"},
		{false, none, "Submit"},
	} {
		t.Run(c.primary, func(t *testing.T) {
			e := newFormEnv(t, allRights...)
			rec := sampleRecord()
			rec.Lines[0]["qty"] = "" // a Problem on lines[1].qty
			docscreentest.Seed(e.docs, sampleInfo(42, doctype.StateDraft), rec)
			e.extras.SetApproval(docscreentest.Sample, 42, docscreen.ApprovalView{Needed: c.needed, Preview: c.preview})

			res := e.get(t, doc42+"/edit/check")
			if res.Code != http.StatusOK {
				t.Fatalf("status = %d, body %.300s", res.Code, res.Body)
			}
			page := uitest.Doc(t, res)
			if got := stepCount(page.Selection); got != "Step 3 of 3" {
				t.Errorf("stepper = %q, want Step 3 of 3", got)
			}
			if got := norm(page.Find("h1#step-title").Text()); got != "Check and submit" {
				t.Errorf("heading = %q", got)
			}
			for _, s := range []struct{ title, slug, status, not string }{
				{"Details", "details", "Complete", "Needs attention"},
				{"Items", "items", "Needs attention", "Complete"},
			} {
				sec := checkSection(page.Selection, s.title)
				if sec.Length() != 1 {
					t.Errorf("found %d %q sections, want 1", sec.Length(), s.title)
					continue
				}
				status := norm(sec.Find(".check-status").Text())
				if status != s.status {
					t.Errorf("%s status = %q, want %q", s.title, status, s.status)
				}
				change := sec.Find(".card-head a")
				href, _ := change.Attr("href")
				if u, err := url.Parse(href); err != nil || u.Path != doc42+"/edit/"+s.slug || u.Query().Get("return") != "check" {
					t.Errorf("%s Change link = %q, want %s/edit/%s?return=check", s.title, href, doc42, s.slug)
				}
			}
			if got := bannerTexts(page.Selection, "info"); !slices.Contains(got, c.preview) {
				t.Errorf("info banners = %q, want the preview %q", got, c.preview)
			}
			text := norm(page.Find("#wizard").Text())
			for _, want := range []string{"₹94,164.00", "SHREE Shree Steel Traders", "MS Angle 50x50x5"} {
				if !strings.Contains(text, want) {
					t.Errorf("check page lacks %q", want)
				}
			}

			btn := buttonsNamed(page.Selection, c.primary)
			if btn.Length() != 1 || goquery.NodeName(btn) != "button" {
				t.Fatalf("found %d %q buttons, want one submit button", btn.Length(), c.primary)
			}
			action, form := postTarget(page, btn)
			if m, _ := form.Attr("method"); !strings.EqualFold(m, "post") || action != doc42+"/submit" {
				t.Errorf("%q posts (%s) to %q, want POST %s/submit", c.primary, m, action, doc42)
			}
			if form.Find(`input[type="hidden"][name="version"][value="3"]`).Length() == 0 {
				t.Errorf("%q's form carries no version 3", c.primary)
			}
			other := map[bool]string{true: "Submit", false: "Submit for approval"}[c.needed]
			if buttonsNamed(page.Selection, other).Length() != 0 {
				t.Errorf("the check page also shows %q", other)
			}
			more := page.Find(".more-actions").Text()
			for _, want := range []string{"Delete draft", "Print"} {
				if !strings.Contains(more, want) {
					t.Errorf("More actions lacks %q", want)
				}
			}
		})
	}

	// A complete draft has every section Complete.
	e := newFormEnv(t, allRights...)
	e.seed(doctype.StateDraft)
	page := uitest.Doc(t, e.get(t, doc42+"/edit/check"))
	for _, title := range []string{"Details", "Items"} {
		if got := norm(checkSection(page.Selection, title).Find(".check-status").Text()); got != "Complete" {
			t.Errorf("complete draft: %s status = %q, want Complete", title, got)
		}
	}
}

func TestCheckPage_PrintHonoursPrintURL(t *testing.T) {
	const printURL = "/files/print/docscreentest.Sample/42?copy=office"
	e := newFormEnv(t, allRights...)
	e.d.PrintURL = func(name string, id int64) string {
		if name != docscreentest.Sample || id != 42 {
			return "/wrong"
		}
		return printURL
	}
	e.remount()
	e.seed(doctype.StateDraft)
	res := e.get(t, doc42+"/edit/check")
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body %.300s", res.Code, res.Body)
	}
	printLink := buttonsNamed(uitest.Doc(t, res).Find(".more-actions"), "Print")
	if printLink.Length() != 1 {
		t.Fatalf("found %d Print links under More actions, want 1", printLink.Length())
	}
	if href, _ := printLink.Attr("href"); href != printURL {
		t.Errorf("Print href = %q, want Deps.PrintURL's %q", href, printURL)
	}
	if tgt, _ := printLink.Attr("target"); tgt != "_blank" {
		t.Errorf("Print target = %q, want a new tab", tgt)
	}
}

// An htmx action that leaves the document page answers 200 with the page
// it leads to (its check page, or the list after delete), banner rendered,
// HX-Push-Url set; never 204 + HX-Redirect. The response replaces the
// action form's hx-target.
func TestDirectAction_HTMXLeavingPageRendersTarget(t *testing.T) {
	cases := []struct {
		action string
		state  doctype.State
		rights []auth.Right
		from   string // page holding the action's form
		result doc.Result
		to     string
		banner string
		check  bool // the target is a check page
	}{
		{"withdraw", doctype.StatePending, []auth.Right{"read", "write", "submit", "delete", "print"}, doc42,
			doc.Result{ID: 42, State: doctype.StateDraft, Version: 4}, doc42 + "/edit/check", "Withdrawn. You can change the draft and submit it again.", true},
		{"revise", doctype.StateRejected, allRights, doc42,
			doc.Result{ID: 42, State: doctype.StateDraft, Version: 4}, doc42 + "/edit/check", "", true},
		{"amend", doctype.StateCancelled, allRights, doc42,
			doc.Result{ID: 43, State: doctype.StateDraft, Version: 1}, samplePath + "/43/edit/check", "Amending SO/26-27/00001", true},
		{"delete", doctype.StateDraft, allRights, doc42 + "/delete",
			doc.Result{ID: 42}, samplePath, "Deleted Draft 42.", false},
	}
	for _, c := range cases {
		t.Run(c.action, func(t *testing.T) {
			e := newFormEnv(t, append(c.rights, "create")...)
			e.d.Engine = stateEngine{e.docs}
			e.remount()
			e.seed(c.state)
			docscreentest.Seed(e.docs, sampleInfo(43, doctype.StateDraft), sampleRecord())
			e.extras.SetOrigin(docscreentest.Sample, 43, docscreen.Origin{AmendedFrom: "SO/26-27/00001", AmendedFromID: 42})
			e.docs.SetResult(docscreentest.Sample, 42, doctype.Action(c.action), c.result, nil)

			from := uitest.Doc(t, e.get(t, c.from))
			form := from.Find(`form[action="` + doc42 + "/" + c.action + `"]`).FilterFunction(func(_ int, s *goquery.Selection) bool {
				_, ok := s.Attr("hx-post")
				return ok
			})
			if form.Length() != 1 {
				t.Fatalf("%s has %d htmx forms posting %s, want 1", c.from, form.Length(), c.action)
			}
			target, _ := form.Attr("hx-target")

			rec := e.post(t, doc42+"/"+c.action, nil, "HX-Request", "true")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body %.300s", rec.Code, rec.Body)
			}
			if h := rec.Header().Get("HX-Redirect"); h != "" {
				t.Errorf("HX-Redirect = %q; the target page is rendered directly", h)
			}
			if got := pathOf(t, rec.Header().Get("HX-Push-Url")); got != c.to {
				t.Errorf("HX-Push-Url = %q, want %q", got, c.to)
			}
			if !slices.Contains(rec.Header().Values("Vary"), "HX-Request") {
				t.Errorf("Vary = %q", rec.Header().Values("Vary"))
			}
			if ids := rootIDs(t, rec); !slices.Equal(ids, []string{strings.TrimPrefix(target, "#")}) {
				t.Errorf("response roots = %q, want the form's hx-target %q", ids, target)
			}
			page := uitest.Doc(t, rec)
			if c.check {
				if got := norm(page.Find("h1#step-title").Text()); got != "Check and submit" {
					t.Errorf("heading = %q, want the check page", got)
				}
			} else if got := norm(page.Find("h1").First().Text()); got != "Sample Orders" {
				t.Errorf("h1 = %q, want the list page", got)
			}
			got := bannerTexts(page.Selection, "success")
			if want := []string{c.banner}; c.banner != "" && !slices.Equal(got, want) {
				t.Errorf("success banners = %q, want %q", got, want)
			}
			// Rendered directly, so the next full page has nothing left over.
			if next := bannerTexts(uitest.Doc(t, e.get(t, c.to)).Selection, "success"); len(next) != 0 {
				t.Errorf("the next full page repeats %q", next)
			}
		})
	}
}

var screensOnce sync.Once

func TestScreens_DocscreenCoversItsRoutes(t *testing.T) {
	e := newFormEnv(t, allRights...)
	ids := docscreen.SampleIDs{Draft: 42, Pending: 43, Approved: 44, Rejected: 45, Cancelled: 46, Closed: 47, Lines500: 48}
	list := docscreen.Screens(docscreentest.Sample, ids)
	screensOnce.Do(func() { screens.Register(list...) })

	r := chi.NewRouter()
	docscreen.Routes(e.d)(r)
	if missing := screens.Uncovered(r, screens.DefaultSkip); len(missing) != 0 {
		t.Errorf("routes with no screen: %q", missing)
	}

	urls := map[string]bool{}
	setups := 0
	for _, s := range list {
		urls[s.URL] = true
		if s.Route == "/doc/{doctype}/{id}/edit/{step}" && s.Setup != nil {
			setups++
		}
	}
	for _, want := range []string{
		samplePath + "/new",
		doc42 + "/edit/details",
		doc42 + "/edit/items",
		doc42 + "/edit/check",
		samplePath + "/48/edit/items?all=1",
	} {
		if !urls[want] {
			t.Errorf("no screen at %s", want)
		}
	}
	if setups < 2 {
		t.Errorf("%d edit-step screens have a Setup; want the 422 and the 409 variants", setups)
	}
}

// An amended draft's page is the check page, so the link to the document it
// amends is there (DOC-092).
func TestCheckPage_AmendedFromLink(t *testing.T) {
	e := newFormEnv(t, allRights...)
	docscreentest.Seed(e.docs, sampleInfo(43, doctype.StateDraft), sampleRecord())
	e.extras.SetOrigin(docscreentest.Sample, 43, docscreen.Origin{AmendedFrom: "SO/26-27/00001", AmendedFromID: 42})

	page := uitest.Doc(t, e.get(t, samplePath+"/43/edit/check"))
	if got := norm(page.Find(`a[href="` + doc42 + `"]`).First().Text()); got != "Amended from SO/26-27/00001" {
		t.Errorf("link = %q, want the amended document", got)
	}
	page = uitest.Doc(t, e.get(t, doc42+"/edit/check"))
	if strings.Contains(page.Text(), "Amended from") {
		t.Error("a draft that amends nothing shows an Amended from link")
	}
}

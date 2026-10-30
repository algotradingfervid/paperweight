package wizard_test

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
	"github.com/vardhan/internaltools/internal/ui/wizard"
)

func steps(labels ...string) []wizard.Step {
	out := make([]wizard.Step, len(labels))
	for i, l := range labels {
		out[i] = wizard.Step{Slug: strings.ToLower(strings.ReplaceAll(l, " ", "-")), Label: l}
	}
	return out
}

func docModel() wizard.Model {
	return wizard.Model{
		Mode:     wizard.Document,
		Title:    "Purchase order",
		DocLabel: "Draft 42",
		BaseURL:  "/doc/po/42/edit",
		Steps:    steps("Party", "Lines", "Terms", "Check and submit"),
		Current:  1,
		Furthest: 2,
		Version:  7,
	}
}

func TestModel_Validate(t *testing.T) {
	ok := docModel()
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid model: %v", err)
	}

	two := docModel()
	two.Steps = steps("Party", "Check and submit")
	two.Current, two.Furthest = 0, 0
	if err := two.Validate(); err == nil || err.Error() != "A wizard needs 3 to 5 steps." {
		t.Errorf("2 steps: err = %v", err)
	}

	review := docModel()
	review.Steps = steps("Party", "Lines", "Terms", "Review")
	if review.Validate() == nil {
		t.Error(`last step "Review" must fail`)
	}

	docSave := docModel()
	docSave.Steps = steps("Party", "Lines", "Terms", "Check and save")
	if docSave.Validate() == nil {
		t.Error(`Document mode ending "Check and save" must fail`)
	}

	master := docSave
	master.Mode = wizard.Master
	if err := master.Validate(); err != nil {
		t.Errorf(`Master mode ending "Check and save": %v`, err)
	}

	named := docSave
	named.Mode = wizard.Master
	named.Steps = steps("Party", "Lines", "Terms", "Import")
	if named.Validate() == nil {
		t.Error(`last step "Import" without LastLabel must fail`)
	}
	named.LastLabel = "Import"
	if err := named.Validate(); err != nil {
		t.Errorf(`LastLabel "Import": %v`, err)
	}
	named.Steps = steps("Party", "Lines", "Terms", "Check and save")
	if named.Validate() == nil {
		t.Error("LastLabel replaces the mode's label, so that label must fail")
	}

	ahead := docModel()
	ahead.Current, ahead.Furthest = 2, 1
	if ahead.Validate() == nil {
		t.Error("Current > Furthest must fail")
	}

	dup := docModel()
	dup.Steps[1].Slug = dup.Steps[0].Slug
	if dup.Validate() == nil {
		t.Error("duplicate slugs must fail")
	}

	bad := docModel()
	bad.Steps[0].Slug = "Party Step"
	if bad.Validate() == nil {
		t.Error("slug outside ^[a-z0-9-]+$ must fail")
	}

	past := docModel()
	past.Furthest = 4
	if past.Validate() == nil {
		t.Error("Furthest >= len(Steps) must fail")
	}
}

func TestModel_Helpers(t *testing.T) {
	m := docModel()
	if got := m.StepURL(2); got != "/doc/po/42/edit/terms" {
		t.Errorf("StepURL(2) = %q", got)
	}
	if m.Heading() != "Lines" {
		t.Errorf("Heading = %q", m.Heading())
	}
	if m.Last() {
		t.Error("Last on step 2 of 4")
	}
	m.Current = 3
	if !m.Last() {
		t.Error("Last on step 4 of 4")
	}
}

func TestStepper_Markup(t *testing.T) {
	doc := uitest.Render(t, wizard.Stepper(docModel()))
	nav := doc.Find("nav.stepper")
	if got := nav.AttrOr("aria-label", ""); got != "Purchase order steps" {
		t.Errorf("aria-label = %q", got)
	}
	if got := strings.TrimSpace(doc.Find("p.stepper__count#step-count").Text()); got != "Step 2 of 4" {
		t.Errorf("count = %q", got)
	}
	li := doc.Find("ol.stepper__list > li.stepper__step")
	if li.Length() != 4 {
		t.Fatalf("want 4 steps, got %d", li.Length())
	}
	s1 := li.Eq(0)
	if a := s1.Find("a"); a.AttrOr("href", "") != "/doc/po/42/edit/party" || !strings.Contains(a.Text(), "Completed: ") {
		t.Errorf("step 1 must be a link with Completed: , got %q", s1.Text())
	}
	if s1.Find("span.visually-hidden").Text() != "Completed: " {
		t.Error("step 1 Completed text must be visually hidden")
	}
	s2 := li.Eq(1)
	if s2.AttrOr("aria-current", "") != "step" || s2.Find("a").Length() != 0 {
		t.Errorf("step 2 must be current and not a link: %q", s2.Text())
	}
	if li.Eq(2).Find("a").AttrOr("href", "") != "/doc/po/42/edit/terms" {
		t.Error("step 3 (visited) must be a link")
	}
	s4 := li.Eq(3)
	if s4.Find("a").Length() != 0 || s4.Find("span").Length() != 1 {
		t.Error("step 4 must be a plain span")
	}
	p := doc.Find("progress.stepper__bar")
	if p.AttrOr("max", "") != "4" || p.AttrOr("value", "") != "2" || p.AttrOr("aria-labelledby", "") != "step-count" {
		t.Errorf("progress attrs: max=%q value=%q", p.AttrOr("max", ""), p.AttrOr("value", ""))
	}
	if doc.Find("[style]").Length() != 0 {
		t.Error("no style attributes")
	}
	if strings.Contains(doc.Text(), "%") {
		t.Error("never a percentage")
	}
}

// renderFrame renders Frame with one typed input as its children.
func renderFrame(t *testing.T, m wizard.Model, bar wizard.Bar) *goquery.Document {
	t.Helper()
	child := templ.Raw(`<input name="party" value="typed">`)
	return uitest.Render(t, templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		return wizard.Frame(m, bar).Render(templ.WithChildren(ctx, child), w)
	}))
}

func TestFrame_MasterModeHasNoVersionOrFinishLater(t *testing.T) {
	d := renderFrame(t, docModel(), wizard.Bar{})
	if v := d.Find("input[name=version]"); v.Length() != 1 || v.AttrOr("value", "") != "7" {
		t.Errorf("Document mode needs hidden version=7, got %d", v.Length())
	}
	if d.Find("button[value=finish]").Length() != 1 {
		t.Error("Document mode needs Save and finish later")
	}

	m := docModel()
	m.Mode = wizard.Master
	m.Steps = steps("Details", "Addresses", "Contacts", "Check and save")
	mm := renderFrame(t, m, wizard.Bar{})
	if mm.Find("input[name=version]").Length() != 0 {
		t.Error("Master mode must have no version input")
	}
	if mm.Find("button[value=finish]").Length() != 0 {
		t.Error("Master mode must have no Save and finish later (CD-81)")
	}
}

// Below comes last in #wizard, after the action bar and outside the step form,
// with or without After.
func TestFrame_BelowFollowsTheBar(t *testing.T) {
	below := templ.Raw(`<p id="below">x</p>`)
	for name, after := range map[string]templ.Component{"no After": nil, "After": templ.Raw(`<p id="after">y</p>`)} {
		t.Run(name, func(t *testing.T) {
			m := docModel()
			m.Mode, m.After, m.Below = wizard.Master, after, below
			m.Steps[len(m.Steps)-1].Label = "Check and save"
			doc := renderFrame(t, m, wizard.Bar{})
			if doc.Find("#wizard-form #below").Length() != 0 {
				t.Errorf("Below is inside the step form")
			}
			if doc.Find("#wizard > #below:last-child").Length() != 1 {
				html, _ := doc.Find("#wizard").Html()
				t.Errorf("Below isn't last in #wizard: %s", html)
			}
		})
	}
}

// Without Below the frame ends as before.
func TestFrame_NoBelow(t *testing.T) {
	m := docModel()
	m.Mode = wizard.Master
	m.Steps[len(m.Steps)-1].Label = "Check and save"
	if doc := renderFrame(t, m, wizard.Bar{}); doc.Find("#below").Length() != 0 {
		t.Errorf("a frame without Below shows it")
	}
}

func TestFrame_WarningsBanner(t *testing.T) {
	m := docModel()
	m.Warnings = []string{"CEMENT-OPC OPC Cement 53 Grade is marked inactive."}
	d := renderFrame(t, m, wizard.Bar{})
	b := d.Find("#wizard .banner--warning")
	if b.Length() != 1 || !strings.Contains(b.Text(), m.Warnings[0]) {
		t.Fatalf("want one warning banner with the text, got %d", b.Length())
	}
	if b.NextAllFiltered("h1#step-title").Length() != 1 {
		t.Error("warning banner must sit above the h1")
	}
	if docNo := renderFrame(t, docModel(), wizard.Bar{}); docNo.Find(".banner--warning").Length() != 0 {
		t.Error("no warnings → no banner")
	}
}

func TestFrame_FocusTarget(t *testing.T) {
	d := renderFrame(t, docModel(), wizard.Bar{})
	h := d.Find("h1#step-title")
	if _, ok := h.Attr("autofocus"); !ok || h.AttrOr("tabindex", "") != "-1" || strings.TrimSpace(h.Text()) != "Lines" {
		t.Errorf("h1 must be the focus target with the step label: %q", h.Text())
	}

	m := docModel()
	m.Errors = []components.FieldErr{{ID: "party", Message: "Choose a party."}}
	e := renderFrame(t, m, wizard.Bar{})
	if _, ok := e.Find(".error-summary").Attr("autofocus"); !ok {
		t.Error("error summary must take autofocus")
	}
	if _, ok := e.Find("h1#step-title").Attr("autofocus"); ok {
		t.Error("h1 must not have autofocus when errors exist")
	}
	if e.Find("[autofocus]").Length() != 1 {
		t.Error("exactly one autofocus")
	}

	c := docModel()
	c.Conflict = "Someone else changed this."
	cd := renderFrame(t, c, wizard.Bar{})
	if _, ok := cd.Find("h1#step-title").Attr("autofocus"); ok {
		t.Error("h1 must not have autofocus on a conflict")
	}
	w := cd.Find("#wizard-conflict")
	if _, ok := w.Attr("autofocus"); !ok || w.AttrOr("tabindex", "") != "-1" || w.Find(".banner--danger").Length() != 1 {
		t.Error("conflict banner wrapper must take autofocus with tabindex -1")
	}
	if cd.Find("[autofocus]").Length() != 1 {
		t.Error("exactly one autofocus on a conflict")
	}
}

func TestFrame_ConflictKeepsValues(t *testing.T) {
	m := docModel()
	m.Conflict = "This document was changed by someone else. Reload to see their changes."
	m.ReloadURL = "/doc/po/42/edit/lines"
	d := renderFrame(t, m, wizard.Bar{})
	b := d.Find(".banner--danger")
	if b.Length() != 1 || !strings.Contains(b.Text(), m.Conflict) {
		t.Fatalf("want one danger banner with the conflict text")
	}
	a := b.Find("a")
	if strings.TrimSpace(a.Text()) != "Reload" || a.AttrOr("href", "") != m.ReloadURL {
		t.Errorf("Reload link = %q → %q", a.Text(), a.AttrOr("href", ""))
	}
	if d.Find("form input[name=party]").AttrOr("value", "") != "typed" {
		t.Error("children (typed values) must still render")
	}
	if renderFrame(t, docModel(), wizard.Bar{}).Find(".banner--danger").Length() != 0 {
		t.Error("no conflict → no danger banner")
	}
}

func TestFrame_BackAndFinishButtons(t *testing.T) {
	m := docModel()
	m.ReturnTo = "check"
	d := renderFrame(t, m, wizard.Bar{Back: true})
	if d.Find("#wizard").Length() != 1 {
		t.Fatal("no #wizard")
	}
	f := d.Find("form")
	if f.AttrOr("hx-post", "") != "/doc/po/42/edit/lines" || f.AttrOr("hx-target", "") != "#wizard" || f.AttrOr("action", "") != "/doc/po/42/edit/lines" {
		t.Errorf("form attrs: hx-post=%q hx-target=%q", f.AttrOr("hx-post", ""), f.AttrOr("hx-target", ""))
	}
	if d.Find("form input[name=return]").AttrOr("value", "") != "check" {
		t.Error("hidden return missing")
	}
	if d.Find("button[name=nav][value=back]").Length() != 1 || !strings.Contains(d.Find("button[name=nav][value=back]").Text(), "Back") {
		t.Error("Back must be a nav=back submit button")
	}
	if fin := d.Find("button[name=nav][value=finish]"); fin.Length() != 1 || strings.TrimSpace(fin.Text()) != "Save and finish later" {
		t.Error("Save and finish later missing")
	}
	var primary string
	d.Find("button[type=submit]").Each(func(_ int, s *goquery.Selection) {
		if _, ok := s.Attr("name"); !ok {
			primary = strings.TrimSpace(s.Text())
		}
	})
	if primary != "Save and continue" {
		t.Errorf("primary = %q", primary)
	}
	if strings.Contains(d.Find("#wizard").Text(), "Purchase order · Draft 42") == false {
		t.Error("title line missing")
	}

	plain := renderFrame(t, docModel(), wizard.Bar{Primary: "Submit for approval"})
	if plain.Find("button[value=back]").Length() != 0 {
		t.Error("no Back when Bar.Back is false")
	}
	if plain.Find("input[name=return]").Length() != 0 {
		t.Error("no return input when ReturnTo is empty")
	}
	if !strings.Contains(plain.Find("button[type=submit]").Text(), "Submit for approval") {
		t.Error("Bar.Primary label not used")
	}
}

func TestRespond_Outcomes(t *testing.T) {
	page := func(fragment bool) templ.Component {
		if fragment {
			return templ.Raw(`<div id="wizard">fragment</div>`)
		}
		return templ.Raw(`<html><body>full</body></html>`)
	}
	cases := []struct {
		name                         string
		o                            wizard.Outcome
		htmx                         bool
		status                       int
		push, redirect, location, in string
	}{
		{"advance htmx", wizard.Advance, true, 200, "/next", "", "", "fragment"},
		{"advance plain", wizard.Advance, false, 303, "", "", "/next", ""},
		{"invalid htmx", wizard.Invalid, true, 422, "", "", "", "fragment"},
		{"invalid plain", wizard.Invalid, false, 422, "", "", "", "full"},
		{"conflict htmx", wizard.Conflict, true, 409, "", "", "", "fragment"},
		{"conflict plain", wizard.Conflict, false, 409, "", "", "", "full"},
		{"finish htmx", wizard.Finish, true, 200, "", "/next", "", ""},
		{"finish plain", wizard.Finish, false, 303, "", "", "/next", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := c.o
			h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				wizard.Respond(w, r, o, "/next", page)
			})
			var hdr []string
			if c.htmx {
				hdr = []string{"HX-Request", "true"}
			}
			rec := uitest.Do(t, h, http.MethodPost, "/step", url.Values{"a": {"1"}}, hdr...)
			if rec.Code != c.status {
				t.Errorf("status = %d, want %d", rec.Code, c.status)
			}
			if got := rec.Header().Get("Vary"); got != "HX-Request" {
				t.Errorf("Vary = %q", got)
			}
			if got := rec.Header().Get("HX-Push-Url"); got != c.push {
				t.Errorf("HX-Push-Url = %q, want %q", got, c.push)
			}
			if got := rec.Header().Get("HX-Redirect"); got != c.redirect {
				t.Errorf("HX-Redirect = %q, want %q", got, c.redirect)
			}
			if got := rec.Header().Get("Location"); got != c.location {
				t.Errorf("Location = %q, want %q", got, c.location)
			}
			body := rec.Body.String()
			if c.in != "" && !strings.Contains(body, c.in) {
				t.Errorf("body %q lacks %q", body, c.in)
			}
			if c.in == "" && (strings.Contains(body, "fragment") || strings.Contains(body, "full")) {
				t.Errorf("page must be unused, body %q", body)
			}
		})
	}
}

func TestNav(t *testing.T) {
	for in, want := range map[string]string{"back": "back", "finish": "finish", "": "", "other": ""} {
		r, _ := http.NewRequest(http.MethodPost, "/", strings.NewReader(url.Values{"nav": {in}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if got := wizard.Nav(r); got != want {
			t.Errorf("Nav(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCheckSection_StatusWords(t *testing.T) {
	for st, word := range map[wizard.StepStatus]string{wizard.Complete: "Complete", wizard.Attention: "Needs attention"} {
		d := uitest.Render(t, templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
			return wizard.CheckSection("Party", st, "/doc/po/42/edit/party").Render(templ.WithChildren(ctx, templ.Raw(`<p class="summary-x">Acme</p>`)), w)
		}))
		sec := d.Find("section.card")
		if !strings.Contains(sec.Find("h2").Text(), "Party") {
			t.Errorf("%s: title missing", word)
		}
		if !strings.Contains(sec.Text(), word) || sec.Find("svg").Length() == 0 {
			t.Errorf("%s: want the word and an icon, got %q", word, sec.Text())
		}
		a := sec.Find("a")
		if a.AttrOr("href", "") != "/doc/po/42/edit/party" || strings.Join(strings.Fields(a.Text()), " ") != "Change Party" || a.Find(".visually-hidden").Text() != "Party" {
			t.Errorf("%s: Change link = %q", word, a.Text())
		}
		if sec.Find("p.summary-x").Length() != 1 {
			t.Errorf("%s: children missing", word)
		}
	}
}

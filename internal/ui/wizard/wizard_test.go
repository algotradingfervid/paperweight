package wizard_test

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"slices"
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
		Steps:    steps("Party", "Lines", "Terms", "Check"),
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
	two.Steps = steps("Party", "Check")
	two.Current, two.Furthest = 0, 0
	if err := two.Validate(); err == nil || err.Error() != "A wizard needs 3 to 5 steps." {
		t.Errorf("2 steps: err = %v", err)
	}

	review := docModel()
	review.Steps = steps("Party", "Lines", "Terms", "Review")
	if review.Validate() == nil {
		t.Error(`last step "Review" must fail`)
	}

	master := docModel()
	master.Mode = wizard.Master
	if err := master.Validate(); err != nil {
		t.Errorf(`Master mode ending "Check": %v`, err)
	}

	for _, mode := range []wizard.Mode{wizard.Document, wizard.Master} {
		old := docModel()
		old.Mode = mode
		for _, label := range []string{"Check and submit", "Check and save"} {
			old.Steps = steps("Party", "Lines", "Terms", label)
			if old.Validate() == nil {
				t.Errorf(`mode %d ending %q must fail`, mode, label)
			}
		}
	}

	named := docModel()
	named.Mode = wizard.Master
	named.Steps = steps("Party", "Lines", "Terms", "Import")
	if named.Validate() == nil {
		t.Error(`last step "Import" without LastLabel must fail`)
	}
	named.LastLabel = "Import"
	if err := named.Validate(); err != nil {
		t.Errorf(`LastLabel "Import": %v`, err)
	}
	named.Steps = steps("Party", "Lines", "Terms", "Check")
	if named.Validate() == nil {
		t.Error("LastLabel replaces Check, so that label must fail")
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

// TestStepper_Markup: Stepper v2 (UI v2 §5.6, A-13): dots joined by lines,
// done steps a check and "Completed: ", the current step's label the
// h2#step-title, no <progress> and no "Step n of N" in view.
func TestStepper_Markup(t *testing.T) {
	doc := uitest.Render(t, wizard.Stepper(docModel()))
	nav := doc.Find("nav.stepper")
	if got := nav.AttrOr("aria-label", ""); got != "Purchase order steps" {
		t.Errorf("aria-label = %q", got)
	}
	if doc.Find("progress").Length() != 0 {
		t.Error("no <progress> bar (A-13)")
	}
	count := doc.Find("#step-count")
	if got := strings.TrimSpace(count.Text()); got != "Step 2 of 4" || !count.HasClass("visually-hidden") || !count.HasClass("stepper__count") {
		t.Errorf("count = %q (class %q), want visually hidden Step 2 of 4", got, count.AttrOr("class", ""))
	}
	li := doc.Find("ol.stepper__list > li.stepper__step")
	if li.Length() != 4 {
		t.Fatalf("want 4 steps, got %d", li.Length())
	}
	li.Each(func(i int, s *goquery.Selection) {
		if dot := s.Find(".stepper__dot"); dot.Length() != 1 || dot.AttrOr("aria-hidden", "") != "true" {
			t.Errorf("step %d: want one dot hidden from assistive tech", i+1)
		}
	})
	s1 := li.Eq(0)
	if !s1.HasClass("is-done") {
		t.Error("step 1 must be done")
	}
	if a := s1.Find("a"); a.AttrOr("href", "") != "/doc/po/42/edit/party" || !strings.Contains(a.Text(), "Completed: ") {
		t.Errorf("step 1 must be a link with Completed: , got %q", s1.Text())
	}
	if s1.Find("span.visually-hidden").Text() != "Completed: " {
		t.Error("step 1 Completed text must be visually hidden")
	}
	if s1.Find(".stepper__dot svg").Length() != 1 || strings.TrimSpace(s1.Find(".stepper__dot").Text()) != "" {
		t.Error("a done step's dot holds the check icon, not its number")
	}
	s2 := li.Eq(1)
	if !s2.HasClass("is-current") || s2.AttrOr("aria-current", "") != "step" || s2.Find("a").Length() != 0 {
		t.Errorf("step 2 must be current and not a link: %q", s2.Text())
	}
	h := s2.Find("h2#step-title")
	if h.Length() != 1 || strings.TrimSpace(h.Text()) != "Lines" || h.AttrOr("tabindex", "") != "-1" || h.AttrOr("aria-describedby", "") != "step-count" {
		t.Errorf("current step heading = %q (tabindex %q, describedby %q)", h.Text(), h.AttrOr("tabindex", ""), h.AttrOr("aria-describedby", ""))
	}
	if _, ok := h.Attr("autofocus"); !ok {
		t.Error("with no errors the step heading takes autofocus")
	}
	s3 := li.Eq(2)
	if !s3.HasClass("is-visited") || s3.Find("a").AttrOr("href", "") != "/doc/po/42/edit/terms" {
		t.Error("step 3 (visited) must be a link")
	}
	// Numbers come from a CSS counter: each item's text is its label alone.
	for i, want := range []string{"Completed: Party", "Lines", "Terms", "Check"} {
		if got := strings.Join(strings.Fields(li.Eq(i).Text()), " "); got != want {
			t.Errorf("step %d text = %q, want %q", i+1, got, want)
		}
	}
	s4 := li.Eq(3)
	if s4.Find("a").Length() != 0 || strings.TrimSpace(s4.Find(".stepper__label").Text()) != "Check" || s4.HasClass("is-done") || s4.HasClass("is-visited") {
		t.Error("step 4 must be plain upcoming text")
	}
	if doc.Find("h1, h2").Length() != 1 || doc.Find("svg").Length() != 1 {
		t.Errorf("one heading and one check icon: %d headings, %d icons", doc.Find("h1, h2").Length(), doc.Find("svg").Length())
	}
	if doc.Find("[style]").Length() != 0 {
		t.Error("no style attributes")
	}
	if strings.Contains(doc.Text(), "%") {
		t.Error("never a percentage")
	}
}

// On the first step nothing is done or visited; on the last every earlier
// step is done.
func TestStepper_FirstAndLast(t *testing.T) {
	first := docModel()
	first.Current, first.Furthest = 0, 0
	d := uitest.Render(t, wizard.Stepper(first))
	if d.Find("li.is-done, li.is-visited, a").Length() != 0 || d.Find("li.is-current").Index() != 0 {
		t.Error("step 1 of a new wizard: only the current step, no links")
	}
	last := docModel()
	last.Current, last.Furthest = 3, 3
	d = uitest.Render(t, wizard.Stepper(last))
	if d.Find("li.is-done").Length() != 3 || d.Find("li.is-done svg").Length() != 3 {
		t.Error("check step: three done steps with checks")
	}
	if got := strings.TrimSpace(d.Find("h2#step-title").Text()); got != "Check" {
		t.Errorf("heading = %q", got)
	}
}

// With errors or a conflict the summary or banner takes focus, not the
// heading.
func TestStepper_NoAutofocusOnErrorsOrConflict(t *testing.T) {
	e := docModel()
	e.Errors = []components.FieldErr{{ID: "party", Message: "Choose a party."}}
	c := docModel()
	c.Conflict = "Someone else changed this."
	for name, m := range map[string]wizard.Model{"errors": e, "conflict": c} {
		if _, ok := uitest.Render(t, wizard.Stepper(m)).Find("h2#step-title").Attr("autofocus"); ok {
			t.Errorf("%s: the heading must not take autofocus", name)
		}
	}
}

// TestFrame_OneHeadingEach (A-13): the frame has one h1 (the document) and
// one h2#step-title, no <progress>, and the step label appears once.
func TestFrame_OneHeadingEach(t *testing.T) {
	d := renderFrame(t, docModel(), wizard.Bar{})
	if h := d.Find("h1"); h.Length() != 1 || strings.TrimSpace(h.Text()) != "Draft 42" {
		t.Errorf("want one h1 with the document, got %d: %q", h.Length(), h.Text())
	}
	if d.Find("h2#step-title").Length() != 1 || d.Find("#step-title").Length() != 1 {
		t.Error("want one h2#step-title")
	}
	if d.Find("progress").Length() != 0 {
		t.Error("no <progress> (A-13)")
	}
	if n := strings.Count(d.Find("#wizard").Text(), "Lines"); n != 1 {
		t.Errorf("the step label appears %d times, want once", n)
	}
}

// TestFrame_StepIntro: the current step's Intro is one line under the
// stepper; a step without one has none.
func TestFrame_StepIntro(t *testing.T) {
	m := docModel()
	m.Steps[0].Intro = "Who you're buying from."
	m.Steps[1].Intro = "Add what you're buying."
	d := renderFrame(t, m, wizard.Bar{})
	p := d.Find("#wizard > p.step-intro")
	if p.Length() != 1 || strings.TrimSpace(p.Text()) != "Add what you're buying." {
		t.Fatalf("intro = %d %q, want the current step's", p.Length(), p.Text())
	}
	if p.PrevFiltered("nav.stepper").Length() != 1 {
		t.Error("the intro sits right under the stepper")
	}
	if renderFrame(t, docModel(), wizard.Bar{}).Find(".step-intro").Length() != 0 {
		t.Error("no Intro, no line")
	}
}

// TestModel_LongLabels (A-14): labels over 12 characters, counted in
// characters, not bytes.
func TestModel_LongLabels(t *testing.T) {
	m := docModel()
	m.Steps = steps("Twelve chars", "Thirteen char", "Größenangabe", "Check and submit")
	if got := m.LongLabels(); !slices.Equal(got, []string{"Thirteen char", "Check and submit"}) {
		t.Errorf("LongLabels = %q", got)
	}
	if wizard.MaxLabel != 12 {
		t.Errorf("MaxLabel = %d, want 12", wizard.MaxLabel)
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
	m.Steps = steps("Details", "Addresses", "Contacts", "Check")
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
			m.Steps[len(m.Steps)-1].Label = "Check"
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
	m.Steps[len(m.Steps)-1].Label = "Check"
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
	if b.NextAllFiltered("form#wizard-form").Length() != 1 || b.PrevAllFiltered("nav.stepper").Length() != 1 {
		t.Error("warning banner must sit after the stepper and above the step form")
	}
	if docNo := renderFrame(t, docModel(), wizard.Bar{}); docNo.Find(".banner--warning").Length() != 0 {
		t.Error("no warnings → no banner")
	}
}

func TestFrame_FocusTarget(t *testing.T) {
	d := renderFrame(t, docModel(), wizard.Bar{})
	h := d.Find("h2#step-title")
	if _, ok := h.Attr("autofocus"); !ok || h.AttrOr("tabindex", "") != "-1" || strings.TrimSpace(h.Text()) != "Lines" {
		t.Errorf("h2 must be the focus target with the step label: %q", h.Text())
	}

	m := docModel()
	m.Errors = []components.FieldErr{{ID: "party", Message: "Choose a party."}}
	e := renderFrame(t, m, wizard.Bar{})
	if _, ok := e.Find(".error-summary").Attr("autofocus"); !ok {
		t.Error("error summary must take autofocus")
	}
	if _, ok := e.Find("h2#step-title").Attr("autofocus"); ok {
		t.Error("h2 must not have autofocus when errors exist")
	}
	if e.Find("[autofocus]").Length() != 1 {
		t.Error("exactly one autofocus")
	}

	c := docModel()
	c.Conflict = "Someone else changed this."
	cd := renderFrame(t, c, wizard.Bar{})
	if _, ok := cd.Find("h2#step-title").Attr("autofocus"); ok {
		t.Error("h2 must not have autofocus on a conflict")
	}
	w := cd.Find("#wizard-conflict")
	if _, ok := w.Attr("autofocus"); !ok || w.AttrOr("tabindex", "") != "-1" || w.Find(".banner--warning").Length() != 1 {
		t.Error("conflict banner wrapper must take autofocus with tabindex -1")
	}
	if cd.Find("[autofocus]").Length() != 1 {
		t.Error("exactly one autofocus on a conflict")
	}
}

func TestFrame_ConflictKeepsValues(t *testing.T) {
	m := docModel()
	m.Conflict = "Someone else changed Draft 42 after you opened it. Reload it and make your changes again."
	m.ReloadURL = "/doc/po/42/edit/lines"
	d := renderFrame(t, m, wizard.Bar{})
	b := d.Find(".banner--warning")
	if b.Length() != 1 || !strings.Contains(b.Text(), m.Conflict) {
		t.Fatalf("want one warning banner with the conflict text (UI v2 §5.14)")
	}
	if d.Find(".banner--danger").Length() != 0 {
		t.Error("a conflict is a warning, not a danger banner")
	}
	const line = "Your entries stay on this page until you reload, so you can copy them."
	if !strings.Contains(strings.Join(strings.Fields(b.Text()), " "), line) {
		t.Errorf("banner text = %q, want the line %q", b.Text(), line)
	}
	a := b.Find("a")
	if a.Length() != 1 || strings.Join(strings.Fields(a.Text()), " ") != "Reload Draft 42" || a.AttrOr("href", "") != m.ReloadURL || !a.HasClass("btn") {
		t.Errorf("Reload button = %q → %q (class %q), want the button Reload Draft 42 → %s", a.Text(), a.AttrOr("href", ""), a.AttrOr("class", ""), m.ReloadURL)
	}
	if a.Find("svg").Length() != 1 {
		t.Error("the Reload button has no icon")
	}
	if d.Find("form input[name=party]").AttrOr("value", "") != "typed" {
		t.Error("children (typed values) must still render")
	}
	if renderFrame(t, docModel(), wizard.Bar{}).Find(".banner").Length() != 0 {
		t.Error("no conflict → no banner")
	}
}

// The Reload button names only the record being edited (A-29): a new
// document has none yet, and a master wizard's DocLabel names a kind of
// record, never one record.
func TestFrame_ConflictReloadNamesOnlyTheEditedRecord(t *testing.T) {
	for _, tc := range []struct {
		name  string
		mode  wizard.Mode
		label string
		want  string
	}{
		{"draft", wizard.Document, "Draft 57", "Reload Draft 57"},
		{"numbered", wizard.Document, "PO/26-27/00001", "Reload PO/26-27/00001"},
		{"new document", wizard.Document, "", "Reload"},
		{"master wizard", wizard.Master, "Parties", "Reload"},
	} {
		m := docModel()
		m.Mode, m.DocLabel = tc.mode, tc.label
		m.Conflict, m.ReloadURL = "Someone else changed it.", "/x"
		a := renderFrame(t, m, wizard.Bar{}).Find(".banner--warning a")
		if got := strings.Join(strings.Fields(a.Text()), " "); got != tc.want {
			t.Errorf("%s: Reload button = %q, want %q", tc.name, got, tc.want)
		}
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
	if strings.TrimSpace(d.Find("#wizard h1").Text()) != "Draft 42" {
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
			return wizard.CheckSection("Party", "handshake", st, "/doc/po/42/edit/party").Render(templ.WithChildren(ctx, templ.Raw(`<p class="summary-x">Acme</p>`)), w)
		}))
		sec := d.Find("section.card")
		if !strings.Contains(sec.Find("h2").Text(), "Party") {
			t.Errorf("%s: title missing", word)
		}
		if !strings.Contains(sec.Text(), word) || sec.Find("svg").Length() == 0 {
			t.Errorf("%s: want the word and an icon, got %q", word, sec.Text())
		}
		a := sec.Find("a")
		if a.AttrOr("href", "") != "/doc/po/42/edit/party" || strings.TrimSpace(a.Text()) != "Change" || a.AttrOr("aria-label", "") != "Change Party" {
			t.Errorf("%s: Change link = %q (aria-label %q)", word, a.Text(), a.AttrOr("aria-label", ""))
		}
		if sec.Find("p.summary-x").Length() != 1 {
			t.Errorf("%s: children missing", word)
		}
	}
}

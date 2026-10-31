package wizard_test

import (
	"context"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/ui/uitest"
	"github.com/vardhan/internaltools/internal/ui/wizard"
)

// M0-26 R4 (CD-282, CD-286): with Bar.EnterPrimary the step form starts with
// components.EnterSubmit(primary), an empty, unseen copy of the primary, so
// Enter (implicit submission presses the form's first submit) always presses
// the primary and never a submit button of the step's body.

// m26BodyButton is a body with a submit button of its own, as GRN's Items
// step has (`Receive all pending`), and an input to press Enter in.
const m26BodyButton = `<input name="received" value="4"><button type="submit" name="fill" value="all">Receive all pending</button>`

// m26Frame renders Frame with m26BodyButton as its children.
func m26Frame(t *testing.T, m wizard.Model, bar wizard.Bar) *goquery.Document {
	t.Helper()
	child := templ.Raw(m26BodyButton)
	return uitest.Render(t, templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		return wizard.Frame(m, bar).Render(templ.WithChildren(ctx, child), w)
	}))
}

// m26OwnedSubmits is every submit button the form form owns, in tree order:
// a submit inside it without a form attribute, or one anywhere naming it.
func m26OwnedSubmits(d *goquery.Document, form string) []*goquery.Selection {
	var out []*goquery.Selection
	d.Find("button, input").Each(func(_ int, s *goquery.Selection) {
		typ := strings.ToLower(s.AttrOr("type", ""))
		switch goquery.NodeName(s) {
		case "button":
			if typ != "" && typ != "submit" {
				return
			}
		default:
			if typ != "submit" && typ != "image" {
				return
			}
		}
		if f, ok := s.Attr("form"); ok {
			if f == form {
				out = append(out, s)
			}
			return
		}
		if s.Closest("form").AttrOr("id", "") == form {
			out = append(out, s)
		}
	})
	return out
}

// m26Copies is every submit that looks like the Enter copy: tabindex -1 and
// aria-hidden.
func m26Copies(d *goquery.Document) *goquery.Selection {
	return d.Find(`button[tabindex="-1"][aria-hidden="true"], input[type="submit"][tabindex="-1"][aria-hidden="true"]`)
}

// m26Attr is sel's attribute name, or "<none>".
func m26Attr(sel *goquery.Selection, name string) string {
	v, ok := sel.Attr(name)
	if !ok {
		return "<none>"
	}
	return v
}

// m26WantCopy fails unless s is the Enter copy of a primary posting
// name=value (name "" posts nothing of its own): an empty <button
// type=submit>, tabindex -1, aria-hidden, no form attribute, sized by a class
// (an @utility), never hidden, display:none or visually hidden.
func m26WantCopy(t *testing.T, s *goquery.Selection, name, value, what string) {
	t.Helper()
	html, _ := goquery.OuterHtml(s)
	if goquery.NodeName(s) != "button" || m26Attr(s, "type") != "submit" {
		t.Errorf("%s: the form's first submit is %.200s, want the Enter copy <button type=submit>", what, html)
	}
	if txt := strings.TrimSpace(s.Text()); txt != "" || s.Children().Length() != 0 {
		t.Errorf("%s: the Enter copy holds %q (%d elements), want it empty: %.200s", what, txt, s.Children().Length(), html)
	}
	if got := m26Attr(s, "tabindex"); got != "-1" {
		t.Errorf("%s: the Enter copy's tabindex = %q, want -1: %.200s", what, got, html)
	}
	if got := m26Attr(s, "aria-hidden"); got != "true" {
		t.Errorf("%s: the Enter copy's aria-hidden = %q, want true: %.200s", what, got, html)
	}
	if name == "" {
		if got := m26Attr(s, "name"); got != "<none>" {
			t.Errorf("%s: the Enter copy has name %q, want none, as the primary: %.200s", what, got, html)
		}
		if got := m26Attr(s, "value"); got != "<none>" && got != "" {
			t.Errorf("%s: the Enter copy has value %q, want none, as the primary: %.200s", what, got, html)
		}
	} else if n, v := m26Attr(s, "name"), m26Attr(s, "value"); n != name || v != value {
		t.Errorf("%s: the Enter copy posts %s=%s, want the primary's %s=%s: %.200s", what, n, v, name, value, html)
	}
	if got := m26Attr(s, "form"); got != "<none>" {
		t.Errorf("%s: the Enter copy has form=%q, want no form attribute (it sits in the form): %.200s", what, got, html)
	}
	for _, a := range []string{"hidden", "style", "disabled"} {
		if _, ok := s.Attr(a); ok {
			t.Errorf("%s: the Enter copy has a %s attribute, want none: %.200s", what, a, html)
		}
	}
	classes := strings.Fields(s.AttrOr("class", ""))
	if len(classes) == 0 {
		t.Errorf("%s: the Enter copy has no class, want the 0×0 @utility: %.200s", what, html)
	}
	for _, c := range []string{"hidden", "sr-only", "visually-hidden", "invisible"} {
		if slices.Contains(classes, c) {
			t.Errorf("%s: the Enter copy has class %s, want it sized 0×0, not hidden: %.200s", what, c, html)
		}
	}
}

// HOOK-045 (wizard unit): EnterPrimary puts the copy first among the step
// form's submits, ahead of the body's own button and the bar; it copies the
// primary's nav (PrimaryNav "stay" posts nav=stay); with m.After the bar is
// outside the form but the copy is inside it with no form attribute. The bar
// itself is unchanged. NoPrimary, or EnterPrimary false, renders no copy.
func TestFrame_EnterPrimary(t *testing.T) {
	cases := []struct {
		name        string
		bar         wizard.Bar
		after       bool
		label       string // the visible primary
		navN, navV  string // what the copy posts
		wantBackBtn bool
	}{
		{name: "default primary", bar: wizard.Bar{Back: true, EnterPrimary: true}, label: "Save and continue", wantBackBtn: true},
		{name: "nav=stay primary", bar: wizard.Bar{Primary: "Add selected", PrimaryNav: "stay", Back: true, EnterPrimary: true}, label: "Add selected", navN: "nav", navV: "stay", wantBackBtn: true},
		{name: "After", bar: wizard.Bar{Primary: "Add selected", PrimaryNav: "stay", EnterPrimary: true}, after: true, label: "Add selected", navN: "nav", navV: "stay"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := docModel()
			if c.after {
				m.After = templ.Raw(`<p id="after">y</p>`)
			}
			d := m26Frame(t, m, c.bar)
			owned := m26OwnedSubmits(d, "wizard-form")
			if len(owned) == 0 {
				t.Fatal("#wizard-form owns no submit button")
			}
			m26WantCopy(t, owned[0], c.navN, c.navV, c.name)
			if n := m26Copies(d).Length(); n != 1 {
				t.Errorf("%d Enter copies on the page, want 1", n)
			}
			if owned[0].Closest("#wizard-form").Length() != 1 {
				t.Errorf("the Enter copy is outside #wizard-form, want it the form's own first submit")
			}
			if owned[0].Closest(".actionbar").Length() != 0 {
				t.Errorf("the Enter copy is in the action bar, want it at the start of the step form")
			}
			body := d.Find(`button[name="fill"]`)
			if body.Length() != 1 {
				t.Fatalf("%d body buttons, want the child's one", body.Length())
			}
			bodyAt := slices.IndexFunc(owned, func(s *goquery.Selection) bool { return s.IsSelection(body) })
			if bodyAt <= 0 {
				t.Errorf("the body's button is submit %d of the form, want it after the Enter copy", bodyAt)
			}
			p := d.Find("#wizard .actionbar .btn--primary")
			if p.Length() != 1 || strings.TrimSpace(p.Text()) != c.label {
				t.Errorf("visible primaries = %d (%q), want the bar's one %q, unchanged", p.Length(), strings.TrimSpace(p.Text()), c.label)
			}
			if n := d.Find(`button[name="nav"][value="back"]`).Length(); (n == 1) != c.wantBackBtn || n > 1 {
				t.Errorf("%d Back buttons, want %v", n, c.wantBackBtn)
			}
			if d.Find(`#wizard [style]`).Length() != 0 {
				t.Errorf("a style attribute in the frame, want none (CSP)")
			}
		})
	}

	for _, c := range []struct {
		name string
		bar  wizard.Bar
	}{
		{"NoPrimary", wizard.Bar{NoPrimary: true, EnterPrimary: true}},
		{"EnterPrimary false", wizard.Bar{Back: true}},
		{"EnterPrimary false, nav=stay", wizard.Bar{Primary: "Add selected", PrimaryNav: "stay"}},
	} {
		t.Run(c.name+" has no copy", func(t *testing.T) {
			d := m26Frame(t, docModel(), c.bar)
			if n := m26Copies(d).Length(); n != 0 {
				html, _ := goquery.OuterHtml(m26Copies(d).First())
				t.Errorf("%d Enter copies (%.200s), want none", n, html)
			}
			owned := m26OwnedSubmits(d, "wizard-form")
			if len(owned) == 0 || m26Attr(owned[0], "name") != "fill" {
				t.Errorf("the form's first submit isn't the body's button, want nothing added before it")
			}
		})
	}
}

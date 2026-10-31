package wizard_test

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/wizard"
)

// M0-17 (CD-263 G6): Bar.PrimaryNav makes the primary post nav=<value>, and
// Bar.BackLink is a link in place of the Back submit. Save and finish later
// stays, and the primary is still the bar's first button (Enter presses it).
// With m.After the bar is outside the step form, so the primary posts it
// through its form attribute; the link has none.
func TestFrame_PrimaryNavBackLink(t *testing.T) {
	back := &components.Link{Label: "Back to items", Href: "/doc/po/42/edit/lines"}
	bar := wizard.Bar{Primary: "Add selected", Back: true, PrimaryNav: "stay", BackLink: back}
	for name, after := range map[string]templ.Component{"no After": nil, "After": templ.Raw(`<p id="after">y</p>`)} {
		t.Run(name, func(t *testing.T) {
			m := docModel()
			m.After = after
			d := renderFrame(t, m, bar)
			p := d.Find("#wizard .actionbar .btn--primary")
			if p.Length() != 1 {
				t.Fatalf("%d primary buttons, want 1", p.Length())
			}
			if goquery.NodeName(p) != "button" || p.AttrOr("type", "") != "submit" || strings.TrimSpace(p.Text()) != "Add selected" {
				t.Errorf("primary = <%s type=%q> %q, want the submit Add selected", goquery.NodeName(p), p.AttrOr("type", ""), strings.TrimSpace(p.Text()))
			}
			if n, v := p.AttrOr("name", "<none>"), p.AttrOr("value", "<none>"); n != "nav" || v != "stay" {
				t.Errorf("primary posts %s=%s, want nav=stay", n, v)
			}
			if first := d.Find(`#wizard .actionbar button[type="submit"]`).First(); strings.TrimSpace(first.Text()) != "Add selected" {
				t.Errorf("the bar's first submit is %q, want the primary", strings.TrimSpace(first.Text()))
			}
			if n := d.Find(`button[name="nav"][value="back"]`).Length(); n != 0 {
				t.Errorf("%d Back submit buttons, want none: BackLink replaces it", n)
			}
			a := d.Find("#wizard .actionbar a")
			if a.Length() != 1 || strings.TrimSpace(a.Text()) != back.Label || a.AttrOr("href", "") != back.Href {
				t.Errorf("bar links = %d (%q → %q), want one %q → %q", a.Length(), strings.TrimSpace(a.Text()), a.AttrOr("href", ""), back.Label, back.Href)
			}
			if _, ok := a.Attr("form"); ok {
				t.Errorf("the Back link has a form attribute, want none: it saves nothing")
			}
			if fin := d.Find(`button[name="nav"][value="finish"]`); fin.Length() != 1 || strings.TrimSpace(fin.Text()) != "Save and finish later" {
				t.Errorf("Save and finish later: %d buttons, want 1", fin.Length())
			}
			wantForm := "<none>"
			if after != nil {
				wantForm = "wizard-form"
				if d.Find("#wizard-form .actionbar").Length() != 0 {
					t.Errorf("with After the bar is inside the step form")
				}
			}
			if got := p.AttrOr("form", "<none>"); got != wantForm {
				t.Errorf("primary form attribute = %q, want %q", got, wantForm)
			}
		})
	}

	t.Run("zero keeps the default", func(t *testing.T) {
		d := renderFrame(t, docModel(), wizard.Bar{Back: true})
		p := d.Find("#wizard .actionbar .btn--primary")
		if p.Length() != 1 || strings.TrimSpace(p.Text()) != "Save and continue" {
			t.Fatalf("primary = %q, want Save and continue", strings.TrimSpace(p.Text()))
		}
		if _, ok := p.Attr("name"); ok {
			t.Errorf("primary has a name, want none with PrimaryNav empty")
		}
		if n := d.Find(`button[name="nav"][value="back"]`).Length(); n != 1 {
			t.Errorf("%d Back submit buttons, want 1 with BackLink nil", n)
		}
		if n := d.Find("#wizard .actionbar a").Length(); n != 0 {
			t.Errorf("%d bar links, want none with BackLink nil", n)
		}
	})
}

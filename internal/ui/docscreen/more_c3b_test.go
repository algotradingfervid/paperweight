package docscreen_test

import (
	"context"
	"io"
	"slices"
	"testing"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// UIV2-C3b (CD-363): when every shown input of a card is More, its fields
// show without a group; GroupMore is the one rule docscreen's step and line
// page use.

// c3bInput is a named stand-in input, so the test can tell inputs apart.
type c3bInput string

func (c3bInput) Render(context.Context, io.Writer) error { return nil }

func c3bNames(ins []templ.Component) []string {
	var out []string
	for _, in := range ins {
		out = append(out, string(in.(c3bInput)))
	}
	return out
}

func TestGroupMore(t *testing.T) {
	ref := doctype.Field{Name: "ref", Label: "Reference", Type: doctype.Text}
	project := doctype.Field{Name: "project", Label: "Project", Type: doctype.Text, More: true}
	urgent := doctype.Field{Name: "urgent", Label: "Urgent", Type: doctype.Check, More: true}
	print := doctype.Field{Name: "no_print", Label: "No print", Type: doctype.Check, Invert: "Print", More: true}
	in := func(f doctype.Field, value, err string) docscreen.MoreInput {
		return docscreen.MoreInput{Field: f, Value: value, Error: err, Input: c3bInput(f.Name)}
	}
	for _, c := range []struct {
		name          string
		ins           []docscreen.MoreInput
		plain, group  []string
		labels        []string
		open, present bool
	}{
		{name: "no More fields", ins: []docscreen.MoreInput{in(ref, "", "")}, plain: []string{"ref"}},
		{name: "none at all"},
		{name: "one More field only", ins: []docscreen.MoreInput{in(project, "", "")}, plain: []string{"project"}},
		{name: "all More", ins: []docscreen.MoreInput{in(project, "Panvel", ""), in(urgent, "", "")}, plain: []string{"project", "urgent"}},
		{name: "mixed closed", ins: []docscreen.MoreInput{in(project, "", ""), in(ref, "R-1", ""), in(print, "false", "")},
			plain: []string{"ref"}, group: []string{"project", "no_print"}, labels: []string{"Project", "Print"}, present: true},
		{name: "open on value", ins: []docscreen.MoreInput{in(ref, "", ""), in(project, "Panvel", "")},
			plain: []string{"ref"}, group: []string{"project"}, labels: []string{"Project"}, open: true, present: true},
		{name: "open on a Check set", ins: []docscreen.MoreInput{in(ref, "", ""), in(urgent, "true", "")},
			plain: []string{"ref"}, group: []string{"urgent"}, labels: []string{"Urgent"}, open: true, present: true},
		{name: "open on a Check set as 1", ins: []docscreen.MoreInput{in(ref, "", ""), in(urgent, "1", "")},
			plain: []string{"ref"}, group: []string{"urgent"}, labels: []string{"Urgent"}, open: true, present: true},
		{name: "a Check unset as 0", ins: []docscreen.MoreInput{in(ref, "", ""), in(urgent, "0", "")},
			plain: []string{"ref"}, group: []string{"urgent"}, labels: []string{"Urgent"}, present: true},
		{name: "open on error", ins: []docscreen.MoreInput{in(ref, "", ""), in(project, "", "Check this field.")},
			plain: []string{"ref"}, group: []string{"project"}, labels: []string{"Project"}, open: true, present: true},
		{name: "value outside opens nothing", ins: []docscreen.MoreInput{in(ref, "R-1", "Bad"), in(project, "", "")},
			plain: []string{"ref"}, group: []string{"project"}, labels: []string{"Project"}, present: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			g := docscreen.GroupMore(c.ins)
			if got := c3bNames(g.Plain); !slices.Equal(got, c.plain) {
				t.Errorf("Plain = %q, want %q", got, c.plain)
			}
			if got := c3bNames(g.Group); !slices.Equal(got, c.group) {
				t.Errorf("Group = %q, want %q", got, c.group)
			}
			if !slices.Equal(g.Labels, c.labels) {
				t.Errorf("Labels = %q, want %q", g.Labels, c.labels)
			}
			if g.Open != c.open {
				t.Errorf("Open = %v, want %v", g.Open, c.open)
			}
			if n, want := len(g.Inputs()), len(c.plain)+map[bool]int{true: 1}[c.present]; n != want {
				t.Errorf("Inputs() has %d components, want %d (Plain, then the group when present)", n, want)
			}
		})
	}
}

// Through the step and the line page: with every shown field More there is
// no group and each field shows; with a plain field beside them, a group.
func TestMoreC3b_AllMoreShowsFieldsWithoutGroup(t *testing.T) {
	for _, c := range []struct {
		name, target string
		hide         []string
		group        bool
		ids          []string
	}{
		{"step all More", c1Doc + "/edit/details", []string{"ref", "secret"}, false, []string{"f-project", "f-need_by", "f-urgent", "f-no_print"}},
		{"step mixed", c1Doc + "/edit/details", []string{"secret"}, true, []string{"f-ref", "f-project"}},
		{"line all More", c1Doc + "/lines/1", []string{"item", "line_secret"}, false, []string{"f-line_note"}},
		{"line mixed", c1Doc + "/lines/1", []string{"line_secret"}, true, []string{"f-item", "f-line_note"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newC1Env(t, c.hide...)
			seedC1(e, nil, nil)
			page := c1Get(t, e, c.target)
			if n := page.Find("form details.disclosure").Length(); (n == 1) != c.group || n > 1 {
				t.Errorf("%d More details groups, want group = %v", n, c.group)
			}
			for _, id := range c.ids {
				if page.Find("form #"+id).Length() != 1 {
					t.Errorf("#%s is not in the form", id)
				}
			}
		})
	}
}

// UIV2-C3b (UI v2 §5.6): a docscreen wizard step's × goes to the list from a
// new document, asking first; from a saved draft it goes straight to the
// document page, or to the list from the check step, which is that page.
func TestWizardClose_Docscreen(t *testing.T) {
	for _, c := range []struct {
		name, target, close string
		asks                bool
	}{
		{"new", "/doc/" + c1Type + "/new", "/doc/" + c1Type, true},
		{"draft", c1Doc + "/edit/details", c1Doc, false},
		{"draft, a later step", c1Doc + "/edit/items", c1Doc, false},
		{"draft, check step", c1Doc + "/edit/check", "/doc/" + c1Type, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newC1Env(t)
			seedC1(e, nil, nil)
			page := c1Get(t, e, c.target)
			x := page.Find(".wizard-head > a.wizard-close")
			if x.Length() != 1 || x.AttrOr("href", "") != c.close {
				t.Errorf("×: %d, href %q; want %s", x.Length(), x.AttrOr("href", ""), c.close)
			}
			if got := page.Find("dialog#leave-wizard").Length() == 1; got != c.asks {
				t.Errorf("leave dialog shown = %v, want %v", got, c.asks)
			}
			if got := page.Find("body.focus-mode").Length(); got != 1 {
				t.Error("the wizard page isn't in focus mode")
			}
		})
	}
}

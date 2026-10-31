package docscreen_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
)

// M0-53 (CD-326 (1), (4), (7)): CardsOnly puts the Items step and the check
// page in the form column; CheckExtra.Covers leaves fields out of the check
// page's details; the totals sit apart from the words; an empty document's
// "Add at least one …" uses LineNoun; ReasonExtra.NoteWarn draws the note as
// a warning banner.

// m53Width is the page's content column class: form-width or content.
func m53Width(t *testing.T, page *goquery.Document, what string) string {
	t.Helper()
	col := page.Find("main .stack").First()
	switch {
	case col.HasClass("form-width"):
		return "form-width"
	case col.HasClass("content"):
		return "content"
	}
	t.Fatalf("%s: no content column; main .stack classes %q", what, col.AttrOr("class", ""))
	return ""
}

// CD-326 (1): with CardsOnly the Items step and the check page use the form
// width (40rem), as the details step does; without it, the content width.
func TestModule_CardsOnlyFormWidth(t *testing.T) {
	e, id := m01bHooked(t, docscreen.Module{})
	for _, c := range []struct {
		name string
		m    docscreen.Module
		want string
	}{
		{"CardsOnly", docscreen.Module{CardsOnly: true}, "form-width"},
		{"no module flag", docscreen.Module{}, "content"},
	} {
		m01bMount(e, docscreentest.Hooked, c.m)
		for _, step := range []string{"items", "check"} {
			what := c.name + " " + step
			page := m01bOK(t, e.get(t, m01bURL(id)+"/edit/"+step), what)
			if got := m53Width(t, page, what); got != c.want {
				t.Errorf("%s: column %s, want %s", what, got, c.want)
			}
		}
		page := m01bOK(t, e.get(t, m01bURL(id)+"/edit/terms"), c.name+" terms")
		if got := m53Width(t, page, c.name+" terms"); got != "form-width" {
			t.Errorf("%s terms: column %s, want form-width", c.name, got)
		}
	}
}

// m53Check is a Check giving one total row and covering the named fields.
func m53Check(covers ...string) func(docscreen.Ctx) (docscreen.CheckExtra, error) {
	return func(docscreen.Ctx) (docscreen.CheckExtra, error) {
		return docscreen.CheckExtra{Totals: []components.Pair{{Label: "To pay", Value: "₹1.00", Num: true}}, Covers: covers}, nil
	}
}

// CD-326 (4): a field CheckExtra.Covers names is left out of its step's
// details on the check page; the others stay. The module's totals sit in a
// spaced column with the grand total's words, apart from them.
func TestModule_CheckCovers(t *testing.T) {
	e, id := m01bHooked(t, docscreen.Module{Check: m53Check()})
	page := m01bOK(t, e.get(t, m01bURL(id)+"/edit/check"), "GET check")
	labels, _ := m01bPairs(page, "Terms")
	if !slices.Contains(labels, "Reference") || !slices.Contains(labels, "Remarks") {
		t.Fatalf("without Covers the Terms details = %q, want Reference and Remarks", labels)
	}

	m01bMount(e, docscreentest.Hooked, docscreen.Module{Check: m53Check("ref")})
	page = m01bOK(t, e.get(t, m01bURL(id)+"/edit/check"), "GET check with Covers")
	labels, _ = m01bPairs(page, "Terms")
	if slices.Contains(labels, "Reference") {
		t.Errorf("Covers names ref and the Terms details still show Reference: %q", labels)
	}
	if !slices.Contains(labels, "Remarks") {
		t.Errorf("Covers names only ref and Remarks went too: %q", labels)
	}

	totals := page.Find("dl.summary").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return norm(s.Find("dt").First().Text()) == "To pay"
	})
	if totals.Length() != 1 {
		t.Fatalf("%d totals lists, want 1", totals.Length())
	}
	if !totals.Parent().HasClass("stack") {
		t.Errorf("the module's totals sit in %q, want a spaced column (stack)", totals.Parent().AttrOr("class", ""))
	}
}

// CD-326 (4): an empty document's check page asks for at least one line in
// the module's LineNoun.Title; with none, docscreen's "line".
func TestModule_CheckNoLinesNoun(t *testing.T) {
	for _, c := range []struct {
		name string
		m    docscreen.Module
		want string
	}{
		{"LineNoun", docscreen.Module{LineNoun: docscreen.LineNoun{Title: "Expense"}}, "Add at least one expense."},
		{"no LineNoun", docscreen.Module{}, "Add at least one line."},
	} {
		e, id := m20Env(t, 0, c.m)
		page := m01bOK(t, e.get(t, m01bURL(id)+"/edit/check"), c.name)
		items := checkSection(page.Selection, "Items")
		if got := norm(items.Text()); !strings.Contains(got, c.want) {
			t.Errorf("%s: Items section = %q, want %q", c.name, got, c.want)
		}
		if c.want != "Add at least one line." && strings.Contains(norm(page.Text()), "Add at least one line.") {
			t.Errorf("%s: the page still says Add at least one line.", c.name)
		}
	}
}

// CD-326 (7): NoteWarn draws the reason page's note as a warning banner;
// without it the note is an info banner.
func TestModule_ReasonNoteWarn(t *testing.T) {
	e := newHookedEnv(t)
	id := m04ApprovedHooked(t, e)
	const note = "₹4,000.00 is left on this advance. Closing treats it as returned."
	for _, c := range []struct {
		warn bool
		want string
	}{{true, "banner--warning"}, {false, "banner--info"}} {
		m04Mount(e, docscreentest.Hooked, docscreen.Module{Reason: func(docscreen.Ctx, doctype.Action) (docscreen.ReasonExtra, error) {
			return docscreen.ReasonExtra{Note: note, NoteWarn: c.warn}, nil
		}})
		page := m04Page(t, e.get(t, m04URL(docscreentest.Hooked, id)+"/close"), http.StatusOK, "GET close")
		banner := page.Find(".banner").FilterFunction(func(_ int, s *goquery.Selection) bool {
			return strings.Contains(norm(s.Text()), note)
		})
		if banner.Length() != 1 {
			t.Fatalf("NoteWarn %v: %d banners with the note, want 1", c.warn, banner.Length())
		}
		if !banner.HasClass(c.want) {
			t.Errorf("NoteWarn %v: banner classes %q, want %s", c.warn, banner.AttrOr("class", ""), c.want)
		}
	}
}

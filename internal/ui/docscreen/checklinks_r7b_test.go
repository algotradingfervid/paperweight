package docscreen_test

import (
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// r7bLinks is each listed problem on the Items section: its text and href
// ("" for plain text).
func r7bLinks(page *goquery.Document) map[string]string {
	out := map[string]string{}
	checkSection(page.Selection, "Items").Find("ul > li").Each(func(_ int, s *goquery.Selection) {
		out[norm(s.Text())] = s.Find("a").AttrOr("href", "")
	})
	return out
}

// R7b: a Check problem on a line links to that line's card, whose id is
// stable (line-{n}; the 21st card's one id stays R14's line-reveal, the
// target Show all and a link to line 21 share). On a collapsed Check page
// (more than 20 lines, CD-395) a line past the 20th is reached through
// ?all=1#<id>; with ?all=1, or 20 lines or fewer, the link is the bare
// fragment. A problem with no line, or on a line the draft no longer has,
// stays plain text.
func TestR7b_CheckLinksReachTheLine(t *testing.T) {
	check := doc42 + "/edit/check"
	problems := []doctype.FieldError{
		{Field: "qty", Line: 1, Message: "Line 1: Check the quantity."},
		{Field: "qty", Line: 3, Message: "Line 3: Check the quantity."},
		{Field: "qty", Line: 20, Message: "Line 20: Check the quantity."},
		{Field: "qty", Line: 21, Message: "Line 21: Check the quantity."},
		{Field: "qty", Line: 25, Message: "Line 25: Check the quantity."},
		{Field: "qty", Line: 30, Message: "Line 30: No such line any more."},
		{Field: "lines", Message: "Add the freight line."},
	}
	mount := func(e *docEnv, ps []doctype.FieldError) {
		e.d.Modules = map[string]docscreen.Module{docscreentest.Sample: {Check: func(docscreen.Ctx) (docscreen.CheckExtra, error) {
			return docscreen.CheckExtra{Problems: ps}, nil
		}}}
		e.remount()
	}
	e := newLinesEnv(t, manyLines(25))
	mount(e, problems)
	for _, c := range []struct {
		path string
		want map[string]string
	}{
		{check, map[string]string{
			"Line 1: Check the quantity.":     "#line-1",
			"Line 3: Check the quantity.":     "#line-3",
			"Line 20: Check the quantity.":    "#line-20",
			"Line 21: Check the quantity.":    check + "?all=1#line-reveal",
			"Line 25: Check the quantity.":    check + "?all=1#line-25",
			"Line 30: No such line any more.": "",
			"Add the freight line.":           "",
		}},
		{check + "?all=1", map[string]string{
			"Line 1: Check the quantity.":     "#line-1",
			"Line 3: Check the quantity.":     "#line-3",
			"Line 20: Check the quantity.":    "#line-20",
			"Line 21: Check the quantity.":    "#line-reveal",
			"Line 25: Check the quantity.":    "#line-25",
			"Line 30: No such line any more.": "",
			"Add the freight line.":           "",
		}},
	} {
		page := uitest.Doc(t, e.get(t, c.path))
		got := r7bLinks(page)
		if len(got) != len(c.want) {
			t.Errorf("%s: problems %q, want %q", c.path, got, c.want)
		}
		for text, href := range c.want {
			if h, ok := got[text]; !ok || h != href {
				t.Errorf("%s: %q links to %q (listed %v), want %q", c.path, text, h, ok, href)
			}
		}
		// Every rendered card carries its id and can take focus.
		page.Find(".linelist ol.lines > li.lines-card").Each(func(i int, s *goquery.Selection) {
			want := "line-" + itoa(int64(i+1))
			if i == 20 {
				want = "line-reveal"
			}
			if s.AttrOr("id", "") != want || s.AttrOr("tabindex", "") != "-1" || norm(s.Find(".lines-no").Text()) != itoa(int64(i+1)) {
				t.Errorf("%s: card %d id %q tabindex %q, want %q and -1", c.path, i+1, s.AttrOr("id", ""), s.AttrOr("tabindex", ""), want)
			}
		})
	}
	full := uitest.Doc(t, e.get(t, check+"?all=1"))
	if full.Find("#line-25").Length() != 1 || full.Find("#line-reveal").Length() != 1 || full.Find("#line-21").Length() != 0 {
		t.Errorf("?all=1: #line-25 %d, #line-reveal %d, #line-21 %d; want 1, 1, 0", full.Find("#line-25").Length(), full.Find("#line-reveal").Length(), full.Find("#line-21").Length())
	}

	few := newLinesEnv(t, threeLines())
	mount(few, problems[1:2])
	if got := r7bLinks(uitest.Doc(t, few.get(t, check)))["Line 3: Check the quantity."]; got != "#line-3" {
		t.Errorf("3 lines: line 3 links to %q, want #line-3", got)
	}
}

// R7b round 2: a card's id is its line's position, the FieldError.Line a
// problem names, never the number a module's LineCard shows (GRN shows the
// PO line number). With a hook numbering the cards 102, 103, 104, the
// problem on line 2 links #line-2, which is the second card (showing 103),
// and no card carries an id from its shown number.
func TestR7b_CardIDIsThePosition(t *testing.T) {
	e := newLinesEnv(t, threeLines())
	e.d.Modules = map[string]docscreen.Module{docscreentest.Sample: {
		Check: func(docscreen.Ctx) (docscreen.CheckExtra, error) {
			return docscreen.CheckExtra{Problems: []doctype.FieldError{{Field: "qty", Line: 2, Message: "Line 2: Check the quantity."}}}, nil
		},
		LineCard: func(_ docscreen.Ctx, l docscreen.LineView) components.LineCard {
			c := l.Card
			c.No = 101 + l.No
			return c
		},
	}}
	e.remount()
	for _, path := range []string{doc42 + "/edit/check", doc42 + "/edit/items"} {
		page := uitest.Doc(t, e.get(t, path))
		cards := page.Find(".linelist ol.lines > li.lines-card")
		if cards.Length() != 3 {
			t.Fatalf("%s: %d cards, want 3", path, cards.Length())
		}
		cards.Each(func(i int, s *goquery.Selection) {
			if id, no := s.AttrOr("id", ""), norm(s.Find(".lines-no").Text()); id != "line-"+itoa(int64(i+1)) || no != itoa(int64(102+i)) {
				t.Errorf("%s: card %d has id %q showing %q, want line-%d showing %d", path, i+1, id, no, i+1, 102+i)
			}
		})
	}
	if got := r7bLinks(uitest.Doc(t, e.get(t, doc42+"/edit/check")))["Line 2: Check the quantity."]; got != "#line-2" {
		t.Errorf("line 2 links to %q, want #line-2", got)
	}
}

package wizard_test

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/ui/wizard"
)

// UI v2 C3c (CD-376 (1), CD-378 (1)): "Save and finish later" is a ghost
// button on its own row in the page flow directly after the sticky footer,
// not inside it, and after the primary in tree order, so Enter in a field
// still presses the primary. With or without After.
func TestFrame_FinishLaterRowAfterTheFooter(t *testing.T) {
	for _, after := range []bool{false, true} {
		m := docModel()
		if after {
			m.After = templ.Raw(`<div id="after"></div>`)
		}
		d := renderFrame(t, m, wizard.Bar{Back: true})
		bar := d.Find("#wizard .actionbar")
		if bar.Length() != 1 {
			t.Fatalf("after=%v: %d .actionbar, want 1", after, bar.Length())
		}
		if n := bar.Find(".actionbar-later").Length(); n != 0 {
			t.Errorf("after=%v: the finish-later row is inside the footer, want it after", after)
		}
		row := bar.Next()
		if !row.HasClass("actionbar-later") {
			t.Fatalf("after=%v: the footer's next sibling is %q, want the .actionbar-later row", after, row.AttrOr("class", ""))
		}
		fin := row.Children()
		if fin.Length() != 1 || fin.AttrOr("value", "") != "finish" || strings.TrimSpace(fin.Text()) != "Save and finish later" || !fin.HasClass("btn-ghost") {
			t.Errorf("after=%v: the row holds %d elements (%q), want only the ghost Save and finish later", after, fin.Length(), fin.Text())
		}
		var order []string
		d.Find("#wizard button").Each(func(_ int, s *goquery.Selection) {
			order = append(order, strings.TrimSpace(s.Text()))
		})
		if got := strings.Join(order, "|"); got != "Save and continue|Back|Save and finish later" {
			t.Errorf("after=%v: buttons in tree order = %q, want the primary, Back, then Save and finish later", after, got)
		}
		if d.Find(".actionbar-later").Length() != 1 {
			t.Errorf("after=%v: %d .actionbar-later, want one", after, d.Find(".actionbar-later").Length())
		}
	}
}

// A wizard with no "Save and finish later" (a master wizard, a document's
// check step) renders no empty row for it.
func TestFrame_NoFinishLaterNoRow(t *testing.T) {
	master := docModel()
	master.Mode = wizard.Master
	last := docModel()
	last.Current = len(last.Steps) - 1
	for name, m := range map[string]wizard.Model{"master": master, "check step": last} {
		d := renderFrame(t, m, wizard.Bar{Back: true})
		if n := d.Find(".actionbar-later").Length(); n != 0 {
			t.Errorf("%s: %d .actionbar-later, want none", name, n)
		}
		if d.Find("#wizard .actionbar").Length() != 1 {
			t.Errorf("%s: no footer", name)
		}
	}
}

// Bar.Summary is the footer's summary row (UI v2 §5.6, A-22): its first
// child, before the buttons; without one the footer has no summary row.
func TestFrame_SummaryRowFirstInTheFooter(t *testing.T) {
	d := renderFrame(t, docModel(), wizard.Bar{Back: true, Summary: templ.Raw(`<span id="sum">2 items</span>`)})
	first := d.Find("#wizard .actionbar").Children().First()
	if !first.HasClass("actionbar-summary") || first.Find("#sum").Length() != 1 || strings.TrimSpace(first.Text()) != "2 items" {
		t.Errorf("the footer's first child = %q %q, want the .actionbar-summary row holding the summary", first.AttrOr("class", ""), first.Text())
	}
	if n := renderFrame(t, docModel(), wizard.Bar{Back: true}).Find(".actionbar-summary").Length(); n != 0 {
		t.Errorf("without a Summary: %d .actionbar-summary, want none", n)
	}
}

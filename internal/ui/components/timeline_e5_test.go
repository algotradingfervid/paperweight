package components_test

import (
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// The status timeline (UI v2 §5.13 item 4, A-26) is an <ol> named Progress.
// Each step has its word; a done step a check and "done" for screen
// readers ("(done)"); the current, rejected and ended steps carry aria-current="step"
// and their own class; a later step neither.
func TestStatusTimeline_Marks(t *testing.T) {
	doc := uitest.Render(t, components.StatusTimeline([]components.LifeStep{
		{Label: "Draft", Mark: components.LifeDone},
		{Label: "Rejected", Mark: components.LifeRejected},
		{Label: "Approved", Mark: components.LifeTodo},
	}))
	ol := doc.Find("ol.lifecycle")
	if ol.Length() != 1 || ol.AttrOr("aria-label", "") != "Progress" {
		t.Fatalf("want one ol.lifecycle named Progress, got %d %q", ol.Length(), ol.AttrOr("aria-label", ""))
	}
	type row struct{ class, current, label, text string }
	var got []row
	ol.Children().Each(func(_ int, s *goquery.Selection) {
		got = append(got, row{s.AttrOr("class", ""), s.AttrOr("aria-current", "-"), squash(s.Find(".lifecycle__label").Text()), squash(s.Text())})
	})
	want := []row{
		{"lifecycle__step is-done", "-", "Draft (done)", "Draft (done)"},
		{"lifecycle__step is-rejected", "step", "Rejected", "Rejected"},
		{"lifecycle__step", "-", "Approved", "Approved"},
	}
	if len(got) != len(want) {
		t.Fatalf("steps = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("step %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	dots := ol.Find(".lifecycle__dot")
	if dots.Length() != 3 || dots.Eq(0).Find("svg").Length() != 1 || dots.Eq(1).Find("svg").Length() != 1 || dots.Eq(2).Find("svg").Length() != 0 {
		t.Error("want a glyph in the done and rejected dots and none in the later one")
	}
	dots.Each(func(i int, s *goquery.Selection) {
		if s.AttrOr("aria-hidden", "") != "true" {
			t.Errorf("dot %d is not aria-hidden", i)
		}
	})
}

// LifeCurrent and LifeEnded are the other two aria-current marks.
func TestStatusTimeline_CurrentAndEnded(t *testing.T) {
	for _, c := range []struct {
		mark  components.LifeMark
		class string
	}{{components.LifeCurrent, "lifecycle__step is-current"}, {components.LifeEnded, "lifecycle__step is-ended"}} {
		doc := uitest.Render(t, components.StatusTimeline([]components.LifeStep{{Label: "Closed", Mark: c.mark}}))
		li := doc.Find("li")
		if li.AttrOr("class", "") != c.class || li.AttrOr("aria-current", "") != "step" || squash(li.Text()) != "Closed" {
			t.Errorf("mark %d: li class=%q aria-current=%q text=%q, want %q step Closed", c.mark, li.AttrOr("class", ""), li.AttrOr("aria-current", ""), squash(li.Text()), c.class)
		}
		if li.Find(".lifecycle__dot svg").Length() != 0 {
			t.Errorf("mark %d: the dot has a glyph", c.mark)
		}
	}
}

package components_test

import (
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// UI v2 E5b (§5.13 item 2, N14, A-25): the success block is one
// role="status" region with the tick (hidden from screen readers), the
// action's text exactly, and one button per follow-up link; with no links it
// has no buttons.
func TestSuccessBlock(t *testing.T) {
	for _, c := range []struct {
		name  string
		links []components.Link
		want  []string
	}{
		{"one follow-up", []components.Link{{Label: "New sample order", Href: "/doc/x/new"}}, []string{"New sample order|/doc/x/new"}},
		{"no follow-ups", nil, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			doc := uitest.Render(t, components.SuccessBlock(components.SuccessProps{Message: "Submitted for approval to Ravi Kulkarni (Purchase Manager).", Links: c.links}))
			block := doc.Find(".success-block")
			if block.Length() != 1 || block.AttrOr("role", "") != "status" {
				t.Fatalf("want one .success-block with role=status, got %d %q", block.Length(), block.AttrOr("role", ""))
			}
			// It takes focus where it lands (R2).
			if _, auto := block.Attr("autofocus"); !auto || block.AttrOr("tabindex", "") != "-1" {
				t.Errorf("block autofocus %v, tabindex %q, want autofocus and -1", auto, block.AttrOr("tabindex", ""))
			}
			if got := squash(block.Find(".success-block__text").Text()); got != "Submitted for approval to Ravi Kulkarni (Purchase Manager)." {
				t.Errorf("text = %q", got)
			}
			tick := block.Find(".success-block__tick")
			if tick.Length() != 1 || tick.AttrOr("aria-hidden", "") != "true" || tick.Find("svg circle").Length() != 1 || tick.Find("svg path").Length() != 1 {
				t.Errorf("want one aria-hidden tick with a circle and a check path; got %d", tick.Length())
			}
			var got []string
			block.Find("a.btn").Each(func(_ int, s *goquery.Selection) {
				got = append(got, squash(s.Text())+"|"+s.AttrOr("href", ""))
			})
			if len(got) != len(c.want) || (len(got) > 0 && got[0] != c.want[0]) {
				t.Errorf("follow-ups = %q, want %q", got, c.want)
			}
		})
	}
}

// Step dates and notes (E5b, §5.13 item 4): a step with a Note shows it and a
// step with a Date shows it (dd-mm) under the word; a step with neither shows
// only its word.
func TestStatusTimeline_DateAndNote(t *testing.T) {
	doc := uitest.Render(t, components.StatusTimeline([]components.LifeStep{
		{Label: "Approved", Mark: components.LifeDone, Date: "05-11"},
		{Label: "Received", Mark: components.LifeCurrent, Note: "Part"},
		{Label: "Closed", Mark: components.LifeTodo},
	}))
	type row struct{ date, note, text string }
	var got []row
	doc.Find("li").Each(func(_ int, s *goquery.Selection) {
		got = append(got, row{squash(s.Find(".lifecycle__date").Text()), squash(s.Find(".lifecycle__note").Text()), squash(s.Text())})
	})
	want := []row{{"05-11", "", "Approved (done) 05-11"}, {"", "Part", "Received Part"}, {"", "", "Closed"}}
	if len(got) != len(want) {
		t.Fatalf("steps = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("step %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

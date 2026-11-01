package components_test

import (
	"os"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// UI v2 C4 (§5.14): the ErrorSummary heading counts the errors, the line under
// it says the other entries are kept, each error links to its field, and the
// box takes focus on render.

func TestErrorSummary_CountHeading(t *testing.T) {
	all := []components.FieldErr{
		{ID: "f-vendor", Message: "Choose a vendor."},
		{ID: "f-date", Message: "Order date can't be in the future."},
		{ID: "f-site", Message: "Choose a site."},
	}
	for _, tc := range []struct {
		errs []components.FieldErr
		want string
	}{
		{all[:1], "1 thing to fix before you continue"},
		{all[:2], "2 things to fix before you continue"},
		{all, "3 things to fix before you continue"},
	} {
		doc := uitest.Render(t, components.ErrorSummary(tc.errs))
		s := doc.Find(".error-summary")
		if s.Length() != 1 {
			t.Fatalf("%d errors: %d summaries", len(tc.errs), s.Length())
		}
		h := s.Find("h2#error-summary-title")
		if got := strings.Join(strings.Fields(h.Text()), " "); got != tc.want {
			t.Errorf("%d errors: heading = %q, want %q", len(tc.errs), got, tc.want)
		}
		if h.Find("svg").Length() != 1 {
			t.Errorf("%d errors: heading has no icon", len(tc.errs))
		}
		if got := s.AttrOr("aria-labelledby", ""); got != "error-summary-title" {
			t.Errorf("%d errors: aria-labelledby = %q", len(tc.errs), got)
		}
		if got := s.ChildrenFiltered("p").Text(); got != "Your other entries are kept." {
			t.Errorf("%d errors: line = %q", len(tc.errs), got)
		}
		if _, ok := s.Attr("autofocus"); !ok || s.AttrOr("tabindex", "") != "-1" {
			t.Errorf("%d errors: summary must take focus on render (autofocus, tabindex=-1)", len(tc.errs))
		}
		links := s.Find("li a")
		if links.Length() != len(tc.errs) {
			t.Fatalf("%d errors: %d links", len(tc.errs), links.Length())
		}
		for i, e := range tc.errs {
			a := links.Eq(i)
			if a.AttrOr("href", "") != "#"+e.ID || a.Text() != e.Message {
				t.Errorf("%d errors: link %d = %q %q", len(tc.errs), i, a.AttrOr("href", ""), a.Text())
			}
		}
	}
}

// The box is white with a 2 px danger border, a 6 px start edge and a
// text-coloured focus ring (§5.14, DL §3.5, A12).
func TestErrorSummary_CSS(t *testing.T) {
	css, err := os.ReadFile("../css/components/field.css")
	if err != nil {
		t.Fatal(err)
	}
	block := m21CSSBlock(string(css), "error-summary")
	for _, want := range []string{
		"background: var(--color-surface);",
		"border: var(--border-w-strong) solid var(--color-danger-600);",
		"border-inline-start-width: calc(var(--space-1) + var(--border-w-strong));",
		"&:focus, &:focus-visible { outline: var(--focus-ring-w) solid var(--color-text); outline-offset: var(--focus-ring-offset); }",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("error-summary block lacks %q", want)
		}
	}
	if strings.Contains(block, "--color-danger-50") {
		t.Error("error-summary must be white, not the danger tint")
	}
}

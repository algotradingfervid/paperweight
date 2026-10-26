package components_test

import (
	"context"
	"io"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// Enter in a field submits the form's default button: the first submit
// button in tree order whose form owner is the form. The bar sits after
// the form and posts it through form="…", so its primary must come before
// Back in tree order, or Enter goes back (design-system §8.3).
func TestActionBar_PrimaryFirstInTreeOrder(t *testing.T) {
	const formID = "wizard-form"
	page := templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := components.Form(components.FormProps{ID: formID, Action: "/doc/po/42/edit/items", HX: true, Target: "#wizard"}).Render(ctx, w); err != nil {
			return err
		}
		return components.ActionBar(components.ActionBarProps{
			Primary:     &components.ButtonProps{Label: "Save and continue", Variant: components.Primary},
			Secondary:   &components.ButtonProps{Label: "Back", Name: "nav", Value: "back", Variant: components.Secondary},
			FinishLater: true,
			Form:        formID,
		}).Render(ctx, w)
	})
	doc := uitest.Render(t, page)

	owned := doc.Find(`button[type="submit"]`).FilterFunction(func(_ int, s *goquery.Selection) bool {
		if f, ok := s.Attr("form"); ok {
			return f == formID
		}
		return s.Closest("form#"+formID).Length() == 1
	})
	labels := owned.Map(func(_ int, s *goquery.Selection) string { return text(s) })
	for _, want := range []string{"Save and continue", "Back", "Save and finish later"} {
		found := false
		for _, l := range labels {
			found = found || l == want
		}
		if !found {
			t.Errorf("submit buttons of #%s = %q, want %q among them", formID, labels, want)
		}
	}
	if owned.Length() == 0 {
		t.Fatal("no submit button belongs to the form")
	}
	first := owned.First()
	if got := text(first); got != "Save and continue" {
		t.Errorf("first submit of #%s in tree order = %q (all %q), want Save and continue", formID, got, labels)
	}
	if n, _ := first.Attr("name"); n == "nav" {
		t.Errorf("the default button posts nav=%s", first.AttrOr("value", ""))
	}
}

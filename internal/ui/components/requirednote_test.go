package components_test

import (
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// A form with a required field starts with "Fields marked * are required.",
// its asterisk the same shape the labels carry and hidden from screen readers,
// which hear each label's " (required)" instead (UI v2 §5.5 item 2). A form
// with none shows nothing.
func TestRequiredNote_OnlyWithARequiredField(t *testing.T) {
	doc := uitest.Render(t, components.RequiredNote(true))
	p := doc.Find("p")
	if p.Length() != 1 {
		t.Fatalf("want one paragraph, got %d", p.Length())
	}
	if got := strings.Join(strings.Fields(p.Text()), " "); got != "Fields marked * are required." {
		t.Errorf("text = %q, want Fields marked * are required.", got)
	}
	star := p.Find("span.field__req")
	if star.Length() != 1 || star.Text() != "*" {
		t.Errorf("want one span.field__req holding *, got %d %q", star.Length(), star.Text())
	}
	if got, _ := star.Attr("aria-hidden"); got != "true" {
		t.Errorf(`asterisk aria-hidden = %q, want "true"`, got)
	}

	if html, _ := uitest.Render(t, components.RequiredNote(false)).Find("body").Html(); strings.TrimSpace(html) != "" {
		t.Errorf("RequiredNote(false) rendered %q, want nothing", html)
	}
}

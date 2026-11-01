package components_test

import (
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// The eye toggle sits inside the password box (UI v2 §5.3): the control wraps
// the input, the toggle button (named by its text, which password.js flips
// between Show and Hide password) and the two hidden eye glyphs CSS swaps.
func TestPasswordInput_EyeInsideBox(t *testing.T) {
	doc := uitest.Render(t, components.PasswordInput(components.FieldProps{ID: "f-pw", Name: "pw", Label: "Password"}, "current-password"))
	box := doc.Find(".field__control.password")
	if box.Length() != 1 {
		t.Fatalf("%d .field__control.password, want 1", box.Length())
	}
	if box.Find("input#f-pw[type=password]").Length() != 1 {
		t.Error("password input is not inside the box")
	}
	btn := box.Find("button.password__toggle[type=button][data-toggle-password]")
	if btn.Length() != 1 {
		t.Fatalf("%d toggles in the box, want 1", btn.Length())
	}
	if got := strings.TrimSpace(btn.Text()); got != "Show password" {
		t.Errorf("toggle name = %q, want Show password", got)
	}
	if c, _ := btn.Attr("aria-controls"); c != "f-pw" {
		t.Errorf("aria-controls = %q", c)
	}
	if btn.Find("svg").Length() != 0 {
		t.Error("toggle holds an icon; password.js replaces its text, so the glyphs sit beside it")
	}
	eye := box.Find(".password__eye")
	if h, _ := eye.Attr("aria-hidden"); h != "true" {
		t.Errorf("eye aria-hidden = %q, want true", h)
	}
	if eye.Find(".password__show svg").Length() != 1 || eye.Find(".password__hide svg").Length() != 1 {
		t.Error("want one show glyph and one hide glyph")
	}
}

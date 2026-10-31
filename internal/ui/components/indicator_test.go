package components_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// A picker search is its own htmx indicator, so typing never shows the
// page's loading bar (UI v2 §5.14, A-27).
func TestIndicator_PickerSearchUsesItsOwn(t *testing.T) {
	picker := uitest.Render(t, components.Picker(pickerProps())).Find("input[hx-get]")
	if got, _ := picker.Attr("hx-indicator"); got != "this" {
		t.Errorf("picker search hx-indicator = %q, want this", got)
	}
}

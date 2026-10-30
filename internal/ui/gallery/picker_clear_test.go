package gallery

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// A Link's Required decides Clear on every screen, because FieldInput
// passes it to the Picker as Optional.
func TestFieldInput_ClearFollowsRequired(t *testing.T) {
	f := doctype.Field{Name: "customer_id", Label: "Customer", Type: doctype.Link, Link: "customer", Required: true}
	doc := uitest.Render(t, docscreen.FieldInput(f, "f-customer_id", "3", "", docscreen.Deps{}))
	if doc.Find("[data-picker-chosen]").Length() != 1 {
		t.Fatal("no chosen summary rendered")
	}
	if doc.Find("[data-picker-clear]").Length() != 0 {
		t.Error("required Link: Clear rendered")
	}
	f.Required = false
	doc = uitest.Render(t, docscreen.FieldInput(f, "f-customer_id", "3", "", docscreen.Deps{}))
	if got, _ := doc.Find("[data-picker-clear]").Attr("aria-label"); got != "Clear Customer" {
		t.Errorf("optional Link: Clear aria-label = %q, want Clear Customer", got)
	}
}

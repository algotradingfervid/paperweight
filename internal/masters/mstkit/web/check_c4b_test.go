package mstweb_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/ui/icons"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// UI v2 C4b (§5.6): every Check and save section has the file-text icon tile
// and a Change ghost button.
func TestWizard_CheckCardIconTile(t *testing.T) {
	h := wizardHarness(t, nil)
	b := h.Browser(h.Env.Kavya)
	toWizardCheck(t, b)
	doc := b.Get(t, wizardCheck).Doc
	want, _ := uitest.Render(t, icons.Icon("file-text")).Find("svg").Html()
	cards := doc.Find("section.card:has(.card-head > a.btn-ghost)")
	if cards.Length() == 0 {
		t.Fatal("Check and save has no section with a Change ghost button")
	}
	for i := range cards.Length() {
		got, _ := cards.Eq(i).Find(".card-head .card-icon svg").Html()
		if got != want {
			t.Errorf("section %d tile = %q, want the file-text icon", i, got)
		}
	}
}

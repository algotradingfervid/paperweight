package mstweb_test

import (
	"testing"

	mstweb "github.com/vardhan/internaltools/internal/masters/mstkit/web"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// UI v2 C4c: a wizard step's Icon is its Check and save tile; a step without
// one keeps file-text.
func TestWizard_StepIcon(t *testing.T) {
	h := wizardHarness(t, nil, func(s *screen) { s.Wizard.(mstweb.CreateWizard[stubDraft]).Steps[1].Icon = "truck" })
	b := h.Browser(h.Env.Kavya)
	toWizardCheck(t, b)
	uitest.WantCheckTiles(t, b.Get(t, wizardCheck).Doc, uitest.CheckTile{Title: "Details", Icon: "file-text"}, uitest.CheckTile{Title: "More", Icon: "truck"})
}

// A step naming an unknown icon panics when the routes are built, on the
// wizard's steps and on Sections.
func TestWizard_UnknownStepIconPanics(t *testing.T) {
	for name, set := range map[string]func(*screen){
		"wizard step": func(s *screen) { s.Wizard.(mstweb.CreateWizard[stubDraft]).Steps[1].Icon = "no-such-icon" },
		"section": func(s *screen) {
			s.Sections = []docscreen.FormStep{{Slug: "more", Label: "More", Icon: "no-such-icon"}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if got, ok := recover().(string); !ok || got != "mstweb: Item: step more names no icon no-such-icon" {
					t.Errorf("panic = %v", got)
				}
			}()
			wizardHarness(t, nil, set)
		})
	}
}

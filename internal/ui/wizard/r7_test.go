package wizard_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/ui/wizard"
)

// UI v2 §5.6 (shell M2): a master wizard's primary reads Continue, as
// nothing is saved until the last step; a document's reads Save and
// continue; a Bar's own Primary wins in both.
func TestFrame_PrimaryByMode(t *testing.T) {
	master := docModel()
	master.Mode = wizard.Master
	for _, c := range []struct {
		name string
		m    wizard.Model
		bar  wizard.Bar
		want string
	}{
		{"document", docModel(), wizard.Bar{}, "Save and continue"},
		{"master", master, wizard.Bar{}, "Continue"},
		{"master, own label", master, wizard.Bar{Primary: "Save"}, "Save"},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := renderFrame(t, c.m, c.bar).Find(".actionbar button.btn-primary")
			if p.Length() != 1 || norm(p.Text()) != c.want {
				t.Errorf("primary: %d, %q; want %q", p.Length(), norm(p.Text()), c.want)
			}
		})
	}
}

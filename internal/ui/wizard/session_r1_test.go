package wizard_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/ui/wizard"
)

// CD-387 (5): app.js may re-send what carries data-no-write. A master wizard's
// steps write only the session; its last step's primary creates the record, so
// there only Back is marked. A document's steps go by their version.
func TestFrame_NoWriteMark(t *testing.T) {
	master := docModel()
	master.Mode = wizard.Master
	master.Steps = steps("Details", "Addresses", "Check")
	master.Furthest = 2
	last := master
	last.Current = 2
	first := master
	first.Current = 0
	end := docModel()
	end.Current, end.Furthest = 3, 3
	for _, c := range []struct {
		name       string
		m          wizard.Model
		form, back bool
	}{
		{"first master step", first, true, true},
		{"a middle master step", master, true, true},
		{"last master step", last, false, true},
		{"a document step", docModel(), false, false},
		{"a document's last step", end, false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := renderFrame(t, c.m, wizard.Bar{Back: true})
			if _, ok := d.Find("form#wizard-form").Attr("data-no-write"); ok != c.form {
				t.Errorf("form marked = %v, want %v", ok, c.form)
			}
			if _, ok := d.Find(`button[value="back"]`).Attr("data-no-write"); ok != c.back {
				t.Errorf("Back marked = %v", ok)
			}
		})
	}
}

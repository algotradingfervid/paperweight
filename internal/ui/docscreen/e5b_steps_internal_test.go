package docscreen

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// An extra step marked current is refused unless the approved step is the
// current one, so a closed, cancelled or pending document never shows two
// current steps (A-26); on an approved document it takes over.
func TestPageSteps_CurrentOnlyAtApproved(t *testing.T) {
	dt := &doctype.DocType{Name: "e5b.T", Submittable: true, Approvable: true}
	life := map[string]components.LifeStep{"Sent": {Mark: components.LifeCurrent}}
	for _, c := range []struct {
		s    doctype.State
		ok   bool
		want []string
	}{
		{doctype.StateApproved, true, []string{"Draft:d", "Pending approval:d", "Approved:d", "Sent:c"}},
		{doctype.StateClosed, false, nil},
		{doctype.StateCancelled, false, nil},
		{doctype.StatePending, false, nil},
	} {
		t.Run(string(c.s), func(t *testing.T) {
			steps, err := pageSteps(dt, DocView{Info: doctype.Info{State: c.s}}, []string{"Sent"}, life)
			if (err == nil) != c.ok {
				t.Fatalf("err = %v, want ok %v", err, c.ok)
			}
			if got := e5Steps(steps); c.ok && len(got) != len(c.want) {
				t.Fatalf("steps = %q, want %q", got, c.want)
			} else if c.ok {
				for i := range got {
					if got[i] != c.want[i] {
						t.Errorf("steps = %q, want %q", got, c.want)
					}
				}
			}
		})
	}
}

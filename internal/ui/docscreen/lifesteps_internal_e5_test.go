package docscreen

import (
	"fmt"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// e5Steps writes steps as "Label:mark", with the mark as d (done), c
// (current), r (rejected), e (ended) or - (later).
func e5Steps(steps []components.LifeStep) []string {
	marks := map[components.LifeMark]string{components.LifeTodo: "-", components.LifeDone: "d", components.LifeCurrent: "c", components.LifeRejected: "r", components.LifeEnded: "e"}
	out := make([]string, len(steps))
	for i, s := range steps {
		out[i] = s.Label + ":" + marks[s.Mark]
	}
	return out
}

// UI v2 §5.13 item 4, A-26: the steps come from the DocType's lifecycle,
// Draft, Pending approval when it is Approvable, then its approved word.
// Exactly one step is the state: Rejected marks the approval step with the
// word Rejected; Closed and Cancelled add a final step. Labels follow the
// state badge (StateLabels, else the badge's word). A DocType that isn't
// submittable has none.
func TestLifeSteps_PerState(t *testing.T) {
	approvable := &doctype.DocType{Submittable: true, Approvable: true}
	plain := &doctype.DocType{Submittable: true, StateLabels: map[doctype.State]string{doctype.StateApproved: "Submitted"}}
	for _, c := range []struct {
		dt   *doctype.DocType
		s    doctype.State
		want []string
	}{
		{approvable, doctype.StateDraft, []string{"Draft:c", "Pending approval:-", "Approved:-"}},
		{approvable, doctype.StatePending, []string{"Draft:d", "Pending approval:c", "Approved:-"}},
		{approvable, doctype.StateApproved, []string{"Draft:d", "Pending approval:d", "Approved:c"}},
		{approvable, doctype.StateRejected, []string{"Draft:d", "Rejected:r", "Approved:-"}},
		{approvable, doctype.StateClosed, []string{"Draft:d", "Pending approval:d", "Approved:d", "Closed:e"}},
		{approvable, doctype.StateCancelled, []string{"Draft:d", "Pending approval:d", "Approved:d", "Cancelled:e"}},
		{plain, doctype.StateDraft, []string{"Draft:c", "Submitted:-"}},
		{plain, doctype.StateApproved, []string{"Draft:d", "Submitted:c"}},
		{plain, doctype.StateClosed, []string{"Draft:d", "Submitted:d", "Closed:e"}},
		{&doctype.DocType{}, doctype.StateDraft, []string{}},
	} {
		t.Run(fmt.Sprintf("%v/%s", c.dt.Approvable, c.s), func(t *testing.T) {
			if got := e5Steps(lifeSteps(c.dt, c.s)); !slices.Equal(got, c.want) {
				t.Errorf("lifeSteps = %q, want %q", got, c.want)
			}
		})
	}
}

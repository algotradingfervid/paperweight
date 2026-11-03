package docscreen

import (
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// R9 (GRN M7, EXP): the timeline shows no step that didn't happen. A document
// APR passed on submit, with no rule, never waited: its approval step is
// gone, its date too. A draft keeps the step, whatever Needed says for the
// viewer: APR's preview depends on who asks (R9 review). A step reached through a real approval stays, also after a later cycle was
// rejected; "Approved on submit" in an older cycle doesn't hide a newer real
// approval. An extra step not reached by a closed or cancelled document can
// never be reached, so it is gone; on a live or rejected document it is a
// later step.
func TestPageSteps_NoStepThatDidNotHappen(t *testing.T) {
	dt := &doctype.DocType{Name: "r9.T", Submittable: true, Approvable: true}
	auto := []HistoryRow{{Action: "Approved on submit (no approval needed)", At: "05-11-2026"}, {Action: "Submitted", At: "05-11-2026"}}
	real := []HistoryRow{{Action: "Approved", At: "06-11-2026"}, {Action: "Submitted", At: "05-11-2026"}}
	later := append(slices.Clone(real), auto...) // newest first: a real approval after an automatic one
	audit := []HistoryRow{{Action: "Submitted", At: "05-11-2026"}}
	for _, c := range []struct {
		name  string
		s     doctype.State
		needs bool
		hist  []HistoryRow
		life  map[string]components.LifeStep
		want  []string
	}{
		{"approved, no rule", doctype.StateApproved, false, auto, nil, []string{"Draft:-", "Approved:c:05-11", "Sent:-"}},
		{"closed, no rule", doctype.StateClosed, false, auto, nil, []string{"Draft:-", "Approved:d:05-11", "Closed:e"}},
		{"cancelled, no rule", doctype.StateCancelled, false, auto, nil, []string{"Draft:-", "Approved:d:05-11", "Cancelled:e"}},
		{"approved by a person", doctype.StateApproved, false, real, nil, []string{"Draft:-", "Pending approval:d:05-11", "Approved:c:06-11", "Sent:-"}},
		{"approved by a person after an automatic cycle", doctype.StateApproved, false, later, nil, []string{"Draft:-", "Pending approval:d:05-11", "Approved:c:06-11", "Sent:-"}},
		{"pending", doctype.StatePending, true, audit, nil, []string{"Draft:-", "Pending approval:c:05-11", "Approved:-", "Sent:-"}},
		{"draft that needs approval", doctype.StateDraft, true, nil, nil, []string{"Draft:c", "Pending approval:-", "Approved:-", "Sent:-"}},
		{"draft that needs none for this viewer", doctype.StateDraft, false, nil, nil, []string{"Draft:c", "Pending approval:-", "Approved:-", "Sent:-"}},
		{"rejected", doctype.StateRejected, false, nil, nil, []string{"Draft:-", "Rejected:r", "Approved:-", "Sent:-"}},
		{"closed, extra reached", doctype.StateClosed, false, real, map[string]components.LifeStep{"Sent": {Mark: components.LifeDone}}, []string{"Draft:-", "Pending approval:d:05-11", "Approved:d:06-11", "Sent:-", "Closed:e"}},
		{"closed, extra never reached", doctype.StateClosed, false, real, nil, []string{"Draft:-", "Pending approval:d:05-11", "Approved:d:06-11", "Closed:e"}},
		{"cancelled, extra never reached", doctype.StateCancelled, false, real, nil, []string{"Draft:-", "Pending approval:d:05-11", "Approved:d:06-11", "Cancelled:e"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			v := DocView{Info: doctype.Info{State: c.s}, Approval: ApprovalView{Needed: c.needs, History: c.hist}}
			steps, err := pageSteps(dt, v, []string{"Sent"}, c.life)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, s := range steps {
				row := e5Steps([]components.LifeStep{s})[0]
				if s.Mark == components.LifeDone && s.Date == "" {
					row = s.Label + ":-" // Draft's and an extra's date aren't this test's
				}
				if s.Date != "" {
					row += ":" + s.Date
				}
				got = append(got, row)
			}
			if !slices.Equal(got, c.want) {
				t.Errorf("steps = %q, want %q", got, c.want)
			}
			current := 0
			for _, s := range steps {
				if s.Mark == components.LifeCurrent || s.Mark == components.LifeRejected || s.Mark == components.LifeEnded {
					current++
				}
			}
			if current != 1 {
				t.Errorf("%d steps are the state, want exactly one (A-26)", current)
			}
		})
	}
}

// R9 (§5.13 item 2): the success block's heading and line. A module's own
// action gives the heading, with its line after the flash's text; a document
// sent for approval says who it went to, from the text's own name; an
// approved one takes the DocType's word, also a label such as Submitted.
func TestBlockText(t *testing.T) {
	approved := &doctype.DocType{}
	labelled := &doctype.DocType{StateLabels: map[doctype.State]string{doctype.StateApproved: "Submitted"}}
	for _, c := range []struct {
		name        string
		pv          pageView
		title, line string
	}{
		{"module", pageView{DT: approved, Success: "PO/1 marked as sent.", Extra: PageExtra{Success: &components.SuccessProps{Title: "Sent to the vendor", Message: "It went to Shree."}}}, "Sent to the vendor", "PO/1 marked as sent. It went to Shree."},
		{"module, nothing to add", pageView{DT: approved, Success: "PO/1 marked as sent.", Extra: PageExtra{Success: &components.SuccessProps{Title: "Sent to the vendor"}}}, "Sent to the vendor", "PO/1 marked as sent."},
		{"sent for approval", pageView{DT: approved, Success: "Submitted for approval to Ravi (Manager)."}, "Submitted for approval", "It went to Ravi (Manager). You'll see it in My requests until it's decided."},
		{"approved", pageView{DT: approved, Success: "Approved. PO/1 has been numbered."}, "Approved", "PO/1 has been numbered."},
		{"approved, labelled", pageView{DT: labelled, Success: "Submitted. PR/1 has been numbered."}, "Submitted", "PR/1 has been numbered."},
	} {
		if title, line := blockText(c.pv); title != c.title || line != c.line {
			t.Errorf("%s: %q, %q, want %q, %q", c.name, title, line, c.title, c.line)
		}
	}
}

package doctype

import "testing"

// DOC-110
func TestDoc_StateLabels_PerDocType(t *testing.T) {
	global := map[State]string{
		StateDraft:     "Draft",
		StatePending:   "Waiting for approval",
		StateApproved:  "Approved",
		StateClosed:    "Closed",
		StateRejected:  "Rejected",
		StateCancelled: "Cancelled",
	}
	a := DocType{Name: "test.A", StateLabels: map[State]string{StateApproved: "Submitted", StateClosed: ""}}
	b := DocType{Name: "test.B"}

	if got := StateLabel(a, StateApproved); got != "Submitted" {
		t.Errorf("StateLabel(A, approved) = %q, want %q", got, "Submitted")
	}
	if got := StateLabel(b, StateApproved); got != "Approved" {
		t.Errorf("StateLabel(B, approved) = %q, want %q", got, "Approved")
	}
	for s, want := range global {
		if s != StateApproved {
			if got := StateLabel(a, s); got != want {
				t.Errorf("StateLabel(A, %q) = %q, want the global label %q (missing or blank in A's map)", s, got, want)
			}
		}
		if got := StateLabel(b, s); got != want {
			t.Errorf("StateLabel(B, %q) = %q, want the global label %q", s, got, want)
		}
	}
}

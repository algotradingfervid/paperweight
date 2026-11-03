package wizard_test

import (
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/ui/uitest"
	"github.com/vardhan/internaltools/internal/ui/wizard"
)

// RR1a 3: an earlier step whose Status is Attention isn't done: it is a
// numbered link marked is-attention with "Needs attention: " for screen
// readers and no check; a Complete or unmarked earlier step stays done.
func TestStepper_AttentionIsNotDone(t *testing.T) {
	m := docModel()
	m.Current = 2
	m.Steps[0].Status = wizard.Attention
	m.Steps[1].Status = wizard.Complete
	li := uitest.Render(t, wizard.Stepper(m)).Find("ol.stepper__list > li")
	s1 := li.Eq(0)
	if s1.HasClass("is-done") || !s1.HasClass("is-attention") || s1.Find("svg").Length() != 0 {
		t.Errorf("step 1 (Attention) class %q with %d icons, want is-attention, not is-done, no check", s1.AttrOr("class", ""), s1.Find("svg").Length())
	}
	if a := s1.Find("a"); a.AttrOr("href", "") != "/doc/po/42/edit/party" || strings.Join(strings.Fields(a.Text()), " ") != "Needs attention: Party" || a.Find("span.visually-hidden").Text() != "Needs attention: " {
		t.Errorf("step 1 link %q to %q, want Needs attention: Party (prefix visually hidden) to its step", a.Text(), a.AttrOr("href", ""))
	}
	if s2 := li.Eq(1); !s2.HasClass("is-done") || strings.Join(strings.Fields(s2.Text()), " ") != "Completed: Lines" {
		t.Errorf("step 2 (Complete) class %q text %q, want done", s2.AttrOr("class", ""), s2.Text())
	}
}

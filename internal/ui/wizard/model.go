// Package wizard is the wizard framework (design-system §8.3): the step
// model, the stepper, the step frame, the check page sections and the HTTP
// response helper. Document wizards save the draft at every step; master
// create wizards hold step values in the session until "Check and save"
// (CD-50).
package wizard

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/ui/components"
)

// StepStatus is a step's state on the check page.
type StepStatus string

// Step statuses; the zero value shows no status.
const (
	Complete  StepStatus = "complete"
	Attention StepStatus = "attention"
)

// Step is one wizard step: its URL slug, label and status.
type Step struct {
	Slug, Label string
	Status      StepStatus
}

// Mode says where step values live (CD-50).
type Mode int

// Wizard modes.
const (
	// Document saves the draft on every step POST.
	Document Mode = iota
	// Master keeps step values in the session and saves once at the end.
	Master
)

// Model is everything the stepper and frame show. Current is the step on
// screen and Furthest the furthest step reached, both 0-based. Action is
// the step form's action ("" = StepURL(Current)), Target its hx-target
// ("" = "#wizard"; a form whose response leaves the wizard names #main),
// and After renders after
// the step form, inside #wizard, for content holding forms of its own
// (forms can't nest). Below renders after the action bar, last in #wizard and
// outside the step form, so Enter in a field can't press it. LastLabel replaces the mode's required last step label
// for a wizard that ends in something else (the master import ends in
// "Import", design-system §8.7).
type Model struct {
	Mode                               Mode
	Title, DocLabel, BaseURL, ReturnTo string
	LastLabel                          string
	Steps                              []Step
	Current, Furthest                  int
	Version                            int
	Errors                             []components.FieldErr
	Warnings                           []string
	Stored                             []string // a draft's stored problems: shown, but no failed step
	Conflict, ReloadURL                string
	Action, Target                     string
	After, Below                       templ.Component
}

var slugRe = regexp.MustCompile(`^[a-z0-9-]+$`)

// lastLabel is the required label of a mode's last step.
func (m Model) lastLabel() string {
	if m.LastLabel != "" {
		return m.LastLabel
	}
	if m.Mode == Master {
		return "Check and save"
	}
	return "Check and submit"
}

// Validate checks the wizard's shape (design-system §8.3).
func (m Model) Validate() error {
	n := len(m.Steps)
	if n < 3 || n > 5 {
		return errors.New("A wizard needs 3 to 5 steps.")
	}
	if want := m.lastLabel(); m.Steps[n-1].Label != want {
		return fmt.Errorf("wizard: the last step must be %q, got %q", want, m.Steps[n-1].Label)
	}
	seen := map[string]bool{}
	for _, s := range m.Steps {
		if !slugRe.MatchString(s.Slug) {
			return fmt.Errorf("wizard: step slug %q must match %s", s.Slug, slugRe)
		}
		if seen[s.Slug] {
			return fmt.Errorf("wizard: step slug %q is used twice", s.Slug)
		}
		seen[s.Slug] = true
	}
	if m.Current < 0 || m.Current > m.Furthest || m.Furthest >= n {
		return fmt.Errorf("wizard: need 0 ≤ Current (%d) ≤ Furthest (%d) < %d", m.Current, m.Furthest, n)
	}
	return nil
}

// StepURL is step i's URL.
func (m Model) StepURL(i int) string { return m.BaseURL + "/" + m.Steps[i].Slug }

// formAction is the step form's action.
func (m Model) formAction() string {
	if m.Action != "" {
		return m.Action
	}
	return m.StepURL(m.Current)
}

// formTarget is the step form's hx-target.
func (m Model) formTarget() string {
	if m.Target != "" {
		return m.Target
	}
	return "#wizard"
}

// Last reports whether the current step is the check step.
func (m Model) Last() bool { return m.Current == len(m.Steps)-1 }

// Heading is the current step's label, used as the page heading.
func (m Model) Heading() string { return m.Steps[m.Current].Label }

package docscreen_test

import (
	"reflect"
	"testing"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/wizard"
)

// M0-15 (a): the types CD-263 adds (hooks spec §7 item 5), with no
// behaviour yet. A type is pinned exactly where CD-263 or the purchase UX
// design (§6, G2 to G5 and G7) states it; otherwise only the name is
// pinned, and the task that builds the behaviour chooses the type.
// Step.Top (M0-17), approval.Status (M0-19) and the UX-E types (M0-20)
// are not pinned here.

// G2: CheckExtra.Cards []templ.Component.
var m15CheckCards []templ.Component = docscreen.CheckExtra{}.Cards

// G3 to G5: PageExtra.Notice, More and Primary.
var (
	m15PageNotice  *components.BannerProps = docscreen.PageExtra{}.Notice
	m15PageMore    []components.MoreAction = docscreen.PageExtra{}.More
	m15PagePrimary *components.ButtonProps = docscreen.PageExtra{}.Primary
)

// G7: StateBadgeLabel(s, label) and Row.StateLabel take dt.StateLabels[s]
// or an empty string.
var (
	m15BadgeLabel func(doctype.State, string) templ.Component = components.StateBadgeLabel
	m15RowLabel   string                                      = components.Row{}.StateLabel
)

// G6: StepBar has Primary, Stay and Back; wizard.Bar has PrimaryNav and
// BackLink (names only).
var (
	m15StepBarPrimary = docscreen.StepBar{}.Primary
	m15StepBarStay    = docscreen.StepBar{}.Stay
	m15StepBarBack    = docscreen.StepBar{}.Back
	m15WizPrimaryNav  = wizard.Bar{}.PrimaryNav
	m15WizBackLink    = wizard.Bar{}.BackLink
)

// G9(a): ApprovalView.Roles and Why (names only; CD-266 leaves the fields'
// shape to AD-10).
var (
	m15ApprovalRoles = docscreen.ApprovalView{}.Roles
	m15ApprovalWhy   = docscreen.ApprovalView{}.Why
)

// TestM15_GapTypes pins the CD-263 additions: the vars above compile only
// when every name exists with the stated type, and Step.Bar is a hook that
// takes the Ctx first, as every hook does (hooks spec §3), and returns a
// StepBar (CD-263 G6).
func TestM15_GapTypes(t *testing.T) {
	_ = []any{
		m15CheckCards, m15PageNotice, m15PageMore, m15PagePrimary, m15BadgeLabel, m15RowLabel,
		m15StepBarPrimary, m15StepBarStay, m15StepBarBack, m15WizPrimaryNav, m15WizBackLink,
		m15ApprovalRoles, m15ApprovalWhy,
	}
	bar := reflect.TypeOf(docscreen.Step{}.Bar)
	if bar.Kind() != reflect.Func {
		t.Fatalf("Step.Bar is a %s, want a func (CD-263 G6)", bar)
	}
	if bar.NumIn() == 0 || bar.In(0) != reflect.TypeFor[docscreen.Ctx]() {
		t.Errorf("Step.Bar = %s, want docscreen.Ctx as its first parameter (hooks spec §3)", bar)
	}
	if bar.NumOut() == 0 || bar.Out(0) != reflect.TypeFor[docscreen.StepBar]() {
		t.Errorf("Step.Bar = %s, want it to return a docscreen.StepBar first (CD-263 G6)", bar)
	}
}

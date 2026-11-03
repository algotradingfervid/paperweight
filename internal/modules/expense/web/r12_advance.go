package expenseweb

import (
	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// The header facts (UI v2 §5.13 item 3); docscreen leaves out what Module.Hide
// hides, and CD-321 blanks the Advance for a viewer who can't read it
// (CD-392 (9)).
var (
	claimKeyFacts   = []string{"employee_id", "claim_date", "purpose", "advance_id"}
	advanceKeyFacts = []string{"employee_id", "advance_date", "purpose"}
)

// Advance Details copy (UI v2 §5.5, §5.6), and the claim's card beside it.
const (
	introAdvance    = "What the advance is for and the day you're asking for it."
	hintAdvanceDate = "The day you're asking for the money."
	advanceCard     = "About this advance"
	advanceCardDesc = "The purpose and date print on the advance."
	claimCardTitle  = "About this claim"
	claimCardDesc   = "The purpose and date print on the claim."
)

// advanceSteps is the advance's Details body; docscreen's own post saves it.
func advanceSteps() map[string]docscreen.Step {
	return map[string]docscreen.Step{"details": {Body: advanceDetailsBody}}
}

// advanceDetailsBody draws the header's editable fields in one card under the
// required line, from the post on a redraw, the stored draft, or the new
// document's defaults. A field the module hides is drawn as nothing.
func advanceDetailsBody(c docscreen.Ctx, s docscreen.StepState) (templ.Component, error) {
	val := func(name string) string {
		switch {
		case s.Posted:
			return c.Req.PostFormValue(name)
		case c.View != nil:
			return c.View.Inputs[name]
		}
		return s.Defaults[name]
	}
	var parts []templ.Component
	for _, x := range []struct{ name, hint string }{{"employee_id", ""}, {"advance_date", hintAdvanceDate}, {"purpose", hintPurpose}} {
		if hides(c, x.name) {
			continue
		}
		f, _ := c.DT.Field(x.name)
		f.Hint = x.hint
		parts = append(parts, docscreen.FieldInput(f, components.FieldID(x.name), val(x.name), fieldError(s.Errors, x.name), c.Deps))
	}
	return templ.Join(components.RequiredNote(true),
		card(components.CardProps{Title: advanceCard, Icon: "hand-coins", Description: advanceCardDesc}, parts)), nil
}

// fieldError is the message of name's first error (Details holds no line
// field).
func fieldError(errs []doctype.FieldError, name string) string {
	for _, e := range errs {
		if e.Field == name {
			return e.Message
		}
	}
	return ""
}

// advanceLine is docscreen's generic line form through Module.Line, which
// gives the page the sticky Save line the claim's has (UI v2 §5.6).
// advanceLine, advanceLineBody, advanceLineFields and advanceLineSave are
// deleted once R7 gives every line page its footer (CD-387 (6)).
func advanceLine() *docscreen.Line {
	return &docscreen.Line{Body: advanceLineBody, Save: advanceLineSave}
}

// advanceLineFields are the line fields the viewer may edit.
func advanceLineFields(c docscreen.Ctx) []doctype.Field {
	var out []doctype.Field
	for _, f := range c.DT.Fields {
		if f.OnLine && !f.ReadOnly && !hides(c, f.Name) {
			out = append(out, f)
		}
	}
	return out
}

// advanceLineBody is every editable line field, the More ones grouped as the
// generic line page groups them (UI v2 §5.5 item 3).
func advanceLineBody(c docscreen.Ctx, no int, s docscreen.StepState) (templ.Component, error) {
	l := newLineState(c, no, s)
	var shown []docscreen.MoreInput
	for _, f := range advanceLineFields(c) {
		v, msg := l.value(f.Name), l.errorOf(f.Name)
		shown = append(shown, docscreen.MoreInput{
			Field: f, Value: v, Error: msg,
			Input: docscreen.FieldInput(f, components.FieldID(f.Name), v, msg, c.Deps),
		})
	}
	return templ.Join(docscreen.GroupMore(shown).Inputs()...), nil
}

// advanceLineSave reads the posted line as the generic page does: each
// field's plain value, or NUM's text as a field error.
func advanceLineSave(c docscreen.Ctx, _ int) (docscreen.Save, error) {
	out := docscreen.Save{Values: map[string]string{}}
	for _, f := range advanceLineFields(c) {
		v, fe := docscreen.ToPlain(f, c.Req.PostFormValue(f.Name))
		if fe != nil {
			out.Errors = append(out.Errors, *fe)
			continue
		}
		out.Values[f.Name] = v
	}
	return out, nil
}

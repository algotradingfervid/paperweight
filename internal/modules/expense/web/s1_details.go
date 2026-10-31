package expenseweb

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// Claim details (UX §5.3): a custom body so the advance can be radios
// (spec §3.1: a picker can't filter) and an Administrator types an employee
// code. Texts marked PROPOSED in the UX doc were accepted as written (CD-270
// Q8).
const (
	hintCode     = "Leave blank for your own claim. Type a code to enter a claim for someone else, including someone who has left."
	hintPurpose  = `For example "Pune Metro site visit".`
	hintLess     = "Left blank, the smaller of what's left and the claim total is used."
	hintRemarks  = "For your manager. Not printed."
	hintPaidOnly = "Only advances marked as paid are listed. Ask the Director if yours is missing."
	hintClosed   = "This advance is closed. Choose another, or No advance."
	hintCanceled = "This advance is cancelled. Choose another, or No advance."
	msgNoCode    = "No employee has the code %s."
	labelCode    = "Employee code"
	labelNoAdv   = "No advance"
	summaryAdv   = "Set against an advance"
)

// detailsFields are the fields the step always posts, in DOC's plain form;
// advanceFields only when the radios were drawn (advanceDrawn).
var (
	detailsFields = []string{"purpose", "claim_date", "remarks"}
	advanceFields = []string{"advance_id", "less_advance"}
)

// advanceDrawn is the hidden input the radios carry: a post without it
// leaves the stored advance and Less advance as they are, since the viewer
// saw neither (an advance they can't read, or only unpaid ones).
const advanceDrawn = "advance_drawn"

// detailsView is the step body.
type detailsView struct {
	code                   *components.FieldProps // nil unless an Administrator
	purpose, date, remarks templ.Component
	advance                bool // the disclosure is shown
	open                   bool
	radios                 *components.FieldProps // nil: the hint alone (only unpaid advances)
	options                []components.Option
	less                   templ.Component
}

// detailsBody draws the step from the stored draft, or from the post on a
// 422 or 409, so nothing typed is lost.
func (d Deps) detailsBody(c docscreen.Ctx, s docscreen.StepState) (templ.Component, error) {
	val := func(name string) string {
		switch {
		case s.Posted:
			return c.Req.PostFormValue(name)
		case c.View != nil:
			return c.View.Inputs[name]
		}
		return s.Defaults[name]
	}
	errOf := func(name string) string {
		for _, e := range s.Errors {
			if e.Field == name && e.Line == 0 {
				return e.Message
			}
		}
		return ""
	}
	input := func(name, hint, value string) templ.Component {
		f, _ := c.DT.Field(name)
		f.Hint = hint
		return docscreen.FieldInput(f, components.FieldID(name), value, errOf(name), c.Deps)
	}
	date := val("claim_date")
	if date == "" && !s.Posted && c.View == nil && c.Deps.Today != nil {
		date, _ = c.Deps.Today() // a new claim is dated today (UX §5.3)
	}
	v := detailsView{
		purpose: input("purpose", hintPurpose, val("purpose")),
		date:    input("claim_date", "", date),
		remarks: input("remarks", hintRemarks, val("remarks")),
	}
	if isAdmin(c.Principal) {
		code, err := d.codeOf(c, s)
		if err != nil {
			return nil, err
		}
		v.code = &components.FieldProps{
			ID: components.FieldID("employee_id"), Name: "employee_code", Label: labelCode, Hint: hintCode,
			Error: errOf("employee_id"), Value: code, Optional: true,
			Attrs: templ.Attributes{"autocapitalize": "characters", "spellcheck": "false"},
		}
	}

	chosen, less := val("advance_id"), val("less_advance")
	opts, err := d.advanceOptions(c, chosen)
	if err != nil {
		return nil, err
	}
	// A stored Less advance alone draws nothing: with an advance the viewer
	// can't read it would offer only No advance, and a save would clear it.
	typed := errOf("advance_id") != "" || errOf("less_advance") != ""
	unpaid := false
	if len(opts) == 0 && !typed {
		if unpaid, err = d.unpaidOnly(c); err != nil {
			return nil, err
		}
	}
	v.advance = len(opts) > 0 || typed || unpaid
	v.open = chosen != "" || less != "" || typed
	if len(opts) > 0 || typed {
		v.radios = &components.FieldProps{
			ID: components.FieldID("advance_id"), Name: "advance_id", Label: "Advance", Hint: hintPaidOnly,
			Error: errOf("advance_id"), Value: chosen,
		}
		v.options = append([]components.Option{{Value: "", Label: labelNoAdv}}, opts...)
		v.less = input("less_advance", hintLess, less)
	}
	return detailsBodyView(v), nil
}

// codeOf is the employee code input's value: the posted one, else the
// stored employee's code (the engine never stores a claim without one).
func (d Deps) codeOf(c docscreen.Ctx, s docscreen.StepState) (string, error) {
	if s.Posted {
		return c.Req.PostFormValue("employee_code"), nil
	}
	if c.View == nil {
		return "", nil
	}
	id, err := strconv.ParseInt(c.View.Inputs["employee_id"], 10, 64)
	if err != nil {
		return "", err
	}
	var code string
	err = c.Deps.DB.Read(c.Req.Context(), func(tx *db.Tx) error {
		e, err := d.Employee.Get(c.Req.Context(), tx, id)
		code = e.Code
		return err
	})
	return code, err
}

// employeeOf is the claim's employee: the stored one, or on a new claim the
// creator's; ok is false when there is none.
func (d Deps) employeeOf(c docscreen.Ctx) (int64, bool, error) {
	if c.View == nil {
		return d.Service.OwnEmployee(c.Req.Context(), c.Principal)
	}
	// The engine never stores a draft claim without its employee.
	id, err := strconv.ParseInt(c.View.Inputs["employee_id"], 10, 64)
	return id, err == nil, err
}

// advanceOptions are the employee's approved, paid advances with something
// left that the viewer can read (AdvanceChoices, CD-312), oldest first,
// then the chosen advance when it isn't among them (closed, cancelled, no
// longer paid or used up) and the viewer can read it. One the viewer can't
// read is never named (CD-233).
func (d Deps) advanceOptions(c docscreen.Ctx, chosen string) ([]components.Option, error) {
	ctx := c.Req.Context()
	emp, ok, err := d.employeeOf(c)
	if err != nil {
		return nil, err
	}
	var choices []expense.AdvanceChoice
	if ok {
		if choices, err = d.Service.AdvanceChoices(ctx, c.Principal, emp); err != nil {
			return nil, err
		}
	}
	var opts []components.Option
	for _, a := range choices {
		opts = append(opts, components.Option{
			Value: strconv.FormatInt(a.ID, 10),
			Label: a.Label + " · " + types.FormatINR(a.Left) + " left",
			Hint:  a.Purpose + " · " + types.FormatDate(a.Date) + " · advance " + types.FormatINR(a.Amount),
		})
	}
	if chosen == "" || slices.ContainsFunc(opts, func(o components.Option) bool { return o.Value == chosen }) {
		return opts, nil
	}
	id, err := strconv.ParseInt(chosen, 10, 64)
	if err != nil {
		return opts, nil // a crafted value names nothing
	}
	info, ok, err := d.readableAdvance(ctx, c, id)
	if err != nil || !ok {
		return opts, err
	}
	o := components.Option{Value: chosen, Label: info.Display}
	switch info.State {
	case doctype.StateClosed:
		o.Hint = hintClosed
	case doctype.StateCancelled:
		o.Hint = hintCanceled
	}
	return append(opts, o), nil
}

// readableAdvance is advance id's Info when the viewer can read it, checked
// by AUTH from the document's own Info (CD-312), never Deps.LinkRead.
func (d Deps) readableAdvance(ctx context.Context, c docscreen.Ctx, id int64) (doctype.Info, bool, error) {
	var info doctype.Info
	var ok bool
	err := c.Deps.DB.Read(ctx, func(tx *db.Tx) error {
		err := d.Auth.CheckLinkTx(ctx, tx, c.Principal, expense.AdvanceDocType, id)
		if errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		info, err = d.Engine.Info(ctx, tx, expense.AdvanceDocType, id)
		ok = err == nil
		return err
	})
	return info, ok, err
}

// unpaidOnly reports whether the claim's employee has an approved advance
// with something left that isn't marked as paid yet (CD-271 G1).
func (d Deps) unpaidOnly(c docscreen.Ctx) (bool, error) {
	emp, ok, err := d.employeeOf(c)
	if err != nil || !ok {
		return false, err
	}
	return d.Service.HasUnpaidAdvance(c.Req.Context(), c.Principal, emp)
}

// detailsSave is the step's values in DOC's plain form. Only an
// Administrator's post sets the employee, from the code (blank is their own
// claim); anyone else's employee_id is dropped (HOOK-010, EUX-019). An
// unknown code is a 422 with nothing written.
func (d Deps) detailsSave(c docscreen.Ctx) (docscreen.Save, error) {
	s := docscreen.Save{Values: map[string]string{}}
	names := detailsFields
	if c.Req.PostFormValue(advanceDrawn) != "" {
		names = append(slices.Clone(names), advanceFields...)
	}
	for _, name := range names {
		f, _ := c.DT.Field(name)
		v, fe := docscreen.ToPlain(f, c.Req.PostFormValue(name))
		if fe != nil {
			s.Errors = append(s.Errors, *fe)
			continue
		}
		s.Values[name] = v
	}
	if !isAdmin(c.Principal) {
		return s, nil
	}
	code := strings.TrimSpace(c.Req.PostFormValue("employee_code"))
	if code == "" {
		s.Values["employee_id"] = ""
		return s, nil
	}
	var e employee.Employee
	var found bool
	err := c.Deps.DB.Read(c.Req.Context(), func(tx *db.Tx) error {
		var err error
		e, found, err = d.Employee.ByCode(c.Req.Context(), tx, code)
		return err
	})
	if err != nil {
		return docscreen.Save{}, err
	}
	if !found {
		s.Errors = append(s.Errors, doctype.FieldError{Field: "employee_id", Message: fmt.Sprintf(msgNoCode, code)})
		return s, nil
	}
	s.Values["employee_id"] = strconv.FormatInt(e.ID, 10)
	return s, nil
}

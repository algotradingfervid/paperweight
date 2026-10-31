package doc_test

import (
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// The field errors x1Hooks.BeforeSubmit returns through doc.Invalid.
var x1SubmitFields = []doctype.FieldError{
	{Field: "note", Line: 0, Message: "Note is needed before submitting."},
	{Field: "line_note", Line: 1, Message: "Line 1: Add a receipt or say why there isn't one."},
}

// x1WantInvalid fails unless err is doc.Invalid(x1SubmitFields) as an
// HTTPError: KindInvalid, the fix-fields text, 422 and exactly those fields.
func x1WantInvalid(t *testing.T, what string, err error) {
	t.Helper()
	var de *doc.Error
	if !errors.As(err, &de) {
		t.Fatalf("%s: err = %v (%T), want a *doc.Error of kind %q", what, err, err, doc.KindInvalid)
	}
	if de.Kind != doc.KindInvalid {
		t.Errorf("%s: Kind = %q, want %q", what, de.Kind, doc.KindInvalid)
	}
	if de.Message != x1MsgFixFields {
		t.Errorf("%s: Message = %q, want %q", what, de.Message, x1MsgFixFields)
	}
	var he doctype.HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("%s: err = %T, want a doctype.HTTPError", what, err)
	}
	if he.Status() != 422 {
		t.Errorf("%s: Status() = %d, want 422", what, he.Status())
	}
	fixturetest.Eq(t, he.FieldErrors(), x1SubmitFields)
}

// A module's BeforeSubmit returning doc.Invalid(fields) refuses submit and
// approve with a 422 carrying those fields; state and version are unchanged
// and nothing is written.
func TestDoc_Invalid_FromBeforeSubmit(t *testing.T) {
	// doc.Invalid itself: the exported twin of the engine's invalid.
	x1WantInvalid(t, "doc.Invalid", doc.Invalid(x1SubmitFields))

	h := fixturetest.New(t)
	x1Tables(t, h)
	ctx := t.Context()
	draft := h.MustDo(t, ctx, h.Priya, x1Allow, 0, doc.ActionSave, x1In(0, map[string]string{"note": "Travel"}))
	pending := h.MustDo(t, ctx, h.Priya, x1Allow, 0, doc.ActionSave, x1In(0, map[string]string{"note": "Travel"}))
	pending = h.MustDo(t, ctx, h.Priya, x1Allow, pending.ID, doc.ActionSubmit, trIn(pending.Version))
	if pending.State != doc.StatePending {
		t.Fatalf("setup submit Result.State = %q, want pending", pending.State)
	}
	before := []doc.Doc{h.Load(t, x1Allow, draft.ID), h.Load(t, x1Allow, pending.ID)}
	count := h.AuditCount(t)
	bad := x1WithSubmitErr(ctx, doc.Invalid(x1SubmitFields))

	_, err := h.Do(t, bad, h.Priya, x1Allow, draft.ID, doc.ActionSubmit, trIn(draft.Version))
	x1WantInvalid(t, "submit", err)
	trState(t, h, x1Allow, draft.ID, doc.StateDraft, draft.Version)

	_, err = h.Do(t, bad, h.Ravi, x1Allow, pending.ID, doc.ActionApprove, trIn(pending.Version))
	x1WantInvalid(t, "approve", err)
	trState(t, h, x1Allow, pending.ID, doc.StatePending, pending.Version)

	fixturetest.Eq(t, []doc.Doc{h.Load(t, x1Allow, draft.ID), h.Load(t, x1Allow, pending.ID)}, before)
	if got := h.AuditCount(t); got != count {
		t.Errorf("AuditCount = %d, want %d (unchanged)", got, count)
	}
}

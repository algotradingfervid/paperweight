package doctype

import (
	"slices"
	"testing"
)

// Supporting tests for WP1-DOC Task 1 (no DOC case ID): Actions and FieldError.Path.

func TestActions_AllTenInOrder(t *testing.T) {
	want := []Action{
		ActionSave, ActionSubmit, ActionWithdraw, ActionApprove, ActionReject,
		ActionRevise, ActionClose, ActionCancel, ActionAmend, ActionDelete,
	}
	if got := Actions(); !slices.Equal(got, want) {
		t.Errorf("Actions() = %v, want %v", got, want)
	}
}

func TestFieldError_Path(t *testing.T) {
	cases := []struct {
		e    FieldError
		want string
	}{
		{FieldError{Field: "qty", Line: 2, Message: "Line 2: Quantity is required."}, "lines[2].qty"},
		{FieldError{Field: "rate", Line: 1}, "lines[1].rate"},
		{FieldError{Field: "supplier_id", Line: 0}, "supplier_id"},
	}
	for _, c := range cases {
		if got := c.e.Path(); got != c.want {
			t.Errorf("%+v.Path() = %q, want %q", c.e, got, c.want)
		}
	}
}

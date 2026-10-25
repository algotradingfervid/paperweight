// This file is in package auth (not auth_test) because TestSpec_ScopeOptions
// calls the unexported dimColumns, as Task 1 of the WP1-AUTH plan specifies.
// It imports nothing that imports auth, so there is no cycle.
package auth

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// TestRightFor_R4Mapping checks R4: which right each lifecycle action needs.
func TestRightFor_R4Mapping(t *testing.T) {
	tests := []struct {
		action doctype.Action
		isNew  bool
		want   Right
	}{
		{doctype.ActionSave, true, "create"},
		{doctype.ActionSave, false, "write"},
		{doctype.ActionSubmit, false, "submit"},
		{doctype.ActionWithdraw, false, "submit"},
		{doctype.ActionClose, false, "submit"},
		{doctype.ActionApprove, false, "approve"},
		{doctype.ActionReject, false, "approve"},
		{doctype.ActionRevise, false, "write"},
		{doctype.ActionCancel, false, "cancel"},
		{doctype.ActionAmend, false, "amend"},
		{doctype.ActionDelete, false, "delete"},
		{doctype.Action("print"), false, "print"},
	}
	for _, tc := range tests {
		got, err := RightFor(tc.action, tc.isNew)
		if err != nil || got != tc.want {
			t.Errorf("RightFor(%q, %v) = (%q, %v), want (%q, nil)", tc.action, tc.isNew, got, err, tc.want)
		}
	}
	if got, err := RightFor(doctype.Action("reopen"), false); err == nil {
		t.Errorf("RightFor(\"reopen\", false) = (%q, nil), want an error", got)
	}

	// The constants carry exactly these values.
	consts := map[Right]string{
		RightRead: "read", RightCreate: "create", RightWrite: "write", RightSubmit: "submit",
		RightCancel: "cancel", RightAmend: "amend", RightDelete: "delete", RightApprove: "approve",
		RightPrint: "print", RightExport: "export", RightImport: "import",
	}
	for c, want := range consts {
		if string(c) != want {
			t.Errorf("Right constant = %q, want %q", c, want)
		}
	}
}

// stubMaster makes a DocType a master (CD-21).
type stubMaster struct{}

func (stubMaster) Lookup(context.Context, *db.Tx, int64) (doctype.MasterInfo, bool, error) {
	return doctype.MasterInfo{}, false, nil
}

// TestSpec_Applies checks which rights apply to a DocType's shape.
func TestSpec_Applies(t *testing.T) {
	item := &doctype.DocType{Name: "authtest.Item", Master: stubMaster{}, Importable: true}
	order := &doctype.DocType{Name: "authtest.Order", Submittable: true, Approvable: true}
	receipt := &doctype.DocType{Name: "authtest.Receipt", Submittable: true}

	tests := []struct {
		dt   *doctype.DocType
		r    Right
		want bool
	}{
		{item, RightSubmit, false},
		{item, RightCancel, false},
		{item, RightAmend, false},
		{item, RightApprove, false},
		{item, RightImport, true},
		{order, RightApprove, true},
		{order, RightImport, false},
		{receipt, RightSubmit, true},
		{receipt, RightApprove, false},
	}
	for _, tc := range tests {
		if got := Applies(tc.dt, tc.r); got != tc.want {
			t.Errorf("Applies(%s, %s) = %v, want %v", tc.dt.Name, tc.r, got, tc.want)
		}
	}
}

// TestSpec_ScopeOptions checks the offered scopes (CD-11) and the split of
// dimension columns into header and line columns (CD-09).
func TestSpec_ScopeOptions(t *testing.T) {
	claim := &doctype.DocType{Name: "authtest.Claim", EmployeeField: "employee_id"}
	if got, want := ScopeOptions(claim), []Scope{ScopeOwn, ScopeTeam, ScopeAll}; !slices.Equal(got, want) {
		t.Errorf("ScopeOptions(claim) = %v, want %v", got, want)
	}
	order := &doctype.DocType{Name: "authtest.Order"}
	if got, want := ScopeOptions(order), []Scope{ScopeOwn, ScopeAll}; !slices.Equal(got, want) {
		t.Errorf("ScopeOptions(order) = %v, want %v", got, want)
	}

	sited := &doctype.DocType{Name: "authtest.Order", SiteFields: []string{"site_id", "lines.site_id"}}
	header, lines := dimColumns(sited, DimSite)
	if want := []string{"site_id"}; !slices.Equal(header, want) || !slices.Equal(lines, want) {
		t.Errorf("dimColumns(order, DimSite) = (%v, %v), want (%v, %v)", header, lines, want, want)
	}
}

// TestMessages_Denied checks the denial texts, the error identities and the
// doctype.HTTPError mapping (CD-35, CD-44, CD-54, CD-76).
func TestMessages_Denied(t *testing.T) {
	texts := []struct {
		name string
		err  error
		want string
	}{
		{"DeniedError write", &DeniedError{Right: RightWrite, Label: "Purchase Order"},
			"You don't have permission to change Purchase Order. Ask your administrator for a role that allows it."},
		{"DeniedError OwnOnly delete", &DeniedError{Right: RightDelete, Label: "Purchase Order", Plural: "Purchase Orders", OwnOnly: true},
			"You can only delete Purchase Orders you created."},
		{"DeniedError TeamOnly approve", &DeniedError{Right: RightApprove, Label: "Expense Claim", Plural: "Expense Claims", TeamOnly: true},
			"You can only approve your own Expense Claims and those of people who report to you."},
		{"ValueError site", &ValueError{Dim: DimSite, Allowed: []string{"PUNE-WH"}},
			"You can only use Site PUNE-WH. Choose one of your allowed sites."},
	}
	for _, tc := range texts {
		if got := tc.err.Error(); got != tc.want {
			t.Errorf("%s: Error() = %q, want %q", tc.name, got, tc.want)
		}
	}

	for name, err := range map[string]error{
		"DeniedError": &DeniedError{},
		"ValueError":  &ValueError{},
		"LinkError":   &LinkError{},
	} {
		if !errors.Is(err, ErrForbidden) {
			t.Errorf("errors.Is(&%s{}, ErrForbidden) = false, want true", name)
		}
	}

	if ErrNotFound != doctype.ErrNotFound {
		t.Errorf("ErrNotFound is not doctype.ErrNotFound (CD-54)")
	}
	const notFound = "This record doesn't exist or you don't have access to it."
	if MsgNotFound != notFound {
		t.Errorf("MsgNotFound = %q, want %q", MsgNotFound, notFound)
	}
	if got := ErrNotFound.Error(); got != MsgNotFound {
		t.Errorf("ErrNotFound.Error() = %q, want MsgNotFound %q (CD-76)", got, MsgNotFound)
	}

	var (
		_ doctype.HTTPError = &Problem{}
		_ doctype.HTTPError = &DeniedError{}
		_ doctype.HTTPError = &ValueError{}
		_ doctype.HTTPError = &LinkError{}
	)
	if got := (&Problem{Code: 409, Msg: "x"}).Error(); got != "x" {
		t.Errorf("Problem.Error() = %q, want its Msg %q", got, "x")
	}
	if got := ErrLoginInvalid.Status(); got != 401 {
		t.Errorf("ErrLoginInvalid.Status() = %d, want 401", got)
	}
	statuses := []struct {
		name string
		err  doctype.HTTPError
		want int
	}{
		{"Problem{Code: 409}", &Problem{Code: 409, Msg: "x"}, 409},
		{"DeniedError", &DeniedError{}, 403},
		{"ValueError", &ValueError{}, 403},
		{"LinkError", &LinkError{}, 403},
	}
	for _, tc := range statuses {
		if got := tc.err.Status(); got != tc.want {
			t.Errorf("(&%s).Status() = %d, want %d", tc.name, got, tc.want)
		}
		if fe := tc.err.FieldErrors(); fe != nil {
			t.Errorf("(&%s).FieldErrors() = %v, want nil", tc.name, fe)
		}
	}
}

package auth

import (
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// The rights a role can hold on a DocType. The scope constants are in
// contract.go (CD-84).
const (
	RightRead    Right = "read"
	RightCreate  Right = "create"
	RightWrite   Right = "write"
	RightSubmit  Right = "submit"
	RightCancel  Right = "cancel"
	RightAmend   Right = "amend"
	RightDelete  Right = "delete"
	RightApprove Right = "approve"
	RightPrint   Right = "print"
	RightExport  Right = "export"
	RightImport  Right = "import"
)

// AllRights lists every right in display order.
var AllRights = []Right{
	RightRead, RightCreate, RightWrite, RightSubmit, RightCancel, RightAmend,
	RightDelete, RightApprove, RightPrint, RightExport, RightImport,
}

// Canonical role names.
const (
	RoleAdministrator = "Administrator"
	RoleEmployee      = "Employee"
)

// scopeRank orders scopes: all > team > own > "" (not granted).
var scopeRank = map[Scope]int{ScopeOwn: 1, ScopeTeam: 2, ScopeAll: 3}

// Wider returns the wider of two scopes.
func Wider(a, b Scope) Scope {
	if scopeRank[b] > scopeRank[a] {
		return b
	}
	return a
}

// RightFor returns the right a lifecycle action needs (spec §3.1 R4). isNew
// picks create over write for save.
func RightFor(a doctype.Action, isNew bool) (Right, error) {
	switch a {
	case doctype.ActionSave:
		if isNew {
			return RightCreate, nil
		}
		return RightWrite, nil
	case doctype.ActionSubmit, doctype.ActionWithdraw, doctype.ActionClose:
		return RightSubmit, nil
	case doctype.ActionApprove, doctype.ActionReject:
		return RightApprove, nil
	case doctype.ActionRevise:
		return RightWrite, nil
	case doctype.ActionCancel:
		return RightCancel, nil
	case doctype.ActionAmend:
		return RightAmend, nil
	case doctype.ActionDelete:
		return RightDelete, nil
	case doctype.Action(RightPrint):
		return RightPrint, nil
	}
	return "", fmt.Errorf("auth.RightFor: action %q has no right; use one of the doctype.Action constants or print", a)
}

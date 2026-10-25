package approval

import (
	"errors"
	"fmt"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/types"
)

// Error kinds. Every UserError wraps one of the first eleven; the last two are
// programming errors (500), never UserErrors.
var (
	ErrNotAdmin       = errors.New("approval: not an Administrator")
	ErrInvalidRule    = errors.New("approval: invalid rule")
	ErrDuplicateRule  = errors.New("approval: duplicate rule")
	ErrRuleInUse      = errors.New("approval: rule in use")
	ErrRuleConflict   = errors.New("approval: rule changed by someone else")
	ErrNoApprover     = errors.New("approval: no approver")
	ErrNotPending     = errors.New("approval: not waiting for approval")
	ErrNotEligible    = errors.New("approval: not an eligible approver")
	ErrSelfApproval   = errors.New("approval: self-approval")
	ErrReasonRequired = errors.New("approval: reason required")
	ErrReasonTooLong  = errors.New("approval: reason too long")
	ErrUnknownAction  = errors.New("approval: unknown action")
	ErrNotEvaluated   = errors.New("approval: Open called without an evaluated requirement")
)

// UserError is an APR error a handler shows to the user (CD-44).
type UserError struct {
	Kind  error
	Msg   string
	Field string // "" = no field error
}

var _ doctype.HTTPError = (*UserError)(nil)

func (e *UserError) Error() string { return e.Msg }

// Unwrap gives Kind, plus auth.ErrForbidden for the permission kinds.
func (e *UserError) Unwrap() []error {
	switch e.Kind {
	case ErrNotAdmin, ErrNotEligible, ErrSelfApproval:
		return []error{e.Kind, auth.ErrForbidden}
	}
	return []error{e.Kind}
}

// Status is the HTTP status of the error's kind.
func (e *UserError) Status() int {
	switch e.Kind {
	case ErrNotAdmin, ErrNotEligible, ErrSelfApproval:
		return 403
	case ErrRuleInUse, ErrRuleConflict, ErrNoApprover, ErrNotPending:
		return 409
	}
	return 422 // ErrInvalidRule, ErrDuplicateRule, ErrReasonRequired, ErrReasonTooLong
}

// FieldErrors puts the message on the form field, if any.
func (e *UserError) FieldErrors() []doctype.FieldError {
	if e.Field == "" {
		return nil
	}
	return []doctype.FieldError{{Field: e.Field, Message: e.Msg}}
}

func userErr(kind error, field, format string, args ...any) error {
	return &UserError{Kind: kind, Msg: fmt.Sprintf(format, args...), Field: field}
}

func notAdmin() error {
	return userErr(ErrNotAdmin, "", "You don't have permission to change approval rules. Ask an Administrator.")
}

func invalidFromAmount() error {
	return userErr(ErrInvalidRule, "from_amount", "From amount must be ₹0.00 or more.")
}

func noApproverRole() error {
	return userErr(ErrInvalidRule, "role", "Choose an approver role for this rule.")
}

func noFallbackRole() error {
	return userErr(ErrInvalidRule, "fallback_role", "Choose a fallback role for claims whose employee has no reporting manager.")
}

// managerNotAvailable lists the labels of the employee-based DocTypes.
func managerNotAvailable(labels []string) error {
	return userErr(ErrInvalidRule, "role", "Reporting manager approval is only available for %s. Choose a role.", strings.Join(labels, " or "))
}

func masterDocType(label string) error {
	return userErr(ErrInvalidRule, "doctype", "%s is a master and can't need approval. Choose a document type.", label)
}

func noDocType() error {
	return userErr(ErrInvalidRule, "doctype", "Choose a document type.")
}

// roleDisabled names the field ("role" or "fallback_role") that holds role.
func roleDisabled(field, role string) error {
	return userErr(ErrInvalidRule, field, "Role %s is disabled. Choose an active role.", role)
}

func duplicateRule(label string, from types.Money) error {
	return userErr(ErrDuplicateRule, "from_amount", "A %s rule from %s already exists. Edit that rule instead.", label, types.FormatINR(from))
}

func ruleInUse(n int64) error {
	docs := "documents"
	if n == 1 {
		docs = "document"
	}
	return userErr(ErrRuleInUse, "", "This rule has been used by %s %s. Disable it instead of deleting.", types.FormatCount(n), docs)
}

func ruleConflict() error {
	return userErr(ErrRuleConflict, "", "This rule was changed by someone else. Reload and try again.")
}

func noApprover(label, role string) error {
	return userErr(ErrNoApprover, "", "No one else can approve this %s. Give the %s role to another active user, then submit again.", label, role)
}

// notEligible names the manager first when one is designated ("" = none).
func notEligible(title string, amount types.Money, manager string, roles []string) error {
	if manager != "" {
		return userErr(ErrNotEligible, "", "Only %s or a user with the %s role can approve %s (%s).", manager, joinRoles(roles), title, types.FormatINR(amount))
	}
	return userErr(ErrNotEligible, "", "Only a user with the %s role can approve %s (%s).", joinRoles(roles), title, types.FormatINR(amount))
}

func selfApproval(label string, roles []string) error {
	return userErr(ErrSelfApproval, "", "You can't approve your own %s. Ask another %s to approve it.", label, joinRoles(roles))
}

// noLongerPending is for a state a person's decision produced (CD-26);
// outcome is approved, rejected or withdrawn.
func noLongerPending(title, outcome, name string) error {
	return userErr(ErrNotPending, "", "%s is no longer waiting for approval. It was %s by %s.", title, outcome, name)
}

func notPending(title string, state doctype.State) error {
	return userErr(ErrNotPending, "", "%s is not waiting for approval. It is %s.", title, stateLabel(state))
}

func reasonRequired() error {
	return userErr(ErrReasonRequired, "reason", "Enter a reason for rejecting.")
}

func reasonTooLong() error {
	return userErr(ErrReasonTooLong, "reason", "Reason must be 1,000 characters or fewer.")
}

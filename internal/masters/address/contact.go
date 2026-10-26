package address

// Stubs so the package's Task 7 tests compile; MST Task 7 replaces this file.

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/errs"
)

// Contact is one Contact record (MST Task 7).
type Contact struct {
	FirstName, MiddleName, LastName string
}

// EmailInput is one email of a contact (MST Task 7).
type EmailInput struct {
	Email   string
	Primary bool
}

// PhoneInput is one phone number of a contact (MST Task 7).
type PhoneInput struct {
	Number          string
	Mobile, Primary bool
}

// ContactInput is what CreateContact accepts (MST Task 7).
type ContactInput struct {
	PartyID, BranchID               int64
	FirstName, MiddleName, LastName string
	Emails                          []EmailInput
	Phones                          []PhoneInput
	Primary                         bool
}

// FullName is MST Task 7's.
func FullName(Contact) string { return "" }

// NormalizeEmail is MST Task 7's.
func NormalizeEmail(string) (string, error) { return "", errs.ErrNotImplemented }

// NormalizeMobile is MST Task 7's.
func NormalizeMobile(string) (string, error) { return "", errs.ErrNotImplemented }

// ValidateContact is MST Task 7's.
func ValidateContact(ContactInput) (ContactInput, error) {
	return ContactInput{}, errs.ErrNotImplemented
}

// CreateContact is MST Task 7's.
func (s *Service) CreateContact(context.Context, auth.Principal, ContactInput) (mstkit.Saved, error) {
	return mstkit.Saved{}, errs.ErrNotImplemented
}

// SetPrimaryContactTx is MST Task 7's.
func (s *Service) SetPrimaryContactTx(context.Context, *db.Tx, auth.Principal, int64, int64) error {
	return errs.ErrNotImplemented
}

// UpdateContact is MST Task 7's (standard surface Update for Contact).
func (s *Service) UpdateContact(context.Context, auth.Principal, int64, int, ContactInput) (mstkit.Saved, error) {
	return mstkit.Saved{}, errs.ErrNotImplemented
}

// SetContactDisabled is MST Task 7's (standard surface SetDisabled for Contact).
func (s *Service) SetContactDisabled(context.Context, auth.Principal, int64, int, bool) error {
	return errs.ErrNotImplemented
}

// DeleteContact is MST Task 7's (standard surface Delete for Contact).
func (s *Service) DeleteContact(context.Context, auth.Principal, int64, int) error {
	return errs.ErrNotImplemented
}

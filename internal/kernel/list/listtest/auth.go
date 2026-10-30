package listtest

import (
	"context"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Auth is an auth.Service for list tests: Can denies the rights in Deny with
// AUTH's message, Scope returns Pred, and both record the right in Calls.
// Every other method comes from authfake.New().
type Auth struct {
	auth.Service
	Deny  map[auth.Right]bool
	Pred  auth.Predicate
	Calls []auth.Right
}

// NewAuth returns an Auth that allows everything with no scope filter.
func NewAuth() *Auth { return &Auth{Service: authfake.New()} }

func (a *Auth) Can(_ context.Context, _ auth.Principal, dtName string, r auth.Right, _ *auth.RecordRef) error {
	a.Calls = append(a.Calls, r)
	if a.Deny[r] {
		label := dtName
		if dt, ok := doctype.Lookup(dtName); ok {
			label = dt.Label
		}
		return fmt.Errorf("%w: You don't have permission to %s %s. Ask your administrator for a role that allows it.", auth.ErrForbidden, r, label)
	}
	return nil
}

// CanTx is Can: the double has no transactions.
func (a *Auth) CanTx(ctx context.Context, _ *db.Tx, p auth.Principal, dtName string, r auth.Right, rec *auth.RecordRef) error {
	return a.Can(ctx, p, dtName, r, rec)
}

func (a *Auth) Scope(_ context.Context, _ auth.Principal, _ string, r auth.Right) (auth.Predicate, error) {
	a.Calls = append(a.Calls, r)
	return a.Pred, nil
}

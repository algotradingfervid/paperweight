package doc

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Close, cancel, delete and amend (Task 10).

func (e *engine) close(context.Context, *db.Tx, auth.Principal, *DocType, Doc, ActionInput) (Result, error) {
	return Result{}, errNotImplemented
}

func (e *engine) cancel(context.Context, *db.Tx, auth.Principal, *DocType, Doc, ActionInput) (Result, error) {
	return Result{}, errNotImplemented
}

func (e *engine) delete(context.Context, *db.Tx, auth.Principal, *DocType, Doc, ActionInput) (Result, error) {
	return Result{}, errNotImplemented
}

func (e *engine) amend(context.Context, *db.Tx, auth.Principal, *DocType, Doc, ActionInput) (Result, error) {
	return Result{}, errNotImplemented
}

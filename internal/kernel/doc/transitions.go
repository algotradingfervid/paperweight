package doc

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// The approval transitions (Task 9).

func (e *engine) submit(context.Context, *db.Tx, auth.Principal, *DocType, Doc, ActionInput) (Result, error) {
	return Result{}, errNotImplemented
}

func (e *engine) approve(context.Context, *db.Tx, auth.Principal, *DocType, Doc, ActionInput) (Result, error) {
	return Result{}, errNotImplemented
}

func (e *engine) reject(context.Context, *db.Tx, auth.Principal, *DocType, Doc, ActionInput) (Result, error) {
	return Result{}, errNotImplemented
}

func (e *engine) revise(context.Context, *db.Tx, auth.Principal, *DocType, Doc, ActionInput) (Result, error) {
	return Result{}, errNotImplemented
}

func (e *engine) withdraw(context.Context, *db.Tx, auth.Principal, *DocType, Doc, ActionInput) (Result, error) {
	return Result{}, errNotImplemented
}

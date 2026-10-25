package doc

import (
	"context"

	"github.com/vardhan/internaltools/internal/platform/db"
)

// guard returns the refusal of action a in state s for the document x, or
// nil when a is allowed (Task 8).
func guard(State, Action, string) *Error { return nil }

// notWaiting is the refusal of approve or reject outside pending_approval
// (Task 8, CD-26).
func (e *engine) notWaiting(context.Context, *db.Tx, Doc, string) *Error { return nil }

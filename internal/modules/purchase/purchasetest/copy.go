package purchasetest

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
)

// CreateHook is the world's real engine with a step before and after each
// create of a purchase.Order (Do with id 0 and ActionSave). Every call still
// goes to the real engine, so it refuses exactly what the engine refuses.
// P12's tests use it to change the world between Copy's read and its create
// (Before), and between its create and its copy_notes write (After).
type CreateHook struct {
	doc.Engine
	Before func(ctx context.Context, p auth.Principal)
	After  func(ctx context.Context, p auth.Principal, res doc.Result)
}

// Do runs Before, the real Do, then After when the create succeeded.
func (h *CreateHook) Do(ctx context.Context, p auth.Principal, dt string, id int64, a doc.Action, in doc.ActionInput) (doc.Result, error) {
	create := dt == purchase.DocType && id == 0 && a == doctype.ActionSave
	if create && h.Before != nil {
		h.Before(ctx, p)
	}
	res, err := h.Engine.Do(ctx, p, dt, id, a, in)
	if create && err == nil && h.After != nil {
		h.After(ctx, p, res)
	}
	return res, err
}

// ServiceWith is a purchase.Service over e's world, as New builds it, with
// engine in place of the world's engine.
func (e *Env) ServiceWith(engine doc.Engine) *purchase.Service {
	return purchase.New(purchase.ServiceDeps{
		DB: e.DB, Engine: engine, Auth: e.Auth, Audit: audit.Default, Approvals: e.Approvals, Comments: e.Comments, Clock: e.Clock,
	})
}

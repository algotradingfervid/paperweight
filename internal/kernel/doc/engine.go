package doc

import (
	"context"
	"errors"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Deps are the engine's collaborators. A nil Audit is audit.Default; a nil
// ParseValue is PlainValue; nil Numbers fields are the naming functions.
type Deps struct {
	DB         *db.DB
	Clock      clock.Clock
	Auth       auth.Service
	Naming     naming.Service
	Numbers    NumberFuncs
	Approval   approval.Service
	Files      files.Service
	Audit      audit.Writer
	ParseValue func(Field, string) (any, error)
}

type engine struct{ Deps }

// NewEngine returns the document engine. It panics if doctype.CheckRegistry
// fails (CD-107).
func NewEngine(d Deps) Engine {
	if err := doctype.CheckRegistry(); err != nil {
		panic(fmt.Sprintf("doc: NewEngine: %v", err))
	}
	if d.Audit == nil {
		d.Audit = audit.Default
	}
	if d.ParseValue == nil {
		d.ParseValue = PlainValue
	}
	d.Numbers = d.Numbers.withDefaults()
	return &engine{d}
}

// rights is the right each action needs (R4); create needs "create", and
// approve and reject are APR's (CD-52).
var rights = map[Action]auth.Right{
	ActionSave: "write", ActionSubmit: "submit", ActionWithdraw: "submit", ActionClose: "submit",
	ActionRevise: "write", ActionCancel: "cancel", ActionAmend: "amend", ActionDelete: "delete",
}

// Do runs action a on document id of doctype as one BEGIN IMMEDIATE
// transaction (the plan's Do pipeline).
func (e *engine) Do(ctx context.Context, p auth.Principal, doctype string, id int64, a Action, in ActionInput) (Result, error) {
	dt, ok := Lookup(doctype)
	if !ok || !dt.IsDocument() {
		return Result{}, errGone()
	}
	var res Result
	err := e.DB.Write(ctx, func(tx *db.Tx) (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf(msgInternal, doctype, a, r)
			}
		}()
		res, err = e.do(ctx, tx, p, dt, id, a, in)
		return err
	})
	if errors.Is(err, db.ErrBusy) {
		return Result{}, &Error{Kind: KindBusy, Message: db.ErrBusy.Error(), Err: db.ErrBusy}
	}
	if err != nil {
		return Result{}, err
	}
	return res, nil
}

func (e *engine) do(ctx context.Context, tx *db.Tx, p auth.Principal, dt *DocType, id int64, a Action, in ActionInput) (Result, error) {
	if a == ActionSave && id == 0 {
		return e.create(ctx, tx, p, dt, in)
	}
	d, err := loadDoc(ctx, tx, dt, id)
	if err != nil {
		return Result{}, err
	}
	decision := a == ActionApprove || a == ActionReject
	right := rights[a]
	if decision {
		right = "read" // visibility only; CheckApprover owns the approve right (CD-52)
	}
	if err := e.Auth.Can(ctx, p, dt.Name, right, recordRef(dt, d)); err != nil {
		return Result{}, err
	}
	if decision {
		if err := e.Approval.CheckApprover(ctx, tx, p, dt.Name, id); err != nil {
			return Result{}, err
		}
	}
	x := naming.Label(d.Number, d.ID)
	if a == ActionSubmit && d.State != StateDraft && in.Version != d.Version {
		return Result{}, Refused(fmt.Sprintf(msgAlreadySubmitted, x))
	}
	var refusal *Error
	if decision && d.State != StatePending {
		refusal = e.notWaiting(ctx, tx, d, x)
	} else {
		refusal = guard(d.State, a, x)
	}
	if refusal != nil {
		return Result{}, refusal
	}
	if in.Version != d.Version {
		return Result{}, stale(x)
	}
	switch a {
	case ActionSave:
		return e.save(ctx, tx, p, dt, d, in)
	case ActionSubmit:
		return e.submit(ctx, tx, p, dt, d, in)
	case ActionApprove:
		return e.approve(ctx, tx, p, dt, d, in)
	case ActionReject:
		return e.reject(ctx, tx, p, dt, d, in)
	case ActionRevise:
		return e.revise(ctx, tx, p, dt, d, in)
	case ActionWithdraw:
		return e.withdraw(ctx, tx, p, dt, d, in)
	case ActionClose:
		return e.close(ctx, tx, p, dt, d, in)
	case ActionCancel:
		return e.cancel(ctx, tx, p, dt, d, in)
	case ActionAmend:
		return e.amend(ctx, tx, p, dt, d, in)
	case ActionDelete:
		return e.delete(ctx, tx, p, dt, d, in)
	}
	return Result{}, errNotImplemented
}

func stale(x string) *Error {
	return &Error{Kind: KindStale, Message: fmt.Sprintf(msgStale, x)}
}

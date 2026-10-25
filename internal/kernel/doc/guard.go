package doc

import (
	"context"
	"fmt"
	"slices"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// allowedActions is every action each state permits (plan Task 8).
var allowedActions = map[State][]Action{
	StateDraft:     {ActionSave, ActionSubmit, ActionDelete},
	StatePending:   {ActionWithdraw, ActionApprove, ActionReject},
	StateApproved:  {ActionClose, ActionCancel},
	StateRejected:  {ActionRevise},
	StateCancelled: {ActionAmend},
}

// guard returns the refusal of action a in state s for the document x, or
// nil when a is allowed (Task 8).
func guard(s State, a Action, x string) *Error {
	if !slices.Contains(doctype.Actions(), a) {
		return Refused(fmt.Sprintf(msgUnknownAction, x))
	}
	if slices.Contains(allowedActions[s], a) {
		return nil
	}
	return Refused(refusal(s, a, x))
}

// refusal is the plan's refusal table, matched top to bottom.
func refusal(s State, a Action, x string) string {
	in := func(as ...Action) bool { return slices.Contains(as, a) }
	switch {
	case in(ActionApprove, ActionReject):
		return fmt.Sprintf(msgNotWaiting, x, doctype.StateLabel(s))
	case s == StateDraft && a == ActionWithdraw:
		return fmt.Sprintf(msgNotSubmitted, x)
	case s == StateDraft && a == ActionRevise:
		return msgOnlyRejected
	case s == StateDraft && a == ActionCancel:
		return msgOnlyApprovedCanc
	case s != StateApproved && s != StateClosed && a == ActionClose:
		return msgOnlyApprovedClose
	case s != StateApproved && s != StateClosed && a == ActionAmend:
		return msgOnlyCancelled
	case s == StateApproved && a == ActionAmend:
		return fmt.Sprintf(msgCancelFirst, x)
	case s == StatePending && in(ActionSave, ActionSubmit, ActionRevise):
		return fmt.Sprintf(msgPendingLocked, x)
	case s == StatePending && a == ActionCancel:
		return fmt.Sprintf(msgPendingCancel, x)
	case s == StatePending && a == ActionDelete:
		return fmt.Sprintf(msgPendingDelete, x)
	case s == StateApproved && in(ActionSave, ActionSubmit, ActionRevise):
		return fmt.Sprintf(msgApprovedLocked, x)
	case s == StateApproved && a == ActionWithdraw:
		return fmt.Sprintf(msgApprovedWithdraw, x)
	case (s == StateApproved || s == StateClosed) && a == ActionDelete:
		return msgApprovedDelete
	case s == StateRejected && in(ActionSave, ActionSubmit, ActionCancel, ActionWithdraw):
		return fmt.Sprintf(msgRejectedLocked, x)
	case s == StateRejected && a == ActionDelete:
		return msgRejectedDelete
	case s == StateClosed:
		return fmt.Sprintf(msgClosed, x)
	case s == StateCancelled && a == ActionDelete:
		return msgCancelledDelete
	case s == StateCancelled:
		return fmt.Sprintf(msgCancelledLocked, x)
	}
	return fmt.Sprintf(msgUnknownAction, x)
}

// transitionActions are the audit actions that move a document between states.
var transitionActions = []string{"submit", "withdraw", "approve", "reject", "revise", "close", "cancel"}

// decidedWords is the past tense of the person's decisions notWaiting names.
var decidedWords = map[string]string{"approve": "approved", "reject": "rejected", "withdraw": "withdrawn"}

// notWaiting is the refusal of approve or reject outside pending_approval
// (Task 8, CD-26): "no longer waiting" when a person's approve, reject or
// withdraw produced the current state, else "not waiting". A failed read
// keeps the plain refusal and carries the error in Err.
func (e *engine) notWaiting(ctx context.Context, tx *db.Tx, d Doc, x string) *Error {
	plain := Refused(fmt.Sprintf(msgNotWaiting, x, doctype.StateLabel(d.State)))
	rows, err := audit.ForDoc(ctx, tx, d.DocType, d.ID)
	if err != nil {
		plain.Err = err
		return plain
	}
	for i := len(rows) - 1; i >= 0; i-- {
		row := rows[i]
		if !slices.Contains(transitionActions, row.Action) {
			continue
		}
		verb, ok := decidedWords[row.Action]
		if !ok || State(row.To) != d.State {
			return plain
		}
		u, found, err := e.Auth.User(ctx, tx, row.By)
		if err != nil {
			plain.Err = err
			return plain
		}
		if !found {
			return plain
		}
		return Refused(fmt.Sprintf(msgNoLongerWaiting, x, verb, u.Name))
	}
	return plain
}

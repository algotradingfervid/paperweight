package doc

import (
	"context"
	"fmt"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// The approval transitions (Task 9).

// revalidate checks the stored d before it leaves draft or is approved: the
// core field checks with Required and at least one line (CD-178), the source
// documents, then Hooks.Validate. Every error becomes one KindInvalid error.
func (e *engine) revalidate(ctx context.Context, tx *db.Tx, dt *DocType, d Doc) ([]string, error) {
	n := normalized{Header: d.Header, links: map[linkKey]doctype.MasterInfo{}}
	for i, l := range d.Lines {
		n.Lines = append(n.Lines, nline{ID: l.ID, Values: l.Values, Pos: i})
	}
	errs, warnings := checkFields(ctx, tx, dt, &n, true, masters)
	if n.err != nil {
		return nil, n.err
	}
	fe, err := e.checkSources(ctx, tx, dt, d)
	if err != nil {
		return nil, err
	}
	if fe != nil {
		errs = append(errs, *fe)
	}
	if errs = append(errs, dt.Hooks.Validate(ctx, tx, d.ID)...); len(errs) > 0 {
		return nil, invalid(byPosition(dt, errs))
	}
	return warnings, nil
}

// enterApproved moves d to approved: it assigns the number when d has none
// (R1), sets state and number with a guarded version bump, then applies the
// effects and recomputes the roll-ups with cause.
func (e *engine) enterApproved(ctx context.Context, tx *db.Tx, p auth.Principal, dt *DocType, d Doc, cause string) (string, error) {
	number := d.Number
	if number == "" {
		var err error
		if number, err = e.assign(ctx, tx, dt, d); err != nil {
			return "", err
		}
	}
	if err := e.setState(ctx, tx, p, dt, d, StateApproved, []string{quote("number")}, []any{number}); err != nil {
		return "", err
	}
	if err := dt.Hooks.ApplyEffects(ctx, tx, d.ID); err != nil {
		return "", err
	}
	if err := e.recomputeRollUps(ctx, tx, p, dt, d, number, cause); err != nil {
		return "", err
	}
	return number, nil
}

// assign gets d's number from NAM: the amendment number of its root's number
// when d is an amendment, otherwise the next number for its document date.
func (e *engine) assign(ctx context.Context, tx *db.Tx, dt *DocType, d Doc) (string, error) {
	if d.AmendmentNo > 0 {
		root, err := readRootNumber(ctx, tx, dt, d.AmendmentRootID)
		if err != nil {
			return "", err
		}
		return e.Naming.AssignAmendment(ctx, tx, dt.Name, d.ID, root, d.AmendmentNo)
	}
	date, err := dt.Hooks.Date(ctx, tx, d.ID)
	if err != nil {
		return "", err
	}
	return e.Naming.Assign(ctx, tx, dt.Name, d.ID, date)
}

// readRootNumber reads the number of the amendment root rootID ("" when it has none).
func readRootNumber(ctx context.Context, tx *db.Tx, dt *DocType, rootID int64) (string, error) {
	var number string
	q := "SELECT COALESCE(number, '') FROM " + quote(dt.Table) + " WHERE id = ?"
	if err := tx.SQL().QueryRowContext(ctx, q, rootID).Scan(&number); err != nil {
		return "", fmt.Errorf("doc: read %s %d number: %w", dt.Name, rootID, err)
	}
	return number, nil
}

// setState sets d's state (and cols to args) with a guarded version bump; a
// changed document is stale.
func (e *engine) setState(ctx context.Context, tx *db.Tx, p auth.Principal, dt *DocType, d Doc, s State, cols []string, args []any) error {
	cols = append([]string{quote("state")}, cols...)
	args = append([]any{string(s)}, args...)
	ok, err := updateHeaderGuarded(ctx, tx, dt, d.ID, d.Version, cols, args, e.Clock.Now().UTC(), p.ID)
	if err != nil {
		return err
	}
	if !ok {
		return stale(naming.Label(d.Number, d.ID))
	}
	return nil
}

// transition writes the audit row of d moving to state to.
func (e *engine) transition(ctx context.Context, tx *db.Tx, p auth.Principal, d Doc, a Action, to State, reason string, changes []audit.Change) error {
	return e.Audit.Append(ctx, tx, audit.Entry{
		DocType: d.DocType, DocID: d.ID, Action: string(a), From: string(d.State), To: string(to),
		Reason: reason, Changes: changes, By: p.ID, At: e.Clock.Now().UTC(),
	})
}

func numberChange(number string) audit.Change {
	return audit.Change{Field: "number", After: number, Op: audit.OpSet}
}

func (e *engine) submit(ctx context.Context, tx *db.Tx, p auth.Principal, dt *DocType, d Doc, _ ActionInput) (Result, error) {
	warnings, err := e.revalidate(ctx, tx, dt, d)
	if err != nil {
		return Result{}, err
	}
	if err := dt.Hooks.BeforeSubmit(ctx, tx, d.ID); err != nil {
		return Result{}, err
	}
	amount, err := dt.Hooks.Amount(ctx, tx, d.ID)
	if err != nil {
		return Result{}, err
	}
	var req approval.Requirement
	if dt.Approvable {
		if req, err = e.Approval.Evaluate(ctx, tx, dt.Name, d.ID, amount, p); err != nil {
			return Result{}, err
		}
		if err := e.Approval.Open(ctx, tx, dt.Name, d.ID, req); err != nil {
			return Result{}, err
		}
	}
	res := Result{ID: d.ID, Version: d.Version + 1, Warnings: warnings}
	if req.Needed {
		if err := e.setState(ctx, tx, p, dt, d, StatePending, nil, nil); err != nil {
			return Result{}, err
		}
		if err := e.transition(ctx, tx, p, d, ActionSubmit, StatePending, "", nil); err != nil {
			return Result{}, err
		}
		res.State = StatePending
		return res, nil
	}
	// No approval needed: continue as approve without CheckApprover (CD-23).
	if res.Number, err = e.enterApproved(ctx, tx, p, dt, d, string(ActionSubmit)); err != nil {
		return Result{}, err
	}
	if dt.Approvable {
		if err := e.Approval.Record(ctx, tx, dt.Name, d.ID, doctype.ActionApprove, p, ""); err != nil {
			return Result{}, err
		}
	}
	changes := []audit.Change{{Field: "auto_approved", After: "true", Op: audit.OpSet}, numberChange(res.Number)}
	if err := e.transition(ctx, tx, p, d, ActionSubmit, StateApproved, "", changes); err != nil {
		return Result{}, err
	}
	res.State = StateApproved
	return res, nil
}

func (e *engine) approve(ctx context.Context, tx *db.Tx, p auth.Principal, dt *DocType, d Doc, in ActionInput) (Result, error) {
	warnings, err := e.revalidate(ctx, tx, dt, d)
	if err != nil {
		return Result{}, err
	}
	if err := dt.Hooks.BeforeSubmit(ctx, tx, d.ID); err != nil {
		return Result{}, err
	}
	number, err := e.enterApproved(ctx, tx, p, dt, d, string(ActionApprove))
	if err != nil {
		return Result{}, err
	}
	if err := e.Approval.Record(ctx, tx, dt.Name, d.ID, doctype.ActionApprove, p, in.Reason); err != nil {
		return Result{}, err
	}
	if err := e.transition(ctx, tx, p, d, ActionApprove, StateApproved, "", []audit.Change{numberChange(number)}); err != nil {
		return Result{}, err
	}
	return Result{ID: d.ID, State: StateApproved, Number: number, Version: d.Version + 1, Warnings: warnings}, nil
}

func (e *engine) reject(ctx context.Context, tx *db.Tx, p auth.Principal, dt *DocType, d Doc, in ActionInput) (Result, error) {
	reason := strings.TrimSpace(in.Reason) // APR checks it (CD-157)
	if err := e.setState(ctx, tx, p, dt, d, StateRejected, []string{quote("state_reason")}, []any{reason}); err != nil {
		return Result{}, err
	}
	if err := e.Approval.Record(ctx, tx, dt.Name, d.ID, doctype.ActionReject, p, reason); err != nil {
		return Result{}, err
	}
	if err := e.transition(ctx, tx, p, d, ActionReject, StateRejected, reason, nil); err != nil {
		return Result{}, err
	}
	return Result{ID: d.ID, State: StateRejected, Version: d.Version + 1}, nil
}

func (e *engine) revise(ctx context.Context, tx *db.Tx, p auth.Principal, dt *DocType, d Doc, _ ActionInput) (Result, error) {
	return e.backToDraft(ctx, tx, p, dt, d, ActionRevise, []string{quote("state_reason")}, []any{""})
}

func (e *engine) withdraw(ctx context.Context, tx *db.Tx, p auth.Principal, dt *DocType, d Doc, _ ActionInput) (Result, error) {
	return e.backToDraft(ctx, tx, p, dt, d, ActionWithdraw, nil, nil)
}

// backToDraft moves d to draft for revise or withdraw (a), recording a with APR.
func (e *engine) backToDraft(ctx context.Context, tx *db.Tx, p auth.Principal, dt *DocType, d Doc, a Action, cols []string, args []any) (Result, error) {
	if err := e.setState(ctx, tx, p, dt, d, StateDraft, cols, args); err != nil {
		return Result{}, err
	}
	if err := e.Approval.Record(ctx, tx, dt.Name, d.ID, a, p, ""); err != nil {
		return Result{}, err
	}
	if err := e.transition(ctx, tx, p, d, a, StateDraft, "", nil); err != nil {
		return Result{}, err
	}
	return Result{ID: d.ID, State: StateDraft, Number: d.Number, Version: d.Version + 1}, nil
}

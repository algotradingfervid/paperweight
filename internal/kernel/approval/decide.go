package approval

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/vardhan/internaltools/internal/kernel/approval/dbq"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// maxReason is the longest reject reason, in characters.
const maxReason = 1000

// CheckApprover refuses p unless p may approve or reject the document's open
// request now. DOC calls it after its visibility check and before its
// from-state guard (CD-52); every read happens in the caller's tx (CD-94).
func (a *Approvals) CheckApprover(ctx context.Context, tx *db.Tx, p auth.Principal, dt string, id int64) error {
	in, err := a.info.Info(ctx, tx, dt, id)
	if err != nil {
		return err
	}
	// A record hidden from p is a 404 (doctype.ErrNotFound); having no read
	// right at all falls through to the eligibility message.
	if err := a.auth.Can(ctx, p, dt, "read", recordRef(in)); err != nil && !errors.Is(err, auth.ErrForbidden) {
		return err
	}
	r, ok, err := a.openRequest(ctx, tx, dt, id)
	if err != nil {
		return err
	}
	if !ok {
		return a.notPendingErr(ctx, tx, in)
	}
	s := snapshotOf(r)
	holds, err := a.holdsAny(ctx, p.ID, s)
	if err != nil {
		return err
	}
	if holds && !s.allowSelf && slices.Contains(s.own, p.ID) {
		return selfApproval(in.DocLabel, r.ApproverRoles)
	}
	refuse := notEligible(in.Display, r.Amount, r.ManagerName, r.ApproverRoles)
	if !holds {
		return refuse
	}
	users, err := a.eligible(ctx, s, in)
	if err != nil {
		return err
	}
	if !slices.Contains(users, p.ID) {
		return refuse
	}
	return nil
}

// Record writes p's action on the document to its approval request and
// history, inside the caller's tx (CD-23).
func (a *Approvals) Record(ctx context.Context, tx *db.Tx, dt string, id int64, action doctype.Action, p auth.Principal, reason string) error {
	switch action {
	case doctype.ActionApprove:
		return a.decide(ctx, tx, dt, id, p, OutcomeApproved, HistApproved, reason)
	case doctype.ActionReject:
		reason = strings.TrimSpace(reason)
		if reason == "" {
			return reasonRequired()
		}
		if utf8.RuneCountInString(reason) > maxReason {
			return reasonTooLong()
		}
		return a.decide(ctx, tx, dt, id, p, OutcomeRejected, HistRejected, reason)
	case doctype.ActionRevise:
		r, ok, err := a.LatestRequest(ctx, tx, dt, id)
		if err != nil {
			return err
		}
		var latest *Request
		if ok {
			latest = &r
		}
		name, err := a.userName(ctx, tx, p.ID)
		if err != nil {
			return err
		}
		return a.writeHistory(ctx, tx, dt, id, HistRevised, p, name, latest, sql.NullInt64{}, "", false)
	case doctype.ActionWithdraw:
		r, ok, err := a.openRequest(ctx, tx, dt, id)
		if err != nil {
			return err
		}
		if !ok {
			return a.notPendingFor(ctx, tx, dt, id)
		}
		return a.closeRequest(ctx, tx, dt, id, r, p, OutcomeWithdrawn, HistWithdrawn, "", false)
	}
	return ErrUnknownAction
}

// decide approves or rejects the open request after CheckApprover. An
// approve with no open request is the same submit's no-rule approval.
func (a *Approvals) decide(ctx context.Context, tx *db.Tx, dt string, id int64, p auth.Principal, outcome, hist, reason string) error {
	r, ok, err := a.openRequest(ctx, tx, dt, id)
	if err != nil {
		return err
	}
	if !ok {
		if outcome != OutcomeApproved {
			return a.notPendingFor(ctx, tx, dt, id)
		}
		last, err := dbq.New(tx.SQL()).LastHistory(ctx, dbq.LastHistoryParams{DocType: dt, DocID: id})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err != nil || last.Action != HistSubmitted || last.RuleID.Valid {
			return a.notPendingFor(ctx, tx, dt, id)
		}
		name, err := a.userName(ctx, tx, p.ID)
		if err != nil {
			return err
		}
		return a.writeHistory(ctx, tx, dt, id, HistAutoApproved, p, name, nil, last.Amount, reason, false)
	}
	if err := a.CheckApprover(ctx, tx, p, dt, id); err != nil {
		return err
	}
	return a.closeRequest(ctx, tx, dt, id, r, p, outcome, hist, reason, slices.Contains(snapshotOf(r).own, p.ID))
}

// closeRequest ends the open request r with outcome and writes the history row. A
// request already closed by someone else is refused, not overwritten.
func (a *Approvals) closeRequest(ctx context.Context, tx *db.Tx, dt string, id int64, r Request, p auth.Principal, outcome, hist, reason string, self bool) error {
	name, err := a.userName(ctx, tx, p.ID)
	if err != nil {
		return err
	}
	now := ts(a.clock.Now())
	n, err := dbq.New(tx.SQL()).CloseRequest(ctx, dbq.CloseRequestParams{
		Outcome:   sql.NullString{String: outcome, Valid: true},
		DecidedBy: sql.NullInt64{Int64: int64(p.ID), Valid: true}, DecidedByName: name,
		DecidedAt: sql.NullString{String: now, Valid: true}, Reason: reason, ID: r.ID,
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return a.notPendingFor(ctx, tx, dt, id)
	}
	return a.writeHistory(ctx, tx, dt, id, hist, p, name, &r, sql.NullInt64{}, reason, self)
}

// openRequest is the document's open request; false when none is open.
func (a *Approvals) openRequest(ctx context.Context, tx *db.Tx, dt string, id int64) (Request, bool, error) {
	row, err := dbq.New(tx.SQL()).GetOpenRequest(ctx, dbq.GetOpenRequestParams{DocType: dt, DocID: id})
	if errors.Is(err, sql.ErrNoRows) {
		return Request{}, false, nil
	}
	if err != nil {
		return Request{}, false, err
	}
	r, err := requestOf(row)
	return r, err == nil, err
}

// snapshotOf is the eligibility snapshot stored with r.
func snapshotOf(r Request) snapshot {
	s := snapshot{doctype: r.DocType, roles: r.ApproverRoles, manager: r.ManagerUser, allowSelf: r.AllowSelf}
	for _, u := range []types.UserID{r.Requester, r.Creator, r.Claimant} {
		if u != 0 {
			s.own = append(s.own, u)
		}
	}
	return s
}

// notPendingFor is notPendingErr for a document not yet read.
func (a *Approvals) notPendingFor(ctx context.Context, tx *db.Tx, dt string, id int64) error {
	in, err := a.info.Info(ctx, tx, dt, id)
	if err != nil {
		return err
	}
	return a.notPendingErr(ctx, tx, in)
}

// outcomeState is the document state each request outcome produces.
var outcomeState = map[string]doctype.State{
	OutcomeApproved:  doctype.StateApproved,
	OutcomeRejected:  doctype.StateRejected,
	OutcomeWithdrawn: doctype.StateDraft,
}

// notPendingErr names who decided when the latest request's outcome produced
// the document's current state (CD-26); otherwise it names the state.
func (a *Approvals) notPendingErr(ctx context.Context, tx *db.Tx, in doctype.Info) error {
	r, ok, err := a.LatestRequest(ctx, tx, in.DocType, in.ID)
	if err != nil {
		return err
	}
	if ok && r.Outcome != "" && outcomeState[r.Outcome] == in.State {
		return noLongerPending(in.Display, r.Outcome, r.DecidedByName)
	}
	return notPending(in.Display, in.State)
}

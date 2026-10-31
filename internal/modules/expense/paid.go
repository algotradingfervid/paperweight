package expense

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/expense/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Mark as paid (spec §4, CD-271 G1, CD-281): a Director or Administrator
// records the day an approved advance was paid out, so claims can set it off.

// paidField is the advance's paid date, as stored and as audited.
const paidField = "paid_on"

// MarkPaid sets the advance's paid_on to on at the version the page posted
// (CD-94).
func (s *Service) MarkPaid(ctx context.Context, p auth.Principal, advanceID int64, version int, on types.Date) error {
	return s.db.Write(ctx, func(tx *db.Tx) error {
		a, err := s.paidAdvance(ctx, tx, p, advanceID, version)
		if err != nil {
			return err
		}
		switch advanceDate, err := dateOf(a.AdvanceDate); {
		case err != nil:
			return err
		case on.Compare(types.Today(s.deps.Clock)) > 0:
			return paidOnInvalid(msgPaidFuture)
		case on.Compare(advanceDate) < 0:
			return paidOnInvalid(msgPaidBeforeDate)
		}
		if a.PaidOn.Valid {
			return paidRefused(msgPaidAlready)
		}
		return s.setPaidOn(ctx, tx, p, a, "paid", on.ISO())
	})
}

// UndoPaid clears the advance's paid_on at the version the page posted,
// while no pending, approved or closed claim names it (CD-281 a).
func (s *Service) UndoPaid(ctx context.Context, p auth.Principal, advanceID int64, version int) error {
	return s.db.Write(ctx, func(tx *db.Tx) error {
		a, err := s.paidAdvance(ctx, tx, p, advanceID, version)
		if err != nil {
			return err
		}
		if !a.PaidOn.Valid {
			return paidRefused(msgPaidNotPaid)
		}
		n, err := dbq.New(tx.SQL()).E9ClaimsUsing(ctx, sql.NullInt64{Int64: a.ID, Valid: true})
		if err != nil {
			return err
		}
		if n > 0 {
			return paidRefused(msgPaidClaimUses)
		}
		return s.setPaidOn(ctx, tx, p, a, "unpaid", "")
	})
}

// paidAdvance loads the advance in tx and makes the checks both actions
// share, in order: missing or hidden is AUTH's 404 (CD-245), no cancel on it
// is AUTH's 403, then the version (409), then the state. The right goes
// before the version (lessons §11).
func (s *Service) paidAdvance(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int) (dbq.E9AdvanceRow, error) {
	a, err := dbq.New(tx.SQL()).E9Advance(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return a, auth.ErrNotFound
	}
	if err != nil {
		return a, err
	}
	ref := &auth.RecordRef{DocType: AdvanceDocType, ID: a.ID, CreatedBy: types.UserID(a.CreatedBy), EmployeeID: a.EmployeeID.Int64}
	if err := s.deps.Auth.CanTx(ctx, tx, p, AdvanceDocType, auth.RightCancel, ref); err != nil {
		return a, err
	}
	if int64(version) != a.Version {
		return a, &doc.Error{Kind: doc.KindStale, Message: fmt.Sprintf(msgPaidStale, naming.Label(a.Number.String, a.ID))}
	}
	if a.State != string(doc.StateApproved) {
		return a, paidRefused(msgPaidNotApproved)
	}
	return a, nil
}

// setPaidOn writes paid_on (blank clears it), bumps the version and appends
// the action's audit entry with the date as the change, as the engine's
// system writes do (doc/rollup.go).
func (s *Service) setPaidOn(ctx context.Context, tx *db.Tx, p auth.Principal, a dbq.E9AdvanceRow, action, on string) error {
	now := s.deps.Clock.Now()
	err := dbq.New(tx.SQL()).E9SetPaidOn(ctx, dbq.E9SetPaidOnParams{
		PaidOn:    sql.NullString{String: on, Valid: on != ""},
		UpdatedAt: types.TimestampOf(now).String(),
		UpdatedBy: int64(p.ID),
		ID:        a.ID,
	})
	if err != nil {
		return err
	}
	return audit.Append(ctx, tx, audit.Entry{
		DocType: AdvanceDocType, DocID: a.ID, Action: action, By: p.ID, At: now,
		Changes: []audit.Change{{Field: paidField, Before: a.PaidOn.String, After: on, Op: audit.OpSet}},
	})
}

// checkNotPaid refuses cancelling an advance marked as paid (ReverseEffects).
func checkNotPaid(ctx context.Context, tx *db.Tx, id int64) error {
	a, err := dbq.New(tx.SQL()).AdvanceHeader(ctx, id)
	if err != nil {
		return err
	}
	if a.PaidOn.Valid {
		return doc.Refused(msgPaidCancel)
	}
	return nil
}

// paidRefused is a Mark as paid refusal: 422 with msg, no field.
func paidRefused(msg string) error { return &doc.Error{Kind: doc.KindInvalid, Message: msg} }

// paidOnInvalid is a date rule's refusal: 422 with msg on paid_on, so the
// page shows it at the field and in the summary (UX §5.18).
func paidOnInvalid(msg string) error {
	return &doc.Error{Kind: doc.KindInvalid, Message: msg, Fields: []doc.FieldError{{Field: paidField, Message: msg}}}
}

package doc

import (
	"context"
	"fmt"
	"slices"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// ReassignReason is why Reassign refused a new owner (M0-13). The module maps
// it to its own text.
type ReassignReason string

const (
	ReassignPending    ReassignReason = "pending"     // waiting for approval
	ReassignNotDraft   ReassignReason = "not_draft"   // approved, closed or cancelled
	ReassignSameOwner  ReassignReason = "same_owner"  // to is the current owner
	ReassignDisabled   ReassignReason = "disabled"    // to is disabled
	ReassignCantCreate ReassignReason = "cant_create" // to lacks create, or write as the owner
	ReassignCantSee    ReassignReason = "cant_see"    // to lacks read on the record
)

// ReassignError is Reassign's refusal. It carries no user text.
type ReassignError struct{ Reason ReassignReason }

func (e *ReassignError) Error() string { return "doc: reassign refused: " + string(e.Reason) }

// actionReassign is the Action of the reassign audit entry.
const actionReassign Action = "reassign"

// msgHandover is the reassign audit entry's Reason: from, to display names.
const msgHandover = "Handed over from %s to %s"

// Reassign makes to the owner of draft or rejected document id, in one write
// (CD-259 order): load, read, owner or approve, version, state, SameOwner,
// then the target checks.
func (e *engine) Reassign(ctx context.Context, p auth.Principal, doctype string, id int64, version int, to types.UserID) error {
	dt, ok := Lookup(doctype)
	if !ok || !dt.IsDocument() {
		return errGone()
	}
	return busy(e.DB.Write(ctx, func(tx *db.Tx) error {
		d, rec, err := e.reassigner(ctx, tx, p, dt, id)
		if err != nil {
			return err
		}
		x := naming.Label(d.Number, d.ID)
		if version != d.Version {
			return stale(x)
		}
		switch d.State {
		case StateDraft, StateRejected:
		case StatePending:
			return &ReassignError{Reason: ReassignPending}
		default:
			return &ReassignError{Reason: ReassignNotDraft}
		}
		if to == rec.CreatedBy {
			return &ReassignError{Reason: ReassignSameOwner}
		}
		creators, readers, err := e.targetLists(ctx, tx, dt, rec)
		if err != nil {
			return err
		}
		reason, err := e.targetRefusal(ctx, tx, dt, rec, creators, readers, to)
		if err != nil {
			return err
		}
		if reason != "" {
			return &ReassignError{Reason: reason}
		}
		from, _, err := e.Auth.User(ctx, tx, rec.CreatedBy)
		if err != nil {
			return err
		}
		next, _, err := e.Auth.User(ctx, tx, to)
		if err != nil {
			return err
		}
		ok, err := updateHeaderGuarded(ctx, tx, dt, d.ID, d.Version, []string{quote(dt.OwnerField)}, []any{int64(to)}, e.Clock.Now().UTC(), p.ID)
		if err != nil {
			return err
		}
		if !ok {
			return stale(x)
		}
		return e.transition(ctx, tx, p, d, actionReassign, d.State, fmt.Sprintf(msgHandover, from.Name, next.Name), nil)
	}))
}

// reassigner loads id and checks that p may reassign it: read on the record
// (a hidden one answers as Do does), then owner or approve (AUTH's error).
func (e *engine) reassigner(ctx context.Context, tx *db.Tx, p auth.Principal, dt *DocType, id int64) (Doc, *auth.RecordRef, error) {
	d, err := loadDoc(ctx, tx, dt, id)
	if err != nil {
		return Doc{}, nil, err
	}
	rec := recordRef(dt, d)
	if err := e.Auth.CanTx(ctx, tx, p, dt.Name, auth.RightRead, rec); err != nil {
		return Doc{}, nil, err
	}
	if rec.CreatedBy != p.ID {
		if err := e.Auth.CanTx(ctx, tx, p, dt.Name, auth.RightApprove, rec); err != nil {
			return Doc{}, nil, err
		}
	}
	return d, rec, nil
}

// EligibleOwners lists the users Reassign would accept as the new owner of
// id: after Reassign's caller checks, the enabled holders of create who pass
// the target checks, without the current owner.
func (e *engine) EligibleOwners(ctx context.Context, tx *db.Tx, p auth.Principal, doctype string, id int64) ([]types.UserID, error) {
	dt, ok := Lookup(doctype)
	if !ok || !dt.IsDocument() {
		return nil, errGone()
	}
	_, rec, err := e.reassigner(ctx, tx, p, dt, id)
	if err != nil {
		return nil, err
	}
	creators, readers, err := e.targetLists(ctx, tx, dt, rec)
	if err != nil {
		return nil, err
	}
	var out []types.UserID
	for _, u := range creators {
		if u == rec.CreatedBy {
			continue
		}
		reason, err := e.targetRefusal(ctx, tx, dt, rec, creators, readers, u)
		if err != nil {
			return nil, err
		}
		if reason == "" {
			out = append(out, u)
		}
	}
	return out, nil
}

// targetLists returns the holders of create on dt and of read on rec, the
// target rights that don't depend on the target. Rights come from
// UsersWithRightTx, because CanTx reads the principal's roles.
func (e *engine) targetLists(ctx context.Context, tx *db.Tx, dt *DocType, rec *auth.RecordRef) (creators, readers []types.UserID, err error) {
	if creators, err = e.Auth.UsersWithRightTx(ctx, tx, dt.Name, auth.RightCreate, nil); err != nil {
		return nil, nil, err
	}
	if readers, err = e.Auth.UsersWithRightTx(ctx, tx, dt.Name, auth.RightRead, rec); err != nil {
		return nil, nil, err
	}
	return creators, readers, nil
}

// targetRefusal is why to can't own rec ("" when it can): disabled, not in
// creators, not in readers, or no write on a copy of rec owned by to. A to
// that doesn't exist is a caller bug.
func (e *engine) targetRefusal(ctx context.Context, tx *db.Tx, dt *DocType, rec *auth.RecordRef, creators, readers []types.UserID, to types.UserID) (ReassignReason, error) {
	u, found, err := e.Auth.User(ctx, tx, to)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("doc: reassign %s %d: no user %d", dt.Name, rec.ID, to)
	}
	switch {
	case !u.Enabled:
		return ReassignDisabled, nil
	case !slices.Contains(creators, to):
		return ReassignCantCreate, nil
	case !slices.Contains(readers, to):
		return ReassignCantSee, nil
	}
	owned := *rec
	owned.CreatedBy = to
	writers, err := e.Auth.UsersWithRightTx(ctx, tx, dt.Name, auth.RightWrite, &owned)
	if err != nil {
		return "", err
	}
	if !slices.Contains(writers, to) {
		return ReassignCantCreate, nil
	}
	return "", nil
}

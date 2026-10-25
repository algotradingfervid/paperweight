package approval

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"github.com/vardhan/internaltools/internal/kernel/approval/dbq"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// snapshot is what eligibility needs, built from a Requirement at submit or from a stored Request later.
type snapshot struct {
	doctype   string
	roles     []string
	manager   types.UserID
	allowSelf bool
	own       []types.UserID
}

// recordRef is the auth view of a document.
func recordRef(in doctype.Info) *auth.RecordRef {
	return &auth.RecordRef{
		DocType:    in.DocType,
		ID:         in.ID,
		CreatedBy:  in.CreatedBy,
		EmployeeID: in.EmployeeID,
		ProjectIDs: in.ProjectIDs,
		SiteIDs:    in.SiteIDs,
	}
}

// own lists the users a document belongs to: the requester, its creator and
// the claimant's user (when the employee has one). Zeros are dropped.
func (a *Approvals) own(ctx context.Context, tx *db.Tx, requester types.UserID, in doctype.Info) ([]types.UserID, error) {
	ids := []types.UserID{requester, in.CreatedBy}
	if in.EmployeeID != 0 {
		u, ok, err := a.dir.UserForEmployee(ctx, tx, in.EmployeeID)
		if err != nil {
			return nil, err
		}
		if ok {
			ids = append(ids, u)
		}
	}
	return slices.DeleteFunc(ids, func(u types.UserID) bool { return u == 0 }), nil
}

// eligible lists, sorted by user ID, the users who may approve the document
// now: they hold approve and read on it (auth applies disabled users, scope
// and restrictions), hold one of the snapshot's roles or are its manager, and
// aren't one of its owners unless self-approval is allowed.
func (a *Approvals) eligible(ctx context.Context, s snapshot, in doctype.Info) ([]types.UserID, error) {
	ref := recordRef(in)
	approvers, err := a.auth.UsersWithRight(ctx, s.doctype, "approve", ref)
	if err != nil {
		return nil, err
	}
	readers, err := a.auth.UsersWithRight(ctx, s.doctype, "read", ref)
	if err != nil {
		return nil, err
	}
	out := []types.UserID{}
	for _, u := range approvers {
		if !slices.Contains(readers, u) || slices.Contains(out, u) {
			continue
		}
		if !s.allowSelf && slices.Contains(s.own, u) {
			continue
		}
		ok, err := a.holdsAny(ctx, u, s)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, u)
		}
	}
	slices.Sort(out)
	return out, nil
}

// holdsAny reports whether u is the snapshot's manager or holds one of its roles.
func (a *Approvals) holdsAny(ctx context.Context, u types.UserID, s snapshot) (bool, error) {
	if s.manager != 0 && u == s.manager {
		return true, nil
	}
	for _, r := range s.roles {
		ok, err := a.auth.HasRole(ctx, u, r)
		if err != nil || ok {
			return ok, err
		}
	}
	return false, nil
}

// EligibleApprovers lists who may approve the document's open request now,
// sorted by user ID; [] when no request is open.
func (a *Approvals) EligibleApprovers(ctx context.Context, tx *db.Tx, doctype string, id int64) ([]types.UserID, error) {
	r, err := dbq.New(tx.SQL()).GetOpenRequest(ctx, dbq.GetOpenRequestParams{DocType: doctype, DocID: id})
	if errors.Is(err, sql.ErrNoRows) {
		return []types.UserID{}, nil
	}
	if err != nil {
		return nil, err
	}
	s := snapshot{doctype: doctype, manager: types.UserID(r.ManagerUser.Int64), allowSelf: r.AllowSelf != 0}
	if err := json.Unmarshal([]byte(r.ApproverRoles), &s.roles); err != nil {
		return nil, err
	}
	for _, u := range []int64{r.Requester, r.Creator, r.ClaimantUser.Int64} {
		if u != 0 {
			s.own = append(s.own, types.UserID(u))
		}
	}
	in, err := a.info.Info(ctx, tx, doctype, id)
	if err != nil {
		return nil, err
	}
	return a.eligible(ctx, s, in)
}

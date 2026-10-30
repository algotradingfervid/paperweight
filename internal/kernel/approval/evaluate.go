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

// Evaluate decides whether the document needs approval at amount and, if so,
// under which rule and by whom. It refuses a submit no one could approve.
func (a *Approvals) Evaluate(ctx context.Context, tx *db.Tx, dt string, id int64, amount types.Money, requester auth.Principal) (Requirement, error) {
	in, err := a.info.Info(ctx, tx, dt, id)
	if err != nil {
		return Requirement{}, err
	}
	rows, err := dbq.New(tx.SQL()).ListEnabledRulesForDocType(ctx, dt)
	if err != nil {
		return Requirement{}, err
	}
	rules := make([]Rule, 0, len(rows))
	for _, r := range rows {
		rules = append(rules, ruleOf(r))
	}
	matched, higher, ok := match(rules, amount)
	if !ok {
		return Requirement{Amount: amount, Requester: requester.ID}, nil
	}
	own, err := a.own(ctx, tx, requester.ID, in)
	if err != nil {
		return Requirement{}, err
	}
	var manager types.UserID
	if matched.ApproverType == ApproverManager {
		if manager, err = a.manager(ctx, tx, in, own); err != nil {
			return Requirement{}, err
		}
	}
	req := Requirement{
		Needed:        true,
		RuleID:        matched.ID,
		FromAmount:    matched.FromAmount,
		ApproverRoles: approverRoles(matched, higher),
		ManagerUser:   manager,
		Amount:        amount,
		Requester:     requester.ID,
	}
	s := snapshot{doctype: dt, roles: req.ApproverRoles, manager: manager, allowSelf: matched.AllowSelfApproval, own: own}
	users, err := a.eligible(ctx, tx, s, in)
	if err != nil {
		return Requirement{}, err
	}
	if len(users) == 0 {
		return Requirement{}, noApprover(in.DocLabel, approverRole(matched))
	}
	return req, nil
}

// approverRole is the role that approves under r: its role, or its fallback
// role for a reporting-manager rule.
func approverRole(r Rule) string {
	if r.ApproverType == ApproverManager {
		return r.FallbackRole
	}
	return r.Role
}

// manager is the claimant's direct manager's user when that user exists, can
// read and approve the document and is not one of its owners; else 0.
func (a *Approvals) manager(ctx context.Context, tx *db.Tx, in doctype.Info, own []types.UserID) (types.UserID, error) {
	chain, err := a.dir.ManagerChain(ctx, tx, in.EmployeeID)
	if err != nil || len(chain) == 0 || chain[0] == in.EmployeeID {
		return 0, err
	}
	u, ok, err := a.dir.UserForEmployee(ctx, tx, chain[0])
	if err != nil || !ok || slices.Contains(own, u) {
		return 0, err
	}
	ref := recordRef(in)
	for _, right := range []auth.Right{"approve", "read"} {
		users, err := a.auth.UsersWithRightTx(ctx, tx, in.DocType, right, ref)
		if err != nil || !slices.Contains(users, u) {
			return 0, err
		}
	}
	return u, nil
}

// Open records "submitted" and, when approval is needed, opens the request
// with its snapshot. It reads only req's exported fields.
func (a *Approvals) Open(ctx context.Context, tx *db.Tx, dt string, id int64, req Requirement) error {
	if req.Requester == 0 {
		return ErrNotEvaluated
	}
	q := dbq.New(tx.SQL())
	var rule dbq.ApprovalRule
	if req.Needed {
		var err error
		rule, err = q.GetRule(ctx, req.RuleID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err != nil || rule.DocType != dt {
			return ErrNotEvaluated
		}
	}
	now := ts(a.clock.Now())
	requesterName, err := a.userName(ctx, tx, req.Requester)
	if err != nil {
		return err
	}
	h := dbq.InsertHistoryParams{
		DocType: dt, DocID: id, Action: HistSubmitted, UserID: int64(req.Requester), UserName: requesterName,
		At: now, Amount: sql.NullInt64{Int64: int64(req.Amount), Valid: true},
	}
	if req.Needed {
		reqID, err := a.insertRequest(ctx, tx, q, dt, id, req, rule, requesterName, now)
		if err != nil {
			return err
		}
		h.RequestID = sql.NullInt64{Int64: reqID, Valid: true}
		h.RuleID = sql.NullInt64{Int64: req.RuleID, Valid: true}
	}
	_, err = q.InsertHistory(ctx, h)
	return err
}

// insertRequest stores the open request with its snapshot.
func (a *Approvals) insertRequest(ctx context.Context, tx *db.Tx, q *dbq.Queries, dt string, id int64, req Requirement, rule dbq.ApprovalRule, requesterName, now string) (int64, error) {
	roles, err := json.Marshal(req.ApproverRoles)
	if err != nil {
		return 0, err
	}
	in, err := a.info.Info(ctx, tx, dt, id)
	if err != nil {
		return 0, err
	}
	var claimant sql.NullInt64
	if in.EmployeeID != 0 {
		u, ok, err := a.dir.UserForEmployee(ctx, tx, in.EmployeeID)
		if err != nil {
			return 0, err
		}
		claimant = sql.NullInt64{Int64: int64(u), Valid: ok}
	}
	var manager sql.NullInt64
	var managerName string
	if req.ManagerUser != 0 {
		manager = sql.NullInt64{Int64: int64(req.ManagerUser), Valid: true}
		if managerName, err = a.userName(ctx, tx, req.ManagerUser); err != nil {
			return 0, err
		}
	}
	return q.InsertRequest(ctx, dbq.InsertRequestParams{
		DocType: dt, DocID: id, RuleID: req.RuleID, RuleFromAmount: int64(req.FromAmount), Amount: int64(req.Amount), ApproverRoles: string(roles),
		ManagerUser: manager, ManagerName: managerName, AllowSelf: rule.AllowSelfApproval,
		Requester: int64(req.Requester), RequesterName: requesterName, Creator: int64(in.CreatedBy),
		ClaimantUser: claimant, SubmittedAt: now, CreatedBy: int64(req.Requester),
	})
}

// userName is u's display name ("" when the user is unknown).
func (a *Approvals) userName(ctx context.Context, tx *db.Tx, u types.UserID) (string, error) {
	info, _, err := a.auth.User(ctx, tx, u)
	return info.Name, err
}

// LatestRequest is the document's most recent approval request; false when
// it has none.
func (a *Approvals) LatestRequest(ctx context.Context, tx *db.Tx, dt string, id int64) (Request, bool, error) {
	row, err := dbq.New(tx.SQL()).GetLatestRequest(ctx, dbq.GetLatestRequestParams{DocType: dt, DocID: id})
	if errors.Is(err, sql.ErrNoRows) {
		return Request{}, false, nil
	}
	if err != nil {
		return Request{}, false, err
	}
	r, err := requestOf(row)
	return r, err == nil, err
}

func requestOf(row dbq.ApprovalRequest) (Request, error) {
	r := Request{
		ID: row.ID, RuleID: row.RuleID, FromAmount: types.Money(row.RuleFromAmount), DocType: row.DocType, DocID: row.DocID, Amount: types.Money(row.Amount),
		ManagerUser: types.UserID(row.ManagerUser.Int64), ManagerName: row.ManagerName, AllowSelf: row.AllowSelf != 0,
		Requester: types.UserID(row.Requester), RequesterName: row.RequesterName, Creator: types.UserID(row.Creator),
		Claimant: types.UserID(row.ClaimantUser.Int64), Outcome: row.Outcome.String,
		DecidedBy: types.UserID(row.DecidedBy.Int64), DecidedByName: row.DecidedByName, Reason: row.Reason,
	}
	if err := json.Unmarshal([]byte(row.ApproverRoles), &r.ApproverRoles); err != nil {
		return Request{}, err
	}
	var err error
	if r.SubmittedAt, err = parseTS(row.SubmittedAt); err != nil {
		return Request{}, err
	}
	if row.DecidedAt.Valid {
		if r.DecidedAt, err = parseTS(row.DecidedAt.String); err != nil {
			return Request{}, err
		}
	}
	return r, nil
}

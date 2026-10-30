package approval

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/approval/dbq"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Inbox lists the open requests p may approve now, oldest first (§5, CD-01).
// A request whose document can't be read is logged and skipped.
func (a *Approvals) Inbox(ctx context.Context, p auth.Principal, page types.Page) (types.Result[InboxItem], error) {
	res, _, err := a.InboxOfType(ctx, p, "", page)
	return res, err
}

// InboxOfType is Inbox narrowed to doctype ("" = every type; a type that isn't
// in the inbox gives no rows), with every type of p's whole inbox sorted by
// Label, whatever the filter.
func (a *Approvals) InboxOfType(ctx context.Context, p auth.Principal, doctype string, page types.Page) (types.Result[InboxItem], []InboxType, error) {
	var items []InboxItem
	err := a.db.Read(ctx, func(tx *db.Tx) error {
		rows, err := dbq.New(tx.SQL()).ListOpenRequests(ctx)
		if err != nil {
			return err
		}
		for _, row := range rows {
			r, err := requestOf(row)
			if err != nil {
				return err
			}
			s := snapshotOf(r)
			holds, err := a.holdsAny(ctx, tx, p.ID, s)
			if err != nil {
				return err
			}
			if !holds {
				continue
			}
			in, ok := a.resolve(ctx, tx, r)
			if !ok {
				continue
			}
			ok, err = a.canApprove(ctx, tx, p, r, s, in)
			if err != nil {
				return err
			}
			if ok {
				items = append(items, inboxItem(r, in))
			}
		}
		return nil
	})
	if err != nil {
		return types.Result[InboxItem]{}, nil, err
	}
	all := inboxTypes(items)
	if doctype != "" {
		items = slices.DeleteFunc(items, func(it InboxItem) bool { return it.DocType != doctype })
	}
	return pageOf(items, page), all, nil
}

// inboxTypes counts items by type, sorted by Label.
func inboxTypes(items []InboxItem) []InboxType {
	var out []InboxType
	for _, it := range items {
		i := slices.IndexFunc(out, func(t InboxType) bool { return t.DocType == it.DocType })
		if i < 0 {
			i = len(out)
			out = append(out, InboxType{DocType: it.DocType, Label: it.Label})
		}
		out[i].Count++
	}
	slices.SortStableFunc(out, func(a, b InboxType) int { return strings.Compare(a.Label, b.Label) })
	return out
}

// MyRequests lists the latest request of each document p submitted, newest
// first, with the document's current title and state.
func (a *Approvals) MyRequests(ctx context.Context, p auth.Principal, page types.Page) (types.Result[RequestItem], error) {
	var items []RequestItem
	err := a.db.Read(ctx, func(tx *db.Tx) error {
		rows, err := dbq.New(tx.SQL()).ListLatestRequestsByRequester(ctx, int64(p.ID))
		if err != nil {
			return err
		}
		for _, row := range rows {
			r, err := requestOf(row)
			if err != nil {
				return err
			}
			in, ok := a.resolve(ctx, tx, r)
			if !ok {
				continue
			}
			items = append(items, RequestItem{
				InboxItem: inboxItem(r, in), Outcome: r.Outcome, DecidedByName: r.DecidedByName,
				Reason: r.Reason, DecidedAt: r.DecidedAt, State: string(in.State),
			})
		}
		return nil
	})
	if err != nil {
		return types.Result[RequestItem]{}, err
	}
	return pageOf(items, page), nil
}

// Stuck lists, oldest first, the open requests no one can approve now
// (Administrators only). A request whose document can't be read is skipped.
func (a *Approvals) Stuck(ctx context.Context, p auth.Principal) ([]StuckItem, error) {
	ok, err := a.auth.HasRole(ctx, p.ID, "Administrator")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, notAdminStuck()
	}
	var items []StuckItem
	err = a.db.Read(ctx, func(tx *db.Tx) error {
		rows, err := dbq.New(tx.SQL()).ListOpenRequests(ctx)
		if err != nil {
			return err
		}
		for _, row := range rows {
			r, err := requestOf(row)
			if err != nil {
				return err
			}
			in, ok := a.resolve(ctx, tx, r)
			if !ok {
				continue
			}
			users, err := a.eligible(ctx, tx, snapshotOf(r), in)
			if err != nil {
				return err
			}
			if len(users) == 0 {
				items = append(items, StuckItem{
					InboxItem: inboxItem(r, in),
					Message:   "No active approver: needs " + joinRoles(r.ApproverRoles) + ".",
				})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

// Status is the document's approval line, from its last history row.
func (a *Approvals) Status(ctx context.Context, tx *db.Tx, dt string, id int64) (Status, error) {
	last, err := dbq.New(tx.SQL()).LastHistory(ctx, dbq.LastHistoryParams{DocType: dt, DocID: id})
	if errors.Is(err, sql.ErrNoRows) {
		return Status{}, nil
	}
	if err != nil {
		return Status{}, err
	}
	at, err := parseTS(last.At)
	if err != nil {
		return Status{}, err
	}
	switch last.Action {
	case HistApproved:
		return Status{Line: fmt.Sprintf("Approved by %s on %s", last.UserName, types.FormatIST(at))}, nil
	case HistAutoApproved:
		return Status{Line: "Approved on submit (no approval needed)"}, nil
	case HistRejected:
		return Status{Line: fmt.Sprintf("Rejected by %s on %s: %s", last.UserName, types.FormatIST(at), last.Reason)}, nil
	case HistWithdrawn:
		return Status{Line: fmt.Sprintf("Withdrawn by %s on %s", last.UserName, types.FormatIST(at))}, nil
	case HistRevised:
		r, ok, err := a.LatestRequest(ctx, tx, dt, id)
		if err != nil || !ok || r.Outcome != OutcomeRejected {
			return Status{}, err
		}
		return Status{Line: fmt.Sprintf("Last rejected by %s: %s", r.DecidedByName, r.Reason)}, nil
	case HistSubmitted:
		r, ok, err := a.openRequest(ctx, tx, dt, id)
		if err != nil || !ok {
			return Status{}, err
		}
		who := joinRoles(r.ApproverRoles)
		if r.ManagerName != "" {
			who = r.ManagerName + " or " + who
		}
		return Status{Line: "Waiting for approval from " + who, Pending: true, WaitingOn: who}, nil
	}
	return Status{}, nil
}

// resolve reads r's document; on error it logs and reports false (Review Focus 4).
func (a *Approvals) resolve(ctx context.Context, tx *db.Tx, r Request) (doctype.Info, bool) {
	in, err := a.info.Info(ctx, tx, r.DocType, r.DocID)
	if err != nil {
		slog.Warn("approval: skipping request whose document can't be read",
			"request", r.ID, "doctype", r.DocType, "id", r.DocID, "err", err)
		return doctype.Info{}, false
	}
	return in, true
}

// canApprove reports whether p, who holds one of s's approver slots, may
// approve r's document now: p can read it and is one of its eligible
// approvers (CD-11, CD-52).
func (a *Approvals) canApprove(ctx context.Context, tx *db.Tx, p auth.Principal, r Request, s snapshot, in doctype.Info) (bool, error) {
	if err := a.auth.CanTx(ctx, tx, p, r.DocType, "read", recordRef(in)); err != nil {
		if errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	users, err := a.eligible(ctx, tx, s, in)
	if err != nil {
		return false, err
	}
	return slices.Contains(users, p.ID), nil
}

func inboxItem(r Request, in doctype.Info) InboxItem {
	return InboxItem{
		RequestID: r.ID, DocType: r.DocType, Label: in.DocLabel, DocID: r.DocID,
		Title: in.Display, Party: in.Party, Amount: r.Amount,
		SubmittedBy: r.Requester, SubmittedByName: r.RequesterName, SubmittedAt: r.SubmittedAt,
		Why: WhyText(r.FromAmount),
	}
}

// pageOf cuts one page from all; a zero page is Number 1, Size 25.
func pageOf[T any](all []T, page types.Page) types.Result[T] {
	if page.Number < 1 {
		page.Number = 1
	}
	if page.Size < 1 {
		page.Size = 25
	}
	res := types.Result[T]{Total: len(all)}
	if page.Number-1 >= (len(all)+page.Size-1)/page.Size {
		return res
	}
	start := (page.Number - 1) * page.Size
	res.Rows = all[start:min(start+page.Size, len(all))]
	return res
}

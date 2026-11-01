package purchase

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// RecentSize is how many documents the home's Recent shows (UI v2 §5.12).
const RecentSize = 5

// HomeRow is one document on the home's Recent or Waiting for you: its
// DocType and id, Title (DOC's display), Party (DOC's party, "" when none),
// Total (the PO's rounded total; 0 for a request), State, and Reason (the
// PO's state reason, read by Waiting only; the home shows it on rejected
// drafts).
type HomeRow struct {
	DocType      string
	ID           int64
	Title, Party string
	Total        types.Money
	State        doctype.State
	Reason       string
}

// Recent is p's own purchase orders and requests, latest change first, at
// most RecentSize, keeping only those p's read scope allows (the lists'
// Auth.Scope), so a document p can no longer read is never named.
func (s *Service) Recent(ctx context.Context, p auth.Principal) ([]HomeRow, error) {
	scopes := map[string]auth.Predicate{}
	for _, dt := range []string{DocType, RequestDocType} {
		pred, _, err := s.scope(ctx, p, dt, auth.RightRead)
		if err != nil {
			return nil, err
		}
		scopes[dt] = pred
	}
	var out []HomeRow
	err := s.d.DB.Read(ctx, func(tx *db.Tx) error {
		inOrder, err := scopeIn(ctx, tx, "purchase_order", scopes[DocType])
		if err != nil {
			return err
		}
		inRequest, err := scopeIn(ctx, tx, "purchase_request", scopes[RequestDocType])
		if err != nil {
			return err
		}
		rows, err := dbq.New(tx.SQL()).HomeRecent(ctx, int64(p.ID))
		if err != nil {
			return err
		}
		for _, r := range rows {
			in := inOrder
			if r.Doctype == RequestDocType {
				in = inRequest
			}
			if !in(r.ID) {
				continue
			}
			info, err := s.d.Engine.Info(ctx, tx, r.Doctype, r.ID)
			if err != nil {
				return err
			}
			out = append(out, HomeRow{
				DocType: r.Doctype, ID: r.ID, Title: info.Display, Party: info.Party, Total: types.Money(r.Total), State: doctype.State(r.State),
			})
			if len(out) == RecentSize {
				break
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Waiting is Waiting for you's rows beside the approvals (UI v2 §5.12,
// CD-368 (1)): changes, p's rejected drafts, and toSend, the approved orders
// p may mark sent, each read exactly as its queue (My drafts, To send) with
// that queue's Auth.Scope, in the queue's order. Without a queue's right its
// rows are empty.
func (s *Service) Waiting(ctx context.Context, p auth.Principal) (changes, toSend []HomeRow, err error) {
	for _, q := range []struct {
		name string
		out  *[]HomeRow
	}{{QueueDrafts, &changes}, {QueueToSend, &toSend}} {
		// Without the queue's right, source's lister reads with the
		// match-nothing predicate, so its rows are empty.
		list, _, err := s.source(ctx, p, q.name)
		if err != nil {
			return nil, nil, err
		}
		err = s.d.DB.Read(ctx, func(tx *db.Tx) error {
			l, err := list(ctx, tx)
			if err != nil {
				return err
			}
			for _, r := range l.rows {
				if q.name == QueueDrafts && r.State != doctype.StateRejected {
					continue
				}
				info, err := s.d.Engine.Info(ctx, tx, r.DocType, r.ID)
				if err != nil {
					return err
				}
				reason, err := dbq.New(tx.SQL()).HomeReason(ctx, r.ID)
				if err != nil {
					return err
				}
				*q.out = append(*q.out, HomeRow{DocType: r.DocType, ID: r.ID, Title: info.Display, Party: info.Party, Total: r.Total, State: r.State, Reason: reason})
			}
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
	}
	return changes, toSend, nil
}

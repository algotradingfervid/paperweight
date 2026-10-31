package purchase

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/modkit"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Card keys that have no queue of their own (spec §25). The other cards use
// their queue's name (QueueDrafts and the rest) as their Key.
const (
	CardToApprove = "to-approve"
	CardOverdue   = "overdue"
)

// Card is one Purchase home card (spec §25, UX §5.1).
//
//   - Key is CardToApprove, CardOverdue or the card's queue name.
//   - Count is the card's figure, read through Auth.Scope (AUTH-110).
//   - Href is where its Open link goes: "/modules/purchase/q/{queue}",
//     "/approvals" for To approve, "/reports/purchase/pending?overdue=1" for
//     Overdue deliveries.
//   - NeedsChanges (My drafts only) is how many of Count are rejected.
//   - WaitingDays (Waiting for approval only) is the oldest pending PO's
//     Approvals.WaitingDays (0 before day 3).
type Card struct {
	Key          string
	Count        int
	Href         string
	NeedsChanges int
	WaitingDays  int
}

// HomeView is the Purchase home. Cards holds every card p has the right
// for, in UX §5.1's order (To approve, My drafts, To send, To close, Requests
// to order, Waiting for approval, My requests, Awaiting delivery, Overdue
// deliveries), zero counts included: the page hides those. Comments is
// Comments.RecentComments for p (spec §22, CD-262).
type HomeView struct {
	Cards    []Card
	Comments []modkit.Comment
}

// Card links (UX §5.1).
const (
	hrefQueue   = "/modules/purchase/q/"
	hrefApprove = "/approvals"
	hrefOverdue = "/reports/purchase/pending?overdue=1"
)

// homeQueues are the cards with a queue, in UX §5.1's order; To approve
// comes before them and Overdue deliveries after.
var homeQueues = []string{QueueDrafts, QueueToSend, QueueToClose, QueueRequestsToOrder, QueueWaiting, QueueMyRequests, QueueAwaitingDelivery}

// Home is p's Purchase home: every card p has the right for, with its count
// through Auth.Scope, and p's recent comments (spec §25).
func (s *Service) Home(ctx context.Context, p auth.Principal) (HomeView, error) {
	var v HomeView
	if _, ok, err := s.scope(ctx, p, DocType, auth.RightApprove); err != nil {
		return HomeView{}, err
	} else if ok {
		inbox, _, err := s.d.Approvals.InboxOfType(ctx, p, DocType, types.Page{})
		if err != nil {
			return HomeView{}, err
		}
		v.Cards = append(v.Cards, Card{Key: CardToApprove, Count: inbox.Total, Href: hrefApprove})
	}
	for _, name := range homeQueues {
		list, ok, err := s.source(ctx, p, name)
		if err != nil {
			return HomeView{}, err
		}
		if !ok {
			continue
		}
		var l queueList
		if err := s.d.DB.Read(ctx, func(tx *db.Tx) error {
			l, err = list(ctx, tx)
			return err
		}); err != nil {
			return HomeView{}, err
		}
		c := Card{Key: name, Count: len(l.rows), Href: hrefQueue + name, WaitingDays: l.oldestDays}
		for _, r := range l.rows {
			if r.State == doctype.StateRejected {
				c.NeedsChanges++
			}
		}
		v.Cards = append(v.Cards, c)
	}
	if _, ok, err := s.scope(ctx, p, DocType, auth.RightRead); err != nil {
		return HomeView{}, err
	} else if ok {
		n, err := s.OverdueCount(ctx, p)
		if err != nil {
			return HomeView{}, err
		}
		v.Cards = append(v.Cards, Card{Key: CardOverdue, Count: n, Href: hrefOverdue})
	}
	comments, err := s.d.Comments.RecentComments(ctx, p)
	if err != nil {
		return HomeView{}, err
	}
	v.Comments = comments
	return v, nil
}

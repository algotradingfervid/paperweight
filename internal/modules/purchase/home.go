package purchase

import (
	"context"
	"errors"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/modules/modkit"
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

// errHomeNotBuilt is the P14 stub's error.
var errHomeNotBuilt = errors.New("purchase: Home not implemented (P14)")

// Home is p's Purchase home: every card's count (spec §25). P14 builds it.
func (s *Service) Home(_ context.Context, _ auth.Principal) (HomeView, error) {
	return HomeView{}, errHomeNotBuilt
}

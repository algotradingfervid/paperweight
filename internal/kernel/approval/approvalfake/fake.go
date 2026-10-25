// Package approvalfake is an in-memory approval.Service for tests that checks
// the CD-23 call protocol per (doctype, id).
package approvalfake

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

var _ approval.Service = (*Fake)(nil)

// ErrProtocol is wrapped by every error for a call out of the CD-23 order (WP0 addition).
var ErrProtocol = errors.New("approvalfake: call out of the CD-23 protocol order")

// Call is one recorded Service call; Method is the Go method name.
type Call struct {
	Method  string
	DocType string
	ID      int64
	Action  doctype.Action
	By      auth.UserID
	Reason  string
}

type docKey struct {
	doctype string
	id      int64
}

type Fake struct {
	mu        sync.Mutex
	reqs      map[string]approval.Requirement
	approvers map[auth.UserID]bool
	inbox     map[auth.UserID][]approval.InboxItem
	open      map[docKey]bool
	lastClose map[docKey]doctype.Action
	calls     []Call
}

func New() *Fake {
	return &Fake{
		reqs:      map[string]approval.Requirement{},
		approvers: map[auth.UserID]bool{},
		inbox:     map[auth.UserID][]approval.InboxItem{},
		open:      map[docKey]bool{},
		lastClose: map[docKey]doctype.Action{},
	}
}

// SetRequirement sets what Evaluate returns for doctype (default Needed: false).
func (f *Fake) SetRequirement(doctype string, r approval.Requirement) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r.ApproverRoles = slices.Clone(r.ApproverRoles)
	f.reqs[doctype] = r
}

// AllowApprover lets u pass CheckApprover.
func (f *Fake) AllowApprover(u auth.UserID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.approvers[u] = true
}

func (f *Fake) AddInboxItem(u auth.UserID, it approval.InboxItem) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inbox[u] = append(f.inbox[u], it)
}

// Calls returns the recorded calls in order.
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// Evaluate returns the SetRequirement value with Amount and Requester filled in.
func (f *Fake) Evaluate(ctx context.Context, tx *db.Tx, doctype string, id int64, amount types.Money, requester auth.Principal) (approval.Requirement, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, Call{Method: "Evaluate", DocType: doctype, ID: id, By: requester.ID})
	r := f.reqs[doctype]
	r.ApproverRoles = slices.Clone(r.ApproverRoles)
	r.Amount = amount
	r.Requester = requester.ID
	return r, nil
}

// Open opens a request. A second Open while one is open, or an Open straight
// after a reject (revise must come first), is a protocol error.
func (f *Fake) Open(ctx context.Context, tx *db.Tx, dt string, id int64, req approval.Requirement) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, Call{Method: "Open", DocType: dt, ID: id, By: req.Requester})
	k := docKey{dt, id}
	if f.open[k] {
		return fmt.Errorf("Open on %s %d while a request is already open: %w", dt, id, ErrProtocol)
	}
	if f.lastClose[k] == doctype.ActionReject {
		return fmt.Errorf("Open on %s %d after a reject without a revise: %w", dt, id, ErrProtocol)
	}
	f.open[k] = true
	delete(f.lastClose, k)
	return nil
}

func (f *Fake) CheckApprover(ctx context.Context, tx *db.Tx, p auth.Principal, doctype string, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, Call{Method: "CheckApprover", DocType: doctype, ID: id, By: p.ID})
	if !f.approvers[p.ID] {
		return fmt.Errorf("You can't approve %s %d. Ask an approver to review it: %w", doctype, id, auth.ErrForbidden)
	}
	return nil
}

// Record accepts approve, reject and withdraw only on an open request (each
// closes it), and revise only right after a reject.
func (f *Fake) Record(ctx context.Context, tx *db.Tx, dt string, id int64, action doctype.Action, p auth.Principal, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, Call{Method: "Record", DocType: dt, ID: id, Action: action, By: p.ID, Reason: reason})
	k := docKey{dt, id}
	switch action {
	case doctype.ActionApprove, doctype.ActionReject, doctype.ActionWithdraw:
		if !f.open[k] {
			return fmt.Errorf("Record(%s) on %s %d with no open request: %w", action, dt, id, ErrProtocol)
		}
		f.open[k] = false
		f.lastClose[k] = action
	case doctype.ActionRevise:
		if f.lastClose[k] != doctype.ActionReject {
			return fmt.Errorf("Record(revise) on %s %d not right after a reject: %w", dt, id, ErrProtocol)
		}
		delete(f.lastClose, k)
	default:
		return fmt.Errorf("Record(%s) on %s %d: not an approval action: %w", action, dt, id, ErrProtocol)
	}
	return nil
}

// Inbox pages the items added for p with AddInboxItem (page numbers start at 1; Size 0 = all).
func (f *Fake) Inbox(ctx context.Context, p auth.Principal, page types.Page) (types.Result[approval.InboxItem], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	items := f.inbox[p.ID]
	res := types.Result[approval.InboxItem]{Total: len(items)}
	if page.Size <= 0 {
		res.Rows = slices.Clone(items)
		return res, nil
	}
	start := (max(page.Number, 1) - 1) * page.Size
	if start < len(items) {
		res.Rows = slices.Clone(items[start:min(start+page.Size, len(items))])
	}
	return res, nil
}

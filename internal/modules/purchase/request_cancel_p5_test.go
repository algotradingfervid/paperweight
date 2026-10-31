package purchase_test

import (
	"errors"
	"fmt"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// PO-056 (PO spec §20.3, §26; 03-document.md DOC-052, DOC-053; LinkSpec
// request_line_id): Suresh's cancel of a submitted request is refused while
// a pending, approved or closed PO links one of its rows, with DOC-052's
// text naming that PO and its state; the request stays submitted at its
// version and nothing is written. Undo: once the pending PO is withdrawn, or
// the approved PO cancelled, the cancel goes through. A draft PO linking the
// row never blocks (DOC-053). The PO lines link through the builders'
// request_line_id, with P6's stubs in place.
func TestRequest_CancelBlocked(t *testing.T) {
	env := purchasetest.New(t, purchasetest.WithNoRules())
	// Every PO needs approval, so StopAtPending holds whatever the amount.
	if _, err := env.Approvals.CreateRule(t.Context(), env.Admin, approval.RuleInput{
		DocType: purchase.DocType, FromAmount: 0, ApproverType: approval.ApproverRole, Role: "Purchase Manager",
	}); err != nil {
		t.Fatalf("approval rule from 0: %v", err)
	}

	type stage struct {
		name  string
		build func(t *testing.T, line int64) doc.Doc // the PO linking request row line
		word  string                                 // DOC's state word; "" = doesn't block
		undo  func(t *testing.T, po doc.Doc)         // unblocks the cancel; nil = stays blocked
	}
	link := func(line int64) purchasetest.OrderOpt {
		return purchasetest.OrderLines(map[string]string{"request_line_id": strconv.FormatInt(line, 10)})
	}
	stages := []stage{
		{
			name: "pending",
			build: func(t *testing.T, line int64) doc.Doc {
				return purchasetest.NewOrder(t, env, purchasetest.StopAtPending(), link(line))
			},
			word: "waiting for approval",
			undo: func(t *testing.T, po doc.Doc) {
				p5Must(t, "Priya withdraws the PO")(p5DoOrder(t, env, env.Priya, po.ID, doctype.ActionWithdraw, ""))
			},
		},
		{
			name:  "approved",
			build: func(t *testing.T, line int64) doc.Doc { return purchasetest.NewOrder(t, env, link(line)) },
			word:  "approved",
			undo: func(t *testing.T, po doc.Doc) {
				p5Must(t, "Ravi cancels the PO")(p5DoOrder(t, env, env.Ravi, po.ID, doctype.ActionCancel, "Vendor can't supply"))
			},
		},
		{
			name: "closed",
			build: func(t *testing.T, line int64) doc.Doc {
				po := purchasetest.NewOrder(t, env, link(line))
				p5Must(t, "Priya closes the PO")(p5DoOrder(t, env, env.Priya, po.ID, doctype.ActionClose, "Rest not needed"))
				return p5Load(t, env, purchase.DocType, po.ID)
			},
			word: "closed",
		},
		{
			name: "draft",
			build: func(t *testing.T, line int64) doc.Doc {
				return purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), link(line))
			},
		},
	}
	for _, s := range stages {
		t.Run(s.name, func(t *testing.T) {
			req := purchasetest.NewRequest(t, env, purchasetest.RequestLines(map[string]string{}, map[string]string{"qty": "20"}))
			if req.State != doctype.StateApproved || req.Number == "" || len(req.Lines) != 2 {
				t.Fatalf("built request = %s %q with %d lines, want a submitted request with 2", req.State, req.Number, len(req.Lines))
			}
			po := s.build(t, req.Lines[1].ID)
			if got := p5Plain(po.Lines[0].Values["request_line_id"]); got != strconv.FormatInt(req.Lines[1].ID, 10) {
				t.Fatalf("PO %d line 1 request_line_id = %s, want %d", po.ID, got, req.Lines[1].ID)
			}

			if s.word != "" {
				before := p5CountAll(t, env)
				_, err := p5Do(t, env, env.Suresh, req.ID, doctype.ActionCancel, p5CancelReason)
				label := naming.Label(po.Number, po.ID)
				want := fmt.Sprintf(p5Blocked, req.Number, label, s.word, label)
				var de *doc.Error
				if !errors.As(err, &de) || de.Kind != doc.KindBlocked || de.Status() != 409 || err.Error() != want {
					t.Errorf("cancel with a %s PO: err = %v, want DOC's blocked (409) %q", s.name, err, want)
				}
				p5SameCounts(t, "blocked cancel", before, env)
				if d := p5Load(t, env, purchase.RequestDocType, req.ID); d.State != doctype.StateApproved || d.Version != req.Version {
					t.Errorf("after the blocked cancel the request is %s v%d, want approved v%d", d.State, d.Version, req.Version)
				}
				if s.undo == nil {
					return
				}
				s.undo(t, po)
			}
			res, err := p5Do(t, env, env.Suresh, req.ID, doctype.ActionCancel, p5CancelReason)
			if err != nil || res.State != doctype.StateCancelled {
				t.Fatalf("cancel with the %s PO out of the way: %v (%s), want cancelled", s.name, err, res.State)
			}
			if d := p5Load(t, env, purchase.RequestDocType, req.ID); d.State != doctype.StateCancelled || d.StateReason != p5CancelReason {
				t.Errorf("cancelled request is %s (%q), want cancelled with %q", d.State, d.StateReason, p5CancelReason)
			}
		})
	}
}

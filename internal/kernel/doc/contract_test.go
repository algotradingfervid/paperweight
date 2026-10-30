package doc_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doc/doctest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// TestContract_DocEngine runs the doc.Engine contract suite (WP3 Task 1,
// CD-182) against the real engine on fixture.Order, with DOC's doubles.
func TestContract_DocEngine(t *testing.T) {
	doctest.Run(t, func(t *testing.T) (doc.Engine, doctest.Setup) {
		h := fixturetest.New(t)
		s := &contractSetup{h: h, denied: map[contractRight]bool{}, hidden: map[contractDoc]bool{}}
		h.Approvals.Needed = func(string, types.Money) bool { return s.need }
		h.Auth.Deny = func(p auth.Principal, dt string, r auth.Right) bool { return s.denied[contractRight{p.ID, dt, r}] }
		h.Auth.Hide = func(p auth.Principal, dt string, id int64) bool { return s.hidden[contractDoc{p.ID, dt, id}] }
		return h.Engine, s
	})
}

type contractRight struct {
	user types.UserID
	dt   string
	r    auth.Right
}

type contractDoc struct {
	user types.UserID
	dt   string
	id   int64
}

type contractSetup struct {
	h      *fixturetest.H
	need   bool
	denied map[contractRight]bool
	hidden map[contractDoc]bool
}

func (s *contractSetup) DocType() string          { return fixture.Order }
func (s *contractSetup) Owner() auth.Principal    { return s.h.Priya }
func (s *contractSetup) Approver() auth.Principal { return s.h.Ravi }

func (s *contractSetup) EditField() (string, string) { return "remarks", "Deliver to gate 2" }

func (s *contractSetup) NewInput() doc.ActionInput {
	return doc.ActionInput{
		Values: map[string]string{
			"supplier_id": strconv.FormatInt(s.h.M.Party["SHREE"], 10),
			"project_id":  strconv.FormatInt(s.h.M.Project["PRJ-METRO"], 10),
			"order_date":  "2026-10-24",
			"amount":      "0",
			"remarks":     "Deliver to site office",
		},
		Lines: []map[string]string{{
			"item_id": strconv.FormatInt(s.h.M.Item["MS-ANGLE-50"], 10), "qty": "10.000", "rate": "62.5000",
		}},
	}
}

func (s *contractSetup) Read(ctx context.Context, fn func(tx *db.Tx) error) error {
	return s.h.DB.Read(ctx, fn)
}

func (s *contractSetup) NeedApproval(_ *testing.T, need bool) { s.need = need }

func (s *contractSetup) Deny(_ *testing.T, p auth.Principal, r auth.Right) {
	s.denied[contractRight{p.ID, fixture.Order, r}] = true
}

func (s *contractSetup) Hide(_ *testing.T, p auth.Principal, id int64) {
	s.hidden[contractDoc{p.ID, fixture.Order, id}] = true
}

func (s *contractSetup) BreakMaster(t *testing.T) doc.FieldError {
	t.Helper()
	fixture.SetDisabled(t, s.h.DB, "fixture_party", "SHREE", true)
	return doc.FieldError{Field: "supplier_id", Message: "Supplier SHREE Shree Steel Traders is disabled. Pick an active supplier, or ask an admin to enable it."}
}

// Doc reaches state st through the engine's own actions.
func (s *contractSetup) Doc(t *testing.T, st doc.State) int64 {
	t.Helper()
	h, ctx := s.h, t.Context()
	next := func(p auth.Principal, id int64, a doc.Action) {
		t.Helper()
		d := h.Load(t, fixture.Order, id)
		h.MustDo(t, ctx, p, fixture.Order, id, a, doc.ActionInput{Version: d.Version, Reason: doctest.Reason})
	}
	submit := func(id int64, need bool) {
		t.Helper()
		was := s.need
		s.need = need
		next(h.Priya, id, doc.ActionSubmit)
		s.need = was
	}
	id := h.MustDo(t, ctx, h.Priya, fixture.Order, 0, doc.ActionSave, s.NewInput()).ID
	switch st {
	case doc.StateDraft:
	case doc.StatePending:
		submit(id, true)
	case doc.StateRejected:
		submit(id, true)
		next(h.Ravi, id, doc.ActionReject)
	case doc.StateApproved:
		submit(id, false)
	case doc.StateClosed:
		submit(id, false)
		next(h.Priya, id, doc.ActionClose)
	case doc.StateCancelled:
		submit(id, false)
		next(h.Priya, id, doc.ActionCancel)
	default:
		t.Fatalf("contractSetup.Doc: unknown state %q", st)
	}
	return id
}

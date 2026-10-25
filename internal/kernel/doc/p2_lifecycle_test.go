package doc_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

// Plan Task 16 texts.
const (
	// DOC-036 (CD-38, CD-68).
	p2lMsgCementDisabled = "Line 1: Item CEMENT-OPC OPC Cement 53 Grade is disabled. Pick an active item, or ask an admin to enable it."
	// DOC-067.
	p2lMsgWireDisabled = "Line 2: Item WIRE-18G Binding Wire 18G is disabled. Pick an active item, or ask an admin to enable it."
	// DOC-101: the masters' own reasons, shown as given (line 2 prefixed).
	p2lShreeBlocked     = "Shree Steel Traders is on hold. Ask accounts before ordering."
	p2lWireBlocked      = "Binding Wire 18G is blocked for purchase."
	p2lMsgWireBlockedL2 = "Line 2: Binding Wire 18G is blocked for purchase."
	// DOC-075 (CD-66): db.ErrBusy's text.
	p2lBusyText = "The system is busy. Please try again in a moment."
	// Header-level missing source: the field's Label.
	p2lMsgHeadMissing = "Head document not found. Pick one from the list."

	p2lCancelReason = "Goods returned to the supplier"
	p2lCloseReason  = "Balance not needed"
	p2lRejectReason = "Item disabled"

	// checkBlockers, two or more: X, count, the comma-joined list.
	p2lMsgBlockedMany = "Can't cancel %s because %d documents are linked to it. Cancel them first: %s"
	// The tail after more than 10 blockers (U+2026).
	p2lMsgBlockedMore = " …and %d more."
	// A blocker entry: display name, state word.
	p2lMsgBlockerEntry = "%s (%s)"
	// GR number n.
	p2lGRFmt = "GR/26-27/%05d"
)

// Header-level link DocTypes, test-only: p2l.Child's header field head_id
// holds a p2l.Head id (LinkSpec without OnLine). Their tables are created by
// p2lHeaderTables in the tests that use them; no other DocType links to them.
const (
	p2lHead  = "p2l.Head"
	p2lChild = "p2l.Child"
)

// p2l.Head and p2l.Child are registered for the whole doc test binary, but their tables exist only inside TestDoc_Link_HeaderLevel; a test that queries every registered table would need them.
var _ = func() bool {
	doc.Register(doc.DocType{
		Name: p2lHead, Label: "Head document", PluralLabel: "Head documents",
		Table: "p2l_head", LinesTable: "p2l_head_line", Submittable: true, Hooks: p2lHooks{},
		Fields: []doc.Field{
			{Name: "title", Label: "Title", Type: doc.Text},
			{Name: "head_note", Label: "Note", Type: doc.Text, OnLine: true},
		},
	})
	doc.Register(doc.DocType{
		Name: p2lChild, Label: "Child document", PluralLabel: "Child documents",
		Table: "p2l_child", LinesTable: "p2l_child_line", Submittable: true, Hooks: p2lHooks{},
		Fields: []doc.Field{
			{Name: "head_id", Label: "Head document", Type: doc.Int, Required: true},
			{Name: "child_note", Label: "Note", Type: doc.Text, OnLine: true},
		},
		Links: []doctype.LinkSpec{{Field: "head_id", Target: p2lHead}},
	})
	return true
}()

// p2lHooks are no-op hooks, so the engine treats the p2l DocTypes as documents.
type p2lHooks struct{}

func (p2lHooks) Validate(context.Context, *db.Tx, int64) []doctype.FieldError { return nil }
func (p2lHooks) BeforeSubmit(context.Context, *db.Tx, int64) error            { return nil }
func (p2lHooks) ApplyEffects(context.Context, *db.Tx, int64) error            { return nil }
func (p2lHooks) ReverseEffects(context.Context, *db.Tx, int64) error          { return nil }
func (p2lHooks) Amount(context.Context, *db.Tx, int64) (types.Money, error)   { return 0, nil }
func (p2lHooks) Date(context.Context, *db.Tx, int64) (types.Date, error) {
	return types.Date{Year: 2026, Month: time.October, Day: 24}, nil
}

// p2lHeaderTables creates the p2l tables (the document table convention) on h's database.
func p2lHeaderTables(t *testing.T, h *fixturetest.H) {
	t.Helper()
	for _, s := range []struct{ table, col string }{{"p2l_head", "title"}, {"p2l_child", "head_id"}} {
		colType := "TEXT NOT NULL DEFAULT ''"
		if s.col == "head_id" {
			colType = "INTEGER"
		}
		h.Exec(t, `CREATE TABLE `+s.table+` (
			id INTEGER PRIMARY KEY, number TEXT UNIQUE, state TEXT NOT NULL DEFAULT 'draft',
			state_reason TEXT NOT NULL DEFAULT '', amended_from_id INTEGER, amendment_root_id INTEGER,
			amendment_no INTEGER NOT NULL DEFAULT 0, version INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL, created_by INTEGER NOT NULL, updated_at TEXT NOT NULL, updated_by INTEGER NOT NULL,
			`+s.col+` `+colType+`)`)
		note := strings.TrimPrefix(s.table, "p2l_") + "_note"
		h.Exec(t, `CREATE TABLE `+s.table+`_line (
			id INTEGER PRIMARY KEY, parent_id INTEGER NOT NULL REFERENCES `+s.table+` (id) ON DELETE CASCADE,
			line_no INTEGER NOT NULL, `+note+` TEXT NOT NULL DEFAULT '')`)
	}
}

// p2lLinkedBlockers runs doc.LinkedBlockers in a read transaction.
func p2lLinkedBlockers(t *testing.T, h *fixturetest.H, dt string, id int64) []doc.Blocker {
	t.Helper()
	var got []doc.Blocker
	err := h.DB.Read(t.Context(), func(tx *db.Tx) (err error) {
		got, err = doc.LinkedBlockers(t.Context(), tx, dt, id)
		return err
	})
	if err != nil {
		t.Fatalf("LinkedBlockers(%s, %d): %v", dt, id, err)
	}
	return got
}

// p2lGR is GR number n.
func p2lGR(n int) string { return fmt.Sprintf(p2lGRFmt, n) }

// p2lBlockedMany is checkBlockers' message for order x with the listed
// blockers (at most 10) out of n.
func p2lBlockedMany(x string, n int, listed []string) string {
	msg := fmt.Sprintf(p2lMsgBlockedMany, x, n, strings.Join(listed, ", "))
	if n > 10 {
		return msg + fmt.Sprintf(p2lMsgBlockedMore, n-10)
	}
	return msg + "."
}

// p2lEntry is one listed blocker.
func p2lEntry(display, word string) string { return fmt.Sprintf(p2lMsgBlockerEntry, display, word) }

// p2lDrafts creates n draft receipts by Suresh of qty "1" each on order line ol.
func p2lDrafts(t *testing.T, h *fixturetest.H, ol int64, n int) []doc.Result {
	t.Helper()
	var out []doc.Result
	for range n {
		out = append(out, h.NewReceipt(t, h.Suresh, fixturetest.RL{OrderLineID: ol, Qty: "1"}))
	}
	return out
}

// p2lSubmit submits receipt r by Suresh (no rule, so it auto-approves) and
// fails unless it gets number want.
func p2lSubmit(t *testing.T, h *fixturetest.H, r doc.Result, want string) {
	t.Helper()
	res := h.MustDo(t, t.Context(), h.Suresh, fixture.Receipt, r.ID, doc.ActionSubmit, trIn(r.Version))
	if res.State != doc.StateApproved || res.Number != want {
		t.Fatalf("setup: receipt %d submitted is %s %q, want approved %s", r.ID, res.State, res.Number, want)
	}
}

// p2lMarker fails unless audit row e has the marker change field = id
// (Before "", CD-135 review note on markers).
func p2lMarker(t *testing.T, e audit.Entry, field string, id int64) {
	t.Helper()
	want := audit.Change{Field: field, Before: "", After: strconv.FormatInt(id, 10), Op: "set"}
	if !slices.Contains(e.Changes, want) {
		t.Errorf("%s row Changes = %+v, want it to contain %+v", e.Action, e.Changes, want)
	}
}

// p2lOpen opens another handle on h's database file (CD-08).
func p2lOpen(t *testing.T, opts ...db.Option) *db.DB {
	t.Helper()
	d, err := db.Open(testkit.DBPath(t), opts...)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

// p2lHoldWrite holds a write transaction on d for hold, starting it before it
// returns; the returned func waits for it to end and reports its error.
func p2lHoldWrite(t *testing.T, d *db.DB, hold time.Duration) func() {
	t.Helper()
	locked, done := make(chan struct{}), make(chan error, 1)
	go func() {
		done <- d.Write(context.Background(), func(tx *db.Tx) error {
			if _, err := tx.SQL().ExecContext(context.Background(),
				`UPDATE fixture_party SET name = name WHERE code = 'SHREE'`); err != nil {
				close(locked)
				return err
			}
			close(locked)
			time.Sleep(hold)
			return nil
		})
	}()
	<-locked
	return func() {
		t.Helper()
		if err := <-done; err != nil {
			t.Errorf("holding write: %v", err)
		}
	}
}

// DOC-018
func TestDoc_Cancel_LaterDocOfClosedSource(t *testing.T) {
	h := fixturetest.New(t)
	o, ol := ruOrder(t, h, "10.000")
	r1 := ruApproved(t, h, h.Ravi, fixturetest.RL{OrderLineID: ol[0], Qty: "6"})
	if r1.Number != lkGR {
		t.Fatalf("setup: R1 Number = %q, want %s", r1.Number, lkGR)
	}
	ruWantQty(t, h, ol[0], 6000, "R1 approved")
	o = h.Load(t, fixture.Order, o.ID)
	h.MustDo(t, t.Context(), h.Priya, fixture.Order, o.ID, doc.ActionClose, trIn(o.Version, p2lCloseReason))
	trState(t, h, fixture.Order, o.ID, doc.StateClosed, 0)

	at := time.Date(2026, 10, 24, 7, 30, 0, 0, time.UTC)
	h.Clock.SetTime(at)
	res, err := h.Do(t, t.Context(), h.Meena, fixture.Receipt, r1.ID, doc.ActionCancel, trIn(r1.Version, p2lCancelReason))
	if err != nil {
		t.Fatalf("cancel R1 against a closed order: %v", err)
	}
	if res.State != doc.StateCancelled {
		t.Errorf("Result.State = %q, want cancelled", res.State)
	}
	trState(t, h, fixture.Receipt, r1.ID, doc.StateCancelled, r1.Version+1)
	ruWantQty(t, h, ol[0], 0, "R1 cancelled")
	trState(t, h, fixture.Order, o.ID, doc.StateClosed, 0)

	effects := ruEffects(t, h, o.ID)
	if len(effects) != 2 {
		t.Fatalf("order effect rows = %d, want 2 (R1 approve, R1 cancel): %+v", len(effects), effects)
	}
	ruWantEffect(t, effects[1], h.Meena.ID,
		audit.Ref{DocType: fixture.Receipt, DocID: r1.ID, Number: lkGR, Action: "cancel"},
		[]audit.Change{{Field: "received_qty", LineNo: 1, LineID: ol[0], Before: "6000", After: "0", Op: "set"}})

	// The cancel audit row on R1 (owed by the Task 10 review).
	cancels := trActions(h.Audit(t, fixture.Receipt, r1.ID), "cancel")
	if len(cancels) != 1 {
		t.Fatalf("R1 cancel rows = %+v, want exactly one", cancels)
	}
	c := cancels[0]
	if c.From != string(doc.StateApproved) || c.To != string(doc.StateCancelled) {
		t.Errorf("cancel row %q -> %q, want approved -> cancelled", c.From, c.To)
	}
	if c.Reason != p2lCancelReason {
		t.Errorf("cancel row Reason = %q, want %q", c.Reason, p2lCancelReason)
	}
	if c.By != h.Meena.ID {
		t.Errorf("cancel row By = %d, want %d (meena)", c.By, h.Meena.ID)
	}
	if !c.At.Equal(at) {
		t.Errorf("cancel row At = %v, want %v", c.At, at)
	}
}

// DOC-036
func TestDoc_Approve_DisabledSinceSubmit(t *testing.T) {
	h := fixturetest.New(t)
	res := h.NewOrderWith(t, h.Priya, map[string]string{"amount": "60000.00"},
		fixturetest.OL{Item: "CEMENT-OPC", Qty: "10.000", Rate: "380.0000"})
	h.MustDo(t, t.Context(), h.Priya, fixture.Order, res.ID, doc.ActionSubmit, trIn(res.Version))
	d := trState(t, h, fixture.Order, res.ID, doc.StatePending, 0)
	fixture.SetDisabled(t, h.DB, "fixture_item", "CEMENT-OPC", true)
	count := h.AuditCount(t)

	probe := &fixture.Probe{}
	_, err := h.Do(t, fixture.WithProbe(t.Context(), probe), h.Ravi, fixture.Order, d.ID, doc.ActionApprove, trIn(d.Version))
	lkInvalid(t, err, doctype.FieldError{Field: "item_id", Line: 1, Message: p2lMsgCementDisabled})
	after := trState(t, h, fixture.Order, d.ID, doc.StatePending, d.Version)
	if after.Number != "" {
		t.Errorf("Number = %q, want \"\" (no number on a refused approve)", after.Number)
	}
	if slices.Contains(probe.Hooks(), "ApplyEffects") {
		t.Errorf("hooks = %q, want no ApplyEffects", probe.Hooks())
	}
	if slices.Contains(probe.Calls, "NAM.Assign") {
		t.Errorf("calls = %q, want no NAM.Assign", probe.Calls)
	}
	if got := h.AuditCount(t); got != count {
		t.Errorf("AuditCount = %d, want %d (unchanged)", got, count)
	}

	rej, err := h.Do(t, t.Context(), h.Ravi, fixture.Order, d.ID, doc.ActionReject, trIn(d.Version, p2lRejectReason))
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if rej.State != doc.StateRejected {
		t.Errorf("reject Result.State = %q, want rejected", rej.State)
	}
	if got := trState(t, h, fixture.Order, d.ID, doc.StateRejected, d.Version+1); got.StateReason != p2lRejectReason {
		t.Errorf("StateReason = %q, want %q", got.StateReason, p2lRejectReason)
	}
}

// DOC-057
func TestDoc_LinkedBlockers_IgnoreAmendedFrom(t *testing.T) {
	h := fixturetest.New(t)
	a := termCancelledRefPO(t, h)
	am := termDo(t, h, t.Context(), h.Ravi, a, doc.ActionAmend, "")
	n := h.Load(t, fixture.Order, am.ID)
	termDo(t, h, t.Context(), h.Priya, n, doc.ActionSubmit, "")
	n = h.Load(t, fixture.Order, am.ID)
	termDo(t, h, t.Context(), h.Ravi, n, doc.ActionApprove, "")
	n = h.Load(t, fixture.Order, am.ID)
	if n.State != doc.StateApproved || n.AmendedFromID != a.ID {
		t.Fatalf("setup: amendment %s, AmendedFromID %d; want approved, %d", n.State, n.AmendedFromID, a.ID)
	}

	if got := p2lLinkedBlockers(t, h, fixture.Order, a.ID); len(got) != 0 {
		t.Errorf("LinkedBlockers(A) = %+v, want none (amended_from never blocks)", got)
	}
}

// DOC-058, plus checkBlockers' list for 2 and 11 blockers (Task 11 review).
func TestDoc_Cancel_ManyBlockersCapped(t *testing.T) {
	t.Run("13 blockers", func(t *testing.T) {
		h := fixturetest.New(t)
		o := lkApprovedOrder(t, h, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "100"})
		drafts := p2lDrafts(t, h, o.Lines[0].ID, 13)
		// Submit newest first, so number order differs from id order.
		for i := range drafts {
			p2lSubmit(t, h, drafts[len(drafts)-1-i], p2lGR(i+1))
		}
		o = h.Load(t, fixture.Order, o.ID)
		count := h.AuditCount(t)

		var listed []string
		for i := 1; i <= 10; i++ {
			listed = append(listed, p2lEntry(p2lGR(i), "approved"))
		}
		want := p2lBlockedMany(lkPO, 13, listed)
		if !strings.HasSuffix(want, "GR/26-27/00010 (approved) …and 3 more.") {
			t.Fatalf("test bug: want = %q", want)
		}
		fixturetest.ErrIs(t, lkCancel(t, h, o), doc.KindBlocked, want)
		fixturetest.Eq(t, h.Load(t, fixture.Order, o.ID), o)
		if got := h.AuditCount(t); got != count {
			t.Errorf("AuditCount = %d, want %d (unchanged)", got, count)
		}
	})
	t.Run("11 blockers", func(t *testing.T) {
		h := fixturetest.New(t)
		o := lkApprovedOrder(t, h, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "100"})
		for i, r := range p2lDrafts(t, h, o.Lines[0].ID, 11) {
			p2lSubmit(t, h, r, p2lGR(i+1))
		}
		o = h.Load(t, fixture.Order, o.ID)
		var listed []string
		for i := 1; i <= 10; i++ {
			listed = append(listed, p2lEntry(p2lGR(i), "approved"))
		}
		want := p2lBlockedMany(lkPO, 11, listed)
		if !strings.HasSuffix(want, "GR/26-27/00010 (approved) …and 1 more.") {
			t.Fatalf("test bug: want = %q", want)
		}
		fixturetest.ErrIs(t, lkCancel(t, h, o), doc.KindBlocked, want)
		lkState(t, h, fixture.Order, o.ID, doc.StateApproved)
	})
	t.Run("2 blockers", func(t *testing.T) {
		h := fixturetest.New(t)
		o := lkApprovedOrder(t, h, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "100"})
		drafts := p2lDrafts(t, h, o.Lines[0].ID, 2)
		p2lSubmit(t, h, drafts[1], p2lGR(1))
		h.ForceState(t, fixture.Receipt, drafts[0].ID, doc.StatePending, "")
		o = h.Load(t, fixture.Order, o.ID)
		want := fmt.Sprintf(p2lMsgBlockedMany, lkPO, 2, p2lEntry(p2lGR(1), "approved")+", "+
			p2lEntry(lkDraft(drafts[0].ID), "waiting for approval")) + "."
		fixturetest.ErrIs(t, lkCancel(t, h, o), doc.KindBlocked, want)
		lkState(t, h, fixture.Order, o.ID, doc.StateApproved)
	})
}

// DOC-060
func TestDoc_RollUp_IncludesClosed(t *testing.T) {
	h := fixturetest.New(t)
	_, ol := ruOrder(t, h, "10.000")
	ruApproved(t, h, h.Ravi, fixturetest.RL{OrderLineID: ol[0], Qty: "4"})
	r2 := ruApproved(t, h, h.Ravi, fixturetest.RL{OrderLineID: ol[0], Qty: "2"})
	h.MustDo(t, t.Context(), h.Suresh, fixture.Receipt, r2.ID, doc.ActionClose, trIn(r2.Version, p2lCloseReason))
	trState(t, h, fixture.Receipt, r2.ID, doc.StateClosed, 0)
	ruWantQty(t, h, ol[0], 6000, "R2 closed")

	h.Exec(t, `UPDATE fixture_order_line SET received_qty = 0 WHERE id = ?`, ol[0])
	ruReceiptsNeedApproval(h)
	ruApproved(t, h, h.Ravi, fixturetest.RL{OrderLineID: ol[0], Qty: "1"})
	ruWantQty(t, h, ol[0], 7000, "R3 approved (4 approved + 2 closed + 1)")
}

// DOC-063
func TestDoc_Delete_AmendmentDraftFreesOriginal(t *testing.T) {
	h := fixturetest.New(t)
	a := termCancelledRefPO(t, h)
	d1 := termDo(t, h, t.Context(), h.Ravi, a, doc.ActionAmend, "")
	h.MustDo(t, t.Context(), h.Ravi, fixture.Order, d1.ID, doc.ActionDelete, trIn(d1.Version))

	a = h.Load(t, fixture.Order, a.ID)
	res, err := h.Do(t, t.Context(), h.Ravi, fixture.Order, a.ID, doc.ActionAmend, trIn(a.Version))
	if err != nil {
		t.Fatalf("amend A again after deleting its draft amendment: %v", err)
	}
	if res.ID == 0 || res.ID == a.ID || res.State != doc.StateDraft || res.Version != 1 {
		t.Fatalf("Result = %+v, want a new draft at version 1 (A %d)", res, a.ID)
	}
	n := h.Load(t, fixture.Order, res.ID)
	if n.State != doc.StateDraft || n.Version != 1 || n.AmendedFromID != a.ID || n.AmendmentNo != 1 {
		t.Errorf("new amendment %s AmendedFromID %d AmendmentNo %d; want draft, %d, 1", n.State, n.AmendedFromID, n.AmendmentNo, a.ID)
	}
	trState(t, h, fixture.Order, a.ID, doc.StateCancelled, a.Version)
}

// DOC-066
func TestDoc_Amend_Chain(t *testing.T) {
	h := fixturetest.New(t)
	a := termCancelledRefPO(t, h)
	approve := func(id int64) doc.Result {
		t.Helper()
		d := h.Load(t, fixture.Order, id)
		termDo(t, h, t.Context(), h.Priya, d, doc.ActionSubmit, "")
		d = h.Load(t, fixture.Order, id)
		return termDo(t, h, t.Context(), h.Ravi, d, doc.ActionApprove, "")
	}

	a1 := termDo(t, h, t.Context(), h.Ravi, a, doc.ActionAmend, "")
	if got := approve(a1.ID).Number; got != "PO/26-27/00001-1" {
		t.Fatalf("approved A-1 Number = %q, want PO/26-27/00001-1", got)
	}
	termDo(t, h, t.Context(), h.Meena, h.Load(t, fixture.Order, a1.ID), doc.ActionCancel, p2lCancelReason)
	trState(t, h, fixture.Order, a1.ID, doc.StateCancelled, 0)

	a2 := termDo(t, h, t.Context(), h.Ravi, h.Load(t, fixture.Order, a1.ID), doc.ActionAmend, "")
	n := h.Load(t, fixture.Order, a2.ID)
	if n.AmendedFromID != a1.ID || n.AmendmentNo != 2 || n.AmendmentRootID != a.ID {
		t.Errorf("A-2 AmendedFromID %d AmendmentNo %d AmendmentRootID %d; want %d, 2, %d", n.AmendedFromID, n.AmendmentNo, n.AmendmentRootID, a1.ID, a.ID)
	}
	if got := approve(a2.ID).Number; got != "PO/26-27/00001-2" {
		t.Errorf("approved A-2 Number = %q, want PO/26-27/00001-2", got)
	}

	var chain []int64
	for id := a2.ID; id != 0 && len(chain) < 5; id = h.Load(t, fixture.Order, id).AmendedFromID {
		chain = append(chain, id)
	}
	fixturetest.Eq(t, chain, []int64{a2.ID, a1.ID, a.ID})
}

// DOC-067
func TestDoc_Amend_ValidatedOnSubmit(t *testing.T) {
	h := fixturetest.New(t)
	a := termCancelledRefPO(t, h)
	fixture.SetDisabled(t, h.DB, "fixture_item", "WIRE-18G", true)

	res, err := h.Do(t, t.Context(), h.Ravi, fixture.Order, a.ID, doc.ActionAmend, trIn(a.Version))
	if err != nil {
		t.Fatalf("amend with a disabled item: %v, want the draft created", err)
	}
	n := trState(t, h, fixture.Order, res.ID, doc.StateDraft, 1)
	if n.AmendedFromID != a.ID {
		t.Errorf("AmendedFromID = %d, want %d", n.AmendedFromID, a.ID)
	}

	_, err = h.Do(t, t.Context(), h.Priya, fixture.Order, n.ID, doc.ActionSubmit, trIn(n.Version))
	lkInvalid(t, err, doctype.FieldError{Field: "item_id", Line: 2, Message: p2lMsgWireDisabled})
	trState(t, h, fixture.Order, n.ID, doc.StateDraft, n.Version)
}

// DOC-068
func TestDoc_Amend_AuditBothSides(t *testing.T) {
	h := fixturetest.New(t)
	a := termCancelledRefPO(t, h)
	before := len(h.Audit(t, fixture.Order, a.ID))
	at := time.Date(2026, 10, 24, 9, 15, 0, 0, time.UTC)
	h.Clock.SetTime(at)

	res := termDo(t, h, t.Context(), h.Ravi, a, doc.ActionAmend, "")

	created := h.Audit(t, fixture.Order, res.ID)
	if len(created) != 1 {
		t.Fatalf("A-1 audit rows = %+v, want exactly the create row", created)
	}
	c := created[0]
	if c.Action != "create" || c.To != string(doc.StateDraft) || c.By != h.Ravi.ID || !c.At.Equal(at) {
		t.Errorf("A-1 row %q To %q By %d At %v; want create, draft, %d, %v", c.Action, c.To, c.By, c.At, h.Ravi.ID, at)
	}
	p2lMarker(t, c, "amended_from", a.ID)

	src := h.Audit(t, fixture.Order, a.ID)
	if len(src) != before+1 {
		t.Fatalf("A audit rows = %d, want %d (one amended row)", len(src), before+1)
	}
	m := src[len(src)-1]
	if m.Action != "amended" || m.By != h.Ravi.ID || !m.At.Equal(at) {
		t.Errorf("A's new row %q By %d At %v; want amended, %d, %v", m.Action, m.By, m.At, h.Ravi.ID, at)
	}
	if m.From != "" || m.To != "" {
		t.Errorf("amended row %q -> %q, want both empty (no state change)", m.From, m.To)
	}
	fixturetest.Eq(t, m.Changes, []audit.Change{{Field: "amended_to", Before: "", After: strconv.FormatInt(res.ID, 10), Op: "set"}})
	trState(t, h, fixture.Order, a.ID, doc.StateCancelled, a.Version)
}

// DOC-074
func TestDoc_Concurrency_VersionOnRollUp(t *testing.T) {
	h := fixturetest.New(t)
	res := h.NewOrder(t, h.Priya, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10.000", Rate: "62.5000"})
	s := trSave(t, h, h.Priya, h.Load(t, fixture.Order, res.ID), "v2", nil)
	h.MustDo(t, t.Context(), h.Priya, fixture.Order, res.ID, doc.ActionSubmit, trIn(s.Version))
	o := h.Load(t, fixture.Order, res.ID)
	if o.State != doc.StateApproved || o.Version != 3 {
		t.Fatalf("setup: order %s v%d, want approved at version 3", o.State, o.Version)
	}

	ruApproved(t, h, h.Ravi, fixturetest.RL{OrderLineID: o.Lines[0].ID, Qty: "4"})
	trState(t, h, fixture.Order, o.ID, doc.StateApproved, 4)
	ruApproved(t, h, h.Ravi, fixturetest.RL{OrderLineID: o.Lines[0].ID, Qty: "3"})
	trState(t, h, fixture.Order, o.ID, doc.StateApproved, 5)
}

// DOC-075
func TestDoc_Tx_BusyTimeout(t *testing.T) {
	h := fixturetest.New(t)
	res := h.NewOrder(t, h.Priya, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10"})
	d := h.Load(t, fixture.Order, res.ID)
	e2 := h.EngineOn(p2lOpen(t, db.WithBusyTimeout(200*time.Millisecond)))
	holder := p2lOpen(t)
	count := h.AuditCount(t)
	in := h.In(d)
	in.Values["remarks"] = "Saved while another user writes"

	wait := p2lHoldWrite(t, holder, time.Second)
	_, err := e2.Do(t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
	wait()
	fixturetest.ErrIs(t, err, doc.KindBusy, p2lBusyText)
	if db.ErrBusy.Error() != p2lBusyText {
		t.Errorf("db.ErrBusy = %q, want %q (CD-66)", db.ErrBusy.Error(), p2lBusyText)
	}
	_ = lkHTTP(t, err, 503, p2lBusyText)
	if !errors.Is(err, db.ErrBusy) {
		t.Errorf("errors.Is(err, db.ErrBusy) = false (err %v)", err)
	}
	fixturetest.Eq(t, h.Load(t, fixture.Order, d.ID), d)
	if got := h.AuditCount(t); got != count {
		t.Errorf("AuditCount = %d, want %d (nothing written)", got, count)
	}

	wait = p2lHoldWrite(t, holder, 50*time.Millisecond)
	ok, err := e2.Do(t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
	wait()
	if err != nil {
		t.Fatalf("save after a 50 ms lock: %v, want success", err)
	}
	if ok.Version != d.Version+1 {
		t.Errorf("Result.Version = %d, want %d", ok.Version, d.Version+1)
	}
	if got := h.Load(t, fixture.Order, d.ID).Header["remarks"]; got != any("Saved while another user writes") {
		t.Errorf("remarks = %#v, want the saved text", got)
	}
}

// DOC-078
func TestDoc_Hooks_ShareTx(t *testing.T) {
	h := fixturetest.New(t)
	_, ol := ruOrder(t, h, "10.000")
	ruReceiptsNeedApproval(h)
	r := ruSubmit(t, h, fixturetest.RL{OrderLineID: ol[0], Qty: "4"})
	if r.State != doc.StatePending {
		t.Fatalf("setup: receipt State = %q, want pending_approval", r.State)
	}
	h.Approvals.Fail = map[string]error{"Record:approve": errRuFault}
	probe := &fixture.Probe{}
	_, err := ruDo(t, h, probe, h.Ravi, fixture.Receipt, r.ID, doc.ActionApprove, trIn(r.Version))
	if !errors.Is(err, errRuFault) {
		t.Fatalf("approve with Record:approve failing: err = %v, want the injected fault", err)
	}
	if !slices.Contains(probe.Hooks(), "ApplyEffects") {
		t.Fatalf("hooks = %q, want ApplyEffects to have run (and written its row)", probe.Hooks())
	}
	effectRows := func(id int64) int {
		t.Helper()
		var rows int
		if err := h.DB.SQL().QueryRowContext(t.Context(),
			`SELECT COUNT(*) FROM fixture_effect_log WHERE receipt_id = ?`, id).Scan(&rows); err != nil {
			t.Fatalf("count fixture_effect_log: %v", err)
		}
		return rows
	}
	if rows := effectRows(r.ID); rows != 0 {
		t.Errorf("fixture_effect_log rows for the receipt = %d, want 0 (rolled back with the action)", rows)
	}
	ruUnchanged(t, h, fixture.Receipt, r.ID, doc.StatePending, r.Version)

	// Control: without the fault, the same approve leaves ApplyEffects' row.
	h.Approvals.Fail = nil
	r2 := ruSubmit(t, h, fixturetest.RL{OrderLineID: ol[0], Qty: "3"})
	if r2.State != doc.StatePending {
		t.Fatalf("setup: control receipt State = %q, want pending_approval", r2.State)
	}
	h.MustDo(t, t.Context(), h.Ravi, fixture.Receipt, r2.ID, doc.ActionApprove, trIn(r2.Version))
	if rows := effectRows(r2.ID); rows == 0 {
		t.Errorf("fixture_effect_log rows for the control receipt = 0, want ApplyEffects' row")
	}

	// The Tx hooks get, and the DBTX its SQL() returns (CD-06), can't end the transaction.
	txType, dbtxType := reflect.TypeFor[*db.Tx](), reflect.TypeFor[db.DBTX]()
	if m, ok := txType.MethodByName("SQL"); !ok || m.Type.NumOut() != 1 || m.Type.Out(0) != dbtxType {
		t.Errorf("(*db.Tx).SQL doesn't return db.DBTX")
	}
	for _, typ := range []reflect.Type{txType, dbtxType} {
		for _, name := range []string{"Commit", "Rollback"} {
			if _, ok := typ.MethodByName(name); ok {
				t.Errorf("%s has a %s method; hooks must not be able to end the transaction", typ, name)
			}
		}
	}
}

// DOC-101
func TestDoc_Link_BlockedMaster(t *testing.T) {
	h := fixturetest.New(t)
	a1, a2 := h.ApprovedOrder(t, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10"}), h.ApprovedOrder(t, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10"})
	draft := h.Load(t, fixture.Order, h.RefPO(t).ID) // SHREE; line 2 WIRE-18G
	save := func() error {
		t.Helper()
		in := h.In(draft)
		in.Values["remarks"] = "Deliver to Pune site"
		_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, draft.ID, doc.ActionSave, in)
		return err
	}
	count := h.AuditCount(t)

	fixture.SetBlocked(t, h.DB, "fixture_party", "SHREE", p2lShreeBlocked)
	lkInvalid(t, save(), doctype.FieldError{Field: "supplier_id", Line: 0, Message: p2lShreeBlocked})

	fixture.SetBlocked(t, h.DB, "fixture_item", "WIRE-18G", p2lWireBlocked)
	lkInvalid(t, save(),
		doctype.FieldError{Field: "supplier_id", Line: 0, Message: p2lShreeBlocked},
		doctype.FieldError{Field: "item_id", Line: 2, Message: p2lMsgWireBlockedL2})
	fixturetest.Eq(t, h.Load(t, fixture.Order, draft.ID), draft)
	if got := h.AuditCount(t); got != count {
		t.Errorf("AuditCount = %d, want %d (nothing written)", got, count)
	}

	// An approved order using SHREE can still be closed or cancelled (as DOC-035).
	if _, err := h.Do(t, t.Context(), h.Priya, fixture.Order, a1.ID, doc.ActionClose, trIn(a1.Version, p2lCloseReason)); err != nil {
		t.Errorf("close an approved order with a blocked supplier = %v, want no error", err)
	}
	lkState(t, h, fixture.Order, a1.ID, doc.StateClosed)
	if err := lkCancel(t, h, a2); err != nil {
		t.Errorf("cancel an approved order with a blocked supplier = %v, want no error", err)
	}
	lkState(t, h, fixture.Order, a2.ID, doc.StateCancelled)
}

// Header-level links (owed by the Task 11 review; no case ID): a LinkSpec on
// a header field blocks a cancel through LinkedBlockers' header branch, and a
// missing header source is reported with the field's Label.
func TestDoc_Link_HeaderLevel(t *testing.T) {
	h := fixturetest.New(t)
	p2lHeaderTables(t, h)
	head := h.MustDo(t, t.Context(), h.Priya, p2lHead, 0, doc.ActionSave,
		doc.ActionInput{Values: map[string]string{"title": "Head"}})
	h.ForceState(t, p2lHead, head.ID, doc.StateApproved, "HD/1")

	_, err := h.Do(t, t.Context(), h.Priya, p2lChild, 0, doc.ActionSave,
		doc.ActionInput{Values: map[string]string{"head_id": "9999"}})
	lkInvalid(t, err, doctype.FieldError{Field: "head_id", Line: 0, Message: p2lMsgHeadMissing})

	in := doc.ActionInput{Values: map[string]string{"head_id": lkID(head.ID)}}
	live := h.MustDo(t, t.Context(), h.Priya, p2lChild, 0, doc.ActionSave, in)
	h.MustDo(t, t.Context(), h.Priya, p2lChild, 0, doc.ActionSave, in) // stays a draft: never blocks
	h.ForceState(t, p2lChild, live.ID, doc.StateApproved, "CH/1")

	fixturetest.Eq(t, p2lLinkedBlockers(t, h, p2lHead, head.ID),
		[]doc.Blocker{{DocType: p2lChild, ID: live.ID, Number: "CH/1", State: doc.StateApproved}})

	hd := h.Load(t, p2lHead, head.ID)
	err = lkCancel(t, h, hd)
	fixturetest.ErrIs(t, err, doc.KindBlocked, fmt.Sprintf(lkMsgBlockedOne, "HD/1", "CH/1", "approved", "CH/1"))
	lkState(t, h, p2lHead, head.ID, doc.StateApproved)
}

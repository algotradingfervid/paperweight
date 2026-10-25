package doc_test

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Plan Task 11 texts. Fixed messages are plain constants; format strings
// (containing %) are filled with fmt.Sprintf.
const (
	lkPO  = "PO/26-27/00001"
	lkPO2 = "PO/26-27/00002"
	lkGR  = "GR/26-27/00001"

	lkCancelReason = "Supplier cancelled the order"
	lkCloseReason  = "Balance not needed"

	lkMsgFixFields = "Fix the highlighted fields and save again."

	// %s is the source document's display name.
	lkMsgSourceNotApproved = "Line 1: %s isn't approved yet. Pick an approved purchase order."
	lkMsgSourcePending     = "Line 1: %s is waiting for approval. Pick an approved purchase order."
	lkMsgSourceCancelled   = "Line 1: %s is cancelled. Remove the line or pick another purchase order."
	lkMsgSourceClosed      = "Line 1: %s is closed. Remove the line or pick another purchase order."

	// checkBlockers, one blocker: X, B, state word, B.
	lkMsgBlockedOne = "Can't cancel %s because %s (%s) is linked to it. Cancel %s first."

	// DOC-034 (CD-38, CD-68).
	lkMsgSupplierDisabled = "Supplier RAJKOT Rajkot Castings is disabled. Pick an active supplier, or ask an admin to enable it."
	lkMsgItemDisabled     = "Line 1: Item CEMENT-OPC OPC Cement 53 Grade is disabled. Pick an active item, or ask an admin to enable it."
)

func lkID(v int64) string { return strconv.FormatInt(v, 10) }

func lkDraft(id int64) string { return fmt.Sprintf("Draft %d", id) }

// lkInvalid fails unless err is a KindInvalid *doc.Error with the fix-fields
// message and exactly the field errors want.
func lkInvalid(t *testing.T, err error, want ...doctype.FieldError) {
	t.Helper()
	var de *doc.Error
	if !errors.As(err, &de) {
		t.Fatalf("err = %v (%T), want a *doc.Error of kind %q", err, err, doc.KindInvalid)
	}
	if de.Kind != doc.KindInvalid {
		t.Errorf("Kind = %q, want %q (err %v)", de.Kind, doc.KindInvalid, err)
	}
	if de.Message != lkMsgFixFields {
		t.Errorf("Message = %q, want %q", de.Message, lkMsgFixFields)
	}
	fixturetest.Eq(t, de.Fields, want)
}

// lkSourceRefused fails unless err refuses receipt line 1's order line with msg.
func lkSourceRefused(t *testing.T, err error, msg string) {
	t.Helper()
	lkInvalid(t, err, doctype.FieldError{Field: "order_line_id", Line: 1, Message: msg})
}

// lkReceiptIn is a new receipt's input with one line on order line olID.
func lkReceiptIn(olID int64, qty string) doc.ActionInput {
	return doc.ActionInput{
		Values: map[string]string{"receipt_date": "2026-10-24"},
		Lines:  []map[string]string{{"order_line_id": lkID(olID), "qty": qty}},
	}
}

// lkRows counts the rows of table.
func lkRows(t *testing.T, h *fixturetest.H, table string) int {
	t.Helper()
	var n int
	err := h.DB.Read(t.Context(), func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM "`+table+`"`).Scan(&n)
	})
	if err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// lkApprovedOrder is an approved order (the first, PO/26-27/00001) with lines.
func lkApprovedOrder(t *testing.T, h *fixturetest.H, lines ...fixturetest.OL) doc.Doc {
	t.Helper()
	if len(lines) == 0 {
		lines = []fixturetest.OL{{Item: "MS-ANGLE-50", Qty: "10"}}
	}
	o := h.ApprovedOrder(t, lines...)
	if o.State != doc.StateApproved || o.Number != lkPO {
		t.Fatalf("setup: order %d is %s %q, want approved %s", o.ID, o.State, o.Number, lkPO)
	}
	return o
}

// lkApprovedReceipt saves a receipt by Suresh with lines and submits it (no
// rule, so it is approved as GR/26-27/00001).
func lkApprovedReceipt(t *testing.T, h *fixturetest.H, lines ...fixturetest.RL) doc.Doc {
	t.Helper()
	res := h.NewReceipt(t, h.Suresh, lines...)
	h.MustDo(t, t.Context(), h.Suresh, fixture.Receipt, res.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version})
	r := h.Load(t, fixture.Receipt, res.ID)
	if r.State != doc.StateApproved || r.Number != lkGR {
		t.Fatalf("setup: receipt %d is %s %q, want approved %s", r.ID, r.State, r.Number, lkGR)
	}
	return r
}

// lkBlockedSetup is DOC-052's setup: approved PO/26-27/00001 with approved
// receipt GR/26-27/00001 on its line 1.
func lkBlockedSetup(t *testing.T) (*fixturetest.H, doc.Doc, doc.Doc) {
	t.Helper()
	h := fixturetest.New(t)
	o := lkApprovedOrder(t, h)
	r := lkApprovedReceipt(t, h, fixturetest.RL{OrderLineID: o.Lines[0].ID, Qty: "4"})
	return h, h.Load(t, fixture.Order, o.ID), r
}

// lkCancel cancels d by Meena (cancel right) with a valid reason.
func lkCancel(t *testing.T, h *fixturetest.H, d doc.Doc) error {
	t.Helper()
	_, err := h.Do(t, t.Context(), h.Meena, d.DocType, d.ID, doc.ActionCancel,
		doc.ActionInput{Version: d.Version, Reason: lkCancelReason})
	return err
}

// lkState fails unless the stored document is in state want.
func lkState(t *testing.T, h *fixturetest.H, dt string, id int64, want doc.State) {
	t.Helper()
	if got := h.Load(t, dt, id).State; got != want {
		t.Errorf("%s %d state = %s, want %s", dt, id, got, want)
	}
}

// DOC-017
func TestDoc_Link_ClosedSourceRefused(t *testing.T) {
	h := fixturetest.New(t)
	o := lkApprovedOrder(t, h)
	ol := o.Lines[0].ID

	// Saved before the close: one draft, one pending (a rule applies).
	r1 := h.NewReceipt(t, h.Suresh, fixturetest.RL{OrderLineID: ol, Qty: "2"})
	r2 := h.NewReceipt(t, h.Suresh, fixturetest.RL{OrderLineID: ol, Qty: "3"})
	h.Approvals.Needed = func(string, types.Money) bool { return true }
	h.MustDo(t, t.Context(), h.Suresh, fixture.Receipt, r2.ID, doc.ActionSubmit, doc.ActionInput{Version: r2.Version})
	h.Approvals.Needed = nil
	lkState(t, h, fixture.Receipt, r2.ID, doc.StatePending)

	h.ForceState(t, fixture.Order, o.ID, doc.StateClosed, lkPO)
	want := fmt.Sprintf(lkMsgSourceClosed, lkPO)

	t.Run("new receipt save", func(t *testing.T) {
		rows := lkRows(t, h, "fixture_receipt")
		_, err := h.Do(t, t.Context(), h.Suresh, fixture.Receipt, 0, doc.ActionSave, lkReceiptIn(ol, "1"))
		lkSourceRefused(t, err, want)
		if got := lkRows(t, h, "fixture_receipt"); got != rows {
			t.Errorf("fixture_receipt rows = %d, want %d (nothing written)", got, rows)
		}
	})
	t.Run("draft submitted", func(t *testing.T) {
		d := h.Load(t, fixture.Receipt, r1.ID)
		_, err := h.Do(t, t.Context(), h.Suresh, fixture.Receipt, r1.ID, doc.ActionSubmit, doc.ActionInput{Version: d.Version})
		lkSourceRefused(t, err, want)
		lkState(t, h, fixture.Receipt, r1.ID, doc.StateDraft)
	})
	t.Run("pending approved", func(t *testing.T) {
		d := h.Load(t, fixture.Receipt, r2.ID)
		_, err := h.Do(t, t.Context(), h.Ravi, fixture.Receipt, r2.ID, doc.ActionApprove, doc.ActionInput{Version: d.Version})
		lkSourceRefused(t, err, want)
		lkState(t, h, fixture.Receipt, r2.ID, doc.StatePending)
	})
}

// DOC-034
func TestDoc_Link_DisabledMasterRefused(t *testing.T) {
	t.Run("supplier on save", func(t *testing.T) {
		h := fixturetest.New(t)
		fixture.SetDisabled(t, h.DB, "fixture_party", "RAJKOT", true)
		rows := orderRows(t, h)
		_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, 0, doc.ActionSave, doc.ActionInput{Values: map[string]string{
			"supplier_id": lkID(h.M.Party["RAJKOT"]), "project_id": lkID(h.M.Project["PRJ-METRO"]),
			"order_date": "2026-10-24", "amount": "0",
		}})
		lkInvalid(t, err, doctype.FieldError{Field: "supplier_id", Line: 0, Message: lkMsgSupplierDisabled})
		if got := orderRows(t, h); got != rows {
			t.Errorf("fixture_order rows = %d, want %d (nothing written)", got, rows)
		}
	})
	t.Run("supplier on submit", func(t *testing.T) {
		h := fixturetest.New(t)
		res := h.NewOrderWith(t, h.Priya, map[string]string{"supplier_id": lkID(h.M.Party["RAJKOT"])},
			fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10"})
		fixture.SetDisabled(t, h.DB, "fixture_party", "RAJKOT", true)
		_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, res.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version})
		lkInvalid(t, err, doctype.FieldError{Field: "supplier_id", Line: 0, Message: lkMsgSupplierDisabled})
		lkState(t, h, fixture.Order, res.ID, doc.StateDraft)
	})
	t.Run("supplier on approve", func(t *testing.T) {
		h := fixturetest.New(t)
		res := h.NewOrderWith(t, h.Priya, map[string]string{"supplier_id": lkID(h.M.Party["RAJKOT"]), "amount": "60000.00"},
			fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10"})
		h.MustDo(t, t.Context(), h.Priya, fixture.Order, res.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version})
		d := h.Load(t, fixture.Order, res.ID)
		if d.State != doc.StatePending {
			t.Fatalf("setup: order is %s, want %s", d.State, doc.StatePending)
		}
		fixture.SetDisabled(t, h.DB, "fixture_party", "RAJKOT", true)
		_, err := h.Do(t, t.Context(), h.Ravi, fixture.Order, d.ID, doc.ActionApprove, doc.ActionInput{Version: d.Version})
		lkInvalid(t, err, doctype.FieldError{Field: "supplier_id", Line: 0, Message: lkMsgSupplierDisabled})
		lkState(t, h, fixture.Order, d.ID, doc.StatePending)
	})
	t.Run("item on line 1", func(t *testing.T) {
		h := fixturetest.New(t)
		fixture.SetDisabled(t, h.DB, "fixture_item", "CEMENT-OPC", true)
		rows := orderRows(t, h)
		_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, 0, doc.ActionSave, doc.ActionInput{
			Values: map[string]string{
				"supplier_id": lkID(h.M.Party["SHREE"]), "project_id": lkID(h.M.Project["PRJ-METRO"]),
				"order_date": "2026-10-24", "amount": "0",
			},
			Lines: []map[string]string{{"item_id": lkID(h.M.Item["CEMENT-OPC"]), "qty": "10"}},
		})
		lkInvalid(t, err, doctype.FieldError{Field: "item_id", Line: 1, Message: lkMsgItemDisabled})
		if got := orderRows(t, h); got != rows {
			t.Errorf("fixture_order rows = %d, want %d (nothing written)", got, rows)
		}
	})
}

// DOC-035
func TestDoc_Link_DisabledAfterUse(t *testing.T) {
	h := fixturetest.New(t)
	rajkot := map[string]string{"supplier_id": lkID(h.M.Party["RAJKOT"])}
	line := fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10"}
	d := h.NewOrderWith(t, h.Priya, rajkot, line)
	approved := func() doc.Doc {
		res := h.NewOrderWith(t, h.Priya, rajkot, line)
		h.MustDo(t, t.Context(), h.Priya, fixture.Order, res.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version})
		o := h.Load(t, fixture.Order, res.ID)
		if o.State != doc.StateApproved {
			t.Fatalf("setup: order %d is %s, want approved", o.ID, o.State)
		}
		return o
	}
	a1, a2 := approved(), approved()
	fixture.SetDisabled(t, h.DB, "fixture_party", "RAJKOT", true)

	_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSubmit, doc.ActionInput{Version: d.Version})
	lkInvalid(t, err, doctype.FieldError{Field: "supplier_id", Line: 0, Message: lkMsgSupplierDisabled})
	lkState(t, h, fixture.Order, d.ID, doc.StateDraft)

	err = h.DB.Read(t.Context(), func(tx *db.Tx) error {
		_, err := h.Engine.Load(t.Context(), tx, fixture.Order, a1.ID)
		return err
	})
	if err != nil {
		t.Errorf("Load(A1) = %v, want no error", err)
	}

	if _, err := h.Do(t, t.Context(), h.Priya, fixture.Order, a1.ID, doc.ActionClose,
		doc.ActionInput{Version: a1.Version, Reason: lkCloseReason}); err != nil {
		t.Errorf("close A1 = %v, want no error", err)
	}
	lkState(t, h, fixture.Order, a1.ID, doc.StateClosed)

	if err := lkCancel(t, h, a2); err != nil {
		t.Errorf("cancel A2 = %v, want no error", err)
	}
	lkState(t, h, fixture.Order, a2.ID, doc.StateCancelled)
}

// DOC-037
func TestDoc_Link_SourceStateRules(t *testing.T) {
	cases := []struct {
		state  doc.State
		number string
		msg    string // "" = accepted; %s is the source's display name
	}{
		{doc.StateApproved, lkPO, ""},
		{doc.StateDraft, "", lkMsgSourceNotApproved},
		{doc.StateRejected, "", lkMsgSourceNotApproved},
		{doc.StatePending, "", lkMsgSourcePending},
		{doc.StateCancelled, lkPO, lkMsgSourceCancelled},
		{doc.StateClosed, lkPO, lkMsgSourceClosed},
	}
	for _, c := range cases {
		t.Run(string(c.state), func(t *testing.T) {
			h := fixturetest.New(t)
			res := h.NewOrder(t, h.Priya, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10"})
			h.ForceState(t, fixture.Order, res.ID, c.state, c.number)
			o := h.Load(t, fixture.Order, res.ID)
			_, err := h.Do(t, t.Context(), h.Suresh, fixture.Receipt, 0, doc.ActionSave, lkReceiptIn(o.Lines[0].ID, "1"))
			if c.msg == "" {
				if err != nil {
					t.Fatalf("save on an approved source = %v, want no error", err)
				}
				return
			}
			display := c.number
			if display == "" {
				display = lkDraft(o.ID)
			}
			lkSourceRefused(t, err, fmt.Sprintf(c.msg, display))
		})
	}
}

// DOC-038
func TestDoc_Link_SourceCancelledBeforeSubmit(t *testing.T) {
	h := fixturetest.New(t)
	o := lkApprovedOrder(t, h)
	r := h.NewReceipt(t, h.Suresh, fixturetest.RL{OrderLineID: o.Lines[0].ID, Qty: "4"})

	if err := lkCancel(t, h, o); err != nil {
		t.Fatalf("cancel O with a draft receipt = %v, want no error (drafts don't block)", err)
	}
	lkState(t, h, fixture.Order, o.ID, doc.StateCancelled)

	_, err := h.Do(t, t.Context(), h.Suresh, fixture.Receipt, r.ID, doc.ActionSubmit, doc.ActionInput{Version: r.Version})
	lkSourceRefused(t, err, fmt.Sprintf(lkMsgSourceCancelled, lkPO))
	lkState(t, h, fixture.Receipt, r.ID, doc.StateDraft)
}

// DOC-052
func TestDoc_Cancel_BlockedByLaterDoc(t *testing.T) {
	h, o, _ := lkBlockedSetup(t)
	count := h.AuditCount(t)
	err := lkCancel(t, h, o)
	fixturetest.ErrIs(t, err, doc.KindBlocked, fmt.Sprintf(lkMsgBlockedOne, lkPO, lkGR, "approved", lkGR))
	fixturetest.Eq(t, h.Load(t, fixture.Order, o.ID), o)
	if got := h.AuditCount(t); got != count {
		t.Errorf("AuditCount = %d, want %d (unchanged)", got, count)
	}
}

// DOC-053
func TestDoc_Cancel_BlockingStates(t *testing.T) {
	cases := []struct {
		state  doc.State
		number string
		word   string // "" = not blocking
	}{
		{doc.StatePending, "", "waiting for approval"},
		{doc.StateApproved, lkGR, "approved"},
		{doc.StateClosed, lkGR, "closed"},
		{doc.StateDraft, "", ""},
		{doc.StateRejected, "", ""},
		{doc.StateCancelled, lkGR, ""},
	}
	for _, c := range cases {
		t.Run(string(c.state), func(t *testing.T) {
			h := fixturetest.New(t)
			o := lkApprovedOrder(t, h)
			r := h.NewReceipt(t, h.Suresh, fixturetest.RL{OrderLineID: o.Lines[0].ID, Qty: "4"})
			h.ForceState(t, fixture.Receipt, r.ID, c.state, c.number)
			err := lkCancel(t, h, o)
			if c.word == "" {
				if err != nil {
					t.Fatalf("cancel with a %s receipt = %v, want no error", c.state, err)
				}
				lkState(t, h, fixture.Order, o.ID, doc.StateCancelled)
				return
			}
			b := c.number
			if b == "" {
				b = lkDraft(r.ID)
			}
			fixturetest.ErrIs(t, err, doc.KindBlocked, fmt.Sprintf(lkMsgBlockedOne, lkPO, b, c.word, b))
			lkState(t, h, fixture.Order, o.ID, doc.StateApproved)
		})
	}
}

// DOC-055
func TestDoc_LinkedBlockers_LineLevelDedup(t *testing.T) {
	h := fixturetest.New(t)
	o := lkApprovedOrder(t, h, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10"}, fixturetest.OL{Item: "WIRE-18G", Qty: "5"})
	l2 := o.Lines[1].ID
	r := lkApprovedReceipt(t, h,
		fixturetest.RL{OrderLineID: l2, Qty: "1"}, fixturetest.RL{OrderLineID: l2, Qty: "1"}, fixturetest.RL{OrderLineID: l2, Qty: "1"})

	var got []doc.Blocker
	err := h.DB.Read(t.Context(), func(tx *db.Tx) (err error) {
		got, err = doc.LinkedBlockers(t.Context(), tx, fixture.Order, o.ID)
		return err
	})
	if err != nil {
		t.Fatalf("LinkedBlockers: %v", err)
	}
	fixturetest.Eq(t, got, []doc.Blocker{{DocType: fixture.Receipt, ID: r.ID, Number: lkGR, State: doc.StateApproved}})
}

// DOC-056
func TestDoc_Cancel_LinkCheckFirst(t *testing.T) {
	h, o, _ := lkBlockedSetup(t)
	count := h.AuditCount(t)
	probe := &fixture.Probe{}
	_, err := h.Do(t, fixture.WithProbe(t.Context(), probe), h.Meena, fixture.Order, o.ID, doc.ActionCancel,
		doc.ActionInput{Version: o.Version, Reason: lkCancelReason})
	fixturetest.ErrIs(t, err, doc.KindBlocked, fmt.Sprintf(lkMsgBlockedOne, lkPO, lkGR, "approved", lkGR))
	if slices.Contains(probe.Calls, "Order.ReverseEffects") {
		t.Errorf("probe.Calls = %q, want no Order.ReverseEffects (link check runs first)", probe.Calls)
	}
	after := h.Load(t, fixture.Order, o.ID)
	if after.State != doc.StateApproved || after.Version != o.Version {
		t.Errorf("order = %s v%d, want %s v%d (unchanged)", after.State, after.Version, doc.StateApproved, o.Version)
	}
	for i := range o.Lines {
		if g, w := after.Lines[i].Values["received_qty"], o.Lines[i].Values["received_qty"]; g != w {
			t.Errorf("line %d received_qty = %v, want %v (unchanged)", i+1, g, w)
		}
	}
	fixturetest.Eq(t, after, o)
	if got := h.AuditCount(t); got != count {
		t.Errorf("AuditCount = %d, want %d (unchanged)", got, count)
	}
}

// DOC-098
func TestDoc_References_ListsDocuments(t *testing.T) {
	h := fixturetest.New(t)
	rajkot := h.M.Party["RAJKOT"]
	sup := map[string]string{"supplier_id": lkID(rajkot)}
	d := h.NewOrderWith(t, h.Priya, sup, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10"})
	ares := h.NewOrderWith(t, h.Priya, sup, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10"})
	h.MustDo(t, t.Context(), h.Priya, fixture.Order, ares.ID, doc.ActionSubmit, doc.ActionInput{Version: ares.Version})
	if a := h.Load(t, fixture.Order, ares.ID); a.State != doc.StateApproved || a.Number != lkPO {
		t.Fatalf("setup: order A is %s %q, want approved %s", a.State, a.Number, lkPO)
	}
	// Cancelled order with WIRE-18G on two lines: one reference per (document, field).
	c := h.NewOrder(t, h.Priya, fixturetest.OL{Item: "WIRE-18G", Qty: "5"}, fixturetest.OL{Item: "WIRE-18G", Qty: "2"})
	h.ForceState(t, fixture.Order, c.ID, doc.StateCancelled, lkPO2)

	refs := func(table string, id int64, limit int) ([]doc.Reference, error) {
		var out []doc.Reference
		err := h.DB.Read(t.Context(), func(tx *db.Tx) (err error) {
			out, err = h.Engine.References(t.Context(), tx, table, id, limit)
			return err
		})
		return out, err
	}
	isRef := func(table string, id int64) (string, bool, error) {
		var (
			first string
			ok    bool
		)
		err := h.DB.Read(t.Context(), func(tx *db.Tx) (err error) {
			first, ok, err = h.Engine.IsReferenced(t.Context(), tx, table, id)
			return err
		})
		return first, ok, err
	}

	wantD := doc.Reference{DocType: fixture.Order, Field: "supplier_id", ID: d.ID, Display: lkDraft(d.ID), State: doc.StateDraft}
	wantA := doc.Reference{DocType: fixture.Order, Field: "supplier_id", ID: ares.ID, Display: lkPO, State: doc.StateApproved}

	got, err := refs("fixture_party", rajkot, 10)
	if err != nil {
		t.Fatalf("References(RAJKOT, 10): %v", err)
	}
	fixturetest.Eq(t, got, []doc.Reference{wantD, wantA})

	got, err = refs("fixture_party", rajkot, 1)
	if err != nil {
		t.Fatalf("References(RAJKOT, 1): %v", err)
	}
	fixturetest.Eq(t, got, []doc.Reference{wantD})

	first, ok, err := isRef("fixture_party", rajkot)
	if err != nil || first != lkDraft(d.ID) || !ok {
		t.Errorf("IsReferenced(RAJKOT) = (%q, %v, %v), want (%q, true, nil)", first, ok, err, lkDraft(d.ID))
	}

	got, err = refs("fixture_item", h.M.Item["WIRE-18G"], 10)
	if err != nil {
		t.Fatalf("References(WIRE-18G, 10): %v", err)
	}
	fixturetest.Eq(t, got, []doc.Reference{
		{DocType: fixture.Order, Field: "lines.item_id", ID: c.ID, Display: lkPO2, State: doc.StateCancelled},
	})

	got, err = refs("fixture_item", h.M.Item["SVC-CRANE"], 10)
	if err != nil || len(got) != 0 {
		t.Errorf("References(SVC-CRANE) = (%#v, %v), want (empty, nil)", got, err)
	}
	first, ok, err = isRef("fixture_item", h.M.Item["SVC-CRANE"])
	if err != nil || first != "" || ok {
		t.Errorf("IsReferenced(SVC-CRANE) = (%q, %v, %v), want (\"\", false, nil)", first, ok, err)
	}

	if _, err := refs("fixture_party", rajkot, 0); err == nil {
		t.Error("References(limit 0) = nil error, want an error")
	}
}

package doc_test

import (
	"fmt"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// MOD-GRN G0c texts (CD-272 Gap 2, CD-279 item 10). msgSourceClosed rendered
// by checkSource: linePrefix, naming.Label of the target, the target's
// lower-cased Label.
const (
	// DOC-017 on receipt line %d; %s is the order's display name.
	amcMsgSourceClosedLine = "Line %d: %s is closed. Remove the line or pick another purchase order."
	// DOC-017 on p2l.Child's header link (no line prefix); %s is the head's display name.
	amcMsgHeadClosed = "%s is closed. Remove the line or pick another head document."

	amcHD1 = "HD/26-27/00001"
	amcHD2 = "HD/26-27/00002"
	amcCH  = "CH/26-27/00001"
)

// amcClose closes d by Priya with a valid reason; the setup fails on an error.
func amcClose(t *testing.T, h *fixturetest.H, d doc.Doc) {
	t.Helper()
	h.MustDo(t, t.Context(), h.Priya, d.DocType, d.ID, doc.ActionClose, doc.ActionInput{Version: d.Version, Reason: lkCloseReason})
	lkState(t, h, d.DocType, d.ID, doc.StateClosed)
}

// amcCancel cancels the stored document dt id by Meena; the setup fails on an error.
func amcCancel(t *testing.T, h *fixturetest.H, dt string, id int64) {
	t.Helper()
	if err := lkCancel(t, h, h.Load(t, dt, id)); err != nil {
		t.Fatalf("setup: cancel %s %d: %v", dt, id, err)
	}
	lkState(t, h, dt, id, doc.StateCancelled)
}

// amcAmend amends the stored cancelled document dt id by Suresh (non-admin)
// and returns the new draft.
func amcAmend(t *testing.T, h *fixturetest.H, dt string, id int64) doc.Doc {
	t.Helper()
	d := h.Load(t, dt, id)
	res := h.MustDo(t, t.Context(), h.Suresh, dt, id, doc.ActionAmend, doc.ActionInput{Version: d.Version})
	n := h.Load(t, dt, res.ID)
	if n.State != doc.StateDraft || n.AmendedFromID != id {
		t.Fatalf("setup: amendment %d is %s amended from %d, want a draft amended from %d", n.ID, n.State, n.AmendedFromID, id)
	}
	return n
}

// amcUnchanged fails unless the stored document equals before and the audit
// log still has count rows (nothing written).
func amcUnchanged(t *testing.T, h *fixturetest.H, before doc.Doc, count int) {
	t.Helper()
	fixturetest.Eq(t, h.Load(t, before.DocType, before.ID), before)
	if got := h.AuditCount(t); got != count {
		t.Errorf("AuditCount = %d, want %d (nothing written)", got, count)
	}
}

// amcReceiptOnClosedOrder is the G0c line setup: approved PO/26-27/00001 with
// lines, approved GR/26-27/00001 on its line 1 (qty 4), the order closed,
// then the receipt cancelled. It returns the order and the cancelled receipt.
func amcReceiptOnClosedOrder(t *testing.T, h *fixturetest.H, lines ...fixturetest.OL) (doc.Doc, doc.Doc) {
	t.Helper()
	o := lkApprovedOrder(t, h, lines...)
	r := lkApprovedReceipt(t, h, fixturetest.RL{OrderLineID: o.Lines[0].ID, Qty: "4"})
	amcClose(t, h, h.Load(t, fixture.Order, o.ID))
	amcCancel(t, h, fixture.Receipt, r.ID)
	return h.Load(t, fixture.Order, o.ID), h.Load(t, fixture.Receipt, r.ID)
}

// CD-272 Gap 2, CD-279 item 10: an amendment keeps the closed order line link
// its amended-from receipt held, through save and submit.
func TestDoc_AmendKeepsClosedLink(t *testing.T) {
	h := fixturetest.New(t)
	o, r := amcReceiptOnClosedOrder(t, h)
	n := amcAmend(t, h, fixture.Receipt, r.ID)

	in := h.In(n)
	in.Lines[0]["qty"] = "3"
	res, err := h.Do(t, t.Context(), h.Suresh, fixture.Receipt, n.ID, doc.ActionSave, in)
	if err != nil {
		t.Fatalf("save the amendment keeping its closed link = %v, want no error", err)
	}
	if _, err := h.Do(t, t.Context(), h.Suresh, fixture.Receipt, n.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version}); err != nil {
		t.Fatalf("submit the amendment keeping its closed link = %v, want no error", err)
	}
	got := h.Load(t, fixture.Receipt, n.ID)
	if got.State != doc.StateApproved || got.Number != lkGR+"-1" {
		t.Errorf("amendment is %s %q, want approved %s-1", got.State, got.Number, lkGR)
	}
	lkState(t, h, fixture.Order, o.ID, doc.StateClosed)
}

// CD-272 Gap 2, CD-279 item 10: a line the amended-from receipt didn't hold
// (another line of the same closed order) is refused with DOC-017's text.
func TestDoc_AmendClosedLink_NewLinkRefused(t *testing.T) {
	h := fixturetest.New(t)
	o, r := amcReceiptOnClosedOrder(t, h,
		fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10"}, fixturetest.OL{Item: "WIRE-18G", Qty: "5"})
	n := amcAmend(t, h, fixture.Receipt, r.ID)

	in := h.In(n)
	in.Lines = append(in.Lines, map[string]string{"order_line_id": lkID(o.Lines[1].ID), "qty": "2"})
	count := h.AuditCount(t)
	_, err := h.Do(t, t.Context(), h.Suresh, fixture.Receipt, n.ID, doc.ActionSave, in)
	lkInvalid(t, err, doctype.FieldError{Field: "order_line_id", Line: 2, Message: fmt.Sprintf(amcMsgSourceClosedLine, 2, lkPO)})
	amcUnchanged(t, h, n, count)
}

// CD-272 Gap 2, CD-279 item 10: a cancelled target is refused on the
// amendment as today (DOC-038), on save and on submit.
func TestDoc_AmendClosedLink_CancelledStillRefused(t *testing.T) {
	h := fixturetest.New(t)
	o := lkApprovedOrder(t, h)
	r := lkApprovedReceipt(t, h, fixturetest.RL{OrderLineID: o.Lines[0].ID, Qty: "4"})
	amcCancel(t, h, fixture.Receipt, r.ID) // DOC-052: the receipt blocks the order's cancel
	amcCancel(t, h, fixture.Order, o.ID)
	n := amcAmend(t, h, fixture.Receipt, r.ID)
	want := fmt.Sprintf(lkMsgSourceCancelled, lkPO)

	t.Run("submit", func(t *testing.T) {
		count := h.AuditCount(t)
		_, err := h.Do(t, t.Context(), h.Suresh, fixture.Receipt, n.ID, doc.ActionSubmit, doc.ActionInput{Version: n.Version})
		lkSourceRefused(t, err, want)
		amcUnchanged(t, h, n, count)
	})
	t.Run("save", func(t *testing.T) {
		in := h.In(n)
		in.Lines[0]["qty"] = "3" // a changed value: an unchanged save writes and checks nothing (DOC-029)
		count := h.AuditCount(t)
		_, err := h.Do(t, t.Context(), h.Suresh, fixture.Receipt, n.ID, doc.ActionSave, in)
		lkSourceRefused(t, err, want)
		amcUnchanged(t, h, n, count)
	})
}

// CD-272 Gap 2, CD-279 item 10: a new receipt on the closed order is still
// refused with DOC-017's text, even though a cancelled receipt held the link.
func TestDoc_FreshSourceClosedRefused(t *testing.T) {
	h := fixturetest.New(t)
	o, _ := amcReceiptOnClosedOrder(t, h)

	receipts, lines, count := lkRows(t, h, "fixture_receipt"), lkRows(t, h, "fixture_receipt_line"), h.AuditCount(t)
	_, err := h.Do(t, t.Context(), h.Suresh, fixture.Receipt, 0, doc.ActionSave, lkReceiptIn(o.Lines[0].ID, "1"))
	lkSourceRefused(t, err, fmt.Sprintf(lkMsgSourceClosed, lkPO))
	if got := lkRows(t, h, "fixture_receipt"); got != receipts {
		t.Errorf("fixture_receipt rows = %d, want %d (nothing written)", got, receipts)
	}
	if got := lkRows(t, h, "fixture_receipt_line"); got != lines {
		t.Errorf("fixture_receipt_line rows = %d, want %d (nothing written)", got, lines)
	}
	if got := h.AuditCount(t); got != count {
		t.Errorf("AuditCount = %d, want %d (nothing written)", got, count)
	}
}

// amcSubmitted saves a document of dt by p with header values and one line,
// submits it (no approval: p2l DocTypes aren't Approvable) and returns it,
// failing unless it is approved as number.
func amcSubmitted(t *testing.T, h *fixturetest.H, p auth.Principal, dt string, values, line map[string]string, number string) doc.Doc {
	t.Helper()
	res := h.MustDo(t, t.Context(), p, dt, 0, doc.ActionSave, doc.ActionInput{Values: values, Lines: []map[string]string{line}})
	h.MustDo(t, t.Context(), p, dt, res.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version})
	d := h.Load(t, dt, res.ID)
	if d.State != doc.StateApproved || d.Number != number {
		t.Fatalf("setup: %s %d is %s %q, want approved %s", dt, d.ID, d.State, d.Number, number)
	}
	return d
}

// amcProblems runs Engine.Problems in a read transaction, as the check page does.
func amcProblems(t *testing.T, h *fixturetest.H, dt string, id int64) []doctype.FieldError {
	t.Helper()
	var got []doctype.FieldError
	err := h.DB.Read(t.Context(), func(tx *db.Tx) (err error) {
		got, err = h.Engine.Problems(t.Context(), tx, dt, id)
		return err
	})
	if err != nil {
		t.Fatalf("Problems(%s, %d): %v", dt, id, err)
	}
	return got
}

// CD-272 Gap 2, CD-279 item 10: the header branch (the GRN's
// purchase_order_id). p2l.Child's header head_id links p2l.Head
// (p2_lifecycle_test.go). The amendment keeps its closed header link through
// the check page, save and submit; changing it to another closed head is
// refused with DOC-017's text.
func TestDoc_AmendKeepsClosedHeaderLink(t *testing.T) {
	h := fixturetest.New(t)
	p2lHeaderTables(t, h)
	h.Naming.Prefix = map[string]string{p2lHead: "HD", p2lChild: "CH"}

	head1 := amcSubmitted(t, h, h.Priya, p2lHead, map[string]string{"title": "Head one"}, map[string]string{"head_note": "a"}, amcHD1)
	head2 := amcSubmitted(t, h, h.Priya, p2lHead, map[string]string{"title": "Head two"}, map[string]string{"head_note": "b"}, amcHD2)
	child := amcSubmitted(t, h, h.Suresh, p2lChild, map[string]string{"head_id": lkID(head1.ID)}, map[string]string{"child_note": "Bay 1"}, amcCH)
	amcClose(t, h, h.Load(t, p2lHead, head1.ID))
	amcClose(t, h, h.Load(t, p2lHead, head2.ID))
	amcCancel(t, h, p2lChild, child.ID)
	n := amcAmend(t, h, p2lChild, child.ID)

	t.Run("changed header id refused", func(t *testing.T) {
		in := h.In(n)
		in.Values["head_id"] = lkID(head2.ID)
		count := h.AuditCount(t)
		_, err := h.Do(t, t.Context(), h.Suresh, p2lChild, n.ID, doc.ActionSave, in)
		lkInvalid(t, err, doctype.FieldError{Field: "head_id", Line: 0, Message: fmt.Sprintf(amcMsgHeadClosed, amcHD2)})
		amcUnchanged(t, h, n, count)
	})
	t.Run("kept header link", func(t *testing.T) {
		if got := amcProblems(t, h, p2lChild, n.ID); len(got) != 0 {
			t.Errorf("Problems(amendment) = %+v, want none", got)
		}
		in := h.In(n)
		in.Lines[0]["child_note"] = "Bay 2"
		res, err := h.Do(t, t.Context(), h.Suresh, p2lChild, n.ID, doc.ActionSave, in)
		if err != nil {
			t.Fatalf("save the amendment keeping its closed header link = %v, want no error", err)
		}
		if _, err := h.Do(t, t.Context(), h.Suresh, p2lChild, n.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version}); err != nil {
			t.Fatalf("submit the amendment keeping its closed header link = %v, want no error", err)
		}
		got := h.Load(t, p2lChild, n.ID)
		if got.State != doc.StateApproved || got.Number != amcCH+"-1" {
			t.Errorf("amendment is %s %q, want approved %s-1", got.State, got.Number, amcCH)
		}
	})
}

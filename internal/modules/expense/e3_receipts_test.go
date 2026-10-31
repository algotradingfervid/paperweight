package expense_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// E3 · Receipts (plan MOD-EXP E3; spec §3.3, §5; CD-264).

const (
	e3Deepak = "deepak@vardhan.test" // Employee, EMP-007, reports to Priya
	e3Priya  = "priya@vardhan.test"  // Deepak's reporting manager
	e3Meena  = "meena@vardhan.test"  // Director: cancel (all)
)

// e3NoReceipt is spec §5's submit text, with DOC's `Line {n}: ` (CD-264 (8)).
func e3NoReceipt(n int) string {
	return fmt.Sprintf("Line %d: Add a receipt or say why there isn't one.", n)
}

// e3File045 is FILE-045's text for an expense (files owns it; CD-142).
func e3File045(n int) string {
	return fmt.Sprintf("The receipt on line %d belongs to another document. Attach it to this expense first.", n)
}

// e3File046 is FILE-046's text (files owns it; CD-142).
func e3File046(n int) string {
	return fmt.Sprintf("This file is the receipt on line %d. Clear it there first.", n)
}

// e3WantField fails unless err is the engine's 422 field error (KindInvalid)
// holding text on line (1-based); field "" accepts any field.
func e3WantField(t *testing.T, err error, line int, field, text string) {
	t.Helper()
	var de *doc.Error
	if !errors.As(err, &de) {
		t.Errorf("err = %v (%T), want a *doc.Error with %q on line %d", err, err, text, line)
		return
	}
	if de.Kind != doc.KindInvalid || de.Status() != http.StatusUnprocessableEntity {
		t.Errorf("err kind = %s, status %d, want %s, 422", de.Kind, de.Status(), doc.KindInvalid)
	}
	if !slices.ContainsFunc(de.FieldErrors(), func(f doctype.FieldError) bool {
		return f.Line == line && f.Message == text && (field == "" || f.Field == field)
	}) {
		t.Errorf("field errors = %+v, want %q on line %d field %q", de.FieldErrors(), text, line, field)
	}
}

// e3WantFiles fails unless err is the files refusal of kind with text.
func e3WantFiles(t *testing.T, err error, kind files.Kind, text string) {
	t.Helper()
	var fe *files.Error
	if !errors.As(err, &fe) {
		t.Errorf("err = %v (%T), want a *files.Error %q", err, err, text)
		return
	}
	if fe.Kind != kind || fe.Message != text {
		t.Errorf("files error = kind %d %q, want kind %d %q", fe.Kind, fe.Message, kind, text)
	}
}

func e3Do(t *testing.T, e *expensetest.Env, p auth.Principal, id int64, a doc.Action, in doc.ActionInput) doc.Result {
	t.Helper()
	res, err := e.Engine.Do(context.Background(), p, expense.ClaimDocType, id, a, in)
	if err != nil {
		t.Fatalf("%s claim %d as %s: %v", a, id, p.Name, err)
	}
	return res
}

func e3Edit(t *testing.T, e *expensetest.Env, p auth.Principal, id int64, lines map[int]expensetest.Values) doc.Result {
	t.Helper()
	res, err := e.EditClaim(t, p, id, nil, lines)
	if err != nil {
		t.Fatalf("save claim %d lines %v as %s: %v", id, lines, p.Name, err)
	}
	return res
}

// e3Receipt is a line holding hash as its receipt, with no reason.
func e3Receipt(hash string) expensetest.Values {
	return expensetest.Values{"receipt_sha256": hash, "no_receipt_reason": ""}
}

// e3MetaWith is the attachment among ms with hash.
func e3MetaWith(t *testing.T, ms []files.Meta, hash string) files.Meta {
	t.Helper()
	for _, m := range ms {
		if m.SHA256 == hash {
			return m
		}
	}
	t.Fatalf("no attachment with SHA-256 %s among %+v", hash, ms)
	return files.Meta{}
}

// EXP-006: a line with no receipt and no reason saves as a draft and is
// refused at submit (BeforeSubmit, doc.Invalid) on that line, with nothing
// written; a reason lets it submit.
func TestExp_ReceiptOrReason(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, e3Deepak)

	res := e.DraftClaim(t, deepak, nil, expensetest.Values{"no_receipt_reason": ""})
	if res.State != doc.StateDraft {
		t.Fatalf("saved state = %s, want draft", res.State)
	}
	if got := e.LoadDoc(t, expense.ClaimDocType, res.ID).Lines[0]; got["receipt_sha256"] != "" || got["no_receipt_reason"] != "" {
		t.Fatalf("stored line 1 = %v, want no receipt and no reason", got)
	}

	e.WantRefused(t, e3NoReceipt(1), func() error {
		_, err := e.Engine.Do(context.Background(), deepak, expense.ClaimDocType, res.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version})
		e3WantField(t, err, 1, "", e3NoReceipt(1))
		return err
	})

	res = e3Edit(t, e, deepak, res.ID, map[int]expensetest.Values{1: {"no_receipt_reason": "Paper file"}})
	res = e3Do(t, e, deepak, res.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version})
	if res.State != doc.StatePending {
		t.Errorf("submitted with reason `Paper file`: state = %s, want pending", res.State)
	}

	// {n} is the line's number: line 1 has a reason, line 2 has neither.
	two := e.DraftClaim(t, deepak, nil, nil, expensetest.Values{"no_receipt_reason": ""})
	e.WantRefused(t, e3NoReceipt(2), func() error {
		_, err := e.Engine.Do(context.Background(), deepak, expense.ClaimDocType, two.ID, doc.ActionSubmit, doc.ActionInput{Version: two.Version})
		e3WantField(t, err, 2, "", e3NoReceipt(2))
		return err
	})
}

// EXP-007: a hash not among this claim's attachments is refused on save with
// FILE-045's text on that line's receipt field; a file a line holds can't be
// removed (FILE-046) in draft and rejected, and can be once the line is
// cleared; one bill on two lines saves, and the guard names the first line.
func TestExp_ReceiptLinks(t *testing.T) {
	e := expensetest.New(t)
	deepak, priya := e.P(t, e3Deepak), e.P(t, e3Priya)

	a := e.DraftClaim(t, deepak, nil, nil, nil) // two lines, each with a reason
	b := e.DraftClaim(t, deepak, nil)
	taxi := e.MustAttach(t, deepak, expense.ClaimDocType, a.ID, "taxi.jpg", filestest.JPEG(2048))
	hotel := e.MustAttach(t, deepak, expense.ClaimDocType, b.ID, "hotel.pdf", filestest.PDF(4096))

	// Another claim's receipt.
	e.WantRefused(t, e3File045(2), func() error {
		_, err := e.EditClaim(t, deepak, a.ID, nil, map[int]expensetest.Values{2: e3Receipt(hotel.SHA256)})
		e3WantField(t, err, 2, "receipt_sha256", e3File045(2))
		return err
	})

	// This claim's receipt links, and the file can't be removed in draft.
	res := e3Edit(t, e, deepak, a.ID, map[int]expensetest.Values{2: e3Receipt(taxi.SHA256)})
	if got := e.LoadDoc(t, expense.ClaimDocType, a.ID).Lines[1]["receipt_sha256"]; got != taxi.SHA256 {
		t.Fatalf("stored line 2 receipt = %q, want taxi.jpg's %s", got, taxi.SHA256)
	}
	e.WantRefused(t, e3File046(2), func() error {
		err := e.RemoveFile(deepak, taxi.ID)
		e3WantFiles(t, err, files.KindInUse, e3File046(2))
		return err
	})

	// Rejected: still refused.
	res = e3Do(t, e, deepak, a.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version})
	if res.State != doc.StatePending {
		t.Fatalf("submit: state = %s, want pending", res.State)
	}
	res = e3Do(t, e, priya, a.ID, doc.ActionReject, doc.ActionInput{Version: res.Version, Reason: "Wrong project."})
	if res.State != doc.StateRejected {
		t.Fatalf("reject: state = %s, want rejected", res.State)
	}
	e.WantRefused(t, e3File046(2), func() error {
		err := e.RemoveFile(deepak, taxi.ID)
		e3WantFiles(t, err, files.KindInUse, e3File046(2))
		return err
	})

	// Cleared on the line, the file can be removed.
	res = e3Do(t, e, deepak, a.ID, doc.ActionRevise, doc.ActionInput{Version: res.Version})
	if res.State != doc.StateDraft {
		t.Fatalf("revise: state = %s, want draft", res.State)
	}
	e3Edit(t, e, deepak, a.ID, map[int]expensetest.Values{2: {"receipt_sha256": "", "no_receipt_reason": "Bill lost."}})
	if err := e.RemoveFile(deepak, taxi.ID); err != nil {
		t.Fatalf("remove taxi.jpg after clearing line 2: %v, want nil", err)
	}
	if got := e.FilesOf(t, expense.ClaimDocType, a.ID); len(got) != 0 {
		t.Errorf("claim A's files after the removal = %+v, want none", got)
	}

	// One bill on two lines (`Same receipt as line 1`).
	bill := e.MustAttach(t, deepak, expense.ClaimDocType, a.ID, "bill.pdf", filestest.PDF(3000))
	e3Edit(t, e, deepak, a.ID, map[int]expensetest.Values{1: e3Receipt(bill.SHA256), 2: e3Receipt(bill.SHA256)})
	for i, l := range e.LoadDoc(t, expense.ClaimDocType, a.ID).Lines {
		if l["receipt_sha256"] != bill.SHA256 {
			t.Errorf("stored line %d receipt = %q, want bill.pdf's %s", i+1, l["receipt_sha256"], bill.SHA256)
		}
	}
	e.WantRefused(t, e3File046(1), func() error {
		err := e.RemoveFile(deepak, bill.ID)
		e3WantFiles(t, err, files.KindInUse, e3File046(1))
		return err
	})
}

// EXP-008: amend copies the attachments (new ids, same SHA-256, FILE-043)
// and each line keeps its receipt_sha256, so the links survive: the copies
// are guarded on the new draft, and it saves and submits with no receipt
// refusal.
func TestExp_AmendKeepsReceipts(t *testing.T) {
	e := expensetest.New(t)
	deepak, priya, meena := e.P(t, e3Deepak), e.P(t, e3Priya), e.P(t, e3Meena)

	c := e.DraftClaim(t, deepak, nil, nil, nil)
	taxi := e.MustAttach(t, deepak, expense.ClaimDocType, c.ID, "taxi.jpg", filestest.JPEG(2048))
	hotel := e.MustAttach(t, deepak, expense.ClaimDocType, c.ID, "hotel.pdf", filestest.PDF(4096))
	res := e3Edit(t, e, deepak, c.ID, map[int]expensetest.Values{1: e3Receipt(taxi.SHA256), 2: e3Receipt(hotel.SHA256)})
	res = e3Do(t, e, deepak, c.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version})
	res = e3Do(t, e, priya, c.ID, doc.ActionApprove, doc.ActionInput{Version: res.Version})
	if res.State != doc.StateApproved {
		t.Fatalf("approve: state = %s, want approved", res.State)
	}
	res = e3Do(t, e, meena, c.ID, doc.ActionCancel, doc.ActionInput{Version: res.Version, Reason: "Wrong month."})
	if res.State != doc.StateCancelled {
		t.Fatalf("cancel: state = %s, want cancelled", res.State)
	}

	am := e3Do(t, e, deepak, c.ID, doc.ActionAmend, doc.ActionInput{Version: res.Version})
	if am.State != doc.StateDraft || am.ID == c.ID {
		t.Fatalf("amend = %+v, want a new draft", am)
	}

	// New ids, the same hashes.
	copies := e.FilesOf(t, expense.ClaimDocType, am.ID)
	if len(copies) != 2 {
		t.Fatalf("the amended draft's files = %+v, want 2", copies)
	}
	newTaxi, newHotel := e3MetaWith(t, copies, taxi.SHA256), e3MetaWith(t, copies, hotel.SHA256)
	for _, m := range copies {
		if m.ID == taxi.ID || m.ID == hotel.ID {
			t.Errorf("copied attachment %+v keeps an original id (%d, %d)", m, taxi.ID, hotel.ID)
		}
	}

	// Each line keeps its receipt.
	lines := e.LoadDoc(t, expense.ClaimDocType, am.ID).Lines
	if len(lines) != 2 || lines[0]["receipt_sha256"] != taxi.SHA256 || lines[1]["receipt_sha256"] != hotel.SHA256 {
		t.Fatalf("amended lines = %v, want line 1 %s and line 2 %s", lines, taxi.SHA256, hotel.SHA256)
	}

	// The links hold on the copies: the guard finds each one's line.
	for _, tc := range []struct {
		m    files.Meta
		line int
	}{{newTaxi, 1}, {newHotel, 2}} {
		e.WantRefused(t, e3File046(tc.line), func() error {
			err := e.RemoveFile(deepak, tc.m.ID)
			e3WantFiles(t, err, files.KindInUse, e3File046(tc.line))
			return err
		})
	}

	// Saves and submits with no receipt refusal.
	saved, err := e.EditClaim(t, deepak, am.ID, expensetest.Values{"purpose": "Pune Metro site visit, corrected"}, nil)
	if err != nil {
		t.Fatalf("save the amended draft: %v, want nil", err)
	}
	if saved.Version == am.Version {
		t.Fatalf("save the amended draft: version %d unchanged, want a write", saved.Version)
	}
	sub := e3Do(t, e, deepak, am.ID, doc.ActionSubmit, doc.ActionInput{Version: saved.Version})
	if sub.State != doc.StatePending {
		t.Errorf("submit the amended draft: state = %s, want pending", sub.State)
	}
}

// EXP-009: the receipt check, the submit rule and the remove guard read in
// the caller's transaction (files.Store.ListTx, CD-209, CD-213): every step
// runs as a non-admin on a pool of one connection, where a second connection
// fails testkit.NoSecondConn, and under -tags nestcheck a nested transaction
// exits the binary.
func TestExp_ReceiptCheckSameTx(t *testing.T) {
	e := expensetest.New(t)
	deepak, priya := e.P(t, e3Deepak), e.P(t, e3Priya)

	c := e.DraftClaim(t, deepak, nil, nil, expensetest.Values{"no_receipt_reason": ""})
	other := e.DraftClaim(t, deepak, nil)
	taxi := e.MustAttach(t, deepak, expense.ClaimDocType, c.ID, "taxi.jpg", filestest.JPEG(2048))
	hotel := e.MustAttach(t, deepak, expense.ClaimDocType, other.ID, "hotel.pdf", filestest.PDF(4096))

	do := func(p auth.Principal, a doc.Action, in doc.ActionInput) (doc.Result, error) {
		var res doc.Result
		err := testkit.NoSecondConn(t, e.DB, func(ctx context.Context) error {
			var err error
			res, err = e.Engine.Do(ctx, p, expense.ClaimDocType, c.ID, a, in)
			return err
		})
		return res, err
	}

	// Save: a receipt on this claim passes.
	res, err := do(deepak, doc.ActionSave, e.ClaimEdit(t, c.ID, nil, map[int]expensetest.Values{1: e3Receipt(taxi.SHA256)}))
	if err != nil {
		t.Fatalf("save line 1 with taxi.jpg: %v, want nil", err)
	}

	// Save: another claim's receipt is refused.
	in := e.ClaimEdit(t, c.ID, nil, map[int]expensetest.Values{2: e3Receipt(hotel.SHA256)})
	e.WantRefused(t, e3File045(2), func() error {
		_, err := do(deepak, doc.ActionSave, in)
		e3WantField(t, err, 2, "receipt_sha256", e3File045(2))
		return err
	})

	// Remove: the guard reads through the remove's tx.
	e.WantRefused(t, e3File046(1), func() error {
		err := testkit.NoSecondConn(t, e.DB, func(ctx context.Context) error {
			return e.DB.Write(ctx, func(tx *db.Tx) error { return e.Files.Remove(ctx, tx, deepak, taxi.ID) })
		})
		e3WantFiles(t, err, files.KindInUse, e3File046(1))
		return err
	})

	// Submit: line 2 has neither receipt nor reason.
	e.WantRefused(t, e3NoReceipt(2), func() error {
		_, err := do(deepak, doc.ActionSubmit, doc.ActionInput{Version: res.Version})
		e3WantField(t, err, 2, "", e3NoReceipt(2))
		return err
	})

	// With a reason it submits, and the manager approves (Validate and
	// BeforeSubmit run again on approve).
	res, err = do(deepak, doc.ActionSave, e.ClaimEdit(t, c.ID, nil, map[int]expensetest.Values{2: {"no_receipt_reason": "Auto fare, no bill."}}))
	if err != nil {
		t.Fatalf("save line 2 with a reason: %v, want nil", err)
	}
	if res, err = do(deepak, doc.ActionSubmit, doc.ActionInput{Version: res.Version}); err != nil || res.State != doc.StatePending {
		t.Fatalf("submit = %+v, %v; want pending", res, err)
	}
	if res, err = do(priya, doc.ActionApprove, doc.ActionInput{Version: res.Version}); err != nil || res.State != doc.StateApproved {
		t.Fatalf("approve = %+v, %v; want approved", res, err)
	}
}

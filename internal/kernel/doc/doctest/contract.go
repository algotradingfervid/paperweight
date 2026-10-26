// Package doctest holds the doc.Engine contract suite (WP3 Task 1, CD-182):
// the behaviours other packages rely on, copied from the DOC P1 cases, run
// against the real engine. docfake does not run it: its Do only returns what
// SetResult set (CD-186); its reads are checked against the engine in
// docfake's tests.
package doctest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Setup is what the suite needs from the service under test besides the
// Engine: one approvable DocType with lines, two principals, documents in a
// given state, and control over approval rules, rights and visibility.
type Setup interface {
	// DocType is the approvable document type the suite runs on.
	DocType() string
	// Owner holds every right on DocType unless Deny takes one away.
	Owner() auth.Principal
	// Approver is another user whom APR lets approve and reject DocType.
	Approver() auth.Principal
	// NewInput is a valid save input for a new document with one line
	// (Version 0). It sets EditField to a value other than EditField's.
	NewInput() doc.ActionInput
	// EditField names a header field and a valid new value for it, both in
	// the form value format Load returns.
	EditField() (field, value string)
	// Read runs fn with a read transaction for Load and Info.
	Read(ctx context.Context, fn func(tx *db.Tx) error) error
	// Doc creates a document from NewInput, owned by Owner, in state s, and
	// returns its id. An approved, closed or cancelled document is numbered.
	Doc(t *testing.T, s doc.State) int64
	// NeedApproval sets whether a submit of DocType needs approval from now on.
	NeedApproval(t *testing.T, need bool)
	// Deny takes right r on DocType away from p.
	Deny(t *testing.T, p auth.Principal, r auth.Right)
	// Hide makes document id of DocType invisible to p (outside p's read scope).
	Hide(t *testing.T, p auth.Principal, id int64)
}

// Reason is a valid reason for reject, cancel and close (3–500 characters).
const Reason = "Wrong supplier GSTIN"

// Messages the DOC cases fix (DOC-005, DOC-011, DOC-013, DOC-015, DOC-062,
// DOC-065, DOC-069). %s is the document's display name.
const (
	msgStale             = "Someone else changed %s after you opened it. Reload it and make your changes again."
	msgGone              = "This document no longer exists. It may have been deleted."
	msgAlreadySubmitted  = "%s has already been submitted."
	msgNotWaiting        = "%s is not waiting for approval. It is %s."
	msgNotSubmitted      = "%s hasn't been submitted, so there is nothing to withdraw."
	msgOnlyRejected      = "Only rejected documents can be revised."
	msgOnlyApprovedCanc  = "Only approved documents can be cancelled. Delete the draft instead."
	msgOnlyApprovedClose = "Only approved documents can be closed."
	msgOnlyCancelled     = "Only cancelled documents can be amended."
	msgCancelFirst       = "Only cancelled documents can be amended. Cancel %s first."
	msgPendingLocked     = "%s is waiting for approval and can't be changed. Withdraw it to make changes."
	msgPendingCancel     = "%s is waiting for approval. Withdraw it first if it needs changes."
	msgPendingDelete     = "%s is waiting for approval and can't be deleted. Withdraw it first, then delete it."
	msgApprovedLocked    = "%s is approved and can't be edited. Cancel and amend it to make changes."
	msgApprovedWithdraw  = "%s has already been approved and can't be withdrawn. Cancel and amend it to make changes."
	msgApprovedDelete    = "Approved documents can't be deleted. Cancel it instead."
	msgRejectedLocked    = "%s was rejected. Revise it to move it back to draft."
	msgRejectedDelete    = "Rejected documents can't be deleted. Revise it to move it back to draft, then delete it."
	msgClosed            = "%s is closed and can't be changed."
	msgCancelledLocked   = "%s is cancelled and can't be changed. Amend it to make a corrected copy."
	msgCancelledDelete   = "Cancelled documents are kept for the record and can't be deleted."
	msgCancelReason      = "Enter a reason for cancelling (at least 3 characters)."
	msgCloseReason       = "Enter a reason for closing (at least 3 characters)."
	msgAmended           = "%s has already been amended as %s. Open that document instead."
)

// missingID is a document id no Setup creates.
const missingID int64 = 987654

// Run runs every contract behaviour; newSvc returns a fresh Engine and its Setup.
func Run(t *testing.T, newSvc func(t *testing.T) (doc.Engine, Setup)) {
	t.Helper()
	for _, b := range behaviours {
		t.Run(b.name, func(t *testing.T) {
			e, s := newSvc(t)
			b.run(t, &suite{e: e, s: s, dt: s.DocType()})
		})
	}
}

var behaviours = []struct {
	name string
	run  func(t *testing.T, c *suite)
}{
	{"DOC-001_CreateIsDraftAtVersion1", createIsDraft},
	{"DOC-029_SaveBumpsVersion", saveBumpsVersion},
	{"DOC-069_StaleSaveRefused", staleSave},
	{"DOC-070_StaleTransitionRefused", staleTransition},
	{"DOC-006_SubmitWithoutRuleApproves", submitNoRule},
	{"DOC-007_SubmitWithRuleIsPending", submitWithRule},
	{"DOC-008_ApprovePending", approvePending},
	{"DOC-009_RejectThenRevise", rejectThenRevise},
	{"DOC-095_WithdrawPending", withdrawPending},
	{"DOC-013_CancelNeedsReason", cancelNeedsReason},
	{"DOC-015_CloseNeedsReason", closeNeedsReason},
	{"DOC-061_DeleteDraft", deleteDraft},
	{"DOC-064_AmendCancelledOnce", amendOnce},
	{"DOC-102_MissingIsNotFound", missingNotFound},
	{"DOC-104_HiddenIsNotFound", hiddenNotFound},
	{"DOC-103_DeniedRightIsForbidden", deniedForbidden},
	{"DOC-005_RefusedTransitions", refusedTransitions},
}

type suite struct {
	e  doc.Engine
	s  Setup
	dt string
}

func (c *suite) do(t *testing.T, p auth.Principal, id int64, a doc.Action, in doc.ActionInput) (doc.Result, error) {
	t.Helper()
	return c.e.Do(t.Context(), p, c.dt, id, a, in)
}

func (c *suite) mustDo(t *testing.T, p auth.Principal, id int64, a doc.Action, in doc.ActionInput) doc.Result {
	t.Helper()
	res, err := c.do(t, p, id, a, in)
	if err != nil {
		t.Fatalf("Do(%s %d %s) error = %v, want nil", c.dt, id, a, err)
	}
	return res
}

func (c *suite) load(id int64) (doc.Record, error) {
	var rec doc.Record
	err := c.s.Read(context.Background(), func(tx *db.Tx) (err error) {
		rec, err = c.e.Load(context.Background(), tx, c.dt, id)
		return err
	})
	return rec, err
}

func (c *suite) info(id int64) (doctype.Info, error) {
	var info doctype.Info
	err := c.s.Read(context.Background(), func(tx *db.Tx) (err error) {
		info, err = c.e.Info(context.Background(), tx, c.dt, id)
		return err
	})
	return info, err
}

func (c *suite) mustLoad(t *testing.T, id int64) doc.Record {
	t.Helper()
	rec, err := c.load(id)
	if err != nil {
		t.Fatalf("Load(%s, %d) error = %v, want nil", c.dt, id, err)
	}
	return rec
}

func (c *suite) mustInfo(t *testing.T, id int64) doctype.Info {
	t.Helper()
	info, err := c.info(id)
	if err != nil {
		t.Fatalf("Info(%s, %d) error = %v, want nil", c.dt, id, err)
	}
	return info
}

// in is the stored document as an input at its current version, ready to post back.
func (c *suite) in(t *testing.T, id int64) doc.ActionInput {
	t.Helper()
	rec := c.mustLoad(t, id)
	return doc.ActionInput{Version: rec.Version, Values: rec.Values, Lines: rec.Lines, Reason: Reason}
}

func draftName(id int64) string { return fmt.Sprintf("Draft %d", id) }

// wantResult checks a successful action's Result.
func wantResult(t *testing.T, res doc.Result, id int64, s doc.State, version int) {
	t.Helper()
	if res.ID != id || res.State != s || res.Version != version {
		t.Errorf("Result = {ID:%d State:%s Version:%d}, want {ID:%d State:%s Version:%d}", res.ID, res.State, res.Version, id, s, version)
	}
}

// wantStored checks the stored state and version through Load and Info, and
// that Display is "Draft {id}" for an unnumbered document, else its number.
func (c *suite) wantStored(t *testing.T, id int64, s doc.State, version int) doctype.Info {
	t.Helper()
	rec := c.mustLoad(t, id)
	info := c.mustInfo(t, id)
	if rec.Version != version {
		t.Errorf("Load(%d).Version = %d, want %d", id, rec.Version, version)
	}
	if info.State != s || rec.Info.State != s {
		t.Errorf("state of %d: Info %q, Load %q; want %q", id, info.State, rec.Info.State, s)
	}
	if info.ID != id || info.DocType != c.dt {
		t.Errorf("Info(%d) = {DocType:%q ID:%d}, want {DocType:%q ID:%d}", id, info.DocType, info.ID, c.dt, id)
	}
	wantDisplay := info.Number
	if info.Number == "" {
		wantDisplay = draftName(id)
	}
	if info.Display != wantDisplay {
		t.Errorf("Info(%d).Display = %q, want %q (Number %q)", id, info.Display, wantDisplay, info.Number)
	}
	return info
}

// wantErr checks that err is a *doc.Error of kind with status and message.
func wantErr(t *testing.T, err error, kind doc.ErrorKind, status int, msg string) {
	t.Helper()
	var de *doc.Error
	if !errors.As(err, &de) {
		t.Fatalf("err = %v (%T), want a *doc.Error of kind %q, status %d, message %q", err, err, kind, status, msg)
	}
	if de.Kind != kind || de.Status() != status || de.Message != msg {
		t.Errorf("err = {Kind:%q Status:%d Message:%q}, want {Kind:%q Status:%d Message:%q}", de.Kind, de.Status(), de.Message, kind, status, msg)
	}
}

func wantNumbered(t *testing.T, res doc.Result, info doctype.Info) {
	t.Helper()
	if res.Number == "" || info.Number != res.Number {
		t.Errorf("number: Result %q, Info %q; want the same non-empty number", res.Number, info.Number)
	}
}

func wantUnnumbered(t *testing.T, res doc.Result, info doctype.Info) {
	t.Helper()
	if res.Number != "" || info.Number != "" {
		t.Errorf("number: Result %q, Info %q; want none", res.Number, info.Number)
	}
}

// DOC-001, DOC-097, DOC-102: create gives a draft at version 1, unnumbered,
// shown as "Draft {id}", owned by its creator; Load agrees with Info.
func createIsDraft(t *testing.T, c *suite) {
	in := c.s.NewInput()
	res := c.mustDo(t, c.s.Owner(), 0, doc.ActionSave, in)
	if res.ID <= 0 {
		t.Fatalf("create Result.ID = %d, want > 0", res.ID)
	}
	wantResult(t, res, res.ID, doc.StateDraft, 1)
	info := c.wantStored(t, res.ID, doc.StateDraft, 1)
	wantUnnumbered(t, res, info)
	if info.Display != draftName(res.ID) {
		t.Errorf("Info.Display = %q, want %q", info.Display, draftName(res.ID))
	}
	if info.CreatedBy != c.s.Owner().ID {
		t.Errorf("Info.CreatedBy = %d, want %d", info.CreatedBy, c.s.Owner().ID)
	}
	rec := c.mustLoad(t, res.ID)
	if fmt.Sprintf("%+v", rec.Info) != fmt.Sprintf("%+v", info) {
		t.Errorf("Load.Info = %+v, want Info = %+v", rec.Info, info)
	}
	field, _ := c.s.EditField()
	if got, want := rec.Values[field], in.Values[field]; got != want {
		t.Errorf("Load.Values[%q] = %q, want %q", field, got, want)
	}
	if len(rec.Lines) != len(in.Lines) {
		t.Errorf("Load.Lines has %d lines, want %d", len(rec.Lines), len(in.Lines))
	}
}

// DOC-029, DOC-074: a save that changes a field bumps the version by exactly
// 1; posting the loaded record back unchanged doesn't.
func saveBumpsVersion(t *testing.T, c *suite) {
	id := c.s.Doc(t, doc.StateDraft)
	in := c.in(t, id)
	field, value := c.s.EditField()
	in.Values[field] = value
	res := c.mustDo(t, c.s.Owner(), id, doc.ActionSave, in)
	wantResult(t, res, id, doc.StateDraft, in.Version+1)
	c.wantStored(t, id, doc.StateDraft, in.Version+1)
	if got := c.mustLoad(t, id).Values[field]; got != value {
		t.Errorf("after save Values[%q] = %q, want %q", field, got, value)
	}
	same := c.in(t, id)
	res = c.mustDo(t, c.s.Owner(), id, doc.ActionSave, same)
	if res.Version != same.Version {
		t.Errorf("unchanged save Result.Version = %d, want %d (no bump)", res.Version, same.Version)
	}
	c.wantStored(t, id, doc.StateDraft, same.Version)
}

// DOC-069: a save from an older version is refused (409) and keeps the newer values.
func staleSave(t *testing.T, c *suite) {
	id := c.s.Doc(t, doc.StateDraft)
	old := c.in(t, id)
	field, value := c.s.EditField()
	first := c.in(t, id)
	first.Values[field] = value
	c.mustDo(t, c.s.Owner(), id, doc.ActionSave, first)

	old.Values = c.s.NewInput().Values
	_, err := c.do(t, c.s.Owner(), id, doc.ActionSave, old)
	wantErr(t, err, doc.KindStale, 409, fmt.Sprintf(msgStale, draftName(id)))
	c.wantStored(t, id, doc.StateDraft, old.Version+1)
	if got := c.mustLoad(t, id).Values[field]; got != value {
		t.Errorf("after stale save Values[%q] = %q, want the first save's %q", field, got, value)
	}
}

// DOC-070: transitions check the version too; nothing changes.
func staleTransition(t *testing.T, c *suite) {
	id := c.s.Doc(t, doc.StateDraft)
	in := c.in(t, id)
	in.Version--
	_, err := c.do(t, c.s.Owner(), id, doc.ActionSubmit, in)
	wantErr(t, err, doc.KindStale, 409, fmt.Sprintf(msgStale, draftName(id)))
	c.wantStored(t, id, doc.StateDraft, in.Version+1)

	ap := c.s.Doc(t, doc.StateApproved)
	in = c.in(t, ap)
	in.Version--
	num := c.mustInfo(t, ap).Number
	_, err = c.do(t, c.s.Owner(), ap, doc.ActionCancel, in)
	wantErr(t, err, doc.KindStale, 409, fmt.Sprintf(msgStale, num))
	c.wantStored(t, ap, doc.StateApproved, in.Version+1)
}

// DOC-006, DOC-023: submit with no approval rule approves and numbers the document.
func submitNoRule(t *testing.T, c *suite) {
	c.s.NeedApproval(t, false)
	id := c.s.Doc(t, doc.StateDraft)
	in := c.in(t, id)
	res := c.mustDo(t, c.s.Owner(), id, doc.ActionSubmit, in)
	wantResult(t, res, id, doc.StateApproved, in.Version+1)
	info := c.wantStored(t, id, doc.StateApproved, in.Version+1)
	wantNumbered(t, res, info)
}

// DOC-007: submit with an approval rule leaves the document pending and unnumbered.
func submitWithRule(t *testing.T, c *suite) {
	c.s.NeedApproval(t, true)
	id := c.s.Doc(t, doc.StateDraft)
	in := c.in(t, id)
	res := c.mustDo(t, c.s.Owner(), id, doc.ActionSubmit, in)
	wantResult(t, res, id, doc.StatePending, in.Version+1)
	info := c.wantStored(t, id, doc.StatePending, in.Version+1)
	wantUnnumbered(t, res, info)
}

// DOC-008: approve moves pending to approved and numbers it.
func approvePending(t *testing.T, c *suite) {
	id := c.s.Doc(t, doc.StatePending)
	in := c.in(t, id)
	res := c.mustDo(t, c.s.Approver(), id, doc.ActionApprove, in)
	wantResult(t, res, id, doc.StateApproved, in.Version+1)
	info := c.wantStored(t, id, doc.StateApproved, in.Version+1)
	wantNumbered(t, res, info)
}

// DOC-009: reject keeps the document unnumbered; revise moves it back to draft.
func rejectThenRevise(t *testing.T, c *suite) {
	id := c.s.Doc(t, doc.StatePending)
	in := c.in(t, id)
	res := c.mustDo(t, c.s.Approver(), id, doc.ActionReject, in)
	wantResult(t, res, id, doc.StateRejected, in.Version+1)
	info := c.wantStored(t, id, doc.StateRejected, in.Version+1)
	wantUnnumbered(t, res, info)

	in = c.in(t, id)
	res = c.mustDo(t, c.s.Owner(), id, doc.ActionRevise, in)
	wantResult(t, res, id, doc.StateDraft, in.Version+1)
	info = c.wantStored(t, id, doc.StateDraft, in.Version+1)
	wantUnnumbered(t, res, info)
}

// DOC-095: withdraw moves pending back to an editable, unnumbered draft.
func withdrawPending(t *testing.T, c *suite) {
	id := c.s.Doc(t, doc.StatePending)
	in := c.in(t, id)
	res := c.mustDo(t, c.s.Owner(), id, doc.ActionWithdraw, in)
	wantResult(t, res, id, doc.StateDraft, in.Version+1)
	info := c.wantStored(t, id, doc.StateDraft, in.Version+1)
	wantUnnumbered(t, res, info)

	edit := c.in(t, id)
	field, value := c.s.EditField()
	edit.Values[field] = value
	res = c.mustDo(t, c.s.Owner(), id, doc.ActionSave, edit)
	wantResult(t, res, id, doc.StateDraft, edit.Version+1)
}

// DOC-012, DOC-013: cancel needs a reason (422); with one, approved becomes cancelled.
func cancelNeedsReason(t *testing.T, c *suite) {
	id := c.s.Doc(t, doc.StateApproved)
	num := c.mustInfo(t, id).Number
	for _, r := range []string{"", "  "} {
		in := c.in(t, id)
		in.Reason = r
		_, err := c.do(t, c.s.Owner(), id, doc.ActionCancel, in)
		wantErr(t, err, doc.KindInvalid, 422, msgCancelReason)
		c.wantStored(t, id, doc.StateApproved, in.Version)
	}
	in := c.in(t, id)
	res := c.mustDo(t, c.s.Owner(), id, doc.ActionCancel, in)
	wantResult(t, res, id, doc.StateCancelled, in.Version+1)
	info := c.wantStored(t, id, doc.StateCancelled, in.Version+1)
	if info.Number != num {
		t.Errorf("cancelled Number = %q, want %q kept", info.Number, num)
	}
}

// DOC-014, DOC-015: close needs a reason (422); with one, approved becomes closed.
func closeNeedsReason(t *testing.T, c *suite) {
	id := c.s.Doc(t, doc.StateApproved)
	in := c.in(t, id)
	in.Reason = ""
	_, err := c.do(t, c.s.Owner(), id, doc.ActionClose, in)
	wantErr(t, err, doc.KindInvalid, 422, msgCloseReason)
	c.wantStored(t, id, doc.StateApproved, in.Version)

	in = c.in(t, id)
	res := c.mustDo(t, c.s.Owner(), id, doc.ActionClose, in)
	wantResult(t, res, id, doc.StateClosed, in.Version+1)
	c.wantStored(t, id, doc.StateClosed, in.Version+1)
}

// wantNotFound checks a missing document's error: 404, KindNotFound,
// errors.Is doctype.ErrNotFound.
func wantNotFound(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, doctype.ErrNotFound) {
		t.Errorf("%s error = %v, want errors.Is doctype.ErrNotFound", what, err)
	}
	var de *doc.Error
	if !errors.As(err, &de) || de.Kind != doc.KindNotFound || de.Status() != 404 {
		t.Errorf("%s error = %v (%T), want a *doc.Error of kind %q, status 404", what, err, err, doc.KindNotFound)
	}
}

// DOC-061: deleting a draft removes it: Load, Info and Do then find nothing.
func deleteDraft(t *testing.T, c *suite) {
	id := c.s.Doc(t, doc.StateDraft)
	in := c.in(t, id)
	if _, err := c.do(t, c.s.Owner(), id, doc.ActionDelete, in); err != nil {
		t.Fatalf("Do(delete) error = %v, want nil", err)
	}
	_, err := c.load(id)
	wantNotFound(t, "Load after delete", err)
	_, err = c.info(id)
	wantNotFound(t, "Info after delete", err)
	_, err = c.do(t, c.s.Owner(), id, doc.ActionSave, in)
	wantNotFound(t, "Do(save) after delete", err)
	wantErr(t, err, doc.KindNotFound, 404, msgGone)
}

// DOC-064, DOC-065, DOC-068: amend copies a cancelled document into a new
// unnumbered draft at version 1, leaves the original alone, and only once.
func amendOnce(t *testing.T, c *suite) {
	id := c.s.Doc(t, doc.StateCancelled)
	in := c.in(t, id)
	before := c.mustInfo(t, id)
	res := c.mustDo(t, c.s.Owner(), id, doc.ActionAmend, in)
	if res.ID <= 0 || res.ID == id {
		t.Fatalf("amend Result.ID = %d, want a new document id (not %d)", res.ID, id)
	}
	wantResult(t, res, res.ID, doc.StateDraft, 1)
	info := c.wantStored(t, res.ID, doc.StateDraft, 1)
	wantUnnumbered(t, res, info)
	if info.CreatedBy != c.s.Owner().ID {
		t.Errorf("amendment CreatedBy = %d, want the amender %d", info.CreatedBy, c.s.Owner().ID)
	}
	field, _ := c.s.EditField()
	if got, want := c.mustLoad(t, res.ID).Values[field], in.Values[field]; got != want {
		t.Errorf("amendment Values[%q] = %q, want the original's %q", field, got, want)
	}
	c.wantStored(t, id, doc.StateCancelled, in.Version)
	if got := c.mustInfo(t, id).Number; got != before.Number {
		t.Errorf("original Number after amend = %q, want %q", got, before.Number)
	}

	_, err := c.do(t, c.s.Owner(), id, doc.ActionAmend, c.in(t, id))
	wantErr(t, err, doc.KindRefused, 409, fmt.Sprintf(msgAmended, before.Display, draftName(res.ID)))
}

// DOC-097, DOC-102, DOC-103: a missing document is not found everywhere.
func missingNotFound(t *testing.T, c *suite) {
	_, err := c.load(missingID)
	wantNotFound(t, "Load(missing)", err)
	_, err = c.info(missingID)
	wantNotFound(t, "Info(missing)", err)
	for _, a := range []doc.Action{doc.ActionSave, doc.ActionSubmit, doc.ActionApprove, doc.ActionCancel, doc.ActionDelete} {
		_, err = c.do(t, c.s.Owner(), missingID, a, doc.ActionInput{Version: 1, Reason: Reason})
		wantNotFound(t, fmt.Sprintf("Do(%s, missing)", a), err)
		wantErr(t, err, doc.KindNotFound, 404, msgGone)
	}
}

// DOC-104: approve or reject by a user who can't see the document is AUTH's
// not-found error, and nothing changes.
func hiddenNotFound(t *testing.T, c *suite) {
	id := c.s.Doc(t, doc.StatePending)
	c.s.Hide(t, c.s.Approver(), id)
	in := c.in(t, id)
	for _, a := range []doc.Action{doc.ActionApprove, doc.ActionReject} {
		_, err := c.do(t, c.s.Approver(), id, a, in)
		if !errors.Is(err, doctype.ErrNotFound) {
			t.Errorf("Do(%s) on a hidden document error = %v, want errors.Is doctype.ErrNotFound", a, err)
		}
		if errors.Is(err, auth.ErrForbidden) {
			t.Errorf("Do(%s) on a hidden document error = %v, want not found, not forbidden", a, err)
		}
		if de := (*doc.Error)(nil); errors.As(err, &de) {
			t.Errorf("Do(%s) on a hidden document error = %v, want AUTH's error passed through, not a *doc.Error", a, err)
		}
	}
	c.wantStored(t, id, doc.StatePending, in.Version)
}

// DOC-103 (CD-44): a missing right is AUTH's 403 error passed through, and nothing changes.
func deniedForbidden(t *testing.T, c *suite) {
	cases := []struct {
		state doc.State
		a     doc.Action
		right auth.Right
	}{
		{doc.StateDraft, doc.ActionSave, "write"},
		{doc.StateDraft, doc.ActionSubmit, "submit"},
		{doc.StateDraft, doc.ActionDelete, "delete"},
		{doc.StateApproved, doc.ActionCancel, "cancel"},
		{doc.StateApproved, doc.ActionClose, "submit"},
		{doc.StateCancelled, doc.ActionAmend, "amend"},
	}
	ids := make([]int64, len(cases))
	for i, tc := range cases {
		ids[i] = c.s.Doc(t, tc.state)
	}
	for _, r := range []auth.Right{"write", "submit", "delete", "cancel", "amend"} {
		c.s.Deny(t, c.s.Owner(), r)
	}
	for i, tc := range cases {
		in := c.in(t, ids[i])
		_, err := c.do(t, c.s.Owner(), ids[i], tc.a, in)
		if !errors.Is(err, auth.ErrForbidden) {
			t.Errorf("Do(%s) without %q in %s error = %v, want errors.Is auth.ErrForbidden", tc.a, tc.right, tc.state, err)
		}
		if de := (*doc.Error)(nil); errors.As(err, &de) {
			t.Errorf("Do(%s) without %q in %s error = %v, want AUTH's error passed through, not a *doc.Error", tc.a, tc.right, tc.state, err)
		}
		c.wantStored(t, ids[i], tc.state, in.Version)
	}
}

type refusalRow struct {
	state doc.State
	a     doc.Action
	stale bool   // send the previous version
	msg   string // %s, when present, is the display name
}

var refusalRows = []refusalRow{
	{doc.StateDraft, doc.ActionApprove, false, fmt.Sprintf(msgNotWaiting, "%s", "Draft")},
	{doc.StateDraft, doc.ActionReject, false, fmt.Sprintf(msgNotWaiting, "%s", "Draft")},
	{doc.StateDraft, doc.ActionWithdraw, false, msgNotSubmitted},
	{doc.StateDraft, doc.ActionRevise, false, msgOnlyRejected},
	{doc.StateDraft, doc.ActionCancel, false, msgOnlyApprovedCanc},
	{doc.StateDraft, doc.ActionClose, false, msgOnlyApprovedClose},
	{doc.StateDraft, doc.ActionAmend, false, msgOnlyCancelled},
	{doc.StatePending, doc.ActionSave, false, msgPendingLocked},
	{doc.StatePending, doc.ActionSubmit, false, msgPendingLocked},
	{doc.StatePending, doc.ActionSubmit, true, msgAlreadySubmitted}, // DOC-011
	{doc.StatePending, doc.ActionCancel, false, msgPendingCancel},
	{doc.StatePending, doc.ActionDelete, false, msgPendingDelete},
	{doc.StatePending, doc.ActionClose, false, msgOnlyApprovedClose},
	{doc.StatePending, doc.ActionAmend, false, msgOnlyCancelled},
	{doc.StateApproved, doc.ActionSave, false, msgApprovedLocked},
	{doc.StateApproved, doc.ActionSubmit, false, msgApprovedLocked},
	{doc.StateApproved, doc.ActionSubmit, true, msgAlreadySubmitted}, // DOC-011
	{doc.StateApproved, doc.ActionWithdraw, false, msgApprovedWithdraw},
	{doc.StateApproved, doc.ActionApprove, false, fmt.Sprintf(msgNotWaiting, "%s", "Approved")},
	{doc.StateApproved, doc.ActionAmend, false, msgCancelFirst},
	{doc.StateApproved, doc.ActionDelete, false, msgApprovedDelete},
	{doc.StateRejected, doc.ActionSave, false, msgRejectedLocked},
	{doc.StateRejected, doc.ActionSubmit, false, msgRejectedLocked},
	{doc.StateRejected, doc.ActionCancel, false, msgRejectedLocked},
	{doc.StateRejected, doc.ActionWithdraw, false, msgRejectedLocked},
	{doc.StateRejected, doc.ActionClose, false, msgOnlyApprovedClose},
	{doc.StateRejected, doc.ActionDelete, false, msgRejectedDelete},
	{doc.StateClosed, doc.ActionSave, false, msgClosed},
	{doc.StateClosed, doc.ActionCancel, false, msgClosed},
	{doc.StateClosed, doc.ActionWithdraw, false, msgClosed},
	{doc.StateClosed, doc.ActionAmend, false, msgClosed},
	{doc.StateClosed, doc.ActionApprove, false, fmt.Sprintf(msgNotWaiting, "%s", "Closed")},
	{doc.StateClosed, doc.ActionDelete, false, msgApprovedDelete},
	{doc.StateCancelled, doc.ActionSave, false, msgCancelledLocked},
	{doc.StateCancelled, doc.ActionSubmit, false, msgCancelledLocked},
	{doc.StateCancelled, doc.ActionCancel, false, msgCancelledLocked},
	{doc.StateCancelled, doc.ActionWithdraw, false, msgCancelledLocked},
	{doc.StateCancelled, doc.ActionClose, false, msgOnlyApprovedClose},
	{doc.StateCancelled, doc.ActionApprove, false, fmt.Sprintf(msgNotWaiting, "%s", "Cancelled")},
	{doc.StateCancelled, doc.ActionDelete, false, msgCancelledDelete},
}

// DOC-005, DOC-011, DOC-016, DOC-062: every refused transition is a 409
// KindRefused error with the case's exact message, and nothing changes.
func refusedTransitions(t *testing.T, c *suite) {
	for _, row := range refusalRows {
		name := fmt.Sprintf("%s+%s", row.state, row.a)
		if row.stale {
			name += "_stale"
		}
		t.Run(name, func(t *testing.T) {
			id := c.s.Doc(t, row.state)
			in := c.in(t, id)
			version := in.Version
			if row.stale {
				in.Version--
			}
			p := c.s.Owner()
			if row.a == doc.ActionApprove || row.a == doc.ActionReject {
				p = c.s.Approver()
			}
			display := c.mustInfo(t, id).Display
			_, err := c.do(t, p, id, row.a, in)
			want := row.msg
			if strings.Contains(want, "%s") {
				want = fmt.Sprintf(want, display)
			}
			wantErr(t, err, doc.KindRefused, 409, want)
			c.wantStored(t, id, row.state, version)
		})
	}
}

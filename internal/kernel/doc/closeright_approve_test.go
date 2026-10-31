package doc_test

import (
	"fmt"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/testkit"
)

// MOD-0 M0-24 (CD-280 D2): a submittable, non-approvable DocType whose
// CloseRight is "approve" (as purchase.Request, CD-253/255).
const (
	m024Request = "m024.Request"
	m024Table   = "m024_request"
	m024Label   = "Close request"
	m024Plural  = "Close requests"
)

var _ = func() bool {
	doc.Register(doc.DocType{
		Name: m024Request, Label: m024Label, PluralLabel: m024Plural,
		Table: m024Table, LinesTable: m024Table + "_line", Submittable: true,
		CloseRight: "approve", Hooks: m010Hooks{table: m024Table},
		Fields: []doc.Field{
			{Name: "note", Label: "Note", Type: doc.Text},
			{Name: "stamp", Label: "Stamp", Type: doc.Text},
			{Name: "line_note", Label: "Line note", Type: doc.Text, OnLine: true},
		},
	})
	return true
}()

// m024Tables creates the m024 tables (the document table convention).
func m024Tables(t *testing.T, h *fixturetest.H) {
	t.Helper()
	h.Exec(t, `CREATE TABLE `+m024Table+` (
		id INTEGER PRIMARY KEY, number TEXT UNIQUE, state TEXT NOT NULL DEFAULT 'draft',
		state_reason TEXT NOT NULL DEFAULT '', amended_from_id INTEGER, amendment_root_id INTEGER,
		amendment_no INTEGER NOT NULL DEFAULT 0, version INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL, created_by INTEGER NOT NULL, updated_at TEXT NOT NULL, updated_by INTEGER NOT NULL,
		note TEXT NOT NULL DEFAULT '', stamp TEXT NOT NULL DEFAULT '')`)
	h.Exec(t, `CREATE TABLE `+m024Table+`_line (
		id INTEGER PRIMARY KEY, parent_id INTEGER NOT NULL REFERENCES `+m024Table+` (id) ON DELETE CASCADE,
		line_no INTEGER NOT NULL, line_note TEXT NOT NULL DEFAULT '')`)
}

// m024RealAuth returns an engine on the real auth service over h's database
// and two users set up through AUTH's admin calls: the first holds read and
// approve on m024Request (not submit), the second read and submit (not approve).
func m024RealAuth(t *testing.T, h *fixturetest.H) (doc.Engine, auth.Principal, auth.Principal) {
	t.Helper()
	ctx := t.Context()
	a := auth.New(auth.Deps{
		DB: h.DB, Clock: h.Clock, Directory: testkit.Directory(h.F), Audit: auditfake.New(),
		Refs: &authtest.Refs{}, Info: &authtest.Info{},
	})
	user := func(role, email string, right auth.Right) auth.Principal {
		t.Helper()
		if err := a.CreateRole(ctx, h.Admin, role); err != nil {
			t.Fatalf("CreateRole(%s): %v", role, err)
		}
		rights := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, right: auth.ScopeAll}
		if err := a.SetRights(ctx, h.Admin, role, m024Request, rights); err != nil {
			t.Fatalf("SetRights(%s, %s, {read, %s}) = %v, want nil (CD-280 D2: approve applies under CloseRight approve)", role, m024Request, right, err)
		}
		id, _, err := a.CreateUser(ctx, h.Admin, auth.NewUser{Email: email, Name: email, Password: rqRealPassword, Roles: []string{role}})
		if err != nil {
			t.Fatalf("CreateUser(%s): %v", email, err)
		}
		return rqPrincipal(t, a, id)
	}
	approver := user("Request Closer", "m024-closer@vardhan.test", auth.RightApprove)
	submitter := user("Request Submitter", "m024-submitter@vardhan.test", auth.RightSubmit)
	e := doc.NewEngine(doc.Deps{
		DB: h.DB, Clock: h.Clock, Auth: a, Naming: h.Naming, Numbers: fixturetest.Numbers(),
		Approval: h.Approvals, Files: h.Files,
	})
	return e, approver, submitter
}

// CD-280 D2 on the real auth service and engine: a holder of approve (not
// submit) closes an approved m024Request; a submit-only user is refused with
// AUTH's 403 for approve; an approve action is still refused by DOC (the
// DocType is not approvable, so the document is never waiting).
func TestDoc_Close_CloseRightApprove(t *testing.T) {
	h := fixturetest.New(t)
	m024Tables(t, h)
	e, approver, submitter := m024RealAuth(t, h)
	d := m010Doc(t, h, m024Request, doc.StateApproved)
	in := doc.ActionInput{Version: d.Version, Reason: m010Reason}

	// approve: refused by DOC with its existing not-waiting text.
	count := h.AuditCount(t)
	_, err := e.Do(t.Context(), approver, m024Request, d.ID, doc.ActionApprove, doc.ActionInput{Version: d.Version})
	fixturetest.ErrIs(t, err, doc.KindRefused, fmt.Sprintf(m010NotWaitingFmt, h.Display(t, m024Request, d.ID), "Approved"))
	m010Unchanged(t, h, d, count)

	// close by a submit-only user: AUTH's 403 for approve.
	_, err = e.Do(t.Context(), submitter, m024Request, d.ID, doc.ActionClose, in)
	de, ok := err.(*auth.DeniedError)
	if !ok {
		t.Fatalf("close by a submit-only user: err = %v (%T), want AUTH's *auth.DeniedError unchanged", err, err)
	}
	want := &auth.DeniedError{Right: auth.RightApprove, Label: m024Label, Plural: m024Plural}
	if de.Right != auth.RightApprove || de.Status() != 403 || de.Error() != want.Error() {
		t.Errorf("refusal = %s %d %q; want approve 403 %q", de.Right, de.Status(), de.Error(), want.Error())
	}
	m010Unchanged(t, h, d, count)

	// close by the approve holder.
	res, err := e.Do(t.Context(), approver, m024Request, d.ID, doc.ActionClose, in)
	if err != nil {
		t.Fatalf("close by a user holding approve: %v, want it closed", err)
	}
	after := h.Load(t, m024Request, d.ID)
	if res.State != doc.StateClosed || after.State != doc.StateClosed || after.Version != d.Version+1 {
		t.Errorf("Result.State, State, Version = %q, %q, %d; want closed, closed, %d", res.State, after.State, after.Version, d.Version+1)
	}
	entries := h.Audit(t, m024Request, d.ID)
	if last := entries[len(entries)-1]; last.Action != "close" || last.By != approver.ID {
		t.Errorf("last audit row = %s by %d, want close by %d", last.Action, last.By, approver.ID)
	}
}

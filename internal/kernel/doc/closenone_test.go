package doc_test

import (
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/testkit"
)

// MOD-GRN G0a test DocType: CloseRight is doctype.CloseNone (CD-277 (1),
// CD-279 item 2). Its table is created by g0aTables.
const (
	g0aNoClose      = "g0a.NoClose"
	g0aNoCloseTable = "g0a_noclose"
	// The close refusal for g0aNoClose: "{PluralLabel} can't be closed." (CD-279 (2)).
	g0aCloseText = "Receipt notes can't be closed."
)

var _ = func() bool {
	doc.Register(doc.DocType{
		Name: g0aNoClose, Label: "Receipt note", PluralLabel: "Receipt notes",
		Table: g0aNoCloseTable, LinesTable: g0aNoCloseTable + "_line", Submittable: true,
		CloseRight: doctype.CloseNone, Hooks: m010Hooks{table: g0aNoCloseTable},
		Fields: []doc.Field{
			{Name: "note", Label: "Note", Type: doc.Text},
			{Name: "stamp", Label: "Stamp", Type: doc.Text},
			{Name: "line_note", Label: "Line note", Type: doc.Text, OnLine: true},
		},
	})
	return true
}()

// g0aTables creates the g0aNoClose tables (the document table convention).
func g0aTables(t *testing.T, h *fixturetest.H) {
	t.Helper()
	h.Exec(t, `CREATE TABLE `+g0aNoCloseTable+` (
		id INTEGER PRIMARY KEY, number TEXT UNIQUE, state TEXT NOT NULL DEFAULT 'draft',
		state_reason TEXT NOT NULL DEFAULT '', amended_from_id INTEGER, amendment_root_id INTEGER,
		amendment_no INTEGER NOT NULL DEFAULT 0, version INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL, created_by INTEGER NOT NULL, updated_at TEXT NOT NULL, updated_by INTEGER NOT NULL,
		note TEXT NOT NULL DEFAULT '', stamp TEXT NOT NULL DEFAULT '')`)
	h.Exec(t, `CREATE TABLE `+g0aNoCloseTable+`_line (
		id INTEGER PRIMARY KEY, parent_id INTEGER NOT NULL REFERENCES `+g0aNoCloseTable+` (id) ON DELETE CASCADE,
		line_no INTEGER NOT NULL, line_note TEXT NOT NULL DEFAULT '')`)
}

// g0aRealAuth returns an engine on the real auth service over h's database
// and three users set up through AUTH's admin calls on g0aNoClose: every
// holds every right that applies, all with scope all; ownOnly holds the
// same rights but reads only their own documents, so a document Priya
// created is hidden from them; reader holds only read, scope all (as GRN's
// Purchase User and Director, who hold read, print and export).
func g0aRealAuth(t *testing.T, h *fixturetest.H) (e doc.Engine, every, ownOnly, reader auth.Principal) {
	t.Helper()
	ctx := t.Context()
	a := auth.New(auth.Deps{
		DB: h.DB, Clock: h.Clock, Directory: testkit.Directory(h.F), Audit: auditfake.New(),
		Refs: &authtest.Refs{}, Info: &authtest.Info{},
	})
	dt, ok := doctype.Lookup(g0aNoClose)
	if !ok {
		t.Fatalf("setup: %s is not registered", g0aNoClose)
	}
	user := func(role, email string, read auth.Scope, others bool) auth.Principal {
		t.Helper()
		if err := a.CreateRole(ctx, h.Admin, role); err != nil {
			t.Fatalf("CreateRole(%s): %v", role, err)
		}
		rights := map[auth.Right]auth.Scope{}
		for _, r := range auth.AllRights {
			if others && auth.Applies(dt, r) {
				rights[r] = auth.ScopeAll
			}
		}
		rights[auth.RightRead] = read
		if err := a.SetRights(ctx, h.Admin, role, g0aNoClose, rights); err != nil {
			t.Fatalf("SetRights(%s, %s): %v", role, g0aNoClose, err)
		}
		id, _, err := a.CreateUser(ctx, h.Admin, auth.NewUser{Email: email, Name: email, Password: rqRealPassword, Roles: []string{role}})
		if err != nil {
			t.Fatalf("CreateUser(%s): %v", email, err)
		}
		return rqPrincipal(t, a, id)
	}
	every = user("Every right", "g0a-every@vardhan.test", auth.ScopeAll, true)
	ownOnly = user("Own reader", "g0a-own@vardhan.test", auth.ScopeOwn, true)
	reader = user("Reader only", "g0a-reader@vardhan.test", auth.ScopeAll, false)
	e = doc.NewEngine(doc.Deps{
		DB: h.DB, Clock: h.Clock, Auth: a, Naming: h.Naming, Numbers: fixturetest.Numbers(),
		Approval: h.Approvals, Files: h.Files,
	})
	// Setup check: without the close rule, each user can do everything the
	// grants give, so only CloseNone can refuse the closes below.
	for _, r := range []auth.Right{auth.RightSubmit, auth.RightCancel, auth.RightAmend} {
		if err := a.Can(ctx, every, g0aNoClose, r, nil); err != nil {
			t.Fatalf("setup: the every-right user lacks %s: %v", r, err)
		}
		if err := a.Can(ctx, reader, g0aNoClose, r, nil); err == nil {
			t.Fatalf("setup: the read-only user holds %s", r)
		}
	}
	return e, every, ownOnly, reader
}

// G0a, CD-279 item 2 and CD-264 (7): close on a DocType whose CloseRight is
// doctype.CloseNone is refused as doc.Refused with "{PluralLabel} can't be
// closed." for everyone, Administrator included, and nothing is written.
func TestDoc_CloseNone_Refused(t *testing.T) {
	h := fixturetest.New(t)
	g0aTables(t, h)
	e, every, _, reader := g0aRealAuth(t, h)
	for _, tc := range []struct {
		name string
		p    auth.Principal
	}{
		{"Administrator", h.Admin},
		{"user holding every right", every},
		{"user holding only read", reader}, // the refusal needs read, never the right a close would need
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := m010Doc(t, h, g0aNoClose, doc.StateApproved)
			count := h.AuditCount(t)

			_, err := e.Do(t.Context(), tc.p, g0aNoClose, d.ID, doc.ActionClose,
				doc.ActionInput{Version: d.Version, Reason: m010Reason})
			var de *doc.Error
			if !errors.As(err, &de) {
				t.Fatalf("close: err = %v (%T), want *doc.Error (doc.Refused)", err, err)
			}
			if de.Kind != doc.KindRefused || de.Status() != 409 || de.Error() != g0aCloseText {
				t.Errorf("refusal = %s %d %q; want %s 409 %q", de.Kind, de.Status(), de.Error(), doc.KindRefused, g0aCloseText)
			}
			m010Unchanged(t, h, d, count)
		})
	}

	// Only ActionClose takes the CloseNone branch: submit and cancel on a
	// g0aNoClose document go through as on any submittable DocType.
	t.Run("submit and cancel still work", func(t *testing.T) {
		h.Naming.Prefix = map[string]string{g0aNoClose: "RN"} // submit numbers the document
		d := m010Doc(t, h, g0aNoClose, doc.StateDraft)
		res, err := e.Do(t.Context(), every, g0aNoClose, d.ID, doc.ActionSubmit, doc.ActionInput{Version: d.Version})
		if err != nil {
			t.Fatalf("submit: %v, want success (only close is refused)", err)
		}
		if res.State != doc.StateApproved {
			t.Fatalf("submit Result.State = %q, want approved", res.State)
		}
		a := h.Load(t, g0aNoClose, d.ID)
		res, err = e.Do(t.Context(), every, g0aNoClose, d.ID, doc.ActionCancel, doc.ActionInput{Version: a.Version, Reason: m010Reason})
		if err != nil {
			t.Fatalf("cancel: %v, want success (only close is refused)", err)
		}
		if got := h.Load(t, g0aNoClose, d.ID).State; res.State != doc.StateCancelled || got != doc.StateCancelled {
			t.Errorf("cancel Result.State, State = %q, %q; want cancelled, cancelled", res.State, got)
		}
	})
}

// G0a, CD-279 item 2 and CD-264 (7), with CD-245: the read check comes
// first, so a user who can't read the document gets AUTH's 404, never the
// close text; a missing document stays the engine's not-found.
func TestDoc_CloseNone_HiddenStays404(t *testing.T) {
	h := fixturetest.New(t)
	g0aTables(t, h)
	e, _, ownOnly, _ := g0aRealAuth(t, h)

	t.Run("hidden", func(t *testing.T) {
		d := m010Doc(t, h, g0aNoClose, doc.StateApproved) // created by Priya
		count := h.AuditCount(t)

		_, err := e.Do(t.Context(), ownOnly, g0aNoClose, d.ID, doc.ActionClose,
			doc.ActionInput{Version: d.Version, Reason: m010Reason})
		if err == nil {
			t.Fatal("close of a hidden document: err = nil, want AUTH's 404")
		}
		var se interface{ Status() int }
		if !errors.Is(err, auth.ErrNotFound) || !errors.As(err, &se) || se.Status() != 404 || err.Error() != auth.MsgNotFound {
			t.Errorf("close of a hidden document: err = %q (%T); want AUTH's 404 %q", err.Error(), err, auth.MsgNotFound)
		}
		m010Unchanged(t, h, d, count)
	})

	t.Run("missing", func(t *testing.T) {
		count := h.AuditCount(t)
		_, err := e.Do(t.Context(), ownOnly, g0aNoClose, 999999, doc.ActionClose,
			doc.ActionInput{Version: 1, Reason: m010Reason})
		var de *doc.Error
		if !errors.As(err, &de) || de.Kind != doc.KindNotFound || de.Status() != 404 || !errors.Is(err, doctype.ErrNotFound) {
			t.Fatalf("close of a missing document: err = %v (%T); want the engine's not-found (404)", err, err)
		}
		if de.Error() == g0aCloseText {
			t.Errorf("close of a missing document got the close text %q", g0aCloseText)
		}
		if got := h.AuditCount(t); got != count {
			t.Errorf("AuditCount = %d, want %d (unchanged)", got, count)
		}
	})
}

// TestDoc_CloseRight_OthersUnchanged (G0a, CD-279 item 2, CD-264 (7)) is not
// duplicated here: DOC-108's TestDoc_Close_CloseRight (m010_test.go) already
// covers a blank CloseRight (checks submit) and CloseRight "amend" on the
// real auth service, and is rerun with these tests.

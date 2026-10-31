package doc_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// MOD-0 M0-10 test DocTypes (DOC-107 to DOC-109). Their tables are created by
// m010Tables in the tests that use them.
const (
	m010Plain  = "m010.Plain"  // blank CloseRight; hooks without Closer
	m010Amend  = "m010.Amend"  // CloseRight "amend" (as purchase.Order, CD-247 (3))
	m010Closer = "m010.Closer" // blank CloseRight; hooks implementing doctype.Closer
)

const (
	m010Stamp       = "set by Validate"
	m010ValidateMsg = "Note is wrong."
	m010Reason      = "Balance not needed"
	m010RefusedMsg  = "The stock ledger refused the close."
	m010NumberFmt   = "M10/%05d"
)

var errM010Close = errors.New("m010: OnClose failed")

var _ = func() bool {
	for _, s := range []struct {
		name, label, table, closeRight string
		hooks                          doctype.Hooks
	}{
		{m010Plain, "Plain document", "m010_plain", "", m010Hooks{table: "m010_plain"}},
		{m010Amend, "Amend-close document", "m010_amend", "amend", m010Hooks{table: "m010_amend"}},
		{m010Closer, "Closer document", "m010_closer", "", m010CloserHooks{m010Hooks{table: "m010_closer"}}},
	} {
		doc.Register(doc.DocType{
			Name: s.name, Label: s.label, PluralLabel: s.label + "s",
			Table: s.table, LinesTable: s.table + "_line", Submittable: true,
			CloseRight: s.closeRight, Hooks: s.hooks,
			Fields: []doc.Field{
				{Name: "note", Label: "Note", Type: doc.Text},
				{Name: "stamp", Label: "Stamp", Type: doc.Text},
				{Name: "line_note", Label: "Line note", Type: doc.Text, OnLine: true},
			},
		})
	}
	return true
}()

// m010Tables creates the m010 tables (the document table convention) on h's database.
func m010Tables(t *testing.T, h *fixturetest.H) {
	t.Helper()
	for _, table := range []string{"m010_plain", "m010_amend", "m010_closer"} {
		h.Exec(t, `CREATE TABLE `+table+` (
			id INTEGER PRIMARY KEY, number TEXT UNIQUE, state TEXT NOT NULL DEFAULT 'draft',
			state_reason TEXT NOT NULL DEFAULT '', amended_from_id INTEGER, amendment_root_id INTEGER,
			amendment_no INTEGER NOT NULL DEFAULT 0, version INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL, created_by INTEGER NOT NULL, updated_at TEXT NOT NULL, updated_by INTEGER NOT NULL,
			note TEXT NOT NULL DEFAULT '', stamp TEXT NOT NULL DEFAULT '')`)
		h.Exec(t, `CREATE TABLE `+table+`_line (
			id INTEGER PRIMARY KEY, parent_id INTEGER NOT NULL REFERENCES `+table+` (id) ON DELETE CASCADE,
			line_no INTEGER NOT NULL, line_note TEXT NOT NULL DEFAULT '')`)
	}
}

// m010Rec records what the m010 hooks saw. The hooks act only when the
// context carries one (m010With), so setup saves are untouched.
type m010Rec struct {
	mu sync.Mutex

	validate    bool // Validate writes m010Stamp and returns a field error
	validateIDs []int64
	validateTx  []*db.Tx
	seenFrom    int64  // amended_from_id of the document Validate was given
	seenNote    string // its note
	seenLines   int    // its line count

	closeErr     error
	closeIDs     []int64
	closeTx      []*db.Tx
	closeState   string // state read through tx inside OnClose
	closeVersion int
	closeLast    string // the last audit action read through tx inside OnClose

	errs []error // query failures inside the hooks
}

type m010Key struct{}

func m010With(ctx context.Context, r *m010Rec) context.Context {
	return context.WithValue(ctx, m010Key{}, r)
}

func m010From(ctx context.Context) *m010Rec {
	r, _ := ctx.Value(m010Key{}).(*m010Rec)
	return r
}

// m010Hooks are p2lHooks with a Validate that, when asked, writes on the
// document it is given and returns a field error.
type m010Hooks struct {
	p2lHooks
	table string
}

func (h m010Hooks) Validate(ctx context.Context, tx *db.Tx, id int64) []doctype.FieldError {
	r := m010From(ctx)
	if r == nil || !r.validate {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.validateIDs = append(r.validateIDs, id)
	r.validateTx = append(r.validateTx, tx)
	var from sql.NullInt64
	if err := tx.SQL().QueryRowContext(ctx, `SELECT amended_from_id, note FROM `+h.table+` WHERE id = ?`, id).
		Scan(&from, &r.seenNote); err != nil {
		r.errs = append(r.errs, err)
	}
	r.seenFrom = from.Int64
	if err := tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM `+h.table+`_line WHERE parent_id = ?`, id).
		Scan(&r.seenLines); err != nil {
		r.errs = append(r.errs, err)
	}
	if _, err := tx.SQL().ExecContext(ctx, `UPDATE `+h.table+` SET stamp = ? WHERE id = ?`, m010Stamp, id); err != nil {
		r.errs = append(r.errs, err)
	}
	return []doctype.FieldError{{Field: "note", Message: m010ValidateMsg}}
}

// m010CloserHooks add doctype.Closer to m010Hooks.
type m010CloserHooks struct{ m010Hooks }

var _ doctype.Closer = m010CloserHooks{}

func (h m010CloserHooks) OnClose(ctx context.Context, tx *db.Tx, id int64) error {
	r := m010From(ctx)
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeIDs = append(r.closeIDs, id)
	r.closeTx = append(r.closeTx, tx)
	if err := tx.SQL().QueryRowContext(ctx, `SELECT state, version FROM `+h.table+` WHERE id = ?`, id).
		Scan(&r.closeState, &r.closeVersion); err != nil {
		r.errs = append(r.errs, err)
	}
	entries, err := audit.ForDoc(ctx, tx, m010Closer, id)
	if err != nil {
		r.errs = append(r.errs, err)
	} else if len(entries) > 0 {
		r.closeLast = entries[len(entries)-1].Action
	}
	return r.closeErr
}

// m010Audit records the transaction of every Append and passes the entry to
// the real audit writer.
type m010Audit struct {
	mu  sync.Mutex
	txs []*db.Tx
}

func (a *m010Audit) Append(ctx context.Context, tx *db.Tx, e audit.Entry) error {
	a.mu.Lock()
	a.txs = append(a.txs, tx)
	a.mu.Unlock()
	return audit.Default.Append(ctx, tx, e)
}

func (a *m010Audit) reset() {
	a.mu.Lock()
	a.txs = nil
	a.mu.Unlock()
}

func (a *m010Audit) saw(tx *db.Tx) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return tx != nil && slices.Contains(a.txs, tx)
}

// m010Engine returns an engine on h's doubles whose audit writer is aw.
func m010Engine(h *fixturetest.H, aw *m010Audit) doc.Engine {
	return doc.NewEngine(doc.Deps{
		DB: h.DB, Clock: h.Clock, Auth: h.Auth, Naming: h.Naming, Numbers: fixturetest.Numbers(),
		Approval: h.Approvals, Files: h.Files, Audit: aw,
	})
}

// m010Doc creates a document of dt by Priya with two lines and forces it into s.
func m010Doc(t *testing.T, h *fixturetest.H, dt string, s doc.State) doc.Doc {
	t.Helper()
	res := h.MustDo(t, t.Context(), h.Priya, dt, 0, doc.ActionSave, doc.ActionInput{
		Values: map[string]string{"note": "Cement for block B"},
		Lines:  []map[string]string{{"line_note": "Bay 1"}, {"line_note": "Bay 2"}},
	})
	if s != doc.StateDraft {
		h.ForceState(t, dt, res.ID, s, fmt.Sprintf(m010NumberFmt, res.ID))
	}
	return h.Load(t, dt, res.ID)
}

// m010Unchanged fails unless the document and AuditCount are what they were.
func m010Unchanged(t *testing.T, h *fixturetest.H, before doc.Doc, count int) {
	t.Helper()
	fixturetest.Eq(t, h.Load(t, before.DocType, before.ID), before)
	if got := h.AuditCount(t); got != count {
		t.Errorf("AuditCount = %d, want %d (unchanged)", got, count)
	}
}

// DOC-107
func TestDoc_Amend_RunsValidate(t *testing.T) {
	h := fixturetest.New(t)
	m010Tables(t, h)
	aw := &m010Audit{}
	e := m010Engine(h, aw)
	a := m010Doc(t, h, m010Plain, doc.StateCancelled)
	aw.reset()

	r := &m010Rec{validate: true}
	res, err := e.Do(m010With(t.Context(), r), h.Priya, m010Plain, a.ID, doc.ActionAmend, doc.ActionInput{Version: a.Version})
	if err != nil {
		t.Fatalf("amend with Validate returning a field error: %v, want success (field errors ignored)", err)
	}
	if res.State != doc.StateDraft || res.ID == 0 || res.ID == a.ID {
		t.Fatalf("Result = %+v, want a new draft", res)
	}
	if len(res.Problems) != 0 {
		t.Errorf("Result.Problems = %v, want none (Validate's field errors are ignored)", res.Problems)
	}
	if len(r.errs) != 0 {
		t.Errorf("queries inside Validate failed: %v", r.errs)
	}
	if !slices.Equal(r.validateIDs, []int64{res.ID}) {
		t.Fatalf("Validate called with ids %v, want exactly once with the new draft %d", r.validateIDs, res.ID)
	}
	if !aw.saw(r.validateTx[0]) {
		t.Errorf("Validate's tx is not the amend transaction (the one its audit entries were written in)")
	}
	if r.seenFrom != a.ID || r.seenNote != "Cement for block B" || r.seenLines != 2 {
		t.Errorf("Validate saw amended_from_id, note, lines = %d, %q, %d; want %d, %q, 2 (after the copy)",
			r.seenFrom, r.seenNote, r.seenLines, a.ID, "Cement for block B")
	}

	n := h.Load(t, m010Plain, res.ID)
	if n.State != doc.StateDraft || n.AmendedFromID != a.ID {
		t.Errorf("new draft State, AmendedFromID = %q, %d; want draft, %d", n.State, n.AmendedFromID, a.ID)
	}
	if got := n.Header["stamp"]; got != any(m010Stamp) {
		t.Errorf("new draft stamp = %#v, want %q (Validate's write kept)", got, m010Stamp)
	}
	if got := h.Load(t, m010Plain, a.ID).State; got != doc.StateCancelled {
		t.Errorf("A State = %q, want cancelled", got)
	}
}

// m010RealAuth returns an engine on the real auth service over h's database
// and two users set up through AUTH's admin calls: the first holds read and
// amend on every m010 DocType but not submit, the second read and submit but
// not amend.
func m010RealAuth(t *testing.T, h *fixturetest.H) (doc.Engine, auth.Principal, auth.Principal) {
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
		for _, dt := range []string{m010Plain, m010Amend} {
			rights := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, right: auth.ScopeAll}
			if err := a.SetRights(ctx, h.Admin, role, dt, rights); err != nil {
				t.Fatalf("SetRights(%s, %s): %v", role, dt, err)
			}
		}
		id, _, err := a.CreateUser(ctx, h.Admin, auth.NewUser{Email: email, Name: email, Password: rqRealPassword, Roles: []string{role}})
		if err != nil {
			t.Fatalf("CreateUser(%s): %v", email, err)
		}
		return rqPrincipal(t, a, id)
	}
	amender := user("Amender", "m010-amender@vardhan.test", auth.RightAmend)
	submitter := user("Submitter", "m010-submitter@vardhan.test", auth.RightSubmit)
	e := doc.NewEngine(doc.Deps{
		DB: h.DB, Clock: h.Clock, Auth: a, Naming: h.Naming, Numbers: fixturetest.Numbers(),
		Approval: h.Approvals, Files: h.Files,
	})
	return e, amender, submitter
}

// DOC-108, on the real auth service.
func TestDoc_Close_CloseRight(t *testing.T) {
	h := fixturetest.New(t)
	m010Tables(t, h)
	e, amender, submitter := m010RealAuth(t, h)
	for _, tc := range []struct {
		name         string
		dt           string
		wrong, right auth.Principal
		checked      auth.Right
	}{
		{"blank CloseRight is submit", m010Plain, amender, submitter, auth.RightSubmit},
		{"CloseRight amend", m010Amend, submitter, amender, auth.RightAmend},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := m010Doc(t, h, tc.dt, doc.StateApproved)
			dt, _ := doc.Lookup(tc.dt)
			in := doc.ActionInput{Version: d.Version, Reason: m010Reason}

			count := h.AuditCount(t)
			_, err := e.Do(t.Context(), tc.wrong, tc.dt, d.ID, doc.ActionClose, in)
			de, ok := err.(*auth.DeniedError)
			if !ok {
				t.Fatalf("close by a user without %s: err = %v (%T), want AUTH's *auth.DeniedError unchanged", tc.checked, err, err)
			}
			want := &auth.DeniedError{Right: tc.checked, Label: dt.Label, Plural: dt.PluralLabel}
			if de.Right != tc.checked || de.Status() != 403 || de.Error() != want.Error() {
				t.Errorf("refusal = %s %d %q; want %s 403 %q", de.Right, de.Status(), de.Error(), tc.checked, want.Error())
			}
			m010Unchanged(t, h, d, count)

			res, err := e.Do(t.Context(), tc.right, tc.dt, d.ID, doc.ActionClose, in)
			if err != nil {
				t.Fatalf("close by a user holding %s: %v, want it closed", tc.checked, err)
			}
			after := h.Load(t, tc.dt, d.ID)
			if res.State != doc.StateClosed || after.State != doc.StateClosed || after.Version != d.Version+1 {
				t.Errorf("Result.State, State, Version = %q, %q, %d; want closed, closed, %d", res.State, after.State, after.Version, d.Version+1)
			}
			entries := h.Audit(t, tc.dt, d.ID)
			if last := entries[len(entries)-1]; last.Action != "close" || last.By != tc.right.ID {
				t.Errorf("last audit row = %s by %d, want close by %d", last.Action, last.By, tc.right.ID)
			}
		})
	}
}

// DOC-109
func TestDoc_Close_CloserHook(t *testing.T) {
	h := fixturetest.New(t)
	m010Tables(t, h)
	aw := &m010Audit{}
	e := m010Engine(h, aw)
	in := func(d doc.Doc) doc.ActionInput { return doc.ActionInput{Version: d.Version, Reason: m010Reason} }

	if _, ok := any(m010Hooks{}).(doctype.Closer); ok {
		t.Fatal("setup: m010Hooks must not implement doctype.Closer")
	}

	// OnClose runs once, inside the close tx, after the state and transition.
	d := m010Doc(t, h, m010Closer, doc.StateApproved)
	aw.reset()
	r := &m010Rec{}
	res, err := e.Do(m010With(t.Context(), r), h.Priya, m010Closer, d.ID, doc.ActionClose, in(d))
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if res.State != doc.StateClosed {
		t.Errorf("Result.State = %q, want closed", res.State)
	}
	if !slices.Equal(r.closeIDs, []int64{d.ID}) {
		t.Fatalf("OnClose called with ids %v, want exactly once with %d", r.closeIDs, d.ID)
	}
	if len(r.errs) != 0 {
		t.Errorf("queries inside OnClose failed: %v", r.errs)
	}
	if r.closeState != string(doc.StateClosed) || r.closeVersion != d.Version+1 || r.closeLast != "close" {
		t.Errorf("OnClose read state, version, last audit action = %q, %d, %q; want closed, %d, close",
			r.closeState, r.closeVersion, r.closeLast, d.Version+1)
	}
	if !aw.saw(r.closeTx[0]) {
		t.Errorf("OnClose's tx is not the close transaction (the one its transition was written in)")
	}
	if got := h.Load(t, m010Closer, d.ID).State; got != doc.StateClosed {
		t.Errorf("State = %q, want closed", got)
	}

	// An error from OnClose rolls the close back and is returned like any hook
	// error: a plain error as is, a *doc.Error with its kind and message.
	for _, tc := range []struct {
		name  string
		err   error
		check func(t *testing.T, err error)
	}{
		{"plain error", errM010Close, func(t *testing.T, err error) {
			if !errors.Is(err, errM010Close) {
				t.Errorf("err = %v, want OnClose's error", err)
			}
		}},
		{"refusal", doc.Refused(m010RefusedMsg), func(t *testing.T, err error) {
			fixturetest.ErrIs(t, err, doc.KindRefused, m010RefusedMsg)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := m010Doc(t, h, m010Closer, doc.StateApproved)
			count := h.AuditCount(t)
			r := &m010Rec{closeErr: tc.err}
			_, err := e.Do(m010With(t.Context(), r), h.Priya, m010Closer, d.ID, doc.ActionClose, in(d))
			if !slices.Equal(r.closeIDs, []int64{d.ID}) {
				t.Fatalf("OnClose called with ids %v, want exactly once with %d", r.closeIDs, d.ID)
			}
			tc.check(t, err)
			m010Unchanged(t, h, d, count)
			if got := h.Load(t, m010Closer, d.ID).State; got != doc.StateApproved {
				t.Errorf("State = %q, want approved (rolled back)", got)
			}
		})
	}

	// Hooks without Closer close as before (DOC-014).
	p := m010Doc(t, h, m010Plain, doc.StateApproved)
	r = &m010Rec{closeErr: errM010Close}
	if _, err := e.Do(m010With(t.Context(), r), h.Priya, m010Plain, p.ID, doc.ActionClose, in(p)); err != nil {
		t.Fatalf("close without Closer: %v", err)
	}
	if got := h.Load(t, m010Plain, p.ID).State; got != doc.StateClosed {
		t.Errorf("State without Closer = %q, want closed", got)
	}
}

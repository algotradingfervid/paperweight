package docscreen_test

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
)

// m04NoClose is a test DocType whose CloseRight is doctype.CloseNone, as
// GRN's is (CD-277). No registered DocType sets it yet.
const m04NoClose = "m04.NoClose"

var m04NoCloseOnce sync.Once

// m04NoCloseEnv registers m04NoClose, creates its table on e's database and
// gives the Hooker role (the open user's) every right that applies to it,
// submit included, which a blank CloseRight would close with.
func m04NoCloseEnv(t *testing.T, e *hookedEnv) {
	t.Helper()
	m04NoCloseOnce.Do(func() {
		doctype.Register(doctype.DocType{
			Name: m04NoClose, Label: "No-close receipt", PluralLabel: "No-close receipts",
			Table: "m04_no_close", LinesTable: "m04_no_close_line", Submittable: true, CloseRight: doctype.CloseNone, Hooks: m04Hooks{},
			Fields: []doctype.Field{
				{Name: "remarks", Label: "Remarks", Type: doctype.Text, MaxLen: 500},
				{Name: "note", Label: "Note", Type: doctype.Text, OnLine: true},
			},
		})
	})
	e.fx.Exec(t, `CREATE TABLE m04_no_close (id INTEGER PRIMARY KEY AUTOINCREMENT, number TEXT UNIQUE, state TEXT NOT NULL DEFAULT 'draft',
		state_reason TEXT NOT NULL DEFAULT '', amended_from_id INTEGER, amendment_root_id INTEGER, amendment_no INTEGER NOT NULL DEFAULT 0,
		version INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL, created_by INTEGER NOT NULL, updated_at TEXT NOT NULL, updated_by INTEGER NOT NULL,
		remarks TEXT NOT NULL DEFAULT '')`)
	e.fx.Exec(t, `CREATE TABLE m04_no_close_line (id INTEGER PRIMARY KEY AUTOINCREMENT, parent_id INTEGER NOT NULL, line_no INTEGER NOT NULL, note TEXT NOT NULL DEFAULT '')`)
	e.fx.Naming.Prefix[m04NoClose] = "MN"
	rs := map[auth.Right]auth.Scope{}
	for _, r := range []auth.Right{"read", "create", "write", "submit", "cancel", "amend", "delete"} {
		rs[r] = auth.ScopeAll
	}
	if err := m04Auth(t, e).SetRights(t.Context(), e.fx.Admin, "Hooker", m04NoClose, rs); err != nil {
		t.Fatalf("SetRights(Hooker, %s): %v", m04NoClose, err)
	}
}

// m04RecAuth is the real AUTH service, recording the right of every Can and
// CanTx.
type m04RecAuth struct {
	auth.Service
	mu     sync.Mutex
	rights []auth.Right
}

func (a *m04RecAuth) saw(r auth.Right) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rights = append(a.rights, r)
}

func (a *m04RecAuth) Can(ctx context.Context, p auth.Principal, dt string, r auth.Right, rec *auth.RecordRef) error {
	a.saw(r)
	return a.Service.Can(ctx, p, dt, r, rec)
}

func (a *m04RecAuth) CanTx(ctx context.Context, tx *db.Tx, p auth.Principal, dt string, r auth.Right, rec *auth.RecordRef) error {
	a.saw(r)
	return a.Service.CanTx(ctx, tx, p, dt, r, rec)
}

func (a *m04RecAuth) asked() []auth.Right {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.rights)
}

// CD-277: with CloseRight doctype.CloseNone, the document page shows no
// Close to a user holding every right, and GET /close is a missing record's
// 404, decided before any AUTH call, so AUTH is never asked about the
// unknown right "none" (Applies would refuse it as a 403).
func TestModule_CloseNone(t *testing.T) {
	e := newHookedEnv(t)
	m04NoCloseEnv(t, e)
	ctx := t.Context()
	res, err := e.engine.Do(ctx, e.open, m04NoClose, 0, doc.ActionSave, doc.ActionInput{
		Values: map[string]string{"remarks": "kept open"}, Lines: []map[string]string{{"note": "one"}},
	})
	if err != nil {
		t.Fatalf("saving an %s draft: %v", m04NoClose, err)
	}
	if sub, err := e.engine.Do(ctx, e.open, m04NoClose, res.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version}); err != nil || sub.State != doctype.StateApproved {
		t.Fatalf("submitting %s %d: state %s, err %v; want approved", m04NoClose, res.ID, sub.State, err)
	}
	var reasons m04Calls
	var noFail error
	e.d.Modules = map[string]docscreen.Module{m04NoClose: m04ReasonModule(&reasons, &noFail)}
	rec := &m04RecAuth{Service: e.d.Auth}
	e.d.Auth = rec
	e.remount()
	base := m04URL(m04NoClose, res.ID)

	page := m04Page(t, e.get(t, base), http.StatusOK, "document page")
	if n := page.Find(`a[href="` + base + `/close"]`).Length(); n != 0 {
		t.Errorf("CloseNone: %d Close links on the document page, want none", n)
	}
	if !slices.Contains(page.Find("a").Map(func(_ int, s *goquery.Selection) string { return s.AttrOr("href", "") }), base+"/cancel") {
		t.Error("the document page shows no Cancel: the user's rights aren't what this test needs")
	}

	missing := e.get(t, m04URL(m04NoClose, res.ID+1000)+"/close")
	m04Same404(t, e.get(t, base+"/close"), missing, "CloseNone GET close")
	if got := rec.asked(); slices.Contains(got, auth.Right(doctype.CloseNone)) {
		t.Errorf("AUTH was asked about the right %q: %q", doctype.CloseNone, got)
	}

	// A POST gets the same 404, before any AUTH call or hook, and nothing
	// is written.
	rec.rights = nil
	version := m04Version(t, e, m04NoClose, res.ID)
	form := func() url.Values { return url.Values{"version": {version}, "reason": {"Balance not needed"}} }
	m04Same404(t, e.post(t, base+"/close", form()), e.post(t, m04URL(m04NoClose, res.ID+1000)+"/close", form()), "CloseNone POST close")
	if got := rec.asked(); len(got) != 0 {
		t.Errorf("POST close asked AUTH about %q, want no call", got)
	}
	if reasons.n != 0 {
		t.Errorf("Reason was called %d times on a CloseNone close, want 0", reasons.n)
	}
	if got := m04Version(t, e, m04NoClose, res.ID); got != version {
		t.Errorf("POST close wrote: version %s, want %s", got, version)
	}
}

// m04ReadClose is a test DocType whose CloseRight, read, is none of the
// rights the document page asks about anyway (pageRights).
const m04ReadClose = "m04.ReadClose"

var m04ReadCloseOnce sync.Once

// CD-255: the Close button asks AUTH about the DocType's CloseRight even
// when it isn't one of the page's own rights, so a reader of m04ReadClose,
// with no other right, sees Close and opens the close page.
func TestModule_CloseRightOutsidePageRights(t *testing.T) {
	e := newHookedEnv(t)
	m04ReadCloseOnce.Do(func() {
		doctype.Register(doctype.DocType{
			Name: m04ReadClose, Label: "Read-close order", PluralLabel: "Read-close orders",
			Table: "m04_read_close", LinesTable: "m04_read_close_line", Submittable: true, CloseRight: "read", Hooks: m04Hooks{},
			Fields: []doctype.Field{
				{Name: "remarks", Label: "Remarks", Type: doctype.Text, MaxLen: 500},
				{Name: "note", Label: "Note", Type: doctype.Text, OnLine: true},
			},
		})
	})
	e.fx.Exec(t, `CREATE TABLE m04_read_close (id INTEGER PRIMARY KEY AUTOINCREMENT, number TEXT UNIQUE, state TEXT NOT NULL DEFAULT 'draft',
		state_reason TEXT NOT NULL DEFAULT '', amended_from_id INTEGER, amendment_root_id INTEGER, amendment_no INTEGER NOT NULL DEFAULT 0,
		version INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL, created_by INTEGER NOT NULL, updated_at TEXT NOT NULL, updated_by INTEGER NOT NULL,
		remarks TEXT NOT NULL DEFAULT '')`)
	e.fx.Exec(t, `CREATE TABLE m04_read_close_line (id INTEGER PRIMARY KEY AUTOINCREMENT, parent_id INTEGER NOT NULL, line_no INTEGER NOT NULL, note TEXT NOT NULL DEFAULT '')`)
	e.fx.Naming.Prefix[m04ReadClose] = "MR"
	rs := map[auth.Right]auth.Scope{"read": auth.ScopeAll, "create": auth.ScopeAll, "write": auth.ScopeAll, "submit": auth.ScopeAll}
	if err := m04Auth(t, e).SetRights(t.Context(), e.fx.Admin, "Hooker", m04ReadClose, rs); err != nil {
		t.Fatalf("SetRights(Hooker, %s): %v", m04ReadClose, err)
	}
	ctx := t.Context()
	res, err := e.engine.Do(ctx, e.open, m04ReadClose, 0, doc.ActionSave, doc.ActionInput{
		Values: map[string]string{"remarks": "to close"}, Lines: []map[string]string{{"note": "one"}},
	})
	if err != nil {
		t.Fatalf("saving an %s draft: %v", m04ReadClose, err)
	}
	if sub, err := e.engine.Do(ctx, e.open, m04ReadClose, res.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version}); err != nil || sub.State != doctype.StateApproved {
		t.Fatalf("submitting %s %d: state %s, err %v; want approved", m04ReadClose, res.ID, sub.State, err)
	}
	e.who = m04User(t, e, "m04-readclose@vardhan.test", "M04ReadClose", map[string][]auth.Right{m04ReadClose: {"read"}}, 0)
	base := m04URL(m04ReadClose, res.ID)
	page := m04Page(t, e.get(t, base), http.StatusOK, "reader's document page")
	if page.Find(`.more-actions a[href="`+base+`/close"]`).Length() != 1 {
		t.Errorf("a reader of a DocType whose CloseRight is read sees no Close link")
	}
	m04Page(t, e.get(t, base+"/close"), http.StatusOK, "reader's close page")
}

// M0-4: a PageExtra.LineNotes entry sets only its own line's note, so a
// note the module's LineCard put on another line stays.
func TestModule_PageLineNotesKeepCardNote(t *testing.T) {
	const cardNote = "Set by the module's line summary."
	e := newHookedEnv(t)
	id := m04ApprovedHooked(t, e)
	m04Mount(e, docscreentest.Hooked, docscreen.Module{
		LineCard: func(_ docscreen.Ctx, l docscreen.LineView) components.LineCard {
			card := l.Card
			if l.No == 1 {
				card.Note = cardNote
			}
			return card
		},
		Page: func(docscreen.Ctx) (docscreen.PageExtra, error) {
			return docscreen.PageExtra{LineNotes: map[int]string{2: m04LineNote}}, nil
		},
	})
	page := m04Page(t, e.get(t, m04URL(docscreentest.Hooked, id)), http.StatusOK, "document page")
	cards := checkSection(page.Selection, "Lines").Find("li.lines-card")
	if cards.Length() != 2 {
		t.Fatalf("Lines shows %d cards, want 2", cards.Length())
	}
	if got := norm(cards.Eq(0).Text()); !strings.Contains(got, cardNote) {
		t.Errorf("line 1's card %q lost the LineCard note %q", got, cardNote)
	}
	if got := norm(cards.Eq(1).Text()); !strings.Contains(got, m04LineNote) {
		t.Errorf("line 2's card %q lacks the Page note %q", got, m04LineNote)
	}
}

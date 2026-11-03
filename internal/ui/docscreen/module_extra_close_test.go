package docscreen_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// m04AmendClose is a test DocType whose CloseRight is amend, as
// purchase.Order's is (CD-247 (3), CD-255). No registered DocType sets
// CloseRight yet, so this one is declared here, as doc's m010.Amend is.
const m04AmendClose = "m04.AmendClose"

var m04AmendOnce sync.Once

// m04Hooks are m04AmendClose's Hooks: the date is fixed, the rest no-ops.
type m04Hooks struct{}

func (m04Hooks) Validate(context.Context, *db.Tx, int64) []doctype.FieldError { return nil }
func (m04Hooks) BeforeSubmit(context.Context, *db.Tx, int64) error            { return nil }
func (m04Hooks) ApplyEffects(context.Context, *db.Tx, int64) error            { return nil }
func (m04Hooks) ReverseEffects(context.Context, *db.Tx, int64) error          { return nil }
func (m04Hooks) Amount(context.Context, *db.Tx, int64) (types.Money, error)   { return 0, nil }
func (m04Hooks) Date(context.Context, *db.Tx, int64) (types.Date, error) {
	return types.Date{Year: 2026, Month: time.October, Day: 24}, nil
}

// m04AmendCloseEnv registers m04AmendClose, creates its table on e's
// database and gives the Hooker role (the open user's) read, create, write
// and submit on it.
func m04AmendCloseEnv(t *testing.T, e *hookedEnv) {
	t.Helper()
	m04AmendOnce.Do(func() {
		doctype.Register(doctype.DocType{
			Name: m04AmendClose, Label: "Amend-close order", PluralLabel: "Amend-close orders",
			Table: "m04_amend_close", LinesTable: "m04_amend_close_line", Submittable: true, CloseRight: "amend", Hooks: m04Hooks{},
			Fields: []doctype.Field{
				{Name: "remarks", Label: "Remarks", Type: doctype.Text, MaxLen: 500},
				{Name: "note", Label: "Note", Type: doctype.Text, OnLine: true},
			},
		})
	})
	e.fx.Exec(t, `CREATE TABLE m04_amend_close (id INTEGER PRIMARY KEY AUTOINCREMENT, number TEXT UNIQUE, state TEXT NOT NULL DEFAULT 'draft',
		state_reason TEXT NOT NULL DEFAULT '', amended_from_id INTEGER, amendment_root_id INTEGER, amendment_no INTEGER NOT NULL DEFAULT 0,
		version INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL, created_by INTEGER NOT NULL, updated_at TEXT NOT NULL, updated_by INTEGER NOT NULL,
		remarks TEXT NOT NULL DEFAULT '')`)
	e.fx.Exec(t, `CREATE TABLE m04_amend_close_line (id INTEGER PRIMARY KEY AUTOINCREMENT, parent_id INTEGER NOT NULL, line_no INTEGER NOT NULL, note TEXT NOT NULL DEFAULT '')`)
	e.fx.Naming.Prefix[m04AmendClose] = "MA"
	rs := map[auth.Right]auth.Scope{"read": auth.ScopeAll, "create": auth.ScopeAll, "write": auth.ScopeAll, "submit": auth.ScopeAll}
	if err := m04Auth(t, e).SetRights(t.Context(), e.fx.Admin, "Hooker", m04AmendClose, rs); err != nil {
		t.Fatalf("SetRights(Hooker, %s): %v", m04AmendClose, err)
	}
}

// m04ApprovedAmendClose saves and submits an m04AmendClose as the open user.
func m04ApprovedAmendClose(t *testing.T, e *hookedEnv) int64 {
	t.Helper()
	ctx := t.Context()
	res, err := e.engine.Do(ctx, e.open, m04AmendClose, 0, doc.ActionSave, doc.ActionInput{
		Values: map[string]string{"remarks": "to close"}, Lines: []map[string]string{{"note": "one"}},
	})
	if err != nil {
		t.Fatalf("saving an %s draft: %v", m04AmendClose, err)
	}
	sub, err := e.engine.Do(ctx, e.open, m04AmendClose, res.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version})
	if err != nil || sub.State != doctype.StateApproved {
		t.Fatalf("submitting %s %d: state %s, err %v; want approved", m04AmendClose, res.ID, sub.State, err)
	}
	return res.ID
}

// The Close button and the close reason page check the DocType's
// CloseRight, blank meaning submit (CD-247 (3), CD-255), on the real AUTH
// service: on m04AmendClose (CloseRight amend) a user with amend but not
// submit sees Close, opens the close page and closes; a user with submit
// but not amend sees no Close and gets AUTH's 403 for amend on the page.
// On Hooked (blank CloseRight) it is the other way round, as today.
func TestModule_CloseRight(t *testing.T) {
	e := newHookedEnv(t)
	m04AmendCloseEnv(t, e)
	both := func(rs ...auth.Right) map[string][]auth.Right {
		return map[string][]auth.Right{m04AmendClose: rs, docscreentest.Hooked: rs}
	}
	amender := m04User(t, e, "m04-amender@vardhan.test", "M04Amender", both("read", "amend"), 0)
	submitter := m04User(t, e, "m04-submitter@vardhan.test", "M04Submitter", both("read", "submit"), 0)
	a := m04Auth(t, e)

	for _, c := range []struct {
		name          string
		dt            string
		closer, other auth.Principal
		right         auth.Right // the right the other user lacks
	}{
		{"CloseRight amend", m04AmendClose, amender, submitter, "amend"},
		{"blank CloseRight is submit", docscreentest.Hooked, submitter, amender, "submit"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var id int64
			if c.dt == m04AmendClose {
				id = m04ApprovedAmendClose(t, e)
			} else {
				id = m04ApprovedHooked(t, e)
			}
			base := m04URL(c.dt, id)
			closeLink := `.more-actions a[href="` + base + `/close"]`

			// The user holding CloseRight sees Close and may open and use it.
			e.who = c.closer
			page := m04Page(t, e.get(t, base), http.StatusOK, "closer's document page")
			if page.Find(closeLink).Length() != 1 {
				t.Errorf("user with %s: no Close link to %s/close on the document page", c.right, base)
			}
			m04Page(t, e.get(t, base+"/close"), http.StatusOK, "closer's close page")

			// The other user sees no Close, and the page is AUTH's refusal.
			e.who = c.other
			page = m04Page(t, e.get(t, base), http.StatusOK, "other user's document page")
			if page.Find(closeLink).Length() != 0 {
				t.Errorf("user without %s sees Close", c.right)
			}
			rec := e.get(t, base+"/close")
			if rec.Code != http.StatusForbidden {
				t.Fatalf("user without %s GET close = %d, want 403; body %.300s", c.right, rec.Code, rec.Body)
			}
			v, err := e.d.View(t.Context(), c.other, c.dt, id)
			if err != nil {
				t.Fatalf("View: %v", err)
			}
			want := a.Can(t.Context(), c.other, c.dt, c.right, &v.Record)
			if want == nil || !strings.Contains(uitest.Doc(t, rec).Text(), want.Error()) {
				t.Errorf("close page refusal lacks AUTH's message for %s: %v", c.right, want)
			}

			// The closer's POST closes it.
			e.who = c.closer
			rec = e.post(t, base+"/close", url.Values{"version": {m04Version(t, e, c.dt, id)}, "reason": {"Balance not needed"}})
			if got := location(t, rec); got != base {
				t.Errorf("close went to %q, want %q", got, base)
			}
			if after, err := e.d.View(t.Context(), c.closer, c.dt, id); err != nil || after.Info.State != doctype.StateClosed {
				t.Errorf("after the closer's POST: state %s (err %v), want closed", after.Info.State, err)
			}
		})
	}
}

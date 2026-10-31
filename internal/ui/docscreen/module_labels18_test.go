package docscreen_test

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
)

// m18Labelled is a test DocType with StateLabels, as purchase.Request's
// (approved reads Submitted, CD-247 (4)). No registered DocType sets
// StateLabels yet, so this one is declared here. Its pending label is its
// own; closed has a blank entry, which keeps the component's word, and
// draft has none.
const m18Labelled = "m18.Labelled"

// m18 labels (CD-263 G7: overrides only).
const (
	m18Submitted = "Submitted"
	m18Pending   = "With the buyer"
)

var m18Once sync.Once

// m18Hooks are m18Labelled's Hooks: the date is fixed, the rest no-ops.
type m18Hooks struct{}

func (m18Hooks) Validate(context.Context, *db.Tx, int64) []doctype.FieldError { return nil }
func (m18Hooks) BeforeSubmit(context.Context, *db.Tx, int64) error            { return nil }
func (m18Hooks) ApplyEffects(context.Context, *db.Tx, int64) error            { return nil }
func (m18Hooks) ReverseEffects(context.Context, *db.Tx, int64) error          { return nil }
func (m18Hooks) Amount(context.Context, *db.Tx, int64) (types.Money, error)   { return 0, nil }
func (m18Hooks) Date(context.Context, *db.Tx, int64) (types.Date, error) {
	return types.Date{Year: 2026, Month: time.October, Day: 24}, nil
}

func m18Register() {
	m18Once.Do(func() {
		doctype.Register(doctype.DocType{
			Name: m18Labelled, Label: "Labelled request", PluralLabel: "Labelled requests",
			Table: "m18_labelled", LinesTable: "m18_labelled_line", Submittable: true, Hooks: m18Hooks{},
			StateLabels: map[doctype.State]string{
				doctype.StateApproved: m18Submitted,
				doctype.StatePending:  m18Pending,
				doctype.StateClosed:   "",
			},
			Fields: []doctype.Field{
				{Name: "remarks", Label: "Remarks", Type: doctype.Text, MaxLen: 500},
				{Name: "note", Label: "Note", Type: doctype.Text, OnLine: true},
			},
		})
	})
}

// m18Env registers m18Labelled, creates its tables on e's database, numbers
// it with prefix ML and gives the Hooker role (the open user's) read,
// create, write and submit on it.
func m18Env(t *testing.T, e *hookedEnv) {
	t.Helper()
	m18Register()
	e.fx.Exec(t, `CREATE TABLE m18_labelled (id INTEGER PRIMARY KEY AUTOINCREMENT, number TEXT UNIQUE, state TEXT NOT NULL DEFAULT 'draft',
		state_reason TEXT NOT NULL DEFAULT '', amended_from_id INTEGER, amendment_root_id INTEGER, amendment_no INTEGER NOT NULL DEFAULT 0,
		version INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL, created_by INTEGER NOT NULL, updated_at TEXT NOT NULL, updated_by INTEGER NOT NULL,
		remarks TEXT NOT NULL DEFAULT '')`)
	e.fx.Exec(t, `CREATE TABLE m18_labelled_line (id INTEGER PRIMARY KEY AUTOINCREMENT, parent_id INTEGER NOT NULL, line_no INTEGER NOT NULL, note TEXT NOT NULL DEFAULT '')`)
	e.fx.Naming.Prefix[m18Labelled] = "ML"
	rs := map[auth.Right]auth.Scope{"read": auth.ScopeAll, "create": auth.ScopeAll, "write": auth.ScopeAll, "submit": auth.ScopeAll}
	if err := m04Auth(t, e).SetRights(t.Context(), e.fx.Admin, "Hooker", m18Labelled, rs); err != nil {
		t.Fatalf("SetRights(Hooker, %s): %v", m18Labelled, err)
	}
}

// m18Draft saves a draft of dt as the open user and returns its id and version.
func m18Draft(t *testing.T, e *hookedEnv, dt string) (int64, int) {
	t.Helper()
	if dt == docscreentest.Hooked {
		res, err := e.draft(t, e.open, "METRO-SITE")
		if err != nil {
			t.Fatalf("saving a Hooked draft: %v", err)
		}
		return res.ID, res.Version
	}
	res, err := e.engine.Do(t.Context(), e.open, dt, 0, doc.ActionSave, doc.ActionInput{
		Values: map[string]string{"remarks": "labelled"}, Lines: []map[string]string{{"note": "one"}},
	})
	if err != nil {
		t.Fatalf("saving an %s draft: %v", dt, err)
	}
	return res.ID, res.Version
}

// m18Badge is the text of the document page's one state badge.
func m18Badge(t *testing.T, page *goquery.Document, what string) string {
	t.Helper()
	b := page.Find(".doc-state span.badge")
	if b.Length() != 1 {
		t.Fatalf("%s: want one state badge in .doc-state, got %d", what, b.Length())
	}
	return norm(b.Text())
}

// m18ListRows is a list result with one row of dt per state, ids 1 up.
func m18ListRows(dt string, states ...doctype.State) types.Result[docscreen.ListRow] {
	r := types.Result[docscreen.ListRow]{Total: len(states)}
	for i, s := range states {
		id := int64(i + 1)
		r.Rows = append(r.Rows, docscreen.ListRow{
			Info:   doctype.Info{DocType: dt, ID: id, State: s, Display: "Row " + itoa(id)},
			Values: map[string]string{},
		})
	}
	return r
}

// HOOK-029 (CD-263 G7, CD-269): a DocType's StateLabels reach the
// document page's badge, its list rows' badges and the submit and approve
// banners; a state with no entry, or a blank one, keeps the component's own
// word (never doctype's global label: pending reads Pending approval, not
// Waiting for approval). Hooked and Sample, without StateLabels, are
// unchanged. Submit and the page run on the real engine and AUTH; pending
// and approve on the fakes, since neither test DocType has an approval rule.
func TestModule_StateLabels(t *testing.T) {
	t.Run("submit banner and page badge, real engine", func(t *testing.T) {
		e := newHookedEnv(t)
		m18Env(t, e)
		for _, c := range []struct {
			dt, word string
			hdr      []string
		}{
			{m18Labelled, m18Submitted, nil},
			{m18Labelled, m18Submitted, []string{"HX-Request", "true"}},
			{docscreentest.Hooked, "Approved", nil},
			{docscreentest.Hooked, "Approved", []string{"HX-Request", "true"}},
		} {
			name := c.dt + " full page"
			if c.hdr != nil {
				name = c.dt + " htmx"
			}
			t.Run(name, func(t *testing.T) {
				id, version := m18Draft(t, e, c.dt)
				base := m04URL(c.dt, id)
				rec := e.post(t, base+"/submit", url.Values{"version": {itoa(int64(version))}}, c.hdr...)
				v, err := e.d.View(t.Context(), e.open, c.dt, id)
				if err != nil {
					t.Fatalf("View after submit: %v", err)
				}
				if v.Info.State != doctype.StateApproved || v.Info.Number == "" {
					t.Fatalf("after submit: state %s, number %q; want approved and numbered", v.Info.State, v.Info.Number)
				}
				var page *goquery.Document
				if c.hdr == nil {
					if got := location(t, rec); got != base {
						t.Fatalf("submit went to %q, want %q", got, base)
					}
					page = m04Page(t, e.get(t, base), http.StatusOK, "document page after submit")
				} else {
					page = m04Page(t, rec, http.StatusOK, "htmx submit")
				}
				want := c.word + ". " + v.Info.Number + " has been numbered."
				if got := bannerTexts(page.Selection, "success"); !slices.Equal(got, []string{want}) {
					t.Errorf("success banners = %q, want exactly %q", got, want)
				}
				if got := m18Badge(t, page, "page after submit"); got != c.word {
					t.Errorf("page badge after submit = %q, want %q", got, c.word)
				}
				// A plain GET of the approved document shows the same badge.
				if got := m18Badge(t, m04Page(t, e.get(t, base), http.StatusOK, "document page"), "document page"); got != c.word {
					t.Errorf("document page badge = %q, want %q", got, c.word)
				}
			})
		}
	})

	t.Run("list badges", func(t *testing.T) {
		e := newHookedEnv(t)
		m18Env(t, e)
		lister, ok := e.d.Lister.(*docscreentest.Lister)
		if !ok {
			t.Fatalf("Deps.Lister is %T, want *docscreentest.Lister", e.d.Lister)
		}
		states := []doctype.State{doctype.StateApproved, doctype.StatePending, doctype.StateDraft, doctype.StateClosed}
		for _, c := range []struct {
			dt   string
			want []string // badge per state above
		}{
			{m18Labelled, []string{m18Submitted, m18Pending, "Draft", "Closed"}},
			{docscreentest.Hooked, []string{"Approved", "Pending approval", "Draft", "Closed"}},
		} {
			t.Run(c.dt, func(t *testing.T) {
				lister.Set(c.dt, m18ListRows(c.dt, states...))
				page := m04Page(t, e.get(t, "/doc/"+c.dt), http.StatusOK, "list")
				items := page.Find("ul.datalist-rows > li")
				if items.Length() != len(states) {
					t.Fatalf("list has %d rows, want %d", items.Length(), len(states))
				}
				for i, s := range states {
					b := items.Eq(i).Find("span.badge")
					if b.Length() != 1 {
						t.Errorf("row %d (%s): want one badge, got %d", i+1, s, b.Length())
						continue
					}
					if got := norm(b.Text()); got != c.want[i] {
						t.Errorf("row %d (%s): badge = %q, want %q", i+1, s, got, c.want[i])
					}
					if !b.HasClass("badge--" + string(s)) {
						t.Errorf("row %d (%s): badge class = %q, want the state's badge--%s kept", i+1, s, b.AttrOr("class", ""), s)
					}
				}
			})
		}
	})

	t.Run("pending page and approve banner, fakes", func(t *testing.T) {
		m18Register()
		const number = "ML/26-27/00001"
		for _, c := range []struct {
			dt, pending, approved string
		}{
			{m18Labelled, m18Pending, m18Submitted},
			{docscreentest.Sample, "Pending approval", "Approved"},
		} {
			t.Run(c.dt, func(t *testing.T) {
				e := newDocEnv(t, allRights...)
				for _, r := range allRights {
					e.az.Grant("Buyer", m18Labelled, r, auth.ScopeAll)
				}
				info := sampleInfo(42, doctype.StatePending)
				rec := sampleRecord()
				if c.dt == m18Labelled {
					info.DocType, info.DocLabel = m18Labelled, "Labelled request"
					rec.Values = map[string]string{"remarks": "labelled"}
					rec.Lines = []map[string]string{{"id": "1", "note": "one"}}
				}
				docscreentest.Seed(e.docs, info, rec)
				base := m04URL(c.dt, 42)
				if got := m18Badge(t, m04Page(t, e.get(t, base), http.StatusOK, "pending page"), "pending page"); got != c.pending {
					t.Errorf("pending page badge = %q, want %q", got, c.pending)
				}

				e.docs.SetResult(c.dt, 42, doctype.ActionApprove, doc.Result{ID: 42, State: doctype.StateApproved, Number: number, Version: 4}, nil)
				page := m04Page(t, e.post(t, base+"/approve", nil, "HX-Request", "true"), http.StatusOK, "htmx approve")
				want := c.approved + ". " + number + " has been numbered."
				if got := bannerTexts(page.Selection, "success"); !slices.Equal(got, []string{want}) {
					t.Errorf("approve banners = %q, want exactly %q", got, want)
				}
			})
		}
	})
}

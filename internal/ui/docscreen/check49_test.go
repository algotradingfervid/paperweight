package docscreen_test

import (
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// M0-49 (CD-322, CD-353 (4)): the generic Check switch posts DOC's plain
// form through the real engine. Ticked posts "true"; unticked posts nothing,
// which DOC stores as false (an empty Check is false, doc/sqlgen.go); an
// inverted switch posts "false" when on and "true" when off.

const m49Type = "docscreentest.CheckM49"

var m49Once sync.Once

func registerM49() {
	m49Once.Do(func() {
		doctype.Register(doctype.DocType{
			Name: m49Type, Label: "Check Order", PluralLabel: "Check Orders", Table: "m49_check", LinesTable: "m49_check_line",
			Submittable: true, Hooks: scopeHooks{},
			Fields: []doctype.Field{
				{Name: "supplier_id", Label: "Supplier", Type: doctype.Link, Link: fixture.Party, Required: true},
				{Name: "urgent", Label: "Urgent", Type: doctype.Check, Step: "terms"},
				{Name: "no_print", Label: "No print", Type: doctype.Check, Invert: "Print", Step: "terms"},
				{Name: "item_id", Label: "Item", Type: doctype.Link, Link: fixture.Item, Required: true, OnLine: true},
				{Name: "fragile", Label: "Fragile", Type: doctype.Check, OnLine: true},
			},
		})
	})
}

// newM49Env is the harness on a fixture database and the real engine with
// one draft (RAJKOT, one CEMENT-OPC line) whose Checks are all unset.
func newM49Env(t *testing.T) *scopeEnv {
	t.Helper()
	registerM49()
	h := fixturetest.New(t)
	h.Exec(t, `CREATE TABLE m49_check (id INTEGER PRIMARY KEY AUTOINCREMENT, number TEXT UNIQUE, state TEXT NOT NULL DEFAULT 'draft',
		state_reason TEXT NOT NULL DEFAULT '', amended_from_id INTEGER, amendment_root_id INTEGER, amendment_no INTEGER NOT NULL DEFAULT 0,
		version INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL, created_by INTEGER NOT NULL, updated_at TEXT NOT NULL, updated_by INTEGER NOT NULL,
		supplier_id INTEGER, urgent INTEGER, no_print INTEGER)`)
	h.Exec(t, `CREATE TABLE m49_check_line (id INTEGER PRIMARY KEY AUTOINCREMENT, parent_id INTEGER NOT NULL, line_no INTEGER NOT NULL,
		item_id INTEGER, fragile INTEGER)`)
	h.Naming.Prefix = map[string]string{m49Type: "CK"}
	se := scopeMount(t, h, h.Engine, h.Auth, h.Admin)
	res := h.MustDo(t, t.Context(), h.Admin, m49Type, 0, doc.ActionSave, doc.ActionInput{
		Values: map[string]string{"supplier_id": itoa(h.M.Party["RAJKOT"])},
		Lines:  []map[string]string{{"item_id": itoa(h.M.Item["CEMENT-OPC"])}},
	})
	se.id = res.ID
	return se
}

func m49URL(e *scopeEnv) string { return "/doc/" + m49Type + "/" + itoa(e.id) }

// m49Page GETs target and returns the page and the version it posts.
func m49Page(t *testing.T, e *scopeEnv, target string) (*goquery.Document, string) {
	t.Helper()
	rec := e.get(t, target)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d; body %.300s", target, rec.Code, rec.Body)
	}
	page := uitest.Doc(t, rec)
	v, ok := page.Find(`input[name="version"]`).First().Attr("value")
	if !ok {
		t.Fatalf("GET %s: no version input", target)
	}
	return page, v
}

// m49Switch is what a browser posts for switch name on page when it is on
// or off: the checkbox's value when on, then any hidden value of that name.
func m49Switch(t *testing.T, page *goquery.Document, name string, on bool) []string {
	t.Helper()
	if page.Find(`input[type="checkbox"][role="switch"][name="`+name+`"]`).Length() != 1 {
		t.Fatalf("no switch named %s on the page", name)
	}
	var out []string
	page.Find(`input[name="` + name + `"]`).Each(func(_ int, s *goquery.Selection) {
		if s.AttrOr("type", "") != "checkbox" || on {
			out = append(out, s.AttrOr("value", ""))
		}
	})
	return out
}

// m49On reports whether switch name on page is drawn on.
func m49On(page *goquery.Document, name string) bool {
	_, on := page.Find(`input[role="switch"][name="` + name + `"]`).Attr("checked")
	return on
}

// A generic step's Check, plain and inverted, saved on and off in each
// combination, stores DOC's true/false and redraws as saved.
func TestM49_StepCheckRoundTrips(t *testing.T) {
	e := newM49Env(t)
	terms := m49URL(e) + "/edit/terms"
	for _, c := range []struct {
		urgent, print       bool
		wantUrgent, wantNoP string
	}{
		{true, true, "true", "false"},
		{false, false, "false", "true"},
		{true, false, "true", "true"},
		{false, true, "false", "false"},
	} {
		name := "urgent " + strconv.FormatBool(c.urgent) + ", print " + strconv.FormatBool(c.print)
		page, v := m49Page(t, e, terms)
		form := url.Values{"version": {v}, "urgent": m49Switch(t, page, "urgent", c.urgent), "no_print": m49Switch(t, page, "no_print", c.print)}
		if rec := e.post(t, terms, form); rec.Code != http.StatusSeeOther {
			t.Fatalf("%s: POST terms = %d; body %.500s", name, rec.Code, rec.Body)
		}
		stored := e.fx.Record(t, m49Type, e.id)
		if got := stored.Values["urgent"]; got != c.wantUrgent {
			t.Errorf("%s: stored urgent %q, want %q", name, got, c.wantUrgent)
		}
		if got := stored.Values["no_print"]; got != c.wantNoP {
			t.Errorf("%s: stored no_print %q, want %q", name, got, c.wantNoP)
		}
		page, _ = m49Page(t, e, terms)
		if m49On(page, "urgent") != c.urgent || m49On(page, "no_print") != c.print {
			t.Errorf("%s: redrawn urgent on %v, print on %v", name, m49On(page, "urgent"), m49On(page, "no_print"))
		}
	}
}

// A generic line's Check saves ticked as true and unticked as false (an
// unticked box posts nothing).
func TestM49_LineCheckRoundTrips(t *testing.T) {
	e := newM49Env(t)
	line := m49URL(e) + "/lines/1"
	item := itoa(e.fx.M.Item["CEMENT-OPC"])
	for _, on := range []bool{true, false, true} {
		page, v := m49Page(t, e, line)
		form := url.Values{"version": {v}, "item_id": {item}, "fragile": m49Switch(t, page, "fragile", on)}
		if rec := e.post(t, line, form); rec.Code != http.StatusSeeOther {
			t.Fatalf("on %v: POST line = %d; body %.500s", on, rec.Code, rec.Body)
		}
		if got, want := e.fx.Record(t, m49Type, e.id).Lines[0]["fragile"], strconv.FormatBool(on); got != want {
			t.Errorf("on %v: stored fragile %q, want %q", on, got, want)
		}
	}
}

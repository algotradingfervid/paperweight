package docscreen_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/alexedwards/scs/v2"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
)

const (
	m048Claim  = "docscreentest.LinkClaim"
	m048Number = "SO/26-27/00001" // sample 42's number once approved
)

var registerM048Once sync.Once

// registerM048 registers a DocType whose advance_id links to a DOCUMENT
// (the sample order), listed and searchable so the list and search show it.
func registerM048() {
	registerM048Once.Do(func() {
		doctype.Register(doctype.DocType{
			Name: m048Claim, Label: "Link Claim", PluralLabel: "Link Claims", Table: "docscreentest_linkclaim", Submittable: true,
			Fields: []doctype.Field{
				{Name: "advance_id", Label: "Advance", Type: doctype.Link, Link: docscreentest.Sample, ListPriority: 4},
				{Name: "remarks", Label: "Remarks", Type: doctype.Text},
			},
			SearchFields: []string{"remarks"},
		})
	})
}

// m048Env is the page harness with draft claim 50 and approved claim 51
// (both advance_id 42) and an approved sample 42, its router built with read.
func m048Env(t *testing.T, read func(context.Context, *db.Tx, auth.Principal, string, int64) error) *docEnv {
	t.Helper()
	registerM048()
	e := newDocEnv(t, allRights...)
	for _, r := range allRights {
		e.az.Grant("Buyer", m048Claim, r, auth.ScopeAll)
	}
	e.seed(doctype.StateApproved)
	claim := doctype.Info{DocType: m048Claim, DocLabel: "Link Claim", ID: 50, State: doctype.StateDraft, Display: "Draft 50", CreatedBy: priya.ID}
	docscreentest.Seed(e.docs, claim, doc.Record{Version: 1, Values: map[string]string{"advance_id": "42", "remarks": "site visit"}})
	approved := claim
	approved.ID, approved.State, approved.Number, approved.Display = 51, doctype.StateApproved, "LC-1", "LC-1"
	docscreentest.Seed(e.docs, approved, doc.Record{Version: 1, Values: map[string]string{"advance_id": "42", "remarks": "site visit"}})
	lister := docscreentest.NewLister()
	lister.Set(m048Claim, types.Result[docscreen.ListRow]{Rows: []docscreen.ListRow{{Info: claim, Values: map[string]string{"advance_id": "42"}}}, Total: 1})
	e.d.Lister = lister
	e.d.DocLinkRead = read
	r := web.NewRouter(web.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Sessions: scs.New()})
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(ui.WithShell(r.Context(), ui.Shell{CSRF: csrfToken})))
		})
	})
	docscreen.Routes(e.d)(r)
	e.h = r
	return e
}

// m048Read answers err, and records who asked about what.
type m048Read struct {
	err   error
	asked []string
}

func (m *m048Read) read(_ context.Context, _ *db.Tx, p auth.Principal, target string, id int64) error {
	m.asked = append(m.asked, p.Name+" "+target+" "+itoa(id))
	return m.err
}

// CD-321: a Link to a DOCUMENT shows the target's number only when
// DocLinkRead passes for the viewer; refused or with no hook it shows blank,
// as an empty value does (no summary pair).
func TestDocLink_ReadCheck(t *testing.T) {
	cases := map[string]struct {
		read *m048Read
		show bool
	}{
		"readable":       {&m048Read{}, true},
		"not found":      {&m048Read{err: doctype.ErrNotFound}, false},
		"forbidden":      {&m048Read{err: auth.ErrForbidden}, false},
		"link error":     {&m048Read{err: &auth.LinkError{Label: "Sample Order"}}, false},
		"no hook (fail)": {nil, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			var read func(context.Context, *db.Tx, auth.Principal, string, int64) error
			if c.read != nil {
				read = c.read.read
			}
			e := m048Env(t, read)

			v, err := e.d.View(t.Context(), priya, m048Claim, 50)
			if err != nil {
				t.Fatal(err)
			}
			want := ""
			if c.show {
				want = m048Number
			}
			if got := v.Display["advance_id"]; got != want {
				t.Errorf("Display[advance_id] = %q, want %q", got, want)
			}
			for _, p := range v.Header {
				if p.Label == "Advance" && !c.show {
					t.Errorf("Header has an Advance pair %q for a hidden target", p.Value)
				}
			}
			if v.Inputs["advance_id"] != "42" {
				t.Errorf("Inputs[advance_id] = %q, want the stored id 42", v.Inputs["advance_id"])
			}
			if v.Problems != nil {
				t.Errorf("Problems = %v, want none (the value is set)", v.Problems)
			}

			for _, path := range []string{"/doc/" + m048Claim + "/50/edit/check", "/doc/" + m048Claim + "/51", "/doc/" + m048Claim, "/search?q=site"} {
				rec := e.get(t, path)
				if rec.Code != http.StatusOK {
					t.Fatalf("GET %s: status %d, body %.300s", path, rec.Code, rec.Body)
				}
				if got := strings.Contains(rec.Body.String(), m048Number); got != c.show {
					t.Errorf("GET %s shows %s = %v, want %v", path, m048Number, got, c.show)
				}
			}
			if c.read != nil {
				for _, a := range c.read.asked {
					if a != "Priya Shah "+docscreentest.Sample+" 42" {
						t.Errorf("DocLinkRead asked %q, want Priya about the sample 42", a)
					}
				}
				if len(c.read.asked) == 0 {
					t.Error("DocLinkRead was never asked")
				}
			}
		})
	}
}

// Any other DocLinkRead error is the page's error, not a blank value.
func TestDocLink_ReadError(t *testing.T) {
	broken := errors.New("disk on fire")
	e := m048Env(t, (&m048Read{err: broken}).read)
	if _, err := e.d.View(t.Context(), priya, m048Claim, 50); !errors.Is(err, broken) {
		t.Errorf("View err = %v, want %v", err, broken)
	}
}

package mstweb_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	mstweb "github.com/vardhan/internaltools/internal/masters/mstkit/web"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// No case ID: a save that fails with a plain error is no refusal a form can
// show: it is web.ErrorFrom's 500, without the error's text.
func TestSections_PlainErrorIsNotShown(t *testing.T) {
	svc := stubService{rec: stubRecord{"code": "X"}, err: errors.New("secret detail")}
	h := harness(t, svc, func(s *screen) {
		s.Sections = []docscreen.FormStep{{Slug: "details", Label: "Details", Fields: []string{"code"}}}
	})
	r := h.Browser(h.Env.Kavya).Post(t, record+"/edit/details", url.Values{"version": {"3"}, "code": {"Y"}})
	webtest.WantStatus(t, r, http.StatusInternalServerError)
	webtest.WantNoText(t, r, "secret detail")
}

// cancelTarget is the confirm step's Cancel link.
func cancelTarget(t *testing.T, r webtest.Response) string {
	t.Helper()
	links := r.Doc.Find(`a:contains("Cancel")`)
	if links.Length() != 1 {
		t.Fatalf("%d Cancel links; want 1\nbody: %s", links.Length(), webtest.Squash(r.Body))
	}
	href, _ := links.Attr("href")
	return href
}

// No case ID: a SubForm's delete step cancels to its Back, the page it belongs
// to. Its Action is the row's form, which needs write, and the step needs only
// delete. A record's own step still cancels to the record.
func TestPages_DeleteConfirmCancel(t *testing.T) {
	const (
		rowForm = record + "/rows/1"
		parent  = base + "/parent"
	)
	h := harness(t, stubService{rec: stubRecord{"code": "X"}}, func(s *screen) {
		s.Mount = func(pages mstweb.Pages) func(chi.Router) {
			return func(r chi.Router) {
				r.Get(rowForm, pages.Handle(func(w http.ResponseWriter, r *http.Request, _ auth.Principal) error {
					mstweb.RenderSubForm(w, r, http.StatusOK, mstweb.SubForm{
						ID: 1, Title: "Row", Action: rowForm, Confirm: true,
						Back: components.Link{Label: "Parent", Href: parent}, Form: mstweb.Form{Version: 2},
					})
					return nil
				}))
			}
		}
	})
	if got := cancelTarget(t, h.Get(t, h.Env.Kavya, rowForm)); got != parent {
		t.Errorf("a SubForm's Cancel goes to %q; want its Back, %q", got, parent)
	}
	if got := cancelTarget(t, h.Get(t, h.Env.Kavya, record+"?delete")); got != record {
		t.Errorf("a record's Cancel goes to %q; want %q", got, record)
	}
}

// No case ID: a row step owns only errors that name a row. A Line 0 error on a
// field a row step shares has no row to sit on, so it stays on Check and save.
// The Party wizard's field names don't overlap, so this needs the kit's stub.
func TestWizard_RowStepOwnsOnlyRowErrors(t *testing.T) {
	const rowStep = base + "/new/more"
	onRows := func(s *screen) {
		w := s.Wizard.(mstweb.CreateWizard[stubDraft])
		w.Steps = []docscreen.FormStep{w.Steps[0], {Slug: "more", Label: "More"}}
		w.Rows = map[string]mstweb.RowStep{"more": {
			Legend: "Rows", AddLabel: "Add row", Fields: []doctype.Field{{Name: "note", Label: "Note", Type: doctype.Text}},
		}}
		s.Wizard = w
	}
	for name, c := range map[string]struct {
		line int
		push string
	}{
		"no row": {0, wizardCheck},
		"a row":  {1, rowStep},
	} {
		t.Run(name, func(t *testing.T) {
			err := &refusal{status: http.StatusUnprocessableEntity, text: "x", fields: []doctype.FieldError{{Field: "note", Line: c.line, Message: "Nothing can be saved today."}}}
			h := wizardHarness(t, func(context.Context, auth.Principal, stubDraft) (mstkit.Saved, error) { return mstkit.Saved{}, err }, onRows)
			b := h.Browser(h.Env.Kavya)
			toWizardCheck(t, b)
			r := b.Post(t, wizardCheck, url.Values{})
			webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
			webtest.WantText(t, r, "Nothing can be saved today.")
			if got := r.Header.Get("HX-Push-Url"); got != c.push {
				t.Errorf("HX-Push-Url = %q; want %q", got, c.push)
			}
		})
	}
}

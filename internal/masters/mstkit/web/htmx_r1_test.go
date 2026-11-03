package mstweb_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	mstweb "github.com/vardhan/internaltools/internal/masters/mstkit/web"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// R1 (shell review H3): a single-page master form posts through htmx, so a
// dropped connection reaches the network banner. A plain POST still works.

func TestScreens_FormsPostThroughHTMX(t *testing.T) {
	h := harness(t, stubService{rec: stubRecord{}})
	for _, c := range []struct{ target, action, disabled string }{
		{base + "/new", base + "/new", "#record-form button[type=submit]"},
		{record, record, "#record-form button[type=submit]"},
		{record + "?delete", record + "/delete", "find button[type=submit]"},
	} {
		page := h.Get(t, h.Env.Kavya, c.target)
		webtest.WantStatus(t, page, http.StatusOK)
		f := page.Doc.Find(`#main form[action="` + c.action + `"]`)
		if f.Length() != 1 {
			t.Fatalf("%s: %d forms posting to %s, want 1", c.target, f.Length(), c.action)
		}
		if got := f.AttrOr("hx-post", ""); got != c.action {
			t.Errorf("%s: hx-post = %q", c.target, got)
		}
		if got := f.AttrOr("hx-target", ""); got != "#main" {
			t.Errorf("%s: hx-target = %q, want #main", c.target, got)
		}
		if got := f.AttrOr("method", ""); got != "post" {
			t.Errorf("%s: method = %q, want post: a plain POST still works", c.target, got)
		}
		if got := f.AttrOr("hx-disabled-elt", ""); got != c.disabled {
			t.Errorf("%s: hx-disabled-elt = %q, want %q", c.target, got, c.disabled)
		}
	}
}

// htmx can't take a redirect into a page swap: a save answers HX-Redirect, and
// its flash still shows on the page it lands on.
func TestScreens_HTMXSaveRedirects(t *testing.T) {
	h := harness(t, stubService{rec: stubRecord{}, saved: mstkit.Saved{ID: 7, Warnings: []string{"First warning."}}})
	b := h.Browser(h.Env.Kavya)
	for _, target := range []string{base + "/new", record} {
		resp := b.PostHTMX(t, target, url.Values{"version": {"3"}})
		webtest.WantStatus(t, resp, http.StatusOK)
		if got := resp.Header.Get("HX-Redirect"); got != record {
			t.Fatalf("POST %s: HX-Redirect = %q, want %s", target, got, record)
		}
		page := b.Get(t, record)
		webtest.WantBanner(t, page, "Stub saved.")
	}
}

// A refusal is the #main fragment, never a whole page swapped inside #main.
func TestScreens_HTMXRefusalIsAFragment(t *testing.T) {
	for _, c := range []struct {
		name   string
		err    error
		status int
	}{
		{"field", &mstkit.ValidationError{Fields: []doctype.FieldError{{Field: "name", Message: "Name is wrong."}}}, http.StatusUnprocessableEntity},
		{"no field", &mstkit.ValidationError{Fields: []doctype.FieldError{{Message: "Not allowed."}}}, http.StatusUnprocessableEntity},
		{"stale", &refusal{status: http.StatusConflict, text: "Changed elsewhere."}, http.StatusConflict},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := harness(t, stubService{rec: stubRecord{}, err: c.err})
			resp := h.Browser(h.Env.Kavya).PostHTMX(t, record, url.Values{"version": {"3"}, "name": {"Typed"}})
			webtest.WantStatus(t, resp, c.status)
			// The page's <title> leads, "Error: " in front (R2), then #main.
			title, frag, _ := strings.Cut(strings.TrimSpace(resp.Body), "</title>")
			if !strings.HasPrefix(title, "<title>Error: ") || !strings.HasPrefix(frag, `<main id="main"`) {
				t.Errorf("answer starts %.120s, want the Error: title and the #main fragment", resp.Body)
			}
			if !strings.Contains(resp.Body, `value="Typed"`) {
				t.Errorf("the typed value is lost: %.300s", resp.Body)
			}
		})
	}
}

// A section edit page and a sub-form are refused the same way: as the #main
// fragment, the banner taking focus, never a whole page swapped inside #main.
func TestScreens_HTMXRefusalOnSectionAndSubForm(t *testing.T) {
	const rowForm = record + "/rows/1"
	stale := &refusal{text: "Changed elsewhere.", status: http.StatusConflict}
	for _, c := range []struct {
		name, target string
		opts         []func(*screen)
	}{
		{"section", record + "/edit/details", []func(*screen){onChoice}},
		{"sub-form", rowForm, []func(*screen){func(s *screen) {
			s.Mount = func(pages mstweb.Pages) func(chi.Router) {
				return func(r chi.Router) {
					r.Post(rowForm, pages.Handle(func(w http.ResponseWriter, r *http.Request, _ auth.Principal) error {
						mstweb.RenderSubForm(w, r, http.StatusConflict, mstweb.SubForm{
							ID: 1, Title: "Row", Action: rowForm, Back: components.Link{Label: "Parent", Href: base},
							Form: mstweb.Form{Version: 2, Problem: "Changed elsewhere.", Reload: true},
						})
						return nil
					}))
				}
			}
		}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := harness(t, stubService{rec: stubRecord{"code": "X"}, err: stale}, c.opts...)
			resp := h.Browser(h.Env.Kavya).PostHTMX(t, c.target, url.Values{"version": {"3"}, "code": {"X"}, "size": {"s"}})
			webtest.WantStatus(t, resp, http.StatusConflict)
			// The page's <title> leads, "Error: " in front (R2), then #main.
			title, frag, _ := strings.Cut(strings.TrimSpace(resp.Body), "</title>")
			if !strings.HasPrefix(title, "<title>Error: ") || !strings.HasPrefix(frag, `<main id="main"`) {
				t.Errorf("answer starts %.120s, want the Error: title and the #main fragment", resp.Body)
			}
			if resp.Doc.Find("#record-alert[autofocus]").Length() != 1 {
				t.Errorf("no focused #record-alert: %.300s", resp.Body)
			}
		})
	}
}

// The error summary takes focus when there is one: two elements asking for it
// would leave the second without it.
func TestScreens_OneFocusOnARefusal(t *testing.T) {
	h := harness(t, stubService{rec: stubRecord{}, err: &mstkit.ValidationError{Fields: []doctype.FieldError{
		{Field: "name", Message: "Name is wrong."}, {Message: "Not allowed."},
	}}})
	resp := h.Browser(h.Env.Kavya).PostHTMX(t, record, url.Values{"version": {"3"}})
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	if resp.Doc.Find("#record-alert").Length() != 0 {
		t.Errorf("the banner takes focus beside the summary: %.300s", resp.Body)
	}
	if !strings.Contains(resp.Body, "Not allowed.") {
		t.Errorf("the banner is gone: %.300s", resp.Body)
	}
}

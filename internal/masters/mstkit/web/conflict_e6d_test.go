package mstweb_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	mstweb "github.com/vardhan/internaltools/internal/masters/mstkit/web"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

const e6dMsg = "Producer's own words."

// e6dWantConflict fails unless r's one banner is the warning conflict banner
// (CD-365) with msg and a Reload button reading button to href.
func e6dWantConflict(t *testing.T, r webtest.Response, button, href string) {
	t.Helper()
	b := r.Doc.Find("#main .banner")
	if b.Length() != 1 || !b.HasClass("banner--warning") {
		t.Fatalf("banners = %d (class %q); want one warning\ntext: %s", b.Length(), b.AttrOr("class", ""), r.Text())
	}
	webtest.WantBanner(t, r, e6dMsg)
	a := b.Find("a.btn")
	if got := webtest.Squash(a.Text()); a.Length() != 1 || got != button || a.AttrOr("href", "") != href {
		t.Errorf("Reload button = %q -> %q (%d); want %q -> %s", got, a.AttrOr("href", ""), a.Length(), button, href)
	}
}

// e6dWantDanger fails unless r's one banner is a danger banner with msg and
// no Reload.
func e6dWantDanger(t *testing.T, r webtest.Response) {
	t.Helper()
	b := r.Doc.Find("#main .banner")
	if b.Length() != 1 || !b.HasClass("banner--danger") || b.Find("a").Length() != 0 {
		t.Fatalf("banners = %d (class %q, %d links); want one danger without a link\ntext: %s", b.Length(), b.AttrOr("class", ""), b.Find("a").Length(), r.Text())
	}
	webtest.WantBanner(t, r, e6dMsg)
}

// No case ID: a stale save on a master's form is the conflict banner naming
// the record (CD-365), plain Reload on a new record; a 422 with no field stays
// a danger banner.
func TestScreens_ConflictBanner(t *testing.T) {
	for _, tc := range []struct {
		name, target, button, href string
		sections                   bool
	}{
		{"record", record, "Reload STUB Stub", record, false},
		{"new", base + "/new", "Reload", base + "/new", false},
		{"section", record + "/edit/details", "Reload STUB Stub", record + "/edit/details", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, c := range []struct {
				err    error
				status int
			}{
				{&doc.Error{Kind: doc.KindStale, Message: e6dMsg}, http.StatusConflict},
				{doc.Refused(e6dMsg), http.StatusConflict},
				{&refusal{text: e6dMsg, status: http.StatusUnprocessableEntity}, http.StatusUnprocessableEntity},
			} {
				h := harness(t, stubService{rec: stubRecord{"code": "X"}, err: c.err}, func(s *screen) {
					if tc.sections {
						s.Sections = []docscreen.FormStep{{Slug: "details", Label: "Details", Fields: []string{"code"}}}
					}
				})
				r := h.Post(t, h.Env.Kavya, tc.target, url.Values{"version": {"3"}, "code": {"Y"}})
				webtest.WantStatus(t, r, c.status)
				if doc.IsStale(c.err) {
					e6dWantConflict(t, r, tc.button, tc.href)
				} else {
					e6dWantDanger(t, r)
				}
			}
		})
	}
}

// No case ID: a SubForm's 409 names its own record (its Title), plain Reload
// for a new one; its 422 stays danger.
func TestPages_SubFormConflictBanner(t *testing.T) {
	const rowForm = record + "/rows/1"
	for _, tc := range []struct {
		id     int64
		reload bool
		button string
	}{{1, true, "Reload Row"}, {0, true, "Reload"}, {1, false, ""}} {
		h := harness(t, stubService{rec: stubRecord{"code": "X"}}, func(s *screen) {
			s.Mount = func(pages mstweb.Pages) func(chi.Router) {
				return func(r chi.Router) {
					r.Get(rowForm, pages.Handle(func(w http.ResponseWriter, r *http.Request, _ auth.Principal) error {
						mstweb.RenderSubForm(w, r, http.StatusOK, mstweb.SubForm{
							ID: tc.id, Title: "Row", Action: rowForm,
							Back: components.Link{Label: "Parent", Href: base}, Form: mstweb.Form{Version: 2, Problem: e6dMsg, Reload: tc.reload},
						})
						return nil
					}))
				}
			}
		})
		r := h.Get(t, h.Env.Kavya, rowForm)
		if tc.reload {
			e6dWantConflict(t, r, tc.button, rowForm)
		} else {
			e6dWantDanger(t, r)
		}
	}
}

package mstweb_test

import (
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
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

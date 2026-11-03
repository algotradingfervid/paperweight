package mstweb_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
)

// R2 (UI v2 §5.14): a master wizard step's htmx answer starts with the step's
// <title>, which htmx moves into the head, "Error: " in front on a refusal.
func TestWizard_HTMXAnswerCarriesTheTitle(t *testing.T) {
	h := wizardHarness(t, nil, moreWizard)
	b := h.Browser(h.Env.Kavya)

	r := b.PostHTMX(t, wizardFirst, url.Values{"code": {"X"}, "note": {"bad"}})
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	if got := r.Doc.Find("title").Text(); !strings.HasPrefix(got, "Error: Details · ") {
		t.Errorf("refused: title = %q, want Error: Details · …", got)
	}

	r = b.PostHTMX(t, wizardFirst, url.Values{"code": {"X"}})
	webtest.WantStatus(t, r, http.StatusOK)
	if got := r.Doc.Find("title").Text(); !strings.HasPrefix(got, "More · ") {
		t.Errorf("next step: title = %q, want More · …", got)
	}
}

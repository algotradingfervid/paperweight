package partyweb_test

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
)

// No case ID: UI v2 C1b, §5.5 item 3's first users. Party's Payment terms and
// Active (its Disabled field) are More: the wizard's details step puts Payment
// terms in a closed More details at its end, opened once terms are picked;
// Disabled, set by Disable and Enable, is on no form. The Payment Terms
// section, whose only field it is, shows it without a group.
func TestScreens_PartyMoreDetails(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	page := b.Get(t, stepDetails)
	webtest.WantStatus(t, page, http.StatusOK)
	d := page.Doc.Find("form details.disclosure")
	if d.Length() != 1 || d.Find("#f-default_payment_terms_id").Length() != 1 {
		t.Fatalf("details step: %d More details groups; Payment terms inside = %d", d.Length(), d.Find("#f-default_payment_terms_id").Length())
	}
	if _, open := d.Attr("open"); open {
		t.Error("details step: More details is open with nothing picked")
	}
	if got := webtest.Squash(d.Find("summary").Text()); got != "More details · payment terms" {
		t.Errorf("summary = %q", got)
	}
	if d.Find("#f-pan").Length() != 0 || page.Doc.Find(`[name="disabled"]`).Length() != 0 {
		t.Error("details step: PAN is inside More details, or the form has a Disabled input")
	}

	terms := w.terms(t, "N30", "Net 30")
	webtest.WantStatus(t, b.Post(t, stepDetails, url.Values{
		"code": {"MEHTA"}, "name": {"Mehta Traders"}, "role": {"vendor"},
		"gst_category": {"Unregistered"}, "default_payment_terms_id": {strconv.FormatInt(terms, 10)},
	}), http.StatusSeeOther)
	if _, open := b.Get(t, stepDetails).Doc.Find("form details.disclosure").Attr("open"); !open {
		t.Error("details step: More details is closed with Payment terms picked")
	}

	shree := w.get(t, "SHREE")
	section := b.Get(t, recordURL(shree.ID)+"/edit/terms")
	webtest.WantStatus(t, section, http.StatusOK)
	if section.Doc.Find("form details.disclosure").Length() != 0 || section.Doc.Find("form #f-default_payment_terms_id").Length() != 1 {
		t.Error("Payment Terms section: want its field without a More details group")
	}
}

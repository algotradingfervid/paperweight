package expenseweb_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// A stale Mark as paid or Undo paid shows the ConflictBanner (CD-362): the
// warning with DOC's text and "Reload EA/26-27/00001" to the page's own
// route; a non-conflict refusal stays the danger banner.
func TestPaid_ConflictBanner(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	adv := e.approvedAdvance(t)
	v, _ := e.stored(t, adv.ID)
	for _, tc := range []struct {
		route, paidOn string
		version       int
		status        int
	}{
		{"paid", "21-10-2026", v - 1, http.StatusConflict},
		{"unpaid", "", v - 1, http.StatusConflict},
		{"unpaid", "", v, http.StatusUnprocessableEntity},
	} {
		rec := e.post(t, webMeena, adv.ID, tc.route, form(tc.version, tc.paidOn))
		if rec.Code != tc.status {
			t.Fatalf("%s v%d: status %d, want %d", tc.route, tc.version, rec.Code, tc.status)
		}
		d, err := goquery.NewDocumentFromReader(strings.NewReader(rec.Body.String()))
		if err != nil {
			t.Fatal(err)
		}
		banner := d.Find("#paid-alert .banner")
		if tc.status != http.StatusConflict {
			if banner.Length() != 1 || !banner.HasClass("banner--danger") {
				t.Errorf("%s %d: banner class %q, want the danger banner", tc.route, tc.status, banner.AttrOr("class", ""))
			}
			continue
		}
		if banner.Length() != 1 || !banner.HasClass("banner--warning") {
			t.Fatalf("%s 409: %d banners (class %q), want one warning", tc.route, banner.Length(), banner.AttrOr("class", ""))
		}
		if got := strings.Join(strings.Fields(banner.Text()), " "); !strings.Contains(got, webMsgStale) {
			t.Errorf("%s 409: banner %q lacks DOC's text", tc.route, got)
		}
		a := banner.Find("a.btn")
		want := "/modules/expense/advances/" + strconv.FormatInt(adv.ID, 10) + "/" + tc.route
		if got := strings.Join(strings.Fields(a.Text()), " "); a.Length() != 1 || got != "Reload EA/26-27/00001" || a.AttrOr("href", "") != want {
			t.Errorf("%s 409: button %q -> %q, want Reload EA/26-27/00001 -> %s", tc.route, got, a.AttrOr("href", ""), want)
		}
	}
}

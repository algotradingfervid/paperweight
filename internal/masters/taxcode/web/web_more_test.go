package taxcodeweb_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/taxcode"
	"github.com/vardhan/internaltools/internal/types"
)

// No case ID: the screen behaviour Task 22 adds beyond its named tests.

// seeded is migration 01301's ten codes.
var seeded = []string{"GST0.25", "GST3", "GST5", "GST12", "GST18", "GST28", "GST40", "NIL", "EXEMPT", "NONGST"}

// numMessage is NUM's own text for a rate it refuses.
func numMessage(t *testing.T, rate string) string {
	t.Helper()
	_, err := types.ParsePercent(rate)
	if err == nil {
		t.Fatalf("ParsePercent(%q) accepted it", rate)
	}
	return err.Error()
}

// A non-Taxable code posts its rate empty: that is 0, not a parse error, and
// on a Taxable code it is the service's rule, never "value is required".
func TestScreens_TaxCodeBlankRate(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya

	for _, rate := range []string{"", "  "} {
		code := "NIL2" + strings.ReplaceAll(rate, " ", "S")
		resp := w.h.Post(t, kavya, "/m/tax-code/new", url.Values{"code": {code}, "name": {"Nil two"}, "treatment": {"Nil-Rated"}, "rate": {rate}})
		saved, ok := w.byCode(t, code)
		if !ok {
			t.Fatalf("rate %q: POST saved nothing: %d %s", rate, resp.Status, webtest.Squash(resp.Body))
		}
		webtest.WantRedirect(t, resp, path(saved.ID, ""))
		if saved.Rate != 0 {
			t.Errorf("rate %q stored as %v; want 0", rate, saved.Rate)
		}
	}

	resp := w.h.Post(t, kavya, "/m/tax-code/new", url.Values{"code": {"GST0T"}, "name": {"GST zero"}, "treatment": {"Taxable"}, "rate": {""}})
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, resp, "rate", msgTaxableZero)
}

// A rate NUM refuses keeps NUM's message on the field and the typed text,
// never becomes 0 (CD-37), even on a treatment that ignores the rate, and the
// other fields' errors show with it.
func TestScreens_TaxCodeUnparsedRate(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya

	for _, rate := range []string{"abc", "150", "1.2345"} {
		for _, treatment := range []string{"Taxable", "Nil-Rated"} {
			resp := w.h.Post(t, kavya, "/m/tax-code/new", url.Values{"code": {"NEW1"}, "name": {""}, "treatment": {treatment}, "rate": {rate}})
			webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
			webtest.WantFieldError(t, resp, "rate", numMessage(t, rate))
			webtest.WantFieldError(t, resp, "name", "Name is required.")
			if v, _ := resp.Value("rate"); v != rate {
				t.Errorf("422 page rate value = %q; want the typed %q", v, rate)
			}
		}
	}
	if _, ok := w.byCode(t, "NEW1"); ok {
		t.Error("NEW1 saved despite the 422")
	}

	nil0 := w.mustGet(t, "NIL")
	resp := w.h.Post(t, kavya, path(nil0.ID, ""), url.Values{"version": {ver(nil0.Version)}, "code": {"NIL"}, "name": {"Changed"}, "treatment": {"Nil-Rated"}, "rate": {"abc"}})
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, resp, "rate", numMessage(t, "abc"))
	if got := w.mustGet(t, "NIL"); got != nil0 {
		t.Errorf("NIL after the refused save = %+v; want unchanged %+v", got, nil0)
	}
}

// A form that doesn't parse never reaches the service, so the screen checks
// the right itself: 403 ahead of the 422.
func TestScreens_TaxCodeUnparsedRateNeedsRight(t *testing.T) {
	w := newWorld(t)
	priya := w.h.Env.Priya
	gst18 := w.mustGet(t, "GST18")
	deny := func(r auth.Right) string { return (&auth.DeniedError{Right: r, Label: "Tax Code"}).Error() }

	resp := w.h.Post(t, priya, "/m/tax-code/new", url.Values{"code": {"NEW1"}, "name": {"New"}, "treatment": {"Taxable"}, "rate": {"abc"}})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightCreate))

	resp = w.h.Post(t, priya, path(gst18.ID, ""), url.Values{"version": {ver(gst18.Version)}, "code": {"GST18"}, "name": {"Renamed"}, "treatment": {"Taxable"}, "rate": {"abc"}})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightWrite))
}

// The record page's form holds the stored values as they can be saved again:
// the rate plain, a fraction and 0 included, the treatment as stored.
func TestScreens_TaxCodeFormShowsStored(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	for _, c := range []struct{ code, name, treatment, rate string }{
		{"GST0.25", "GST 0.25%", "Taxable", "0.25"},
		{"NIL", "Nil-Rated", "Nil-Rated", "0"},
	} {
		tc := w.mustGet(t, c.code)
		page := w.h.Get(t, kavya, path(tc.ID, ""))
		webtest.WantStatus(t, page, http.StatusOK)
		webtest.WantText(t, page, c.code+" "+c.name)
		for name, want := range map[string]string{"code": c.code, "name": c.name, "rate": c.rate} {
			if got, _ := page.Value(name); got != want {
				t.Errorf("%s form %s = %q; want %q", c.code, name, got, want)
			}
		}
		if got := page.Doc.Find(`input[name="treatment"][checked]`).AttrOr("value", ""); got != c.treatment {
			t.Errorf("%s form treatment = %q; want %q", c.code, got, c.treatment)
		}
		resp := w.h.Post(t, kavya, path(tc.ID, ""), url.Values{"version": {ver(tc.Version)}, "code": {c.code}, "name": {c.name}, "treatment": {c.treatment}, "rate": {c.rate}})
		webtest.WantRedirect(t, resp, path(tc.ID, ""))
		if got := w.mustGet(t, c.code); got.Rate != tc.Rate || got.Treatment != tc.Treatment {
			t.Errorf("%s after saving its own form = %+v; want %+v", c.code, got, tc)
		}
	}
}

// Priya reads a record's values, with the rate as a percentage, without a
// form. The name has no percentage, so only the rate's own value can show it.
func TestScreens_TaxCodeReadOnlyPage(t *testing.T) {
	w := newWorld(t)
	saved, err := w.svc.Create(context.Background(), w.h.Env.Kavya, taxcode.Input{Code: "SIX", Name: "Six", Treatment: taxcode.Taxable, Rate: 6000})
	if err != nil {
		t.Fatal(err)
	}
	page := w.h.Get(t, w.h.Env.Priya, path(saved.ID, ""))
	webtest.WantStatus(t, page, http.StatusOK)
	for _, text := range []string{"SIX Six", "Taxable", "6%"} {
		webtest.WantText(t, page, text)
	}
	if page.HasControl("rate") {
		t.Error("Priya's record page has a rate control; want values only")
	}
}

// A 0% rate is a rate (ZeroIsNone is Item's, CD-37): the list shows it.
func TestScreens_TaxCodeListShowsZeroRate(t *testing.T) {
	w := newWorld(t)
	nil0 := w.mustGet(t, "NIL")
	text, ok := w.h.Get(t, w.h.Env.Kavya, "/m/tax-code").RowLink(path(nil0.ID, ""))
	if !ok || !strings.Contains(text, "0%") {
		t.Errorf("NIL row = %q (found %v); want it to show 0%%", text, ok)
	}
}

// The list's words are Tax Code's, in sentence case, with the plural and the
// singular as they read inside a sentence.
func TestScreens_TaxCodeListCopy(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	ctx := context.Background()

	list := w.h.Get(t, kavya, "/m/tax-code")
	webtest.WantStatus(t, list, http.StatusOK)
	for _, text := range []string{"Tax Codes", "New tax code", "Search tax codes", "10 tax codes", "Show disabled tax codes"} {
		webtest.WantText(t, list, text)
	}
	webtest.WantText(t, w.h.Get(t, kavya, "/m/tax-code?q=Exempt"), "1 tax code")

	for _, code := range seeded {
		tc := w.mustGet(t, code)
		if err := w.svc.SetDisabled(ctx, kavya, tc.ID, tc.Version, true); err != nil {
			t.Fatal(err)
		}
	}
	webtest.WantText(t, w.h.Get(t, kavya, "/m/tax-code"), "All tax codes are disabled.")

	for _, code := range seeded {
		tc := w.mustGet(t, code)
		if err := w.svc.Delete(ctx, kavya, tc.ID, tc.Version); err != nil {
			t.Fatal(err)
		}
	}
	webtest.WantText(t, w.h.Get(t, kavya, "/m/tax-code"), "No tax codes yet.")
}

// Each successful write shows its success banner on the page the 303 leads
// to (CD-80).
func TestScreens_TaxCodeSuccessFlash(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya

	resp := w.h.Post(t, kavya, "/m/tax-code/new", url.Values{"code": {"GST6"}, "name": {"GST 6%"}, "treatment": {"Taxable"}, "rate": {"6"}})
	gst6, ok := w.byCode(t, "GST6")
	if !ok {
		t.Fatalf("valid POST saved nothing: %d %s", resp.Status, webtest.Squash(resp.Body))
	}
	webtest.WantBanner(t, w.h.Follow(t, kavya, resp), "Tax code saved.")

	resp = w.h.Post(t, kavya, path(gst6.ID, "/disable"), url.Values{"version": {ver(gst6.Version)}})
	webtest.WantBanner(t, w.h.Follow(t, kavya, resp), "Tax code disabled.")

	off := w.mustGet(t, "GST6")
	resp = w.h.Post(t, kavya, path(gst6.ID, "/enable"), url.Values{"version": {ver(off.Version)}})
	webtest.WantBanner(t, w.h.Follow(t, kavya, resp), "Tax code enabled.")

	on := w.mustGet(t, "GST6")
	resp = w.h.Post(t, kavya, path(gst6.ID, "/delete"), url.Values{"version": {ver(on.Version)}})
	webtest.WantRedirect(t, resp, "/m/tax-code")
	webtest.WantBanner(t, w.h.Follow(t, kavya, resp), "Tax code deleted.")
}

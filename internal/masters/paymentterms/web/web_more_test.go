package paymenttermsweb_test

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/paymentterms"
	"github.com/vardhan/internaltools/internal/types"
)

// No case ID: the screen behaviour Task 22 adds beyond its named tests.

// create stores rows under code.
func (w *world) create(t *testing.T, code string, rows ...paymentterms.Row) paymentterms.Terms {
	t.Helper()
	if _, err := w.svc.Create(context.Background(), w.h.Env.Kavya, paymentterms.Input{Code: code, Name: "Terms " + code, Rows: rows}); err != nil {
		t.Fatal(err)
	}
	return w.mustGet(t, code)
}

// numMessage is NUM's own text for a portion it refuses.
func numMessage(t *testing.T, portion string) string {
	t.Helper()
	_, err := types.ParsePercent(portion)
	if err == nil {
		t.Fatalf("ParsePercent(%q) accepted it", portion)
	}
	return err.Error()
}

// Saving a record's form as the page drew it changes nothing but the version:
// Update replaces the rows, so a row the form dropped or reformatted would be
// lost. The percentages include fractions at every precision the type holds.
func TestScreens_PaymentTermsSaveUntouchedKeepsRows(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	for _, c := range []struct {
		code string
		rows []paymentterms.Row
		want []string
	}{
		{"THIRDS", []paymentterms.Row{{Portion: 33333, Days: 0}, {Portion: 33333, Days: 30}, {Portion: 33334, Days: 60}}, []string{"33.333", "33.333", "33.334"}},
		{"SPLIT", []paymentterms.Row{{Portion: 12500, Days: 7}, {Portion: 87500, Days: 90}}, []string{"12.5", "87.5"}},
		{"WHOLE", []paymentterms.Row{{Portion: pct100, Days: 0}}, []string{"100"}},
	} {
		saved := w.create(t, c.code, c.rows...)
		page := w.h.Get(t, kavya, path(saved.ID, ""))
		webtest.WantStatus(t, page, http.StatusOK)

		form := url.Values{"version": {ver(saved.Version)}, "code": {c.code}, "name": {saved.Name}}
		for i, r := range c.rows {
			n := strconv.Itoa(i + 1)
			portion, _ := page.Value("lines[" + n + "].portion")
			if portion != c.want[i] {
				t.Errorf("%s row %s portion = %q; want %q", c.code, n, portion, c.want[i])
			}
			days, _ := page.Value("lines[" + n + "].days")
			if days != strconv.Itoa(r.Days) {
				t.Errorf("%s row %s days = %q; want %d", c.code, n, days, r.Days)
			}
			form.Set("lines["+n+"].portion", portion)
			form.Set("lines["+n+"].days", days)
		}
		if page.HasControl("lines[" + strconv.Itoa(len(c.rows)+1) + "].portion") {
			t.Errorf("%s page has a row past its %d", c.code, len(c.rows))
		}

		webtest.WantRedirect(t, w.h.Post(t, kavya, path(saved.ID, ""), form), path(saved.ID, ""))
		got := w.mustGet(t, c.code)
		if !slices.Equal(got.Rows, c.rows) || got.Version != saved.Version+1 {
			t.Errorf("%s after saving its own form = %+v; want rows %v, version %d", c.code, got, c.rows, saved.Version+1)
		}
	}
}

// Renaming on the page with the rows left as drawn keeps them: the form is
// built from the page's own values.
func TestScreens_PaymentTermsRenameKeepsRows(t *testing.T) {
	w := newWorld(t)
	_, adv50 := w.seed(t)
	page := w.h.Get(t, w.h.Env.Kavya, path(adv50.ID, ""))
	form := url.Values{"version": {ver(adv50.Version)}, "code": {"ADV50"}, "name": {"Renamed"}}
	for _, name := range []string{"lines[1].portion", "lines[1].days", "lines[2].portion", "lines[2].days"} {
		v, ok := page.Value(name)
		if !ok {
			t.Fatalf("record page has no %s", name)
		}
		form.Set(name, v)
	}
	webtest.WantRedirect(t, w.h.Post(t, w.h.Env.Kavya, path(adv50.ID, ""), form), path(adv50.ID, ""))
	if got := w.mustGet(t, "ADV50"); got.Name != "Renamed" || !slices.Equal(got.Rows, adv50.Rows) {
		t.Errorf("after rename ADV50 = %+v; want rows %v", got, adv50.Rows)
	}
}

// Each row that doesn't parse shows its error next to its own input, whatever
// the row and field; the rest keep what was typed. The total is not reported
// because the failed row isn't in it, and the other fields' errors are.
func TestScreens_PaymentTermsUnparsedRows(t *testing.T) {
	w := newWorld(t)
	resp := w.h.Post(t, w.h.Env.Kavya, "/m/payment-terms/new", url.Values{
		"code": {"BAD"}, "name": {""},
		"lines[1].portion": {"25"}, "lines[1].days": {"7.5"},
		"lines[2].portion": {"abc"}, "lines[2].days": {"30"},
		"lines[3].portion": {""}, "lines[3].days": {""},
	})
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, resp, "lines[1].days", "Row 1: Days must be a whole number, 0 or more.")
	webtest.WantFieldError(t, resp, "lines[2].portion", "Row 2: "+numMessage(t, "abc"))
	webtest.WantFieldError(t, resp, "lines[3].portion", "Row 3: Portion is required.")
	webtest.WantFieldError(t, resp, "lines[3].days", "Row 3: Days must be a whole number, 0 or more.")
	webtest.WantFieldError(t, resp, "name", "Name is required.")
	webtest.WantNoText(t, resp, "add up to")
	for name, want := range map[string]string{"lines[1].days": "7.5", "lines[2].portion": "abc", "lines[1].portion": "25"} {
		if got, _ := resp.Value(name); got != want {
			t.Errorf("422 page %s = %q; want the typed %q", name, got, want)
		}
	}
	if _, ok := w.byCode(t, "BAD"); ok {
		t.Error("BAD saved despite the 422")
	}
}

// A portion NUM refuses is its message with the row's prefix.
func TestScreens_PaymentTermsUnparsedPortions(t *testing.T) {
	w := newWorld(t)
	for _, portion := range []string{"150", "1.2345", "-5"} {
		resp := w.h.Post(t, w.h.Env.Kavya, "/m/payment-terms/new", url.Values{
			"code": {"BAD"}, "name": {"Bad"}, "lines[1].portion": {portion}, "lines[1].days": {"0"},
		})
		webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
		webtest.WantFieldError(t, resp, "lines[1].portion", "Row 1: "+numMessage(t, portion))
	}
}

// A refused save leaves the stored rows alone, whether the rows failed to
// parse or the service refused them.
func TestScreens_PaymentTermsRefusedSaveKeepsRows(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	_, adv50 := w.seed(t)
	for _, form := range []url.Values{
		{"lines[1].portion": {"100"}, "lines[1].days": {"-1"}},
		{"lines[1].portion": {"50"}, "lines[1].days": {"1"}},
		{},
	} {
		form.Set("version", ver(adv50.Version))
		form.Set("code", "ADV50")
		form.Set("name", "Changed")
		resp := w.h.Post(t, kavya, path(adv50.ID, ""), form)
		webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
		if got := w.mustGet(t, "ADV50"); got.Name != adv50.Name || !slices.Equal(got.Rows, adv50.Rows) || got.Version != adv50.Version {
			t.Errorf("ADV50 after the refused save = %+v; want unchanged %+v", got, adv50)
		}
	}
}

// A form with no rows at all is the service's total, in the banner: it never
// empties the table.
func TestScreens_PaymentTermsNoRows(t *testing.T) {
	w := newWorld(t)
	net30, _ := w.seed(t)
	resp := w.h.Post(t, w.h.Env.Kavya, path(net30.ID, ""), url.Values{"version": {ver(net30.Version)}, "code": {"NET30"}, "name": {net30.Name}})
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantBanner(t, resp, "Payment portions add up to 0%. They must total 100%.")
}

// The service's row rules show through the form: the total in the banner
// (MST-124, Field ""), a repeat and a zero portion on their own row (MST-126,
// MST-127).
func TestScreens_PaymentTermsRowRules(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya

	resp := w.h.Post(t, kavya, "/m/payment-terms/new", url.Values{
		"code": {"HALF"}, "name": {"Half"}, "lines[1].portion": {"50"}, "lines[1].days": {"30"},
	})
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantBanner(t, resp, msgTotal50)

	resp = w.h.Post(t, kavya, "/m/payment-terms/new", url.Values{
		"code": {"TWICE"}, "name": {"Twice"},
		"lines[1].portion": {"50"}, "lines[1].days": {"30"},
		"lines[2].portion": {"50"}, "lines[2].days": {"30"},
	})
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, resp, "lines[2].days", "Row 2 repeats row 1 (30 days). Merge them into one row of 100%.")

	resp = w.h.Post(t, kavya, "/m/payment-terms/new", url.Values{
		"code": {"ZERO"}, "name": {"Zero"},
		"lines[1].portion": {"0"}, "lines[1].days": {"0"},
		"lines[2].portion": {"100"}, "lines[2].days": {"30"},
	})
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, resp, "lines[1].portion", "Row 1: Portion must be more than 0%.")
}

// Rows posted with gaps in their numbers, as after a removed row in another
// tab, are all kept, in order, and an error names the row as the page draws
// it.
func TestScreens_PaymentTermsGappedRows(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	resp := w.h.Post(t, kavya, "/m/payment-terms/new", url.Values{
		"code": {"GAPS"}, "name": {"Gaps"},
		"lines[2].portion": {"40"}, "lines[2].days": {"10"},
		"lines[5].portion": {"60"}, "lines[5].days": {"20"},
	})
	gaps, ok := w.byCode(t, "GAPS")
	if !ok {
		t.Fatalf("POST saved nothing: %d %s", resp.Status, webtest.Squash(resp.Body))
	}
	if want := []paymentterms.Row{{Portion: 40000, Days: 10}, {Portion: 60000, Days: 20}}; !slices.Equal(gaps.Rows, want) {
		t.Errorf("stored rows = %v; want %v", gaps.Rows, want)
	}

	resp = w.h.Post(t, kavya, "/m/payment-terms/new", url.Values{
		"code": {"GAPS2"}, "name": {"Gaps"},
		"lines[2].portion": {"100"}, "lines[2].days": {"-1"},
	})
	webtest.WantFieldError(t, resp, "lines[1].days", "Row 1: Days must be a whole number, 0 or more.")
}

// A form that doesn't parse never reaches the service, so the screen checks
// the right itself: 403 ahead of the 422.
func TestScreens_PaymentTermsUnparsedRowsNeedRight(t *testing.T) {
	w := newWorld(t)
	priya := w.h.Env.Priya
	net30, _ := w.seed(t)
	deny := func(r auth.Right) string { return (&auth.DeniedError{Right: r, Label: "Payment Terms"}).Error() }

	resp := w.h.Post(t, priya, "/m/payment-terms/new", url.Values{"code": {"BAD"}, "name": {"Bad"}, "lines[1].portion": {"abc"}, "lines[1].days": {"0"}})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightCreate))

	resp = w.h.Post(t, priya, path(net30.ID, ""), url.Values{"version": {ver(net30.Version)}, "code": {"NET30"}, "name": {"Bad"}, "lines[1].portion": {"abc"}, "lines[1].days": {"0"}})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightWrite))
}

// Priya reads a record's rows, each with its percentage and days, without a
// form.
func TestScreens_PaymentTermsReadOnlyPage(t *testing.T) {
	w := newWorld(t)
	saved := w.create(t, "SPLIT", paymentterms.Row{Portion: 12500, Days: 7}, paymentterms.Row{Portion: 87500, Days: 90})
	page := w.h.Get(t, w.h.Env.Priya, path(saved.ID, ""))
	webtest.WantStatus(t, page, http.StatusOK)
	for _, text := range []string{"SPLIT Terms SPLIT", "12.5%", "87.5%", "Portion", "Days", "90"} {
		webtest.WantText(t, page, text)
	}
	if page.HasControl("lines[1].portion") {
		t.Error("Priya's record page has a row control; want values only")
	}
}

// Add and Remove redraw the page with the typed rows changed, in Payment
// Terms' words, and save nothing.
func TestScreens_PaymentTermsRowsEditor(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	net30, _ := w.seed(t)

	page := w.h.Get(t, kavya, path(net30.ID, ""))
	webtest.WantText(t, page, "Payment portions")
	webtest.WantText(t, page, "Add portion")

	resp := w.h.Post(t, kavya, path(net30.ID, ""), url.Values{
		"_rows": {"add"}, "version": {ver(net30.Version)}, "code": {"NET30"}, "name": {net30.Name},
		"lines[1].portion": {"60"}, "lines[1].days": {"30"},
	})
	webtest.WantStatus(t, resp, http.StatusOK)
	if got, _ := resp.Value("lines[1].portion"); got != "60" {
		t.Errorf("after Add row 1 portion = %q; want the typed 60", got)
	}
	if !resp.HasControl("lines[2].portion") || !resp.HasControl("lines[2].days") {
		t.Error("no row 2 after Add")
	}

	resp = w.h.Post(t, kavya, path(net30.ID, ""), url.Values{
		"_rows": {"remove:1"}, "version": {ver(net30.Version)}, "code": {"NET30"}, "name": {net30.Name},
		"lines[1].portion": {"60"}, "lines[1].days": {"30"},
		"lines[2].portion": {"40"}, "lines[2].days": {"45"},
	})
	webtest.WantStatus(t, resp, http.StatusOK)
	if got, _ := resp.Value("lines[1].days"); got != "45" {
		t.Errorf("after Remove row 1 days = %q; want the second row's 45", got)
	}
	if got := w.mustGet(t, "NET30"); got.Version != net30.Version || !slices.Equal(got.Rows, net30.Rows) {
		t.Errorf("NET30 after Add and Remove = %+v; want unchanged %+v", got, net30)
	}
}

// The list's words are Payment Terms', in sentence case, with the plural and
// the singular as they read inside a sentence.
func TestScreens_PaymentTermsListCopy(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	ctx := context.Background()

	webtest.WantText(t, w.h.Get(t, kavya, "/m/payment-terms"), "No payment terms yet.")

	net30, adv50 := w.seed(t)
	list := w.h.Get(t, kavya, "/m/payment-terms")
	for _, text := range []string{"Payment Terms", "New payment term", "Search payment terms", "2 payment terms", "Show disabled payment terms"} {
		webtest.WantText(t, list, text)
	}
	webtest.WantText(t, w.h.Get(t, kavya, "/m/payment-terms?q=advance"), "1 payment term")

	for _, v := range []paymentterms.Terms{net30, adv50} {
		if err := w.svc.SetDisabled(ctx, kavya, v.ID, v.Version, true); err != nil {
			t.Fatal(err)
		}
	}
	webtest.WantText(t, w.h.Get(t, kavya, "/m/payment-terms"), "All payment terms are disabled.")
}

// Each successful write shows its success banner on the page the 303 leads to
// (CD-80).
func TestScreens_PaymentTermsSuccessFlash(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya

	resp := w.h.Post(t, kavya, "/m/payment-terms/new", halfForm("Half"))
	half, ok := w.byCode(t, "HALF")
	if !ok {
		t.Fatalf("valid POST saved nothing: %d %s", resp.Status, webtest.Squash(resp.Body))
	}
	webtest.WantBanner(t, w.h.Follow(t, kavya, resp), "Payment term saved.")

	resp = w.h.Post(t, kavya, path(half.ID, "/disable"), url.Values{"version": {ver(half.Version)}})
	webtest.WantBanner(t, w.h.Follow(t, kavya, resp), "Payment term disabled.")

	off := w.mustGet(t, "HALF")
	resp = w.h.Post(t, kavya, path(half.ID, "/enable"), url.Values{"version": {ver(off.Version)}})
	webtest.WantBanner(t, w.h.Follow(t, kavya, resp), "Payment term enabled.")

	on := w.mustGet(t, "HALF")
	resp = w.h.Post(t, kavya, path(half.ID, "/delete"), url.Values{"version": {ver(on.Version)}})
	webtest.WantRedirect(t, resp, "/m/payment-terms")
	webtest.WantBanner(t, w.h.Follow(t, kavya, resp), "Payment term deleted.")
}

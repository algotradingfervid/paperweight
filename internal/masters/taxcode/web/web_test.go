package taxcodeweb_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/taxcode"
	taxcodeweb "github.com/vardhan/internaltools/internal/masters/taxcode/web"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Screens under test (MST Task 22): /m/tax-code list, /new, /{id}, POST /{id}
// with version, /{id}/disable, /{id}/enable, /{id}/delete and the picker
// /pick/Tax%20Code?q= (CD-195). The ten tax codes are seeded by migration 01301.
const (
	msgNotNotified = "GST rate 10% is not a notified rate. Use one of 0.1, 0.25, 1, 1.5, 3, 5, 6, 7.5, 12, 18, 28, 40." // MST-096
	msgTaxableZero = "A Taxable tax code needs a GST rate above 0. Use Nil-Rated or Exempted for 0%."                   // MST-094
	staleGST6      = "Someone else changed Tax Code GST6 after you opened it. Reload it and make your changes again."
)

type world struct {
	h   *webtest.Harness
	svc *taxcode.Service
}

func newWorld(t *testing.T) *world {
	t.Helper()
	h := webtest.New(t, func(k mstkit.Env) func(chi.Router) { return taxcodeweb.Routes(taxcode.New(k), k) })
	return &world{h: h, svc: taxcode.New(h.Env.Kit)}
}

func (w *world) read(t *testing.T, fn func(ctx context.Context, tx *db.Tx) error) {
	t.Helper()
	ctx := context.Background()
	if err := w.h.Env.DB.Read(ctx, func(tx *db.Tx) error { return fn(ctx, tx) }); err != nil {
		t.Fatal(err)
	}
}

func (w *world) byCode(t *testing.T, code string) (taxcode.TaxCode, bool) {
	t.Helper()
	var v taxcode.TaxCode
	var ok bool
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		var err error
		v, ok, err = w.svc.ByCode(ctx, tx, code)
		return err
	})
	return v, ok
}

func (w *world) mustGet(t *testing.T, code string) taxcode.TaxCode {
	t.Helper()
	v, ok := w.byCode(t, code)
	if !ok {
		t.Fatalf("tax code %s not found", code)
	}
	return v
}

func path(id int64, suffix string) string {
	return "/m/tax-code/" + strconv.FormatInt(id, 10) + suffix
}

func ver(v int) string { return strconv.Itoa(v) }

func TestScreens_TaxCodeCreateEditDisable(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya

	list := w.h.Get(t, kavya, "/m/tax-code")
	webtest.WantStatus(t, list, http.StatusOK)
	for _, code := range []string{"GST18", "NIL", "EXEMPT"} {
		tc := w.mustGet(t, code)
		text, ok := list.RowLink(path(tc.ID, ""))
		if !ok || !strings.Contains(text, tc.Code+" "+tc.Name) {
			t.Errorf("list row %s = %q (found %v); want a link containing %q", code, text, ok, tc.Code+" "+tc.Name)
		}
	}
	if _, ok := list.RowLink("/m/tax-code/new"); !ok {
		t.Error("list has no link to /m/tax-code/new for Kavya (create right)")
	}

	// Treatment options are the stored values, shown as they are stored
	// (CD-42 Options in display order, CD-175).
	blank := w.h.Get(t, kavya, "/m/tax-code/new")
	webtest.WantStatus(t, blank, http.StatusOK)
	var want []webtest.Choice
	for _, v := range []string{"Taxable", "Nil-Rated", "Exempted", "Non-GST", "Zero-Rated"} {
		want = append(want, webtest.Choice{Value: v, Label: v})
	}
	if got := blank.Choices("treatment"); !slices.Equal(got, want) {
		t.Errorf("treatment choices = %v; want %v", got, want)
	}
	for _, name := range []string{"code", "name", "treatment", "rate"} {
		if !blank.HasControl(name) {
			t.Errorf("new tax code form has no control named %q", name)
		}
	}

	// A rate that isn't notified: 422, MST-096's message next to the rate.
	resp := w.h.Post(t, kavya, "/m/tax-code/new", url.Values{"code": {"GST10"}, "name": {"GST 10%"}, "treatment": {"Taxable"}, "rate": {"10"}})
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, resp, "rate", msgNotNotified)
	if v, _ := resp.Value("code"); v != "GST10" {
		t.Errorf("422 page code value = %q; want the typed GST10", v)
	}
	if _, ok := w.byCode(t, "GST10"); ok {
		t.Fatal("GST10 saved despite the 422")
	}

	// Taxable at 0: 422, MST-094's message next to the rate.
	resp = w.h.Post(t, kavya, "/m/tax-code/new", url.Values{"code": {"GST0T"}, "name": {"GST zero"}, "treatment": {"Taxable"}, "rate": {"0"}})
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, resp, "rate", msgTaxableZero)

	// A valid create, posting the stored treatment value: 303.
	resp = w.h.Post(t, kavya, "/m/tax-code/new", url.Values{"code": {"GST6"}, "name": {"GST 6%"}, "treatment": {want[0].Value}, "rate": {"6"}})
	gst6, ok := w.byCode(t, "GST6")
	if !ok {
		t.Fatalf("valid POST saved nothing: %d %s", resp.Status, webtest.Squash(resp.Body))
	}
	webtest.WantRedirect(t, resp, path(gst6.ID, ""))
	if gst6.Name != "GST 6%" || gst6.Treatment != taxcode.Taxable || gst6.Rate != types.Percent(6000) {
		t.Errorf("stored GST6 = %+v; want GST 6%%, Taxable, 6%%", gst6)
	}

	page := w.h.Get(t, kavya, path(gst6.ID, ""))
	webtest.WantStatus(t, page, http.StatusOK)
	if v, _ := page.Value("version"); v != ver(gst6.Version) {
		t.Errorf("record page version = %q; want %d", v, gst6.Version)
	}

	// Edit with the page's version.
	resp = w.h.Post(t, kavya, path(gst6.ID, ""), url.Values{"version": {ver(gst6.Version)}, "code": {"GST6"}, "name": {"GST 6 percent"}, "treatment": {"Taxable"}, "rate": {"6"}})
	webtest.WantRedirect(t, resp, path(gst6.ID, ""))
	edited := w.mustGet(t, "GST6")
	if edited.Name != "GST 6 percent" || edited.Version != gst6.Version+1 {
		t.Fatalf("after edit GST6 = %+v; want GST 6 percent at version %d", edited, gst6.Version+1)
	}

	// Stale version: 409, DOC's message, typed values kept, nothing written.
	resp = w.h.Post(t, kavya, path(gst6.ID, ""), url.Values{"version": {ver(gst6.Version)}, "code": {"GST6"}, "name": {"Stale six"}, "treatment": {"Taxable"}, "rate": {"6"}})
	webtest.WantStatus(t, resp, http.StatusConflict)
	webtest.WantText(t, resp, staleGST6)
	if v, _ := resp.Value("name"); v != "Stale six" {
		t.Errorf("409 page name value = %q; want the typed Stale six", v)
	}
	if got := w.mustGet(t, "GST6"); got.Name != "GST 6 percent" {
		t.Errorf("stale save overwrote GST6: %q", got.Name)
	}

	// Disable: hidden by default, tagged Disabled with include_disabled.
	resp = w.h.Post(t, kavya, path(gst6.ID, "/disable"), url.Values{"version": {ver(edited.Version)}})
	webtest.WantRedirect(t, resp, path(gst6.ID, ""))
	off := w.mustGet(t, "GST6")
	if !off.Disabled {
		t.Fatal("GST6 not disabled after POST /disable")
	}
	if _, ok := w.h.Get(t, kavya, "/m/tax-code").RowLink(path(gst6.ID, "")); ok {
		t.Error("disabled GST6 still in the default list")
	}
	text, ok := w.h.Get(t, kavya, "/m/tax-code?include_disabled=true").RowLink(path(gst6.ID, ""))
	if !ok || !strings.Contains(text, "Disabled") {
		t.Errorf("include_disabled row GST6 = %q (found %v); want it tagged Disabled", text, ok)
	}
	webtest.WantText(t, w.h.Get(t, kavya, path(gst6.ID, "")), "Disabled")

	resp = w.h.Post(t, kavya, path(gst6.ID, "/enable"), url.Values{"version": {ver(off.Version)}})
	webtest.WantRedirect(t, resp, path(gst6.ID, ""))
	if w.mustGet(t, "GST6").Disabled {
		t.Fatal("GST6 still disabled after POST /enable")
	}
	text, ok = w.h.Get(t, kavya, "/m/tax-code").RowLink(path(gst6.ID, ""))
	if !ok || strings.Contains(text, "Disabled") {
		t.Errorf("after enable GST6 row = %q (found %v); want it listed, not Disabled", text, ok)
	}
}

func TestScreens_TaxCodeReadOnlyForbidden(t *testing.T) {
	w := newWorld(t)
	priya := w.h.Env.Priya
	gst18 := w.mustGet(t, "GST18")
	deny := func(r auth.Right) string { return (&auth.DeniedError{Right: r, Label: "Tax Code"}).Error() }

	list := w.h.Get(t, priya, "/m/tax-code")
	webtest.WantStatus(t, list, http.StatusOK)
	if _, ok := list.RowLink(path(gst18.ID, "")); !ok {
		t.Error("Priya's list lacks GST18")
	}
	if _, ok := list.RowLink("/m/tax-code/new"); ok {
		t.Error("Priya's list links to /m/tax-code/new without the create right")
	}

	resp := w.h.Post(t, priya, "/m/tax-code/new", url.Values{"code": {"GST6"}, "name": {"GST 6%"}, "treatment": {"Taxable"}, "rate": {"6"}})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightCreate))
	if _, ok := w.byCode(t, "GST6"); ok {
		t.Error("Priya's create saved GST6")
	}

	resp = w.h.Post(t, priya, path(gst18.ID, ""), url.Values{"version": {ver(gst18.Version)}, "code": {"GST18"}, "name": {"Renamed by Priya"}, "treatment": {"Taxable"}, "rate": {"18"}})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightWrite))

	resp = w.h.Post(t, priya, path(gst18.ID, "/disable"), url.Values{"version": {ver(gst18.Version)}})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightWrite))

	resp = w.h.Post(t, priya, path(gst18.ID, "/delete"), url.Values{"version": {ver(gst18.Version)}})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightDelete))

	if got := w.mustGet(t, "GST18"); got.Name != gst18.Name || got.Disabled || got.Version != gst18.Version {
		t.Errorf("GST18 after Priya's refused writes = %+v; want unchanged %+v", got, gst18)
	}
}

func TestScreens_TaxCodePicker(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	gst12, gst18, exempt := w.mustGet(t, "GST12"), w.mustGet(t, "GST18"), w.mustGet(t, "EXEMPT")

	rows := w.h.GetHTMX(t, kavya, "/pick/Tax%20Code?q=Exempt")
	webtest.WantStatus(t, rows, http.StatusOK)
	got := rows.PickRows()
	if len(got) != 1 || got[0].Value != strconv.FormatInt(exempt.ID, 10) || !strings.Contains(got[0].Text, "EXEMPT Exempted") {
		t.Errorf("pick?q=Exempt = %v; want one row %d \"EXEMPT Exempted\"", got, exempt.ID)
	}

	// q=GST1 matches the codes GST12 and GST18, both shown "{Code} {Name}".
	got = w.h.GetHTMX(t, kavya, "/pick/Tax%20Code?q=GST1").PickRows()
	if len(got) != 2 || got[0].Value != strconv.FormatInt(gst12.ID, 10) || !strings.Contains(got[0].Text, "GST12 GST 12%") ||
		got[1].Value != strconv.FormatInt(gst18.ID, 10) || !strings.Contains(got[1].Text, "GST18 GST 18%") {
		t.Errorf("pick?q=GST1 = %v; want GST12 \"GST 12%%\" then GST18 \"GST 18%%\"", got)
	}

	if err := w.svc.SetDisabled(context.Background(), kavya, gst12.ID, gst12.Version, true); err != nil {
		t.Fatal(err)
	}
	got = w.h.GetHTMX(t, kavya, "/pick/Tax%20Code?q=GST1").PickRows()
	if len(got) != 1 || got[0].Value != strconv.FormatInt(gst18.ID, 10) {
		t.Errorf("pick?q=GST1 after disabling GST12 = %v; want only GST18", got)
	}
}

func TestScreens_TaxCodeDelete(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	saved, err := w.svc.Create(context.Background(), kavya, taxcode.Input{Code: "GST6", Name: "GST 6%", Treatment: taxcode.Taxable, Rate: 6000})
	if err != nil {
		t.Fatal(err)
	}

	resp := w.h.Post(t, kavya, path(saved.ID, "/delete"), url.Values{"version": {ver(saved.Version)}})
	webtest.WantRedirect(t, resp, "/m/tax-code")
	if _, ok := w.byCode(t, "GST6"); ok {
		t.Fatal("GST6 still exists after POST /delete")
	}
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		_, err = w.svc.Get(ctx, tx, saved.ID)
		return nil
	})
	if !errors.Is(err, doctype.ErrNotFound) {
		t.Fatalf("Get after delete = %v; want doctype.ErrNotFound", err)
	}
	page := w.h.Get(t, kavya, path(saved.ID, ""))
	webtest.WantStatus(t, page, http.StatusNotFound)
	webtest.WantText(t, page, doctype.ErrNotFound.Error()) // CD-76
}

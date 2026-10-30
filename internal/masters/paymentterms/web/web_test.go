package paymenttermsweb_test

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
	"github.com/vardhan/internaltools/internal/masters/paymentterms"
	paymenttermsweb "github.com/vardhan/internaltools/internal/masters/paymentterms/web"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Screens under test (MST Task 22): /m/payment-terms list, /new, /{id},
// POST /{id} with version, /{id}/disable, /{id}/enable, /{id}/delete and the
// picker /pick/Payment%20Terms?q= (CD-195). The rows use the line grid and post as
// lines[N].portion and lines[N].days (doctype.FieldError.Path).
const (
	msgDays1   = "Row 1: Days must be a whole number, 0 or more."        // MST-125
	msgTotal50 = "Payment portions add up to 50%. They must total 100%." // MST-124
	staleHalf  = "Someone else changed Payment Terms HALF after you opened it. Reload it and make your changes again."
	pct50      = types.Percent(50000)
	pct100     = types.Percent(100000)
)

type world struct {
	h   *webtest.Harness
	svc *paymentterms.Service
}

func newWorld(t *testing.T) *world {
	t.Helper()
	h := webtest.New(t, func(k mstkit.Env) func(chi.Router) {
		return paymenttermsweb.Routes(paymentterms.New(k), k)
	})
	return &world{h: h, svc: paymentterms.New(h.Env.Kit)}
}

func (w *world) read(t *testing.T, fn func(ctx context.Context, tx *db.Tx) error) {
	t.Helper()
	ctx := context.Background()
	if err := w.h.Env.DB.Read(ctx, func(tx *db.Tx) error { return fn(ctx, tx) }); err != nil {
		t.Fatal(err)
	}
}

func (w *world) byCode(t *testing.T, code string) (paymentterms.Terms, bool) {
	t.Helper()
	var v paymentterms.Terms
	var ok bool
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		var err error
		v, ok, err = w.svc.ByCode(ctx, tx, code)
		return err
	})
	return v, ok
}

func (w *world) mustGet(t *testing.T, code string) paymentterms.Terms {
	t.Helper()
	v, ok := w.byCode(t, code)
	if !ok {
		t.Fatalf("payment terms %s not found", code)
	}
	return v
}

// seed creates NET30 (100% at 30 days) and ADV50 (50% now, 50% at 30 days).
func (w *world) seed(t *testing.T) (net30, adv50 paymentterms.Terms) {
	t.Helper()
	ctx := context.Background()
	for _, in := range []paymentterms.Input{
		{Code: "NET30", Name: "Net 30 days", Rows: []paymentterms.Row{{Portion: pct100, Days: 30}}},
		{Code: "ADV50", Name: "Half advance, half in 30 days", Rows: []paymentterms.Row{{Portion: pct50, Days: 0}, {Portion: pct50, Days: 30}}},
	} {
		if _, err := w.svc.Create(ctx, w.h.Env.Kavya, in); err != nil {
			t.Fatalf("create %s: %v", in.Code, err)
		}
	}
	return w.mustGet(t, "NET30"), w.mustGet(t, "ADV50")
}

func path(id int64, suffix string) string {
	return "/m/payment-terms/" + strconv.FormatInt(id, 10) + suffix
}

func ver(v int) string { return strconv.Itoa(v) }

// halfForm is HALF: 50% at 0 days and 50% at 30 days.
func halfForm(name string) url.Values {
	return url.Values{
		"code": {"HALF"}, "name": {name},
		"lines[1].portion": {"50"}, "lines[1].days": {"0"},
		"lines[2].portion": {"50"}, "lines[2].days": {"30"},
	}
}

func TestScreens_PaymentTermsCreateEditDisable(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	net30, adv50 := w.seed(t)

	list := w.h.Get(t, kavya, "/m/payment-terms")
	webtest.WantStatus(t, list, http.StatusOK)
	for _, v := range []paymentterms.Terms{net30, adv50} {
		text, ok := list.RowLink(path(v.ID, ""))
		if !ok || !strings.Contains(text, v.Code+" "+v.Name) {
			t.Errorf("list row %s = %q (found %v); want a link containing %q", v.Code, text, ok, v.Code+" "+v.Name)
		}
	}
	if _, ok := list.RowLink("/m/payment-terms/new"); !ok {
		t.Error("list has no link to /m/payment-terms/new for Kavya (create right)")
	}

	blank := w.h.Get(t, kavya, "/m/payment-terms/new")
	webtest.WantStatus(t, blank, http.StatusOK)
	for _, name := range []string{"code", "name", "lines[1].portion", "lines[1].days"} {
		if !blank.HasControl(name) {
			t.Errorf("new payment terms form has no control named %q", name)
		}
	}

	// Row 1 with negative days: 422 with MST-125's message next to that
	// row's days cell; the typed rows kept; nothing saved.
	bad := halfForm("Half now, half in 30 days")
	bad.Set("lines[1].days", "-30")
	resp := w.h.Post(t, kavya, "/m/payment-terms/new", bad)
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, resp, "lines[1].days", msgDays1)
	if v, _ := resp.Value("lines[2].days"); v != "30" {
		t.Errorf("422 page lines[2].days = %q; want the typed 30", v)
	}
	if _, ok := w.byCode(t, "HALF"); ok {
		t.Fatal("HALF saved despite the 422")
	}

	// Portions that don't total 100%: 422 with MST-124's message.
	resp = w.h.Post(t, kavya, "/m/payment-terms/new", url.Values{
		"code": {"HALF"}, "name": {"Half"}, "lines[1].portion": {"50"}, "lines[1].days": {"30"},
	})
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantText(t, resp, msgTotal50)
	if _, ok := w.byCode(t, "HALF"); ok {
		t.Fatal("HALF saved despite the 422")
	}

	// A valid create: 303 to the record, rows stored in order.
	resp = w.h.Post(t, kavya, "/m/payment-terms/new", halfForm("Half now, half in 30 days"))
	half, ok := w.byCode(t, "HALF")
	if !ok {
		t.Fatalf("valid POST saved nothing: %d %s", resp.Status, webtest.Squash(resp.Body))
	}
	webtest.WantRedirect(t, resp, path(half.ID, ""))
	wantRows := []paymentterms.Row{{Portion: pct50, Days: 0}, {Portion: pct50, Days: 30}}
	if half.Name != "Half now, half in 30 days" || !slices.Equal(half.Rows, wantRows) {
		t.Errorf("stored HALF = %+v; want rows %v", half, wantRows)
	}

	page := w.h.Get(t, kavya, path(half.ID, ""))
	webtest.WantStatus(t, page, http.StatusOK)
	if v, _ := page.Value("version"); v != ver(half.Version) {
		t.Errorf("record page version = %q; want %d", v, half.Version)
	}
	for name, want := range map[string]string{"lines[1].days": "0", "lines[2].days": "30"} {
		if got, _ := page.Value(name); got != want {
			t.Errorf("record page %s = %q; want %q", name, got, want)
		}
	}

	// Edit with the page's version: rows replaced (one row of 100% at 45).
	resp = w.h.Post(t, kavya, path(half.ID, ""), url.Values{
		"version": {ver(half.Version)}, "code": {"HALF"}, "name": {"Net 45 days"},
		"lines[1].portion": {"100"}, "lines[1].days": {"45"},
	})
	webtest.WantRedirect(t, resp, path(half.ID, ""))
	edited := w.mustGet(t, "HALF")
	if edited.Name != "Net 45 days" || edited.Version != half.Version+1 ||
		!slices.Equal(edited.Rows, []paymentterms.Row{{Portion: pct100, Days: 45}}) {
		t.Fatalf("after edit HALF = %+v; want Net 45 days, [100%% at 45], version %d", edited, half.Version+1)
	}

	// Stale version: 409, DOC's message, typed values kept, nothing written.
	stale := halfForm("Stale terms")
	stale.Set("version", ver(half.Version))
	resp = w.h.Post(t, kavya, path(half.ID, ""), stale)
	webtest.WantStatus(t, resp, http.StatusConflict)
	webtest.WantText(t, resp, staleHalf)
	if v, _ := resp.Value("name"); v != "Stale terms" {
		t.Errorf("409 page name value = %q; want the typed Stale terms", v)
	}
	if got := w.mustGet(t, "HALF"); got.Name != "Net 45 days" {
		t.Errorf("stale save overwrote HALF: %q", got.Name)
	}

	// Disable: hidden by default, tagged Disabled with include_disabled.
	resp = w.h.Post(t, kavya, path(half.ID, "/disable"), url.Values{"version": {ver(edited.Version)}})
	webtest.WantRedirect(t, resp, path(half.ID, ""))
	off := w.mustGet(t, "HALF")
	if !off.Disabled {
		t.Fatal("HALF not disabled after POST /disable")
	}
	if _, ok := w.h.Get(t, kavya, "/m/payment-terms").RowLink(path(half.ID, "")); ok {
		t.Error("disabled HALF still in the default list")
	}
	text, ok := w.h.Get(t, kavya, "/m/payment-terms?include_disabled=true").RowLink(path(half.ID, ""))
	if !ok || !strings.Contains(text, "Disabled") {
		t.Errorf("include_disabled row HALF = %q (found %v); want it tagged Disabled", text, ok)
	}
	webtest.WantText(t, w.h.Get(t, kavya, path(half.ID, "")), "Disabled")

	resp = w.h.Post(t, kavya, path(half.ID, "/enable"), url.Values{"version": {ver(off.Version)}})
	webtest.WantRedirect(t, resp, path(half.ID, ""))
	if w.mustGet(t, "HALF").Disabled {
		t.Fatal("HALF still disabled after POST /enable")
	}
	text, ok = w.h.Get(t, kavya, "/m/payment-terms").RowLink(path(half.ID, ""))
	if !ok || strings.Contains(text, "Disabled") {
		t.Errorf("after enable HALF row = %q (found %v); want it listed, not Disabled", text, ok)
	}
}

func TestScreens_PaymentTermsReadOnlyForbidden(t *testing.T) {
	w := newWorld(t)
	priya := w.h.Env.Priya
	net30, _ := w.seed(t)
	deny := func(r auth.Right) string { return (&auth.DeniedError{Right: r, Label: "Payment Terms"}).Error() }

	list := w.h.Get(t, priya, "/m/payment-terms")
	webtest.WantStatus(t, list, http.StatusOK)
	if _, ok := list.RowLink(path(net30.ID, "")); !ok {
		t.Error("Priya's list lacks NET30")
	}
	if _, ok := list.RowLink("/m/payment-terms/new"); ok {
		t.Error("Priya's list links to /m/payment-terms/new without the create right")
	}

	resp := w.h.Post(t, priya, "/m/payment-terms/new", halfForm("Half"))
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightCreate))
	if _, ok := w.byCode(t, "HALF"); ok {
		t.Error("Priya's create saved HALF")
	}

	resp = w.h.Post(t, priya, path(net30.ID, ""), url.Values{
		"version": {ver(net30.Version)}, "code": {"NET30"}, "name": {"Renamed by Priya"},
		"lines[1].portion": {"100"}, "lines[1].days": {"30"},
	})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightWrite))

	resp = w.h.Post(t, priya, path(net30.ID, "/disable"), url.Values{"version": {ver(net30.Version)}})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightWrite))

	resp = w.h.Post(t, priya, path(net30.ID, "/delete"), url.Values{"version": {ver(net30.Version)}})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightDelete))

	if got := w.mustGet(t, "NET30"); got.Name != net30.Name || got.Disabled || got.Version != net30.Version {
		t.Errorf("NET30 after Priya's refused writes = %+v; want unchanged %+v", got, net30)
	}
}

func TestScreens_PaymentTermsPicker(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	net30, adv50 := w.seed(t)

	rows := w.h.GetHTMX(t, kavya, "/pick/Payment%20Terms?q=Net")
	webtest.WantStatus(t, rows, http.StatusOK)
	got := rows.PickRows()
	if len(got) != 1 || got[0].Value != strconv.FormatInt(net30.ID, 10) || !strings.Contains(got[0].Text, "NET30 Net 30 days") {
		t.Errorf("pick?q=Net = %v; want one row %d \"NET30 Net 30 days\"", got, net30.ID)
	}
	got = w.h.GetHTMX(t, kavya, "/pick/Payment%20Terms?q=advance").PickRows()
	if len(got) != 1 || got[0].Value != strconv.FormatInt(adv50.ID, 10) || !strings.Contains(got[0].Text, "ADV50 Half advance, half in 30 days") {
		t.Errorf("pick?q=advance = %v; want one row %d ADV50", got, adv50.ID)
	}

	if err := w.svc.SetDisabled(context.Background(), kavya, net30.ID, net30.Version, true); err != nil {
		t.Fatal(err)
	}
	if got := w.h.GetHTMX(t, kavya, "/pick/Payment%20Terms?q=Net").PickRows(); len(got) != 0 {
		t.Errorf("pick?q=Net after disabling NET30 = %v; want no rows", got)
	}
}

func TestScreens_PaymentTermsDelete(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	_, adv50 := w.seed(t)

	// An unused record and its rows are deleted (CD-161 CASCADE rows).
	resp := w.h.Post(t, kavya, path(adv50.ID, "/delete"), url.Values{"version": {ver(adv50.Version)}})
	webtest.WantRedirect(t, resp, "/m/payment-terms")
	if _, ok := w.byCode(t, "ADV50"); ok {
		t.Fatal("ADV50 still exists after POST /delete")
	}
	var err error
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		_, err = w.svc.Get(ctx, tx, adv50.ID)
		return nil
	})
	if !errors.Is(err, doctype.ErrNotFound) {
		t.Fatalf("Get after delete = %v; want doctype.ErrNotFound", err)
	}
	page := w.h.Get(t, kavya, path(adv50.ID, ""))
	webtest.WantStatus(t, page, http.StatusNotFound)
	webtest.WantText(t, page, doctype.ErrNotFound.Error()) // CD-76
}

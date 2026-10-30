package expensetypeweb_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/expensetype"
	expensetypeweb "github.com/vardhan/internaltools/internal/masters/expensetype/web"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/taxcode"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Screens under test (MST Task 22): /m/expense-type list, /new, /{id},
// POST /{id} with version, /{id}/disable, /{id}/enable, /{id}/delete and the
// picker /pick/Expense%20Type?q= (CD-195). Field names are the DocType's Field names.
const (
	msgSAC       = "SAC code must be 6 digits starting with 99." // MST-122
	staleLodging = "Someone else changed Expense Type LODGING after you opened it. Reload it and make your changes again."
)

type world struct {
	h   *webtest.Harness
	svc *expensetype.Service
}

func newWorld(t *testing.T) *world {
	t.Helper()
	h := webtest.New(t, func(k mstkit.Env) func(chi.Router) {
		return expensetypeweb.Routes(expensetype.New(k), k)
	})
	return &world{h: h, svc: expensetype.New(h.Env.Kit)}
}

func (w *world) read(t *testing.T, fn func(ctx context.Context, tx *db.Tx) error) {
	t.Helper()
	ctx := context.Background()
	if err := w.h.Env.DB.Read(ctx, func(tx *db.Tx) error { return fn(ctx, tx) }); err != nil {
		t.Fatal(err)
	}
}

func (w *world) byCode(t *testing.T, code string) (expensetype.ExpenseType, bool) {
	t.Helper()
	var v expensetype.ExpenseType
	var ok bool
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		var err error
		v, ok, err = w.svc.ByCode(ctx, tx, code)
		return err
	})
	return v, ok
}

func (w *world) mustGet(t *testing.T, code string) expensetype.ExpenseType {
	t.Helper()
	v, ok := w.byCode(t, code)
	if !ok {
		t.Fatalf("expense type %s not found", code)
	}
	return v
}

func (w *world) gst18(t *testing.T) int64 {
	t.Helper()
	var tc taxcode.TaxCode
	var ok bool
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		var err error
		tc, ok, err = taxcode.New(w.h.Env.Kit).ByCode(ctx, tx, "GST18")
		return err
	})
	if !ok {
		t.Fatal("seeded tax code GST18 missing")
	}
	return tc.ID
}

// seed creates the fixture expense types TRAVEL "Travel" and FOOD "Food" (M9).
func (w *world) seed(t *testing.T) (travel, food expensetype.ExpenseType) {
	t.Helper()
	ctx := context.Background()
	for _, in := range []expensetype.Input{{Code: "TRAVEL", Name: "Travel"}, {Code: "FOOD", Name: "Food"}} {
		if _, err := w.svc.Create(ctx, w.h.Env.Kavya, in); err != nil {
			t.Fatalf("create %s: %v", in.Code, err)
		}
	}
	return w.mustGet(t, "TRAVEL"), w.mustGet(t, "FOOD")
}

func path(id int64, suffix string) string {
	return "/m/expense-type/" + strconv.FormatInt(id, 10) + suffix
}

func ver(v int) string { return strconv.Itoa(v) }

func TestScreens_ExpenseTypeCreateEditDisable(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	travel, food := w.seed(t)
	gst18 := strconv.FormatInt(w.gst18(t), 10)

	list := w.h.Get(t, kavya, "/m/expense-type")
	webtest.WantStatus(t, list, http.StatusOK)
	for _, v := range []expensetype.ExpenseType{travel, food} {
		text, ok := list.RowLink(path(v.ID, ""))
		if !ok || !strings.Contains(text, v.Code+" "+v.Name) {
			t.Errorf("list row %s = %q (found %v); want a link containing %q", v.Code, text, ok, v.Code+" "+v.Name)
		}
	}
	if _, ok := list.RowLink("/m/expense-type/new"); !ok {
		t.Error("list has no link to /m/expense-type/new for Kavya (create right)")
	}

	blank := w.h.Get(t, kavya, "/m/expense-type/new")
	webtest.WantStatus(t, blank, http.StatusOK)
	for _, name := range []string{"code", "name", "default_hsn_sac", "default_tax_code_id"} {
		if !blank.HasControl(name) {
			t.Errorf("new expense type form has no control named %q", name)
		}
	}

	// A 4-digit SAC: 422 with MST-122's message next to the field.
	resp := w.h.Post(t, kavya, "/m/expense-type/new", url.Values{"code": {"LODGING"}, "name": {"Lodging"}, "default_hsn_sac": {"9964"}})
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, resp, "default_hsn_sac", msgSAC)
	if v, _ := resp.Value("code"); v != "LODGING" {
		t.Errorf("422 page code value = %q; want the typed LODGING", v)
	}
	if _, ok := w.byCode(t, "LODGING"); ok {
		t.Fatal("LODGING saved despite the 422")
	}

	// A valid create with a default SAC and tax code: 303 to the record.
	resp = w.h.Post(t, kavya, "/m/expense-type/new", url.Values{
		"code": {"LODGING"}, "name": {"Lodging"}, "default_hsn_sac": {"996311"}, "default_tax_code_id": {gst18},
	})
	lodging, ok := w.byCode(t, "LODGING")
	if !ok {
		t.Fatalf("valid POST saved nothing: %d %s", resp.Status, webtest.Squash(resp.Body))
	}
	webtest.WantRedirect(t, resp, path(lodging.ID, ""))
	if lodging.Name != "Lodging" || lodging.DefaultHSNSAC != "996311" || strconv.FormatInt(lodging.DefaultTaxCodeID, 10) != gst18 {
		t.Errorf("stored LODGING = %+v; want Lodging, 996311, GST18", lodging)
	}

	// The record page posts its version and shows the tax code as
	// "{Code} {Name}" (CD-68).
	page := w.h.Get(t, kavya, path(lodging.ID, ""))
	webtest.WantStatus(t, page, http.StatusOK)
	if v, _ := page.Value("version"); v != ver(lodging.Version) {
		t.Errorf("record page version = %q; want %d", v, lodging.Version)
	}
	webtest.WantText(t, page, "GST18 GST 18%")

	resp = w.h.Post(t, kavya, path(lodging.ID, ""), url.Values{
		"version": {ver(lodging.Version)}, "code": {"LODGING"}, "name": {"Hotel stay"}, "default_hsn_sac": {"996311"}, "default_tax_code_id": {gst18},
	})
	webtest.WantRedirect(t, resp, path(lodging.ID, ""))
	edited := w.mustGet(t, "LODGING")
	if edited.Name != "Hotel stay" || edited.Version != lodging.Version+1 {
		t.Fatalf("after edit LODGING = %+v; want Hotel stay at version %d", edited, lodging.Version+1)
	}

	// Stale version: 409, DOC's message, typed values kept, nothing written.
	resp = w.h.Post(t, kavya, path(lodging.ID, ""), url.Values{
		"version": {ver(lodging.Version)}, "code": {"LODGING"}, "name": {"Stale stay"}, "default_hsn_sac": {"996311"}, "default_tax_code_id": {gst18},
	})
	webtest.WantStatus(t, resp, http.StatusConflict)
	webtest.WantText(t, resp, staleLodging)
	if v, _ := resp.Value("name"); v != "Stale stay" {
		t.Errorf("409 page name value = %q; want the typed Stale stay", v)
	}
	if got := w.mustGet(t, "LODGING"); got.Name != "Hotel stay" {
		t.Errorf("stale save overwrote LODGING: %q", got.Name)
	}

	// Disable: hidden by default, tagged Disabled with include_disabled.
	resp = w.h.Post(t, kavya, path(lodging.ID, "/disable"), url.Values{"version": {ver(edited.Version)}})
	webtest.WantRedirect(t, resp, path(lodging.ID, ""))
	off := w.mustGet(t, "LODGING")
	if !off.Disabled {
		t.Fatal("LODGING not disabled after POST /disable")
	}
	if _, ok := w.h.Get(t, kavya, "/m/expense-type").RowLink(path(lodging.ID, "")); ok {
		t.Error("disabled LODGING still in the default list")
	}
	text, ok := w.h.Get(t, kavya, "/m/expense-type?include_disabled=true").RowLink(path(lodging.ID, ""))
	if !ok || !strings.Contains(text, "Disabled") {
		t.Errorf("include_disabled row LODGING = %q (found %v); want it tagged Disabled", text, ok)
	}
	webtest.WantText(t, w.h.Get(t, kavya, path(lodging.ID, "")), "Disabled")

	resp = w.h.Post(t, kavya, path(lodging.ID, "/enable"), url.Values{"version": {ver(off.Version)}})
	webtest.WantRedirect(t, resp, path(lodging.ID, ""))
	if w.mustGet(t, "LODGING").Disabled {
		t.Fatal("LODGING still disabled after POST /enable")
	}
	text, ok = w.h.Get(t, kavya, "/m/expense-type").RowLink(path(lodging.ID, ""))
	if !ok || strings.Contains(text, "Disabled") {
		t.Errorf("after enable LODGING row = %q (found %v); want it listed, not Disabled", text, ok)
	}
}

func TestScreens_ExpenseTypeReadOnlyForbidden(t *testing.T) {
	w := newWorld(t)
	priya := w.h.Env.Priya
	travel, _ := w.seed(t)
	deny := func(r auth.Right) string { return (&auth.DeniedError{Right: r, Label: "Expense Type"}).Error() }

	list := w.h.Get(t, priya, "/m/expense-type")
	webtest.WantStatus(t, list, http.StatusOK)
	if _, ok := list.RowLink(path(travel.ID, "")); !ok {
		t.Error("Priya's list lacks TRAVEL")
	}
	if _, ok := list.RowLink("/m/expense-type/new"); ok {
		t.Error("Priya's list links to /m/expense-type/new without the create right")
	}

	resp := w.h.Post(t, priya, "/m/expense-type/new", url.Values{"code": {"FUEL"}, "name": {"Fuel"}})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightCreate))
	if _, ok := w.byCode(t, "FUEL"); ok {
		t.Error("Priya's create saved FUEL")
	}

	resp = w.h.Post(t, priya, path(travel.ID, ""), url.Values{"version": {ver(travel.Version)}, "code": {"TRAVEL"}, "name": {"Renamed by Priya"}})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightWrite))

	resp = w.h.Post(t, priya, path(travel.ID, "/disable"), url.Values{"version": {ver(travel.Version)}})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightWrite))

	resp = w.h.Post(t, priya, path(travel.ID, "/delete"), url.Values{"version": {ver(travel.Version)}})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightDelete))

	if got := w.mustGet(t, "TRAVEL"); got.Name != travel.Name || got.Disabled || got.Version != travel.Version {
		t.Errorf("TRAVEL after Priya's refused writes = %+v; want unchanged %+v", got, travel)
	}
}

func TestScreens_ExpenseTypePicker(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	travel, food := w.seed(t)

	rows := w.h.GetHTMX(t, kavya, "/pick/Expense%20Type?q=Trav")
	webtest.WantStatus(t, rows, http.StatusOK)
	got := rows.PickRows()
	if len(got) != 1 || got[0].Value != strconv.FormatInt(travel.ID, 10) || !strings.Contains(got[0].Text, "TRAVEL Travel") {
		t.Errorf("pick?q=Trav = %v; want one row %d \"TRAVEL Travel\"", got, travel.ID)
	}
	got = w.h.GetHTMX(t, kavya, "/pick/Expense%20Type?q=Food").PickRows()
	if len(got) != 1 || got[0].Value != strconv.FormatInt(food.ID, 10) || !strings.Contains(got[0].Text, "FOOD Food") {
		t.Errorf("pick?q=Food = %v; want one row %d \"FOOD Food\"", got, food.ID)
	}

	if err := w.svc.SetDisabled(context.Background(), kavya, food.ID, food.Version, true); err != nil {
		t.Fatal(err)
	}
	if got := w.h.GetHTMX(t, kavya, "/pick/Expense%20Type?q=Food").PickRows(); len(got) != 0 {
		t.Errorf("pick?q=Food after disabling FOOD = %v; want no rows", got)
	}
}

func TestScreens_ExpenseTypeDelete(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	_, food := w.seed(t)

	resp := w.h.Post(t, kavya, path(food.ID, "/delete"), url.Values{"version": {ver(food.Version)}})
	webtest.WantRedirect(t, resp, "/m/expense-type")
	if _, ok := w.byCode(t, "FOOD"); ok {
		t.Fatal("FOOD still exists after POST /delete")
	}
	var err error
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		_, err = w.svc.Get(ctx, tx, food.ID)
		return nil
	})
	if !errors.Is(err, doctype.ErrNotFound) {
		t.Fatalf("Get after delete = %v; want doctype.ErrNotFound", err)
	}
	page := w.h.Get(t, kavya, path(food.ID, ""))
	webtest.WantStatus(t, page, http.StatusNotFound)
	webtest.WantText(t, page, doctype.ErrNotFound.Error()) // CD-76
}

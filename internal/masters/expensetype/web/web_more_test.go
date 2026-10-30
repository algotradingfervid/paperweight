package expensetypeweb_test

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/expensetype"
	"github.com/vardhan/internaltools/internal/masters/taxcode"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// No case ID: the screen behaviour Task 22 adds beyond its named tests.

// msgTaxNotFound is expensetype's refusal of a tax code that doesn't exist.
const msgTaxNotFound = "Tax code not found. Pick one from the list."

// msgTaxDisabled is expensetype's refusal of the disabled GST18.
const msgTaxDisabled = "Tax code GST18 is disabled. Pick another tax code."

// pickerControl is the control a Link field's error is tied to: the picker's
// search input. Its hidden default_tax_code_id input has no message region.
const pickerControl = "q"

// lodging creates LODGING with a default SAC and tax code through the service.
func (w *world) lodging(t *testing.T) expensetype.ExpenseType {
	t.Helper()
	in := expensetype.Input{Code: "LODGING", Name: "Lodging", DefaultHSNSAC: "996311", DefaultTaxCodeID: w.gst18(t)}
	if _, err := w.svc.Create(context.Background(), w.h.Env.Kavya, in); err != nil {
		t.Fatal(err)
	}
	return w.mustGet(t, "LODGING")
}

// A blank tax code is no tax code, on create and on update.
func TestScreens_ExpenseTypeBlankLinkIsNone(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya

	resp := w.h.Post(t, kavya, "/m/expense-type/new", url.Values{"code": {"FUEL"}, "name": {"Fuel"}, "default_tax_code_id": {""}})
	fuel, ok := w.byCode(t, "FUEL")
	if !ok {
		t.Fatalf("blank tax code on create saved nothing: %d %s", resp.Status, webtest.Squash(resp.Body))
	}
	webtest.WantRedirect(t, resp, path(fuel.ID, ""))
	if fuel.DefaultTaxCodeID != 0 {
		t.Errorf("FUEL default tax code = %d; want 0", fuel.DefaultTaxCodeID)
	}

	lodging := w.lodging(t)
	resp = w.h.Post(t, kavya, path(lodging.ID, ""), url.Values{
		"version": {ver(lodging.Version)}, "code": {"LODGING"}, "name": {"Lodging"}, "default_tax_code_id": {""},
	})
	webtest.WantRedirect(t, resp, path(lodging.ID, ""))
	got := w.mustGet(t, "LODGING")
	if got.DefaultTaxCodeID != 0 || got.DefaultHSNSAC != "" {
		t.Errorf("LODGING after clearing = %+v; want no default tax code or HSN/SAC", got)
	}

	// Stored 0 is empty on the form, never "0" or a chosen tax code.
	page := w.h.Get(t, kavya, path(fuel.ID, ""))
	webtest.WantStatus(t, page, http.StatusOK)
	if v, ok := page.Value("default_tax_code_id"); !ok || v != "" {
		t.Errorf("form tax code of FUEL = %q (found %v); want empty", v, ok)
	}
	webtest.WantStatus(t, w.h.Get(t, w.h.Env.Priya, path(fuel.ID, "")), http.StatusOK)
}

// A tax code that isn't a number gets the message of one that doesn't exist,
// on its field, next to the other fields' errors; strconv's text never shows
// and the service isn't reached (CD-37).
func TestScreens_ExpenseTypeBadLink(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	lodging := w.lodging(t)

	refused := func(t *testing.T, resp webtest.Response) {
		t.Helper()
		webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
		webtest.WantText(t, resp, msgTaxNotFound)
		webtest.WantNoText(t, resp, "strconv")
		webtest.WantNoText(t, resp, "invalid syntax")
		webtest.WantNoText(t, resp, "out of range")
		if v, _ := resp.Doc.Find(`input[role="combobox"]`).Attr("aria-invalid"); v != "true" {
			t.Errorf("tax code picker aria-invalid = %q; want true", v)
		}
	}

	for _, bad := range []string{"abc", "12x", "99999999999999999999"} {
		form := url.Values{"code": {"NEW"}, "name": {"New"}, "default_hsn_sac": {"9964"}, "default_tax_code_id": {bad}}
		resp := w.h.Post(t, kavya, "/m/expense-type/new", form)
		refused(t, resp)
		webtest.WantFieldError(t, resp, "default_hsn_sac", msgSAC) // Validate's
		if v, _ := resp.Value("name"); v != "New" {
			t.Errorf("422 page name value = %q; want the typed New", v)
		}
		if _, ok := w.byCode(t, "NEW"); ok {
			t.Fatalf("tax code %q saved NEW", bad)
		}

		refused(t, w.h.Post(t, kavya, path(lodging.ID, ""), url.Values{
			"version": {ver(lodging.Version)}, "code": {"LODGING"}, "name": {"Renamed"}, "default_tax_code_id": {bad},
		}))
		if got := w.mustGet(t, "LODGING"); got != lodging {
			t.Errorf("LODGING after tax code %q = %+v; want unchanged %+v", bad, got, lodging)
		}
	}

	// The service's own refusal of a number that is no tax code reads the same.
	resp := w.h.Post(t, kavya, "/m/expense-type/new", url.Values{"code": {"NEW"}, "name": {"New"}, "default_tax_code_id": {"999999"}})
	refused(t, resp)

	// The form's right goes first: no 422 for a user who can't save at all.
	resp = w.h.Post(t, w.h.Env.Priya, "/m/expense-type/new", url.Values{"code": {"NEW"}, "name": {"New"}, "default_tax_code_id": {"abc"}})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	resp = w.h.Post(t, w.h.Env.Priya, path(lodging.ID, ""), url.Values{
		"version": {ver(lodging.Version)}, "code": {"LODGING"}, "name": {"Renamed"}, "default_tax_code_id": {"abc"},
	})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, (&auth.DeniedError{Right: auth.RightWrite, Label: "Expense Type"}).Error())
}

// The service's tax code check runs on create and on update, and its field
// errors show on the form: a disabled tax code, and on update one that
// doesn't exist.
func TestScreens_ExpenseTypeDisabledTaxCode(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	lodging := w.lodging(t)
	tax := taxcode.New(w.h.Env.Kit)
	var gst18 taxcode.TaxCode
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		var err error
		gst18, err = tax.Get(ctx, tx, lodging.DefaultTaxCodeID)
		return err
	})
	if err := tax.SetDisabled(context.Background(), kavya, gst18.ID, gst18.Version, true); err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(gst18.ID, 10)

	resp := w.h.Post(t, kavya, "/m/expense-type/new", url.Values{"code": {"NEW"}, "name": {"New"}, "default_tax_code_id": {id}})
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, resp, pickerControl, msgTaxDisabled)
	if _, ok := w.byCode(t, "NEW"); ok {
		t.Fatal("disabled tax code saved NEW")
	}

	form := url.Values{"version": {ver(lodging.Version)}, "code": {"LODGING"}, "name": {"Renamed"}, "default_tax_code_id": {id}}
	resp = w.h.Post(t, kavya, path(lodging.ID, ""), form)
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, resp, pickerControl, msgTaxDisabled)

	form.Set("default_tax_code_id", "999999")
	resp = w.h.Post(t, kavya, path(lodging.ID, ""), form)
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, resp, pickerControl, msgTaxNotFound)

	if got := w.mustGet(t, "LODGING"); got != lodging {
		t.Errorf("LODGING after refused saves = %+v; want unchanged %+v", got, lodging)
	}
}

// The record page's form holds every stored value and its title is
// "{Code} {Name}" (CD-68); a reader without write sees the same as text.
func TestScreens_ExpenseTypeRecordValues(t *testing.T) {
	w := newWorld(t)
	lodging := w.lodging(t)

	page := w.h.Get(t, w.h.Env.Kavya, path(lodging.ID, ""))
	webtest.WantStatus(t, page, http.StatusOK)
	want := map[string]string{
		"code": "LODGING", "name": "Lodging", "default_hsn_sac": "996311",
		"default_tax_code_id": strconv.FormatInt(lodging.DefaultTaxCodeID, 10),
	}
	for name, value := range want {
		if v, ok := page.Value(name); !ok || v != value {
			t.Errorf("form %s = %q (found %v); want %q", name, v, ok, value)
		}
	}
	if got := webtest.Squash(page.Doc.Find("h1").First().Text()); got != "LODGING Lodging" {
		t.Errorf("record title = %q; want LODGING Lodging", got)
	}

	read := w.h.Get(t, w.h.Env.Priya, path(lodging.ID, ""))
	webtest.WantStatus(t, read, http.StatusOK)
	webtest.WantText(t, read, "996311")
	webtest.WantText(t, read, "GST18 GST 18%")
}

// The default HSN/SAC renders through CodeInput, checked at /ui/validate/hsn,
// and keeps the field's length limit.
func TestScreens_ExpenseTypeHSNIsCodeInput(t *testing.T) {
	w := newWorld(t)
	input := w.h.Get(t, w.h.Env.Kavya, "/m/expense-type/new").Doc.Find(`input[name="default_hsn_sac"]`)
	if v, _ := input.Attr("hx-post"); v != "/ui/validate/hsn" {
		t.Errorf("default_hsn_sac hx-post = %q; want /ui/validate/hsn", v)
	}
	if v, _ := input.Attr("maxlength"); v != "8" {
		t.Errorf("default_hsn_sac maxlength = %q; want 8", v)
	}
}

// The list's text and the page after each write, in the master's words.
func TestScreens_ExpenseTypeCopy(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya

	empty := w.h.Get(t, kavya, "/m/expense-type")
	webtest.WantStatus(t, empty, http.StatusOK)
	webtest.WantText(t, empty, "Expense Types")
	webtest.WantText(t, empty, "No expense types yet.")
	webtest.WantText(t, empty, "New expense type")
	if !strings.Contains(empty.Body, "Search expense types") {
		t.Error("list has no Search expense types label")
	}

	resp := w.h.Post(t, kavya, "/m/expense-type/new", url.Values{"code": {"FUEL"}, "name": {"Fuel"}})
	fuel, ok := w.byCode(t, "FUEL")
	if !ok {
		t.Fatalf("valid POST saved nothing: %d %s", resp.Status, webtest.Squash(resp.Body))
	}
	webtest.WantBanner(t, w.h.Follow(t, kavya, resp), "Expense type saved.")
	webtest.WantText(t, w.h.Get(t, kavya, "/m/expense-type"), "1 expense type")

	resp = w.h.Post(t, kavya, path(fuel.ID, "/disable"), url.Values{"version": {ver(fuel.Version)}})
	webtest.WantBanner(t, w.h.Follow(t, kavya, resp), "Expense type disabled.")
	webtest.WantText(t, w.h.Get(t, kavya, "/m/expense-type"), "All expense types are disabled.")

	off := w.mustGet(t, "FUEL")
	resp = w.h.Post(t, kavya, path(fuel.ID, "/enable"), url.Values{"version": {ver(off.Version)}})
	webtest.WantBanner(t, w.h.Follow(t, kavya, resp), "Expense type enabled.")

	w.seed(t)
	list := w.h.Get(t, kavya, "/m/expense-type")
	webtest.WantText(t, list, "3 expense types")
	webtest.WantText(t, list, "Show disabled expense types")

	on := w.mustGet(t, "FUEL")
	resp = w.h.Post(t, kavya, path(fuel.ID, "/delete"), url.Values{"version": {ver(on.Version)}})
	webtest.WantBanner(t, w.h.Follow(t, kavya, resp), "Expense type deleted.")
}

// Writes carry the page's version: without it AUTH's 400, with an old one
// DOC's 409 on the record page. Nothing changes.
func TestScreens_ExpenseTypeWriteVersion(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	lodging := w.lodging(t)
	for _, suffix := range []string{"", "/disable", "/delete"} {
		resp := w.h.Post(t, kavya, path(lodging.ID, suffix), url.Values{"code": {"LODGING"}, "name": {"Renamed"}})
		webtest.WantStatus(t, resp, http.StatusBadRequest)
		webtest.WantText(t, resp, auth.ErrBadForm.Error())
	}
	stale := ver(lodging.Version - 1)
	for _, suffix := range []string{"/disable", "/delete"} {
		resp := w.h.Post(t, kavya, path(lodging.ID, suffix), url.Values{"version": {stale}})
		webtest.WantStatus(t, resp, http.StatusConflict)
		webtest.WantBanner(t, resp, staleLodging)
	}
	if got := w.mustGet(t, "LODGING"); got != lodging {
		t.Errorf("LODGING after refused writes = %+v; want unchanged %+v", got, lodging)
	}
}

// The delete confirm step shows the record's title and posts its version.
func TestScreens_ExpenseTypeDeleteConfirm(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	_, food := w.seed(t)

	confirm := w.h.Get(t, kavya, path(food.ID, "?delete"))
	webtest.WantStatus(t, confirm, http.StatusOK)
	webtest.WantText(t, confirm, "Delete FOOD Food?")
	if v, ok := confirm.FormValue(path(food.ID, "/delete"), "version"); !ok || v != ver(food.Version) {
		t.Errorf("confirm form version = %q (found %v); want %d", v, ok, food.Version)
	}
}

// Export is the shared route under this master's Base: a CSV with its fields
// for a user with the right, AUTH's message for one without.
func TestScreens_ExpenseTypeExport(t *testing.T) {
	w := newWorld(t)
	w.lodging(t)

	file := w.h.Get(t, w.h.Env.Kavya, "/m/expense-type/export.csv")
	webtest.WantStatus(t, file, http.StatusOK)
	if got := file.Header.Get("Content-Disposition"); got != `attachment; filename="expense-type.csv"` {
		t.Errorf("Content-Disposition = %q", got)
	}
	if !strings.Contains(file.Body, "LODGING,Lodging,996311,GST18,No") {
		t.Errorf("export = %q; want the LODGING row", file.Body)
	}

	resp := w.h.Get(t, w.h.Env.Priya, "/m/expense-type/export.csv")
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, (&auth.DeniedError{Right: auth.RightExport, Label: "Expense Type"}).Error())
}

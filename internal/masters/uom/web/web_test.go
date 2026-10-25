package uomweb_test

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
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/uom"
	uomweb "github.com/vardhan/internaltools/internal/masters/uom/web"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Screens under test (MST Task 22): /m/uom list, /new, /{id}, POST /{id} with
// version, /{id}/disable, /{id}/enable, /{id}/delete and the picker
// /m/uom/pick?q=. The eleven UoMs are seeded by migration 01251.
const (
	msgDupNOS = "Code NOS is already used by UoM Numbers. Choose another code." // MST-003, MST-077
	staleGram = "Someone else changed UoM GRAM after you opened it. Reload it and make your changes again."
)

type world struct {
	h   *webtest.Harness
	svc *uom.Service
}

func newWorld(t *testing.T) *world {
	t.Helper()
	h := webtest.New(t, func(k mstkit.Env) func(chi.Router) { return uomweb.Routes(uom.New(k), k) })
	return &world{h: h, svc: uom.New(h.Env.Kit)}
}

func (w *world) read(t *testing.T, fn func(ctx context.Context, tx *db.Tx) error) {
	t.Helper()
	ctx := context.Background()
	if err := w.h.Env.DB.Read(ctx, func(tx *db.Tx) error { return fn(ctx, tx) }); err != nil {
		t.Fatal(err)
	}
}

func (w *world) byCode(t *testing.T, code string) (uom.UoM, bool) {
	t.Helper()
	var v uom.UoM
	var ok bool
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		var err error
		v, ok, err = w.svc.ByCode(ctx, tx, code)
		return err
	})
	return v, ok
}

func (w *world) mustGet(t *testing.T, code string) uom.UoM {
	t.Helper()
	v, ok := w.byCode(t, code)
	if !ok {
		t.Fatalf("UoM %s not found", code)
	}
	return v
}

func path(id int64, suffix string) string {
	return "/m/uom/" + strconv.FormatInt(id, 10) + suffix
}

func ver(v int) string { return strconv.Itoa(v) }

func TestScreens_UoMCreateEditDisable(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya

	list := w.h.Get(t, kavya, "/m/uom")
	webtest.WantStatus(t, list, http.StatusOK)
	for _, code := range []string{"NOS", "KGS", "DRUM"} {
		u := w.mustGet(t, code)
		text, ok := list.RowLink(path(u.ID, ""))
		if !ok || !strings.Contains(text, u.Code+" "+u.Name) {
			t.Errorf("list row %s = %q (found %v); want a link containing %q", code, text, ok, u.Code+" "+u.Name)
		}
	}
	if _, ok := list.RowLink("/m/uom/new"); !ok {
		t.Error("list has no link to /m/uom/new for Kavya (create right)")
	}

	blank := w.h.Get(t, kavya, "/m/uom/new")
	webtest.WantStatus(t, blank, http.StatusOK)
	for _, name := range []string{"code", "name", "uqc"} {
		if !blank.HasControl(name) {
			t.Errorf("new UoM form has no control named %q", name)
		}
	}
	whole, ok := blank.CheckboxValue("whole_number")
	if !ok {
		t.Fatal("new UoM form has no whole_number checkbox")
	}

	// A duplicate code in another case: 422 with MST-003's message next to
	// the code field.
	resp := w.h.Post(t, kavya, "/m/uom/new", url.Values{"code": {"nos"}, "name": {"Numbers again"}, "uqc": {"NOS"}})
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, resp, "code", msgDupNOS)
	if v, _ := resp.Value("name"); v != "Numbers again" {
		t.Errorf("422 page name value = %q; want the typed Numbers again", v)
	}

	// A valid create, whole number ticked (the checkbox's own value): 303.
	resp = w.h.Post(t, kavya, "/m/uom/new", url.Values{"code": {"GRAM"}, "name": {"Grams"}, "uqc": {"GMS"}, "whole_number": {whole}})
	gram, ok := w.byCode(t, "GRAM")
	if !ok {
		t.Fatalf("valid POST saved nothing: %d %s", resp.Status, webtest.Squash(resp.Body))
	}
	webtest.WantRedirect(t, resp, path(gram.ID, ""))
	if gram.Name != "Grams" || gram.UQC != "GMS" || !gram.WholeNumber {
		t.Errorf("stored GRAM = %+v; want Grams, GMS, whole number", gram)
	}

	page := w.h.Get(t, kavya, path(gram.ID, ""))
	webtest.WantStatus(t, page, http.StatusOK)
	if v, _ := page.Value("version"); v != ver(gram.Version) {
		t.Errorf("record page version = %q; want %d", v, gram.Version)
	}

	// Edit with the page's version; whole number unticked (not posted).
	resp = w.h.Post(t, kavya, path(gram.ID, ""), url.Values{"version": {ver(gram.Version)}, "code": {"GRAM"}, "name": {"Gram"}, "uqc": {"GMS"}})
	webtest.WantRedirect(t, resp, path(gram.ID, ""))
	edited := w.mustGet(t, "GRAM")
	if edited.Name != "Gram" || edited.WholeNumber || edited.Version != gram.Version+1 {
		t.Fatalf("after edit GRAM = %+v; want Gram, not whole, version %d", edited, gram.Version+1)
	}

	// Stale version: 409, DOC's message, typed values kept, nothing written.
	resp = w.h.Post(t, kavya, path(gram.ID, ""), url.Values{"version": {ver(gram.Version)}, "code": {"GRAM"}, "name": {"Stale grams"}, "uqc": {"GMS"}})
	webtest.WantStatus(t, resp, http.StatusConflict)
	webtest.WantText(t, resp, staleGram)
	if v, _ := resp.Value("name"); v != "Stale grams" {
		t.Errorf("409 page name value = %q; want the typed Stale grams", v)
	}
	if got := w.mustGet(t, "GRAM"); got.Name != "Gram" {
		t.Errorf("stale save overwrote GRAM: %q", got.Name)
	}

	// Disable: hidden by default, tagged Disabled with include_disabled.
	resp = w.h.Post(t, kavya, path(gram.ID, "/disable"), url.Values{"version": {ver(edited.Version)}})
	webtest.WantRedirect(t, resp, path(gram.ID, ""))
	off := w.mustGet(t, "GRAM")
	if !off.Disabled {
		t.Fatal("GRAM not disabled after POST /disable")
	}
	if _, ok := w.h.Get(t, kavya, "/m/uom").RowLink(path(gram.ID, "")); ok {
		t.Error("disabled GRAM still in the default list")
	}
	text, ok := w.h.Get(t, kavya, "/m/uom?include_disabled=true").RowLink(path(gram.ID, ""))
	if !ok || !strings.Contains(text, "Disabled") {
		t.Errorf("include_disabled row GRAM = %q (found %v); want it tagged Disabled", text, ok)
	}
	webtest.WantText(t, w.h.Get(t, kavya, path(gram.ID, "")), "Disabled")

	resp = w.h.Post(t, kavya, path(gram.ID, "/enable"), url.Values{"version": {ver(off.Version)}})
	webtest.WantRedirect(t, resp, path(gram.ID, ""))
	if w.mustGet(t, "GRAM").Disabled {
		t.Fatal("GRAM still disabled after POST /enable")
	}
	text, ok = w.h.Get(t, kavya, "/m/uom").RowLink(path(gram.ID, ""))
	if !ok || strings.Contains(text, "Disabled") {
		t.Errorf("after enable GRAM row = %q (found %v); want it listed, not Disabled", text, ok)
	}
}

func TestScreens_UoMReadOnlyForbidden(t *testing.T) {
	w := newWorld(t)
	priya := w.h.Env.Priya
	kgs := w.mustGet(t, "KGS")
	deny := func(r auth.Right) string { return (&auth.DeniedError{Right: r, Label: "UoM"}).Error() }

	list := w.h.Get(t, priya, "/m/uom")
	webtest.WantStatus(t, list, http.StatusOK)
	if _, ok := list.RowLink(path(kgs.ID, "")); !ok {
		t.Error("Priya's list lacks KGS")
	}
	if _, ok := list.RowLink("/m/uom/new"); ok {
		t.Error("Priya's list links to /m/uom/new without the create right")
	}

	resp := w.h.Post(t, priya, "/m/uom/new", url.Values{"code": {"GRAM"}, "name": {"Grams"}, "uqc": {"GMS"}})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightCreate))
	if _, ok := w.byCode(t, "GRAM"); ok {
		t.Error("Priya's create saved GRAM")
	}

	resp = w.h.Post(t, priya, path(kgs.ID, ""), url.Values{"version": {ver(kgs.Version)}, "code": {"KGS"}, "name": {"Renamed by Priya"}, "uqc": {"KGS"}})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightWrite))

	resp = w.h.Post(t, priya, path(kgs.ID, "/disable"), url.Values{"version": {ver(kgs.Version)}})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightWrite))

	resp = w.h.Post(t, priya, path(kgs.ID, "/delete"), url.Values{"version": {ver(kgs.Version)}})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightDelete))

	if got := w.mustGet(t, "KGS"); got.Name != kgs.Name || got.Disabled || got.Version != kgs.Version {
		t.Errorf("KGS after Priya's refused writes = %+v; want unchanged %+v", got, kgs)
	}
}

func TestScreens_UoMPicker(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	kgs, sqm := w.mustGet(t, "KGS"), w.mustGet(t, "SQM")

	rows := w.h.GetHTMX(t, kavya, "/m/uom/pick?q=Kilo")
	webtest.WantStatus(t, rows, http.StatusOK)
	got := rows.PickRows()
	if len(got) != 1 || got[0].Value != strconv.FormatInt(kgs.ID, 10) || !strings.Contains(got[0].Text, "KGS Kilograms") {
		t.Errorf("pick?q=Kilo = %v; want one row %d \"KGS Kilograms\"", got, kgs.ID)
	}
	got = w.h.GetHTMX(t, kavya, "/m/uom/pick?q=Square").PickRows()
	if len(got) != 1 || got[0].Value != strconv.FormatInt(sqm.ID, 10) || !strings.Contains(got[0].Text, "SQM Square meters") {
		t.Errorf("pick?q=Square = %v; want one row %d \"SQM Square meters\"", got, sqm.ID)
	}

	if err := w.svc.SetDisabled(context.Background(), kavya, sqm.ID, sqm.Version, true); err != nil {
		t.Fatal(err)
	}
	if got := w.h.GetHTMX(t, kavya, "/m/uom/pick?q=Square").PickRows(); len(got) != 0 {
		t.Errorf("pick?q=Square after disabling SQM = %v; want no rows", got)
	}
}

func TestScreens_UoMDelete(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	ctx := context.Background()

	// An unused UoM is deleted.
	saved, err := w.svc.Create(ctx, kavya, uom.Input{Code: "GRAM", Name: "Grams", UQC: "GMS"})
	if err != nil {
		t.Fatal(err)
	}
	resp := w.h.Post(t, kavya, path(saved.ID, "/delete"), url.Values{"version": {ver(saved.Version)}})
	webtest.WantRedirect(t, resp, "/m/uom")
	if _, ok := w.byCode(t, "GRAM"); ok {
		t.Fatal("GRAM still exists after POST /delete")
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

	// A UoM an item uses is refused with the MST-014 refusal (422, no field)
	// and kept. What it names after "used in" is mstkit's (CD-167).
	kgs := w.mustGet(t, "KGS")
	if _, err := item.New(w.h.Env.Kit).Create(ctx, kavya, item.Input{
		Code: "MS-ANGLE-50", Name: "MS Angle 50x50x5", Type: item.TypeGoods, HSNSAC: "7216", StockUoMID: kgs.ID,
	}); err != nil {
		t.Fatal(err)
	}
	resp = w.h.Post(t, kavya, path(kgs.ID, "/delete"), url.Values{"version": {ver(kgs.Version)}})
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantText(t, resp, "Cannot delete UoM KGS: it is used in ")
	webtest.WantText(t, resp, ". Disable it instead.")
	w.mustGet(t, "KGS")
}

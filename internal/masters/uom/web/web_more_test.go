package uomweb_test

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/allmasters"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/uom"
)

// No case ID: the screen behaviour Task 22 adds beyond its named tests.

// A write without the page's version is AUTH's 400 form error (CD-146,
// CD-183), and nothing is written.
func TestScreens_UoMWriteNeedsVersion(t *testing.T) {
	w := newWorld(t)
	kgs := w.mustGet(t, "KGS")
	for _, suffix := range []string{"", "/disable", "/delete"} {
		resp := w.h.Post(t, w.h.Env.Kavya, path(kgs.ID, suffix), url.Values{"code": {"KGS"}, "name": {"Kilos"}, "uqc": {"KGS"}})
		webtest.WantStatus(t, resp, http.StatusBadRequest)
		webtest.WantText(t, resp, auth.ErrBadForm.Error())
	}
	if got := w.mustGet(t, "KGS"); got != kgs {
		t.Errorf("KGS after versionless writes = %+v; want unchanged %+v", got, kgs)
	}
}

// Priya (read only) gets 403 on the new page, as a page or as htmx's
// notice, and sees a record's values without a form or More actions.
func TestScreens_UoMReadOnlyPages(t *testing.T) {
	w := newWorld(t)
	priya := w.h.Env.Priya
	deny := (&auth.DeniedError{Right: auth.RightCreate, Label: "UoM"}).Error()

	resp := w.h.Get(t, priya, "/m/uom/new")
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny)
	resp = w.h.GetHTMX(t, priya, "/m/uom/new")
	webtest.WantStatus(t, resp, http.StatusForbidden)
	if resp.Header.Get("HX-Trigger") == "" {
		t.Error("htmx 403 has no HX-Trigger notice")
	}

	kgs := w.mustGet(t, "KGS")
	page := w.h.Get(t, priya, path(kgs.ID, ""))
	webtest.WantStatus(t, page, http.StatusOK)
	webtest.WantText(t, page, "Kilograms")
	for _, name := range []string{"name", "version"} {
		if page.HasControl(name) {
			t.Errorf("Priya's record page has a %q control; want values only", name)
		}
	}
	webtest.WantNoText(t, page, "More actions")
}

// Delete opens a confirm step first (design-system §7); it posts the
// rendered version to /delete.
func TestScreens_UoMDeleteConfirm(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	box := w.mustGet(t, "BOX")

	page := w.h.Get(t, kavya, path(box.ID, ""))
	if _, ok := page.RowLink(path(box.ID, "?delete")); !ok {
		t.Fatal("record page has no link to the delete confirm step")
	}
	confirm := w.h.Get(t, kavya, path(box.ID, "?delete"))
	webtest.WantStatus(t, confirm, http.StatusOK)
	webtest.WantText(t, confirm, "Delete BOX Box?")
	form := confirm.Doc.Find(`form[action="` + path(box.ID, "/delete") + `"]`)
	if v, _ := form.Find(`input[name="version"]`).Attr("value"); v != ver(box.Version) {
		t.Errorf("confirm form version = %q; want %d", v, box.Version)
	}

	priya := w.h.Get(t, w.h.Env.Priya, path(box.ID, "?delete"))
	if priya.Doc.Find(`form[action="`+path(box.ID, "/delete")+`"]`).Length() != 0 {
		t.Error("Priya (no delete right) gets the delete confirm form")
	}
}

// Each successful write shows its success banner on the page the 303 leads
// to (CD-80, design-system §7): the flash survives the redirect through the
// session.
func TestScreens_UoMSuccessFlash(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya

	resp := w.h.Post(t, kavya, "/m/uom/new", url.Values{"code": {"GRAM"}, "name": {"Grams"}, "uqc": {"GMS"}})
	gram, ok := w.byCode(t, "GRAM")
	if !ok {
		t.Fatalf("valid POST saved nothing: %d %s", resp.Status, webtest.Squash(resp.Body))
	}
	webtest.WantRedirect(t, resp, path(gram.ID, ""))
	page := w.h.Follow(t, kavya, resp)
	webtest.WantStatus(t, page, http.StatusOK)
	webtest.WantBanner(t, page, "UoM saved.")

	resp = w.h.Post(t, kavya, path(gram.ID, "/disable"), url.Values{"version": {ver(gram.Version)}})
	webtest.WantRedirect(t, resp, path(gram.ID, ""))
	page = w.h.Follow(t, kavya, resp)
	webtest.WantStatus(t, page, http.StatusOK)
	webtest.WantBanner(t, page, "UoM disabled.")

	off := w.mustGet(t, "GRAM")
	resp = w.h.Post(t, kavya, path(gram.ID, "/enable"), url.Values{"version": {ver(off.Version)}})
	webtest.WantRedirect(t, resp, path(gram.ID, ""))
	page = w.h.Follow(t, kavya, resp)
	webtest.WantStatus(t, page, http.StatusOK)
	webtest.WantBanner(t, page, "UoM enabled.")

	on := w.mustGet(t, "GRAM")
	resp = w.h.Post(t, kavya, path(gram.ID, "/delete"), url.Values{"version": {ver(on.Version)}})
	webtest.WantRedirect(t, resp, "/m/uom")
	page = w.h.Follow(t, kavya, resp)
	webtest.WantStatus(t, page, http.StatusOK)
	webtest.WantBanner(t, page, "UoM deleted.")
}

// A stale version refuses disable and delete too (CD-94, CD-146): 409 with
// DOC's message on the record page, and nothing changes.
func TestScreens_UoMStaleDisableDelete(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	box := w.mustGet(t, "BOX")
	stale := ver(box.Version - 1)
	for _, suffix := range []string{"/disable", "/delete"} {
		resp := w.h.Post(t, kavya, path(box.ID, suffix), url.Values{"version": {stale}})
		webtest.WantStatus(t, resp, http.StatusConflict)
		webtest.WantBanner(t, resp, "Someone else changed UoM BOX after you opened it.")
	}
	if got := w.mustGet(t, "BOX"); got != box {
		t.Errorf("BOX after stale writes = %+v; want unchanged %+v", got, box)
	}
}

// The search survives the Show disabled link, the Disabled included chip
// and paging; htmx's search answers with the fragment, not the whole page.
func TestScreens_UoMListKeepsSearch(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya

	if _, ok := w.h.Get(t, kavya, "/m/uom?q=Kilo").RowLink("/m/uom?include_disabled=true&q=Kilo"); !ok {
		t.Error("Show disabled link drops the search")
	}
	if _, ok := w.h.Get(t, kavya, "/m/uom").RowLink("/m/uom?include_disabled=true"); !ok {
		t.Error("Show disabled link without a search is not the plain URL")
	}
	all := w.h.Get(t, kavya, "/m/uom?include_disabled=true&q=Kilo")
	if _, ok := all.RowLink("/m/uom?q=Kilo"); !ok {
		t.Error("Disabled included chip drops the search")
	}
	if !all.HasControl("include_disabled") {
		t.Error("searching again drops include_disabled: the bar doesn't keep it")
	}
	chip := w.h.Get(t, kavya, "/m/uom?include_disabled=true").Doc.Find("a.chip")
	if href, _ := chip.Attr("href"); href != "/m/uom" {
		t.Errorf("Disabled included chip without a search = %q; want the plain URL", href)
	}

	for i := range 26 {
		code := "BULK" + strconv.Itoa(i)
		if _, err := w.svc.Create(context.Background(), kavya, uom.Input{Code: code, Name: "Bulk " + code, UQC: "OTH"}); err != nil {
			t.Fatal(err)
		}
	}
	const second = "/m/uom?include_disabled=true&page=2&q=Bulk"
	if _, ok := w.h.Get(t, kavya, "/m/uom?include_disabled=true&q=Bulk").RowLink(second); !ok {
		t.Errorf("first page has no Next link to %s", second)
	}
	const first = "/m/uom?include_disabled=true&page=1&q=Bulk"
	if _, ok := w.h.Get(t, kavya, second).RowLink(first); !ok {
		t.Errorf("second page has no Previous link to %s", first)
	}

	frag := w.h.GetHTMX(t, kavya, "/m/uom?q=Kilo")
	webtest.WantStatus(t, frag, http.StatusOK)
	webtest.WantText(t, frag, "KGS Kilograms")
	if frag.Doc.Find(".app-sidebar").Length() != 0 {
		t.Error("htmx search answered with the whole page")
	}
}

// With a search or the disabled filter on, an empty result says the search
// found nothing: "No UoMs yet." would be untrue.
func TestScreens_UoMNoMatch(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	for _, target := range []string{"/m/uom?q=zzzz", "/m/uom?include_disabled=true&q=zzzz"} {
		resp := w.h.Get(t, kavya, target)
		webtest.WantStatus(t, resp, http.StatusOK)
		webtest.WantText(t, resp, "No results. Try a different search.")
		webtest.WantNoText(t, resp, "No UoMs yet.")
	}
}

// allmasters.New builds the services and Routes mounts the screens and the
// picker from them, as production will (CD-195).
func TestScreens_UoMThroughAllmasters(t *testing.T) {
	var svcs *allmasters.Services
	h := webtest.New(t, func(k mstkit.Env) func(chi.Router) {
		svcs = allmasters.New(k)
		return svcs.Routes
	})
	if svcs.Company == nil || svcs.Terms == nil || svcs.Party == nil || svcs.UoM == nil ||
		svcs.TaxCode == nil || svcs.Item == nil || svcs.ExpenseType == nil {
		t.Errorf("allmasters.New left a service nil: %+v", svcs)
	}

	webtest.WantStatus(t, h.Get(t, h.Env.Kavya, "/m/uom"), http.StatusOK)
	rows := h.GetHTMX(t, h.Env.Kavya, "/pick/UoM?q=Kilo")
	webtest.WantStatus(t, rows, http.StatusOK)
	if got := rows.PickRows(); len(got) != 1 {
		t.Errorf("pick?q=Kilo = %v; want one row", got)
	}
	webtest.WantStatus(t, h.Get(t, h.Env.Kavya, "/m/uom/pick"), http.StatusNotFound)
}

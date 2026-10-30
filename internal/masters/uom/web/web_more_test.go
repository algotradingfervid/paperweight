package uomweb_test

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"
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

// With a search on, an empty result says it found nothing: "No UoMs yet."
// would be untrue. The disabled filter doesn't change that.
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
	h := webtest.NewSessions(t, func(k mstkit.Env, sm *scs.SessionManager) func(chi.Router) {
		svcs = allmasters.New(k)
		return func(r chi.Router) { svcs.Routes(r, allmasters.WebDeps{Sessions: sm}) }
	})
	if svcs.Company == nil || svcs.Terms == nil || svcs.Party == nil || svcs.Address == nil || svcs.UoM == nil ||
		svcs.TaxCode == nil || svcs.Item == nil || svcs.ExpenseType == nil ||
		svcs.Project == nil || svcs.Site == nil || svcs.Employee == nil || svcs.Directory == nil ||
		svcs.Letterhead.S == nil || svcs.Letterhead.AddressLines == nil {
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

// A record's id starts at 1: /0 is no route, for any user, on any method.
func TestScreens_UoMZeroIDIsNotFound(t *testing.T) {
	w := newWorld(t)
	for _, p := range []auth.Principal{w.h.Env.Ravi, w.h.Env.Kavya} {
		for _, target := range []string{"/m/uom/0", "/m/uom/007"} {
			webtest.WantStatus(t, w.h.Get(t, p, target), http.StatusNotFound)
			webtest.WantStatus(t, w.h.Post(t, p, target, url.Values{"version": {"1"}, "code": {"X"}, "name": {"X"}}), http.StatusNotFound)
		}
		webtest.WantStatus(t, w.h.Post(t, p, "/m/uom/0/disable", url.Values{"version": {"1"}}), http.StatusNotFound)
	}
}

// Ravi has no right on UoM: 403 with AUTH's message on the list and on the
// record page. Service.Get has no auth, so the page's read check is the only
// guard.
func TestScreens_UoMNoReadForbidden(t *testing.T) {
	w := newWorld(t)
	ravi := w.h.Env.Ravi
	kgs := w.mustGet(t, "KGS")
	deny := (&auth.DeniedError{Right: auth.RightRead, Label: "UoM"}).Error()
	for _, target := range []string{"/m/uom", path(kgs.ID, "")} {
		resp := w.h.Get(t, ravi, target)
		webtest.WantStatus(t, resp, http.StatusForbidden)
		webtest.WantText(t, resp, deny)
		webtest.WantNoText(t, resp, "Kilograms")
	}
}

// A stale save keeps the version the page posted, in the form and in More
// actions, on the real service (CD-94): loading the row's would turn the
// next save into a silent overwrite.
func TestScreens_UoMStaleSaveKeepsPostedVersion(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	saved, err := w.svc.Create(context.Background(), kavya, uom.Input{Code: "GRAM", Name: "Grams", UQC: "GMS"})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		if _, err := w.svc.Update(context.Background(), kavya, saved.ID, saved.Version+i, uom.Input{Code: "GRAM", Name: "Grams " + ver(i), UQC: "GMS"}); err != nil {
			t.Fatal(err)
		}
	}
	stale := ver(saved.Version + 1)
	resp := w.h.Post(t, kavya, path(saved.ID, ""), url.Values{"version": {stale}, "code": {"GRAM"}, "name": {"Mine"}, "uqc": {"GMS"}})
	webtest.WantStatus(t, resp, http.StatusConflict)
	for _, action := range []string{path(saved.ID, ""), path(saved.ID, "/disable")} {
		if v, ok := resp.FormValue(action, "version"); !ok || v != stale {
			t.Errorf("version in the form posting to %s = %q (found %v); want the posted %s", action, v, ok, stale)
		}
	}
}

// The bar's sort reorders the list, shows what is chosen, and survives the
// links around it. An unknown sort is the list's own refusal.
func TestScreens_UoMListSort(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	firstRow := func(target string) string {
		resp := w.h.Get(t, kavya, target)
		webtest.WantStatus(t, resp, http.StatusOK)
		return webtest.Squash(resp.Doc.Find("a.datalist-link").First().Text())
	}

	page := w.h.Get(t, kavya, "/m/uom")
	var got []string
	for _, c := range page.Choices("sort") {
		got = append(got, c.Value+"="+c.Label)
	}
	want := "name.asc=Name low to high,code.asc=Code low to high,code.desc=Code high to low,name.desc=Name high to low"
	if strings.Join(got, ",") != want {
		t.Errorf("sort options = %q; want %s", got, want)
	}
	if v, _ := page.Value("sort"); v != "name.asc" {
		t.Errorf("default sort shown = %q; want name.asc", v)
	}

	byName, byNameDown := firstRow("/m/uom"), firstRow("/m/uom?sort=name.desc")
	if byName == byNameDown {
		t.Errorf("sort=name.desc changed nothing: first row %q", byName)
	}
	if got := firstRow("/m/uom?sort=code.asc"); !strings.Contains(got, "BAG Bags") {
		t.Errorf("sort=code.asc first row = %q; want BAG Bags", got)
	}

	sorted := w.h.Get(t, kavya, "/m/uom?sort=code.desc&q=K")
	if v, _ := sorted.Value("sort"); v != "code.desc" {
		t.Errorf("chosen sort shown = %q; want code.desc", v)
	}
	if _, ok := sorted.RowLink("/m/uom?include_disabled=true&q=K&sort=code.desc"); !ok {
		t.Error("Show disabled link drops the sort")
	}
	if _, ok := w.h.Get(t, kavya, "/m/uom?include_disabled=true&sort=code.desc").RowLink("/m/uom?sort=code.desc"); !ok {
		t.Error("Disabled included chip drops the sort")
	}

	for _, target := range []string{"/m/uom?sort=uqc.asc", "/m/uom/export.csv?sort=uqc.asc"} {
		resp := w.h.Get(t, kavya, target)
		webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
		webtest.WantText(t, resp, "You can't sort UoMs by 'uqc'.")
	}
}

// Kavya's list offers Export; the file is the list she sees, with csvio's BOM
// and field labels. Read is the route's guard (CD-145): Ravi gets the read
// message and Priya, who reads, the export message and no button.
func TestScreens_UoMExport(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	box := w.mustGet(t, "BOX")
	if err := w.svc.SetDisabled(context.Background(), kavya, box.ID, box.Version, true); err != nil {
		t.Fatal(err)
	}

	if _, ok := w.h.Get(t, kavya, "/m/uom").RowLink("/m/uom/export.csv"); !ok {
		t.Error("Kavya's list has no Export link")
	}
	if _, ok := w.h.Get(t, kavya, "/m/uom?include_disabled=true&q=Kilo&page=1").RowLink("/m/uom/export.csv?include_disabled=true&q=Kilo"); !ok {
		t.Error("Export link doesn't follow the list's search and filter")
	}

	file := w.h.Get(t, kavya, "/m/uom/export.csv")
	webtest.WantStatus(t, file, http.StatusOK)
	if got := file.Header.Get("Content-Type"); got != "text/csv; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := file.Header.Get("Content-Disposition"); got != `attachment; filename="uom.csv"` {
		t.Errorf("Content-Disposition = %q", got)
	}
	if !strings.HasPrefix(file.Body, "\xef\xbb\xbfCode,Name,") {
		t.Errorf("file starts %.30q; want the BOM and the labels", file.Body)
	}
	if !strings.Contains(file.Body, "KGS,Kilograms,") || strings.Contains(file.Body, "BOX,") {
		t.Errorf("export = %q; want KGS and not the disabled BOX", file.Body)
	}
	if _, ok := w.h.Get(t, kavya, "/m/uom?sort=code.desc&q=K").RowLink("/m/uom/export.csv?q=K&sort=code.desc"); !ok {
		t.Error("Export link drops the sort")
	}
	var codes []string
	for _, line := range strings.Split(strings.TrimSpace(w.h.Get(t, kavya, "/m/uom/export.csv?sort=code.desc").Body), "\r\n")[1:] {
		code, _, _ := strings.Cut(line, ",")
		codes = append(codes, code)
	}
	if len(codes) < 2 || !slices.IsSortedFunc(codes, func(a, b string) int { return strings.Compare(b, a) }) {
		t.Errorf("export?sort=code.desc codes = %q; want descending", codes)
	}
	all := w.h.Get(t, kavya, "/m/uom/export.csv?include_disabled=true&q=Box")
	if !strings.Contains(all.Body, "BOX,") || strings.Contains(all.Body, "KGS,") {
		t.Errorf("filtered export = %q; want only BOX", all.Body)
	}

	resp := w.h.Get(t, w.h.Env.Ravi, "/m/uom/export.csv")
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, (&auth.DeniedError{Right: auth.RightRead, Label: "UoM"}).Error())

	resp = w.h.Get(t, w.h.Env.Priya, "/m/uom/export.csv")
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, (&auth.DeniedError{Right: auth.RightExport, Label: "UoM"}).Error())
	if got := resp.Header.Get("Content-Disposition"); got != "" {
		t.Errorf("refused export has Content-Disposition %q: the 403 page would download", got)
	}
	if _, ok := w.h.Get(t, w.h.Env.Priya, "/m/uom").RowLink("/m/uom/export.csv"); ok {
		t.Error("Priya's list links to Export without the export right")
	}
}

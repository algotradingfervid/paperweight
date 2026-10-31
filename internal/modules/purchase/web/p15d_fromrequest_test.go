package purchaseweb

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/web"
)

// p15dOrderForm checks the Create PO page's form: it posts to path with the
// request's version, a vendor_id and the Create purchase order button.
func p15dOrderForm(t *testing.T, page p15dPage, path, version string) {
	t.Helper()
	form := page.doc.Find(`form[action="` + path + `"]`)
	if form.Length() != 1 {
		t.Fatalf("Create PO page has %d forms posting to %s, want 1", form.Length(), path)
	}
	if got := form.Find(`input[name="version"]`).AttrOr("value", ""); got != version {
		t.Errorf("form version = %q, want %q", got, version)
	}
	if form.Find(`input[name="vendor_id"]`).Length() != 1 {
		t.Error("form has no vendor_id input")
	}
	if got := p15dCollapse(form.Find(`button[type="submit"]`).Last().Text()); got != "Create purchase order" {
		t.Errorf("form button = %q, want Create purchase order", got)
	}
}

// PUX-025 (PO-051, spec §20.4, UX §5.18): Priya's Create PO page lists only
// PR-A's rows with pending to order, at that quantity, under the summary
// line, with the vendor picker and its hint. Posting no vendor redraws the
// page with Pick a vendor. at the picker and in the error summary; posting
// SHREE opens the new draft's Details with the flash. A request with
// nothing pending shows §20.4's text, a link back and no form.
func TestPUX025_CreatePOPage(t *testing.T) {
	w := newP15dWeb(t)
	env := w.env
	pr := w.prA(t)
	draft := w.hold(t, purchasetest.StopAtDraft(), pr.Lines[0].ID, "CEMENT-OPC", "BAG", "2523", "25")
	w.hold(t, purchasetest.StopAtApproved(), pr.Lines[1].ID, "WIRE-18G", "KGS", "7217", "25")
	path := p15dOrderPath(pr.ID)

	page := w.get(t, env.Priya, path)
	if page.status != http.StatusOK {
		t.Fatalf("Priya GET Create PO = %d: %s", page.status, page.text())
	}
	if got := page.find("h1"); !slices.Equal(got, []string{"Create a purchase order"}) {
		t.Errorf("h1 = %q, want Create a purchase order", got)
	}
	summary := "From " + pr.Number + " · Suresh Patil · METRO-SITE · Needed by 07-11-2026"
	if !strings.Contains(page.text(), summary) {
		t.Errorf("page lacks the summary %q: %s", summary, page.text())
	}
	if page.doc.Find(`a[href="`+p15dDoc(purchase.RequestDocType, pr.ID)+`"]`).Length() == 0 {
		t.Errorf("page has no Back link to %s", pr.Number)
	}
	if card := p15dCard(page, "Items to order"); card == "" {
		t.Errorf("page has no Items to order card: %s", page.text())
	}
	if got := page.find(".lines-card .num"); !slices.Equal(got, []string{"15 BAG"}) {
		t.Errorf("rows = %q, want only row 1 at its pending 15 BAG", got)
	}
	if got := page.find(".lines-card"); len(got) != 1 || !strings.Contains(got[0], "OPC Cement 53 Grade") || !strings.Contains(got[0], "CEMENT-OPC") {
		t.Errorf("row cards = %q, want OPC Cement 53 Grade, CEMENT-OPC", got)
	}
	if !strings.Contains(page.text(), "The last price paid to this vendor is filled in.") {
		t.Errorf("page lacks the vendor hint: %s", page.text())
	}
	p15dOrderForm(t, page, path, w.version(t, pr.ID))

	a := w.do(t, env.Priya, http.MethodPost, path, url.Values{"version": {w.version(t, pr.ID)}, "vendor_id": {""}})
	if a.status != http.StatusUnprocessableEntity {
		t.Fatalf("POST no vendor = %d: %s", a.status, a.text())
	}
	if got := a.find(".error-summary li"); !slices.Contains(got, "Pick a vendor.") {
		t.Errorf("422 error summary = %q, want Pick a vendor.", got)
	}
	if a.doc.Find(`a[href="#f-vendor_id"]`).Length() != 1 {
		t.Error("422 error summary doesn't link to the vendor picker #f-vendor_id")
	}
	if got := a.find("#f-vendor_id-msg"); !slices.Equal(got, []string{"Pick a vendor."}) {
		t.Errorf("vendor message = %q, want Pick a vendor.", got)
	}
	if got := a.find(".lines-card .num"); !slices.Equal(got, []string{"15 BAG"}) {
		t.Errorf("422 rows = %q, want 15 BAG", got)
	}
	p15dOrderForm(t, a, path, w.version(t, pr.ID))

	shree := w.id(t, "party", "SHREE")
	a = w.do(t, env.Priya, http.MethodPost, path, url.Values{"version": {w.version(t, pr.ID)}, "vendor_id": {shree}})
	var newID int64
	if err := env.DB.SQL().QueryRowContext(t.Context(), `SELECT max(id) FROM purchase_order`).Scan(&newID); err != nil {
		t.Fatal(err)
	}
	if newID == draft.ID || a.status != http.StatusSeeOther || a.location != p15dDoc(purchase.DocType, newID)+"/edit/details" {
		t.Fatalf("POST SHREE = %d to %q (%s), want 303 to the new draft's Details", a.status, a.location, a.text())
	}
	want := []web.Notice{{Level: "success", Text: "Draft " + strconv.FormatInt(newID, 10) + " was created from " + pr.Number + "."}}
	if got := w.flash(t, a); !slices.Equal(got, want) {
		t.Errorf("flash = %+v, want %+v", got, want)
	}

	// Nothing pending now: the text, a link back and no form.
	page = w.get(t, env.Priya, path)
	nothing := "Everything on " + pr.Number + " is already on an order."
	if page.status != http.StatusOK || !slices.Equal(page.banners("info"), []string{nothing}) {
		t.Errorf("GET with nothing pending = %d, notes %q; want 200 with %q", page.status, page.banners("info"), nothing)
	}
	if page.doc.Find("form").Length() != 0 || page.doc.Find(`input[name="vendor_id"]`).Length() != 0 {
		t.Error("nothing pending: the page still has a form")
	}
	if page.doc.Find(`main a[href="`+p15dDoc(purchase.RequestDocType, pr.ID)+`"]`).Length() == 0 {
		t.Errorf("nothing pending: no link back to %s", pr.Number)
	}
}

// PUX-025's other redraws (PUX-027; UX §5.18): a 409 shows DOC-069's text in
// the conflict banner with a Reload link and keeps the chosen vendor and the
// posted version (CD-94, as docscreen's step redraw does); a 422
// that became true after loading (nothing pending) is shown on the page with
// no form.
func TestPUX025_CreatePORedraws(t *testing.T) {
	w := newP15dWeb(t)
	env := w.env
	pr := w.prA(t)
	path := p15dOrderPath(pr.ID)
	shree := w.id(t, "party", "SHREE")
	v, _ := strconv.Atoi(w.version(t, pr.ID))

	a := w.do(t, env.Priya, http.MethodPost, path, url.Values{"version": {strconv.Itoa(v - 1)}, "vendor_id": {shree}})
	stale := "Someone else changed " + pr.Number + " after you opened it. Reload it and make your changes again."
	if a.status != http.StatusConflict || !slices.Equal(a.banners("danger"), []string{stale}) {
		t.Errorf("POST stale = %d, danger %q; want 409 with %q", a.status, a.banners("danger"), stale)
	}
	if a.doc.Find(`.banner--danger a[href="`+path+`"]`).Length() != 1 {
		t.Errorf("409 banner has no Reload link to %s", path)
	}
	if got := a.doc.Find(`input[name="vendor_id"]`).AttrOr("value", ""); got != shree {
		t.Errorf("409 vendor_id = %q, want SHREE's %s kept", got, shree)
	}
	if !strings.Contains(a.text(), "Shree Steel Traders") {
		t.Errorf("409 picker doesn't show the chosen vendor: %s", a.text())
	}
	p15dOrderForm(t, a, path, strconv.Itoa(v-1)) // the posted version, so the next post still conflicts (CD-94)

	w.hold(t, purchasetest.StopAtDraft(), pr.Lines[0].ID, "CEMENT-OPC", "BAG", "2523", "40")
	w.hold(t, purchasetest.StopAtDraft(), pr.Lines[1].ID, "WIRE-18G", "KGS", "7217", "25")
	a = w.do(t, env.Priya, http.MethodPost, path, url.Values{"version": {w.version(t, pr.ID)}, "vendor_id": {shree}})
	nothing := "Everything on " + pr.Number + " is already on an order."
	if a.status != http.StatusUnprocessableEntity || !slices.Equal(a.banners("info"), []string{nothing}) || len(a.banners("danger")) != 0 {
		t.Errorf("POST with nothing pending = %d, info %q, danger %q; want 422 with only %q", a.status, a.banners("info"), a.banners("danger"), nothing)
	}
	if a.doc.Find("form").Length() != 0 {
		t.Error("422 nothing pending: the page still has a form")
	}
}

// PUX-025 on a closed request (spec §20.2: pending to order is 0 once
// closed): the page shows §20.4's text and no form, as the POST would answer.
func TestPUX025_CreatePOClosedRequest(t *testing.T) {
	w := newP15dWeb(t)
	pr := w.prA(t)
	w.act(t, w.env.Suresh, purchase.RequestDocType, pr.ID, doctype.ActionClose, "No longer needed")
	page := w.get(t, w.env.Priya, p15dOrderPath(pr.ID))
	nothing := "Everything on " + pr.Number + " is already on an order."
	if page.status != http.StatusOK || !slices.Equal(page.banners("info"), []string{nothing}) || page.doc.Find("form").Length() != 0 {
		t.Errorf("closed request GET = %d, notes %q, %d forms; want 200 with %q and no form", page.status, page.banners("info"), page.doc.Find("form").Length(), nothing)
	}
}

// PUX-025's redraw keeps a posted vendor's label only for a reader (CD-233):
// a user who may create orders but not read parties, and an unknown vendor
// id, see the raw id; a refusal on another field (a disabled item) is a
// danger banner, with the form kept.
func TestPUX025_CreatePORedrawLabelsAndRefusals(t *testing.T) {
	w := newP15dWeb(t)
	env := w.env
	pr := w.prA(t)
	path := p15dOrderPath(pr.ID)
	shree := w.id(t, "party", "SHREE")
	v, _ := strconv.Atoi(w.version(t, pr.ID))
	stale := url.Values{"version": {strconv.Itoa(v - 1)}, "vendor_id": {shree}}

	creator := w.probe(t, "no-party", auth.RightCreate)
	a := w.do(t, creator, http.MethodPost, path, stale)
	if a.status != http.StatusConflict || strings.Contains(a.text(), "Shree Steel Traders") {
		t.Errorf("no party read: 409 = %d and names the vendor: %s", a.status, a.text())
	}
	if got := a.doc.Find(`input[name="vendor_id"]`).AttrOr("value", ""); got != shree {
		t.Errorf("no party read: vendor_id = %q, want %s kept", got, shree)
	}
	a = w.do(t, env.Priya, http.MethodPost, path, url.Values{"version": {strconv.Itoa(v - 1)}, "vendor_id": {"999999"}})
	if a.status != http.StatusConflict || a.doc.Find(`input[name="vendor_id"]`).AttrOr("value", "") != "999999" {
		t.Errorf("unknown vendor: 409 = %d, vendor_id %q; want 999999 kept", a.status, a.doc.Find(`input[name="vendor_id"]`).AttrOr("value", ""))
	}

	if _, err := env.DB.SQL().ExecContext(t.Context(), `UPDATE item SET disabled = 1 WHERE code = 'CEMENT-OPC'`); err != nil {
		t.Fatal(err)
	}
	a = w.do(t, env.Priya, http.MethodPost, path, url.Values{"version": {w.version(t, pr.ID)}, "vendor_id": {shree}})
	if a.status != http.StatusUnprocessableEntity || len(a.banners("danger")) != 1 || !strings.Contains(a.banners("danger")[0], "CEMENT-OPC") {
		t.Errorf("disabled item: = %d, danger %q; want 422 with the item's refusal", a.status, a.banners("danger"))
	}
	p15dOrderForm(t, a, path, w.version(t, pr.ID))
}

// PUX-025's rights (UX §5.18, CD-245, CD-259): the page needs read on the
// request and create on Purchase Order. Suresh (no create) gets AUTH's 403, a
// request Priya can't read and a missing one are AUTH's 404, and a user
// holding only create on Purchase Order (and read on requests) gets the page.
func TestPUX025_CreatePOPageRights(t *testing.T) {
	w := newP15dWeb(t)
	env := w.env
	pr := w.prA(t)
	denied := (&auth.DeniedError{Right: auth.RightCreate, Label: "Purchase Order", Plural: "Purchase Orders"}).Error()
	if a := w.get(t, env.Suresh, p15dOrderPath(pr.ID)); a.status != http.StatusForbidden || !strings.Contains(a.text(), denied) {
		t.Errorf("Suresh GET = %d %q, want 403 with %q", a.status, a.text(), denied)
	}
	if a := w.get(t, env.Priya, p15dOrderPath(999_999)); a.status != http.StatusNotFound || !strings.Contains(a.text(), auth.MsgNotFound) {
		t.Errorf("missing request GET = %d %q, want 404", a.status, a.text())
	}
	hidden := p15dTechRequest(t, w)
	if a := w.get(t, env.Priya, p15dOrderPath(hidden)); a.status != http.StatusNotFound || !strings.Contains(a.text(), auth.MsgNotFound) {
		t.Errorf("hidden request GET = %d %q, want 404", a.status, a.text())
	}
	creator := w.probe(t, "page-creator", auth.RightCreate)
	if a := w.get(t, creator, p15dOrderPath(pr.ID)); a.status != http.StatusOK {
		t.Errorf("create-only user GET = %d %q, want 200", a.status, a.text())
	}
}

// p15dTechRequest is Ravi's submitted TECH-SITE / PRJ-TECHPARK request, which
// Priya (PRJ-METRO only) can't read.
func p15dTechRequest(t *testing.T, w *p15dWeb) int64 {
	t.Helper()
	env := w.env
	res, err := env.Engine.Do(t.Context(), env.Ravi, purchase.RequestDocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{"site_id": w.id(t, "site", "TECH-SITE"), "project_id": w.id(t, "project", "PRJ-TECHPARK"), "request_date": "2026-10-24"},
		Lines:  []map[string]string{w.row(t, "MS-ANGLE-50", "KGS", "10", "")},
	})
	if err != nil {
		t.Fatalf("save Ravi's TECH-SITE request: %v", err)
	}
	if _, err := env.Engine.Do(t.Context(), env.Ravi, purchase.RequestDocType, res.ID, doctype.ActionSubmit, doc.ActionInput{Version: res.Version}); err != nil {
		t.Fatalf("submit Ravi's TECH-SITE request: %v", err)
	}
	return res.ID
}

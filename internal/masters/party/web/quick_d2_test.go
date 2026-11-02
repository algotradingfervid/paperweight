package partyweb_test

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/party"
)

// No case ID: UI v2 D2 (§5.8, N10, A-18, CD-375). A vendor picker's search
// offers "New vendor "{q}"" to a user with create on Party and Address, with
// the quick-create sheet; POST /m/party/quick saves the party with the
// picker's role and its primary billing address in one write, and answers
// with the picker, the new party chosen.

const (
	quickPath = base + "/quick"
	quickList = "f-vendor_id-list"
	quickForm = "#" + quickList + "-sheet-form"
)

// pickerVals is what a vendor picker's quick slot posts back.
func pickerVals() url.Values {
	return url.Values{
		"pick_id": {"f-vendor_id"}, "pick_label": {"Vendor"}, "pick_required": {"1"},
		"pick_search": {"/pick/Party?role=vendor"}, "pick_name": {"vendor_id"}, "pick_list": {quickList},
	}
}

// saiKrupa is a complete quick form: no GSTIN, so State is typed.
func saiKrupa(kv ...string) url.Values {
	v := with(pickerVals(), "role", "vendor", "quick_code", "SAIKRUPA", "quick_name", "Sai Krupa Sand Suppliers",
		"quick_line1", "Survey 42, Hadapsar", "quick_city", "Pune", "quick_state_code", "27")
	return with(v, kv...)
}

// setRights replaces the Masters Manager's rights on docType.
func (w *world) setRights(t *testing.T, docType string, rights ...auth.Right) {
	t.Helper()
	a, ok := w.h.Env.Kit.Auth.(*auth.Auth)
	if !ok {
		t.Fatalf("Kit.Auth is %T", w.h.Env.Kit.Auth)
	}
	m := map[auth.Right]auth.Scope{}
	for _, r := range rights {
		m[r] = auth.ScopeAll
	}
	if err := a.SetRights(context.Background(), w.h.Env.Admin, "Masters Manager", docType, m); err != nil {
		t.Fatalf("SetRights: %v", err)
	}
}

// newOption is the search's create option text, "" when there is none.
func newOption(r webtest.Response) string {
	return webtest.Squash(r.Doc.Find(`[role="option"][data-quick]`).Text())
}

// A-18: the New option and its sheet show with create on Party and Address,
// whatever the Contact rights, and not without either right.
func TestQuickD2_NewOptionNeedsCreate(t *testing.T) {
	w := newWorld(t)
	r := w.h.GetHTMX(t, w.h.Env.Kavya, "/pick/Party?role=vendor&q=Sai+Krupa")
	webtest.WantStatus(t, r, http.StatusOK)
	if got := newOption(r); got != `New vendor "Sai Krupa"` {
		t.Errorf("New option = %q, want %q", got, `New vendor "Sai Krupa"`)
	}
	if got := webtest.Squash(r.Doc.Find(`.picker__empty-text`).Text()); got != `No vendor matches "Sai Krupa"` {
		t.Errorf("empty state = %q", got)
	}
	sheet := r.Doc.Find(`#pick-party-quick[hx-swap-oob="innerHTML"] dialog#pick-party-sheet`)
	if got := webtest.Squash(sheet.Find("h2").Text()); got != "New vendor" {
		t.Errorf("sheet title = %q, want New vendor", got)
	}
	if got := sheet.Find(`form`).AttrOr("hx-post", ""); got != quickPath {
		t.Errorf("sheet posts to %q, want %s", got, quickPath)
	}
	for name, want := range map[string]string{"role": "vendor", "quick_name": "Sai Krupa", "quick_code": "SAIKRUPA"} {
		if got := sheet.Find(`[name="`+name+`"]`).AttrOr("value", "?"); got != want {
			t.Errorf("sheet %s = %q, want %q", name, got, want)
		}
	}
	var labels []string
	sheet.Find(`label.field__label, legend.field__label`).Each(func(_ int, s *goquery.Selection) { labels = append(labels, webtest.Squash(s.Text())) })
	want := []string{"Code * (required)", "Name * (required)", "GSTIN", "Address line 1 * (required)", "City * (required)", "State * (required)"}
	if !slices.Equal(labels, want) {
		t.Errorf("sheet labels = %q, want %q", labels, want)
	}
	for name, want := range map[string]string{
		"quick_code":  "Suggested from the name. You can change it.",
		"quick_line1": "Saved as their billing address.",
	} {
		if got := webtest.Squash(sheet.Find(`#f-` + name + `-hint`).Text()); got != want {
			t.Errorf("%s hint = %q, want %q", name, got, want)
		}
	}
	if got := sheet.Find(`select[name="quick_state_code"] option`).Length(); got < 30 {
		t.Errorf("State has %d options, want the GST states", got)
	}
	// Rows found: the rows, then the New option last.
	r = w.h.GetHTMX(t, w.h.Env.Kavya, "/pick/Party?role=vendor&q=sh")
	opts := r.Doc.Find(`[role="option"]`)
	if opts.Length() < 2 || opts.Last().AttrOr("data-quick", "") == "" {
		t.Errorf("the New option isn't last after the rows: %d options", opts.Length())
	}
	if got := newOption(w.h.GetHTMX(t, w.h.Env.Kavya, "/pick/Party?role=customer&q=Tata")); got != `New customer "Tata"` {
		t.Errorf("customer New option = %q", got)
	}
	// No role, no quick-create: the party needs one.
	if got := newOption(w.h.GetHTMX(t, w.h.Env.Kavya, "/pick/Party?q=Sai")); got != "" {
		t.Errorf("New option on a search with no role = %q, want none", got)
	}
	// Contact create isn't needed (CD-375).
	w.setRights(t, address.ContactDocType, auth.RightRead)
	if got := newOption(w.h.GetHTMX(t, w.h.Env.Kavya, "/pick/Party?role=vendor&q=Sai")); got == "" {
		t.Error("no New option without Contact create, want one")
	}
	// Read only on Party (Priya): no option, no sheet, the plain no-match row.
	r = w.h.GetHTMX(t, w.h.Env.Priya, "/pick/Party?role=vendor&q=Sai")
	if got := newOption(r); got != "" {
		t.Errorf("New option for Priya = %q, want none", got)
	}
	if r.Doc.Find("dialog").Length() != 0 {
		t.Error("Priya gets the sheet")
	}
	if got := webtest.Squash(r.Doc.Find(`[role="option"]`).Text()); got != "No matches for “Sai”." {
		t.Errorf("Priya's no-match row = %q", got)
	}
	// Party create without Address create: none.
	w.setRights(t, address.DocType, auth.RightRead, auth.RightWrite)
	if got := newOption(w.h.GetHTMX(t, w.h.Env.Kavya, "/pick/Party?role=vendor&q=Sai")); got != "" {
		t.Errorf("New option without Address create = %q, want none", got)
	}
	// Address create without Party create: none.
	w.setRights(t, address.DocType, auth.RightRead, auth.RightCreate)
	w.setRights(t, party.DocType, auth.RightRead)
	if got := newOption(w.h.GetHTMX(t, w.h.Env.Kavya, "/pick/Party?role=vendor&q=Sai")); got != "" {
		t.Errorf("New option without Party create = %q, want none", got)
	}
}

// A-19's write: a vendor and its primary billing address, answered with the
// picker, the new party chosen and Change focused.
func TestQuickD2_SavesVendorAndBillingAddress(t *testing.T) {
	w := newWorld(t)
	r := w.kavya().PostHTMX(t, quickPath, saiKrupa())
	webtest.WantStatus(t, r, http.StatusOK)
	p := w.get(t, "SAIKRUPA")
	if p.Name != "Sai Krupa Sand Suppliers" || !p.IsVendor || p.IsCustomer || p.GSTCategory != "Unregistered" {
		t.Errorf("saved party = %+v", p)
	}
	addrs := w.partyAddresses(t, p.ID)
	if len(addrs) != 1 {
		t.Fatalf("addresses = %+v, want one", addrs)
	}
	a := addrs[0]
	if a.Line1 != "Survey 42, Hadapsar" || a.City != "Pune" || a.StateCode != "27" || a.Country != "India" ||
		!a.IsBilling || !a.PrimaryBilling || a.IsShipping || a.GSTIN != "" {
		t.Errorf("saved address = %+v", a)
	}
	if n := w.count(t, `SELECT count(*) FROM contact WHERE party_id = ?`, p.ID); n != 0 {
		t.Errorf("contacts = %d, want none", n)
	}
	box := r.Doc.Find(`.picker`)
	if box.Length() != 1 || r.Doc.Find(`.field`).Length() != 0 {
		t.Fatalf("answer is not the bare picker\nbody: %s", webtest.Squash(r.Body))
	}
	if got := webtest.Squash(box.Find(`[data-picker-summary]`).Text()); got != "SAIKRUPA Sai Krupa Sand Suppliers" {
		t.Errorf("chosen summary = %q", got)
	}
	if got := box.Find(`input[type="hidden"][name="vendor_id"]`).AttrOr("value", ""); got != strconv.FormatInt(p.ID, 10) {
		t.Errorf("vendor_id = %q, want %d", got, p.ID)
	}
	if _, ok := box.Find(`[data-picker-change]`).Attr("autofocus"); !ok {
		t.Error("Change isn't focused")
	}
	for attr, want := range map[string]string{"id": "f-vendor_id", "aria-label": "Vendor"} {
		if got := box.AttrOr(attr, ""); got != want {
			t.Errorf("picker %s = %q, want %q", attr, got, want)
		}
	}
	if got := box.Find(`input[role="combobox"]`).AttrOr("hx-get", ""); got != "/pick/Party?role=vendor" {
		t.Errorf("search URL = %q", got)
	}
	if slot := r.Doc.Find(`#` + quickList + `-quick[hx-swap-oob="innerHTML"]`); slot.Length() != 1 || slot.Children().Length() != 0 {
		t.Error("the answer doesn't empty the sheet's slot, so the sheet stays open")
	}
}

// With a GSTIN and no State, the State and category come from the GSTIN.
func TestQuickD2_StateFromGSTIN(t *testing.T) {
	w := newWorld(t)
	r := w.kavya().PostHTMX(t, quickPath, saiKrupa("quick_code", "MEHTA", "quick_gstin", gstinMumbai, "quick_state_code", ""))
	webtest.WantStatus(t, r, http.StatusOK)
	p := w.get(t, "MEHTA")
	if p.GSTCategory != "Registered Regular" || p.PAN != panMehta {
		t.Errorf("party = %+v, want Registered Regular with PAN %s", p, panMehta)
	}
	a := w.partyAddresses(t, p.ID)[0]
	if a.StateCode != "27" || a.GSTIN != gstinMumbai || !a.PrimaryBilling {
		t.Errorf("address = %+v", a)
	}
}

// Each refusal saves nothing: rights and a bad role refuse the route; a
// missing field or a taken code redraws the sheet's form with its error.
func TestQuickD2_Refusals(t *testing.T) {
	w := newWorld(t)
	shree := w.get(t, "SHREE")
	parties := func() int { return w.count(t, `SELECT count(*) FROM party`) }
	addresses := func() int { return w.count(t, `SELECT count(*) FROM address`) }
	before, beforeAddr := parties(), addresses()

	webtest.WantStatus(t, w.h.Browser(w.h.Env.Priya).PostHTMX(t, quickPath, saiKrupa()), http.StatusForbidden)
	// The right comes before the role.
	webtest.WantStatus(t, w.h.Browser(w.h.Env.Priya).PostHTMX(t, quickPath, saiKrupa("role", "seller")), http.StatusForbidden)
	for _, role := range []string{"seller", "", "both"} {
		webtest.WantStatus(t, w.kavya().PostHTMX(t, quickPath, saiKrupa("role", role)), http.StatusBadRequest)
	}
	// A role the picker doesn't search by: customer posted from the vendor
	// field, or vendor from a picker with no role.
	webtest.WantStatus(t, w.kavya().PostHTMX(t, quickPath, saiKrupa("role", "customer")), http.StatusBadRequest)
	webtest.WantStatus(t, w.kavya().PostHTMX(t, quickPath, saiKrupa("pick_search", "/pick/Party")), http.StatusBadRequest)
	webtest.WantStatus(t, w.kavya().PostHTMX(t, quickPath, saiKrupa("pick_search", "/pick/%zz?role=vendor")), http.StatusBadRequest)
	webtest.WantStatus(t, w.kavya().PostHTMX(t, quickPath, saiKrupa("role", "customer", "pick_search", "/pick/Party?role=customer")), http.StatusOK)
	w.exec(t, `DELETE FROM address WHERE party_id = (SELECT id FROM party WHERE code = 'SAIKRUPA')`)
	w.exec(t, `DELETE FROM party WHERE code = 'SAIKRUPA'`)
	for _, c := range []struct {
		field, value, msg string
		extra             []string
	}{
		{"quick_name", "", msgNameReq, nil},
		{"quick_line1", "", msgLine1, nil},
		{"quick_city", "", msgCity, nil},
		{"quick_state_code", "", msgState, nil},
		{"quick_code", "", msgCodeReq, nil},
		{"quick_code", "shree", "Code SHREE is already used by Party " + shree.Name + ". Choose another code.", nil},
		{"quick_gstin", gstinBharat, usedByBharat, []string{"quick_state_code", ""}},
	} {
		r := w.kavya().PostHTMX(t, quickPath, saiKrupa(append([]string{c.field, c.value}, c.extra...)...))
		webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
		webtest.WantFieldError(t, r, c.field, c.msg)
		if got := r.Header.Get("HX-Retarget"); got != quickForm {
			t.Errorf("%s: HX-Retarget = %q, want %s", c.field, got, quickForm)
		}
		if got := r.Header.Get("HX-Reswap"); got != "outerHTML" {
			t.Errorf("%s: HX-Reswap = %q, want outerHTML", c.field, got)
		}
		form := r.Doc.Find(quickForm)
		if form.Length() != 1 || r.Doc.Find("dialog").Length() != 0 {
			t.Errorf("%s: the answer isn't the sheet's form alone", c.field)
		}
		if got := form.Find(`[name="quick_city"]`).AttrOr("value", ""); c.field != "quick_city" && got != "Pune" {
			t.Errorf("%s: typed City = %q, want kept", c.field, got)
		}
		if got := form.Find(`[name="role"]`).AttrOr("value", ""); got != "vendor" {
			t.Errorf("%s: role = %q, want kept", c.field, got)
		}
	}
	// The party's and the address's errors show together.
	r := w.kavya().PostHTMX(t, quickPath, saiKrupa("quick_name", "", "quick_line1", ""))
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, r, "quick_name", msgNameReq)
	webtest.WantFieldError(t, r, "quick_line1", msgLine1)
	w.setRights(t, address.DocType, auth.RightRead, auth.RightWrite)
	webtest.WantStatus(t, w.kavya().PostHTMX(t, quickPath, saiKrupa()), http.StatusForbidden)
	w.setRights(t, address.DocType, auth.RightRead, auth.RightCreate)
	w.setRights(t, party.DocType, auth.RightRead)
	webtest.WantStatus(t, w.kavya().PostHTMX(t, quickPath, saiKrupa()), http.StatusForbidden)
	if parties() != before || addresses() != beforeAddr {
		t.Errorf("parties %d→%d, addresses %d→%d after refusals, want no change", before, parties(), beforeAddr, addresses())
	}
}

// The party and its address are one write: an address insert that fails
// leaves no party behind.
func TestQuickD2_AddressFailureLeavesNothing(t *testing.T) {
	w := newWorld(t)
	before := w.count(t, `SELECT count(*) FROM party`)
	w.exec(t, `CREATE TRIGGER uiv2d2_no_address BEFORE INSERT ON address BEGIN SELECT RAISE(ABORT, 'forced'); END`)
	r := w.kavya().PostHTMX(t, quickPath, saiKrupa())
	webtest.WantStatus(t, r, http.StatusInternalServerError)
	if _, ok := w.byCode(t, "SAIKRUPA"); ok {
		t.Error("the party was saved without its address")
	}
	if n := w.count(t, `SELECT count(*) FROM party`); n != before {
		t.Errorf("parties = %d, want %d", n, before)
	}
}

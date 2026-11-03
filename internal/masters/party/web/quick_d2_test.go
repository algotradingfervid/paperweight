package partyweb_test

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
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
	return webtest.Squash(r.Doc.Find(`[role="option"][data-quick] > span`).Text())
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
	sheet := r.Doc.Find(`[data-quick] template dialog#pick-party-sheet`)
	if got := webtest.Squash(sheet.Find("h2").Text()); got != "New vendor" {
		t.Errorf("sheet title = %q, want New vendor", got)
	}
	if got := sheet.Find(`form`).AttrOr("hx-post", ""); got != quickPath {
		t.Errorf("sheet posts to %q, want %s", got, quickPath)
	}
	for name, want := range map[string]string{"role": "vendor", "quick_name": "Sai Krupa", "quick_code": ""} {
		if got := sheet.Find(`[name="`+name+`"]`).AttrOr("value", "?"); got != want {
			t.Errorf("sheet %s = %q, want %q", name, got, want)
		}
	}
	var labels []string
	sheet.Find(`label.field__label, legend.field__label`).Each(func(_ int, s *goquery.Selection) { labels = append(labels, webtest.Squash(s.Text())) })
	if got := sheet.Find(`[name="quick_gstin"]`).AttrOr("hx-post", ""); got != "/ui/validate/gstin-state" {
		t.Errorf("GSTIN checks at %q, want the check that echoes only a GSTIN naming a state", got)
	}
	if got := sheet.Find(`[name="quick_gstin"]`).AttrOr("autocapitalize", ""); got != "characters" {
		t.Errorf("GSTIN autocapitalize = %q, want characters", got)
	}
	if sheet.Find(`div[hidden]:has([name="quick_code"])`).Length() != 1 || sheet.Find(`.quick__given:has([name="quick_state_code"])`).Length() != 1 {
		t.Error("Code stays hidden, and State hides with a GSTIN's echo (CD-388)")
	}
	want := []string{"Code * (required)", "Name * (required)", "GSTIN", "City * (required)", "Address line 1 * (required)", "State * (required)"}
	if !slices.Equal(labels, want) {
		t.Errorf("sheet labels = %q, want %q", labels, want)
	}
	for name, want := range map[string]string{
		"quick_code":  "Made from the name. You can change it.",
		"quick_name":  "The code is made from this. You can change it later in Parties.",
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
	if got := webtest.Squash(r.Doc.Find(`[role="option"]`).Text()); got != `No vendor matches "Sai"` {
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

// R8 (CD-388): the code is made from the name by the existing rule, with the
// next free number when it collides, and stays hidden.
func TestQuickR8_CodeFromTheName(t *testing.T) {
	w := newWorld(t)
	r := w.kavya().PostHTMX(t, quickPath, saiKrupa("quick_code", ""))
	webtest.WantStatus(t, r, http.StatusOK)
	sai := w.get(t, "SAIKRUPASANDSUPPLIER")
	if sai.Name != "Sai Krupa Sand Suppliers" {
		t.Errorf("party = %+v", sai)
	}
	// SHREE exists: a vendor named Shree takes SHREE2.
	r = w.kavya().PostHTMX(t, quickPath, saiKrupa("quick_code", "", "quick_name", "Shree"))
	webtest.WantStatus(t, r, http.StatusOK)
	if got := w.get(t, "SHREE2"); got.Name != "Shree" {
		t.Errorf("collided name: party = %+v", got)
	}
	r = w.kavya().PostHTMX(t, quickPath, saiKrupa("quick_code", "", "quick_name", "Shree"))
	webtest.WantStatus(t, r, http.StatusOK)
	if got := w.get(t, "SHREE3"); got.Name != "Shree" {
		t.Errorf("second collision: party = %+v", got)
	}
}

// A name the rule can't make a code from shows the Code field with its
// refusal, and a code the user then types saves. A blank name asks for the
// name alone: there is nothing to make a code from.
func TestQuickR8_CodeShowsWhenRefused(t *testing.T) {
	w := newWorld(t)
	r := w.kavya().PostHTMX(t, quickPath, saiKrupa("quick_code", "", "quick_name", "श्री साई"))
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, r, "quick_code", msgCodeReq)
	if r.Doc.Find(`div[hidden]:has([name="quick_code"])`).Length() != 0 {
		t.Error("the refused Code stays hidden")
	}
	if got := r.Doc.Find(".error-summary a").First().AttrOr("href", ""); got != "#f-quick_code" {
		t.Errorf("summary link = %q, want the Code field", got)
	}
	webtest.WantStatus(t, w.kavya().PostHTMX(t, quickPath, saiKrupa("quick_code", "SAI", "quick_name", "श्री साई")), http.StatusOK)

	r = w.kavya().PostHTMX(t, quickPath, saiKrupa("quick_code", "", "quick_name", ""))
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, r, "quick_name", msgNameReq)
	if r.Doc.Find(`div[hidden]:has([name="quick_code"])`).Length() != 1 {
		t.Error("a blank name shows the Code field")
	}
	if n := r.Doc.Find(".error-summary li").Length(); n != 1 {
		t.Errorf("%d summary rows, want Name's alone", n)
	}
}

// Another field's refusal leaves the generated code hidden and unposted.
func TestQuickR8_CodeStaysHiddenOnOtherRefusals(t *testing.T) {
	w := newWorld(t)
	r := w.kavya().PostHTMX(t, quickPath, saiKrupa("quick_code", "", "quick_name", "Sai", "quick_gstin", "bad"))
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	if got := r.Doc.Find(`[name="quick_code"]`).AttrOr("value", "x"); got != "" {
		t.Errorf("Code after another field's refusal = %q, want blank: only a refused code shows", got)
	}
}

// A valid GSTIN gives the State: a value left in the hidden field is ignored,
// the redraw keeps the echo that hides State, and a wrong GSTIN doesn't.
func TestQuickR8_GSTINGivesState(t *testing.T) {
	w := newWorld(t)
	echo := `Maharashtra (27)`
	// 29 was chosen before the GSTIN was typed.
	r := w.kavya().PostHTMX(t, quickPath, saiKrupa("quick_code", "MEHTA", "quick_gstin", gstinMumbai, "quick_state_code", "29"))
	webtest.WantStatus(t, r, http.StatusOK)
	if a := w.partyAddresses(t, w.get(t, "MEHTA").ID)[0]; a.StateCode != "27" {
		t.Errorf("state = %q, want the GSTIN's 27", a.StateCode)
	}
	w.exec(t, `DELETE FROM address WHERE party_id = (SELECT id FROM party WHERE code = 'MEHTA')`)
	w.exec(t, `DELETE FROM party WHERE code = 'MEHTA'`)

	r = w.kavya().PostHTMX(t, quickPath, saiKrupa("quick_gstin", gstinMumbai, "quick_state_code", "29", "quick_city", ""))
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	if got := webtest.Squash(r.Doc.Find(`#f-quick_gstin-msg`).Text()); !strings.Contains(got, echo) {
		t.Errorf("GSTIN echo on the redraw = %q, want it to name %s", got, echo)
	}
	if r.Doc.Find(`[name="quick_state_code"] option[selected][value="29"]`).Length() != 0 {
		t.Error("the redraw keeps a State the GSTIN overrides")
	}

	r = w.kavya().PostHTMX(t, quickPath, saiKrupa("quick_gstin", "27AABCS1429B1Z5", "quick_state_code", "29"))
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	if r.Doc.Find(`[name="quick_state_code"] option[selected][value="29"]`).Length() != 1 {
		t.Error("a wrong GSTIN lost the chosen State")
	}
	if got := webtest.Squash(r.Doc.Find(`#f-quick_gstin-msg`).Text()); strings.Contains(got, "Maharashtra") {
		t.Errorf("a wrong GSTIN echoes %q", got)
	}
}

// R8 (UI v2 §5.8): a vendor row shows the primary billing address's GSTIN or
// "No GSTIN", and city; a party without one, a bare name.
func TestPickR8_RowLines(t *testing.T) {
	w := newWorld(t)
	shree := w.get(t, "SHREE")
	line := func(id int64, q string) (string, bool) {
		t.Helper()
		r := w.h.GetHTMX(t, w.h.Env.Kavya, "/pick/Party?role=vendor&q="+q)
		row := r.Doc.Find(`[role="option"][data-value="` + strconv.FormatInt(id, 10) + `"]`)
		return webtest.Squash(row.Find(".picker__line").Text()), row.Length() == 1
	}
	got, ok := line(shree.ID, "Shree")
	addr := w.partyAddresses(t, shree.ID)[0]
	if want := addr.GSTIN + " · " + addr.City; !ok || got != want {
		t.Errorf("row line = %q (found %v), want %q", got, ok, want)
	}
	local := w.get(t, "LOCAL")
	if got, _ := line(local.ID, "Ganesh"); !strings.HasPrefix(got, "No GSTIN · ") {
		t.Errorf("row line without a GSTIN = %q", got)
	}
	// No address at all: a bare name.
	bare := w.create(t, party.Input{Code: "B-BARE", Name: "Bare Party", IsVendor: true})
	if got, ok := line(bare.ID, "Bare"); !ok || got != "" {
		t.Errorf("row without an address: line %q, found %v", got, ok)
	}
	// Disabled billing address: PickLine lists enabled ones.
	w.exec(t, `UPDATE address SET disabled = 1 WHERE party_id = ?`, shree.ID)
	if got, _ := line(shree.ID, "Shree"); got != "" {
		t.Errorf("row line from a disabled address = %q", got)
	}
}

// A reader of Party who may not read Address sees the name and no GSTIN or
// city; read on Address shows it, and read on only their own addresses shows
// it for theirs alone, so the check is on the row.
func TestPickR8_RestrictedReader(t *testing.T) {
	w := newWorld(t)
	shree := w.get(t, "SHREE")
	lineOf := func(code, q string) *goquery.Selection {
		t.Helper()
		id := strconv.FormatInt(w.get(t, code).ID, 10)
		r := w.h.GetHTMX(t, w.h.Env.Kavya, "/pick/Party?role=vendor&q="+q)
		row := r.Doc.Find(`[role="option"][data-value="` + id + `"]`)
		if row.Length() != 1 {
			t.Fatalf("%s: %d rows, want the row: read on Party shows it", code, row.Length())
		}
		return row.Find(".picker__line")
	}
	webtest.WantStatus(t, w.kavya().PostHTMX(t, quickPath, saiKrupa("quick_code", "MINE", "quick_name", "Mine Traders")), http.StatusOK)
	if got := webtest.Squash(lineOf("MINE", "Mine").Text()); got != "No GSTIN · Pune" {
		t.Errorf("line with read on Address = %q", got)
	}
	if lineOf("SHREE", "Shree").Length() != 1 {
		t.Errorf("no line on %s with read on Address", shree.Code)
	}

	w.setRights(t, address.DocType)
	if n := lineOf("SHREE", "Shree").Length(); n != 0 {
		t.Errorf("without read on Address: %d lines", n)
	}

	a, ok := w.h.Env.Kit.Auth.(*auth.Auth)
	if !ok {
		t.Fatalf("Kit.Auth is %T", w.h.Env.Kit.Auth)
	}
	if err := a.SetRights(context.Background(), w.h.Env.Admin, "Masters Manager", address.DocType, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeOwn}); err != nil {
		t.Fatalf("SetRights: %v", err)
	}
	if n := lineOf("SHREE", "Shree").Length(); n != 0 {
		t.Errorf("read on own addresses, someone else's: %d lines", n)
	}
	if n := lineOf("MINE", "Mine").Length(); n != 1 {
		t.Errorf("read on own addresses, their own: %d lines, want 1", n)
	}
}

// Review R8: the line is the primary billing address's even when a shipping
// address and a non-primary billing one come first in ForParty's order.
func TestPickR8_LineIsThePrimaryBilling(t *testing.T) {
	w := newWorld(t)
	p := w.create(t, party.Input{Code: "B-ORDER", Name: "Order Traders", IsVendor: true})
	for _, in := range []address.Input{
		{PartyID: p.ID, Line1: "1 Dock Road", City: "Kolkata", StateCode: "19", IsShipping: true},
		{PartyID: p.ID, Line1: "2 Mall Road", City: "Pune", StateCode: "27", IsBilling: true, PrimaryBilling: true},
		{PartyID: p.ID, Line1: "3 Ring Road", City: "Nagpur", StateCode: "27", IsBilling: true},
	} {
		if _, err := w.addresses.Create(context.Background(), w.h.Env.Kavya, in); err != nil {
			t.Fatalf("address %s: %v", in.City, err)
		}
	}
	// Make the non-primary billing row the oldest too.
	w.exec(t, `UPDATE address SET id = id + 1000 WHERE party_id = ? AND city = 'Pune'`, p.ID)
	if got := w.partyAddresses(t, p.ID); len(got) != 3 || got[0].PrimaryBilling || got[1].PrimaryBilling {
		t.Fatalf("addresses in order %+v, want the primary billing last", got)
	}
	r := w.h.GetHTMX(t, w.h.Env.Kavya, "/pick/Party?role=vendor&q=Order")
	row := r.Doc.Find(`[role="option"][data-value="` + strconv.FormatInt(p.ID, 10) + `"]`)
	if got := webtest.Squash(row.Find(".picker__line").Text()); got != "No GSTIN · Pune" {
		t.Errorf("line = %q, want the primary billing address's", got)
	}
}

package partyweb_test

import (
	"context"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/mstfixture"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// The address and contact pages and actions under a party's record:
//
//	GET, POST /m/party/{id}/addresses/new and /contacts/new
//	GET, POST /m/party/{id}/addresses/{aid} and /contacts/{cid} (save, with version)
//	POST .../{aid or cid}/disable, /enable, /delete (with version)
//	POST .../addresses/{aid}/set-primary (kind=billing or shipping, with version)
//	POST .../contacts/{cid}/set-primary (with version)
//
// Each answers 303 to the record page, with the record's own banner. A form's
// refusal is a 422 on that form, typed values kept; an action's is on the
// record page. A form names its controls by the DocType's fields. A contact's
// lists post as emails[N].email, emails[N].primary, phones[N].number,
// phones[N].mobile and phones[N].primary.

// kolkata is an address SHREE doesn't have: a shipping address with no GSTIN.
func kolkata(partyID int64) address.Input {
	return address.Input{PartyID: partyID, Title: "Kolkata", Line1: "1 Park Street", City: "Kolkata", StateCode: "19", Pincode: "700016", IsShipping: true}
}

func flag(b bool) string {
	if b {
		return "1"
	}
	return ""
}

// addressForm is a stored address as its form posts it.
func addressForm(a address.Address) url.Values {
	return url.Values{
		"version": {strconv.Itoa(a.Version)}, "title": {a.Title}, "line1": {a.Line1}, "line2": {a.Line2}, "city": {a.City},
		"state_code": {a.StateCode}, "country": {a.Country}, "pincode": {a.Pincode}, "gstin": {a.GSTIN},
		"is_billing": {flag(a.IsBilling)}, "is_shipping": {flag(a.IsShipping)},
		"is_primary_billing": {flag(a.PrimaryBilling)}, "is_primary_shipping": {flag(a.PrimaryShipping)},
	}
}

// contactForm is a stored contact as its form posts it.
func contactForm(c address.Contact) url.Values {
	v := url.Values{
		"version": {strconv.Itoa(c.Version)}, "first_name": {c.FirstName}, "middle_name": {c.MiddleName}, "last_name": {c.LastName},
		"is_primary": {flag(c.Primary)},
	}
	for i, e := range c.Emails {
		n := strconv.Itoa(i + 1)
		v.Set("emails["+n+"].email", e.Email)
		v.Set("emails["+n+"].primary", flag(e.Primary))
	}
	for i, ph := range c.Phones {
		n := strconv.Itoa(i + 1)
		v.Set("phones["+n+"].number", ph.Number)
		v.Set("phones["+n+"].mobile", flag(ph.Mobile))
		v.Set("phones["+n+"].primary", flag(ph.Primary))
	}
	return v
}

// addContact makes a contact with one email and one mobile.
func (w *world) addContact(t *testing.T, partyID int64, first, last string, primary bool) int64 {
	t.Helper()
	saved, err := w.addresses.CreateContact(context.Background(), w.h.Env.Kavya, address.ContactInput{
		PartyID: partyID, FirstName: first, LastName: last, Primary: primary,
		Emails: []address.EmailInput{{Email: strings.ToLower(first) + "@shreesteel.test", Primary: true}},
		Phones: []address.PhoneInput{{Number: "9820012345", Mobile: true, Primary: true}},
	})
	if err != nil {
		t.Fatalf("create contact %s: %v", first, err)
	}
	return saved.ID
}

func (w *world) disableAddress(t *testing.T, id int64) {
	t.Helper()
	a := w.address(t, id)
	if err := w.addresses.SetDisabled(context.Background(), w.h.Env.Kavya, id, a.Version, true); err != nil {
		t.Fatalf("disable address %d: %v", id, err)
	}
}

func (w *world) disableContact(t *testing.T, id int64) {
	t.Helper()
	c := w.contact(t, id)
	if err := w.addresses.SetContactDisabled(context.Background(), w.h.Env.Kavya, id, c.Version, true); err != nil {
		t.Fatalf("disable contact %d: %v", id, err)
	}
}

func versionOf(v int) url.Values { return url.Values{"version": {strconv.Itoa(v)}} }

// The record page lists addresses and contacts with their actions, disabled
// ones too, each with its own version.
func TestScreens_PartyRecordPage(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	shree := w.get(t, "SHREE")
	sunil := w.addContact(t, shree.ID, "Sunil", "Mehta", true)
	kiran := w.addContact(t, shree.ID, "Kiran", "Joshi", false)
	w.disableContact(t, kiran)
	w.disableAddress(t, w.m.Addresses["Delhi"])

	page := b.Get(t, recordURL(shree.ID))
	webtest.WantStatus(t, page, http.StatusOK)
	for _, text := range []string{
		"Addresses", "Mumbai", "27AABCS1429B1ZU", "Primary billing", "Thane", "Delhi", "Disabled",
		"Contacts", "Sunil Mehta", "sunil@shreesteel.test", "+919820012345", "Kiran Joshi",
	} {
		webtest.WantText(t, page, text)
	}
	if _, found := page.RowLink(newAddressURL(shree.ID)); !found {
		t.Errorf("no link to add an address")
	}
	if _, found := page.RowLink(newContactURL(shree.ID)); !found {
		t.Errorf("no link to add a contact")
	}
	for _, target := range []string{addressURL(shree.ID, w.m.Addresses["Mumbai"]), addressURL(shree.ID, w.m.Addresses["Delhi"]), contactURL(shree.ID, sunil), contactURL(shree.ID, kiran)} {
		if _, found := page.RowLink(target); !found {
			t.Errorf("no link to %s\ntext: %s", target, page.Text())
		}
	}
	// Each row's action forms carry that row's own version.
	thane := w.address(t, w.m.Addresses["Thane"])
	delhi := w.address(t, w.m.Addresses["Delhi"])
	if got, _ := page.FormValue(addressURL(shree.ID, thane.ID)+"/disable", "version"); got != strconv.Itoa(thane.Version) {
		t.Errorf("Thane's disable version = %q; want %d", got, thane.Version)
	}
	if got, _ := page.FormValue(addressURL(shree.ID, delhi.ID)+"/enable", "version"); got != strconv.Itoa(delhi.Version) {
		t.Errorf("Delhi's enable version = %q; want %d", got, delhi.Version)
	}
	c := w.contact(t, kiran)
	if got, _ := page.FormValue(contactURL(shree.ID, kiran)+"/enable", "version"); got != strconv.Itoa(c.Version) {
		t.Errorf("Kiran's enable version = %q; want %d", got, c.Version)
	}
}

// Add, edit, disable, enable, set-primary and delete an address from the record
// page: exact versions and flags, nothing promoted by a disable (CD-180 D3).
func TestScreens_AddressActions(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	shree := w.get(t, "SHREE")
	id := shree.ID

	page := b.Get(t, newAddressURL(id))
	webtest.WantStatus(t, page, http.StatusOK)
	wantAddressInputs(t, page, "gstin", "pincode", "state_code")
	for _, name := range []string{"title", "line1", "line2", "city", "country", "is_billing", "is_shipping", "is_primary_billing", "is_primary_shipping"} {
		if !page.HasControl(name) {
			t.Errorf("the form lacks %s", name)
		}
	}
	form := addressForm(address.Address{Title: "Kolkata", Line1: "1 Park Street", Line2: "Near the Museum", City: "Kolkata", StateCode: "19", Pincode: "700016", IsShipping: true})
	form.Del("version")
	added := b.Post(t, newAddressURL(id), form)
	webtest.WantRedirect(t, added, recordURL(id))
	webtest.WantBanner(t, b.Get(t, added.Location), "Address saved.")
	all := w.partyAddresses(t, id)
	if len(all) != 4 {
		t.Fatalf("addresses = %+v", all)
	}
	k := all[3]
	if k.Title != "Kolkata" || k.Line1 != "1 Park Street" || k.Line2 != "Near the Museum" || k.City != "Kolkata" ||
		k.StateCode != "19" || k.Country != "India" || k.Pincode != "700016" || !k.IsShipping || k.IsBilling || k.PrimaryShipping {
		t.Errorf("added %+v", k)
	}

	// Every Input field survives the form, and the version moves by one.
	edit := b.Get(t, addressURL(id, k.ID))
	webtest.WantStatus(t, edit, http.StatusOK)
	value(t, edit, "line1", "1 Park Street")
	if got, _ := edit.Value("state_code"); got != "19" {
		t.Errorf("state = %q", got)
	}
	if got, _ := edit.FormValue(addressURL(id, k.ID), "version"); got != strconv.Itoa(k.Version) {
		t.Errorf("hidden version = %q", got)
	}
	webtest.WantRedirect(t, b.Post(t, addressURL(id, k.ID), with(formValues(t, edit, addressURL(id, k.ID)), "city", "Howrah", "pincode", "711101")), recordURL(id))
	got := w.address(t, k.ID)
	want := k
	want.City, want.Pincode, want.Version = "Howrah", "711101", k.Version+1
	if got != want {
		t.Errorf("after edit %+v; want %+v", got, want)
	}

	// MST-060: Delhi ticked billing, then made primary: Mumbai's flag clears.
	delhi := w.address(t, w.m.Addresses["Delhi"])
	webtest.WantRedirect(t, b.Post(t, addressURL(id, delhi.ID), with(addressForm(delhi), "is_billing", "1")), recordURL(id))
	delhi = w.address(t, delhi.ID)
	mumbai := w.address(t, w.m.Addresses["Mumbai"])
	promoted := b.Post(t, addressURL(id, delhi.ID)+"/set-primary", with(versionOf(delhi.Version), "kind", "billing"))
	webtest.WantRedirect(t, promoted, recordURL(id))
	if a := w.address(t, delhi.ID); !a.PrimaryBilling || a.Version != delhi.Version+1 {
		t.Errorf("Delhi after set-primary: %+v", a)
	}
	if a := w.address(t, mumbai.ID); a.PrimaryBilling {
		t.Errorf("Mumbai is still primary billing")
	}
	if n := w.count(t, `SELECT count(*) FROM address WHERE party_id = ? AND is_primary_billing = 1`, id); n != 1 {
		t.Errorf("primary billing addresses = %d", n)
	}
	k = w.address(t, k.ID)
	webtest.WantRedirect(t, b.Post(t, addressURL(id, k.ID)+"/set-primary", with(versionOf(k.Version), "kind", "shipping")), recordURL(id))
	if a := w.address(t, k.ID); !a.PrimaryShipping {
		t.Errorf("Kolkata isn't primary shipping: %+v", a)
	}
	if a := w.address(t, w.m.Addresses["Thane"]); a.PrimaryShipping {
		t.Errorf("Thane is still primary shipping")
	}

	// A primary shipping address may go: its flag clears and nothing is promoted.
	k = w.address(t, k.ID)
	off := b.Post(t, addressURL(id, k.ID)+"/disable", versionOf(k.Version))
	webtest.WantRedirect(t, off, recordURL(id))
	webtest.WantBanner(t, b.Get(t, off.Location), "Address disabled.")
	k = w.address(t, k.ID)
	if !k.Disabled || k.PrimaryShipping {
		t.Errorf("after disable %+v", k)
	}
	if n := w.count(t, `SELECT count(*) FROM address WHERE party_id = ? AND is_primary_shipping = 1`, id); n != 0 {
		t.Errorf("a disable promoted a shipping address: %d primaries", n)
	}
	on := b.Post(t, addressURL(id, k.ID)+"/enable", versionOf(k.Version))
	webtest.WantRedirect(t, on, recordURL(id))
	webtest.WantBanner(t, b.Get(t, on.Location), "Address enabled.")
	k = w.address(t, k.ID)
	if k.Disabled {
		t.Errorf("after enable %+v", k)
	}

	gone := b.Post(t, addressURL(id, k.ID)+"/delete", versionOf(k.Version))
	webtest.WantRedirect(t, gone, recordURL(id))
	webtest.WantBanner(t, b.Get(t, gone.Location), "Address deleted.")
	if n := w.count(t, `SELECT count(*) FROM address WHERE id = ?`, k.ID); n != 0 {
		t.Errorf("the address is still there")
	}
}

// MST-059 through the screen: a party's first address of a type is primary.
func TestScreens_AddressFirstIsPrimary(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	p := w.create(t, party.Input{Code: "V-NEW", Name: "New Vendor", IsVendor: true})
	form := addressForm(address.Address{Title: "Kolkata", Line1: "1 Park Street", City: "Kolkata", StateCode: "19", Pincode: "700016", IsBilling: true})
	form.Del("version")
	webtest.WantRedirect(t, b.Post(t, newAddressURL(p.ID), form), recordURL(p.ID))
	if all := w.partyAddresses(t, p.ID); len(all) != 1 || !all[0].PrimaryBilling {
		t.Errorf("addresses = %+v", all)
	}
}

// An address save's warnings reach the record page (MST-045), and a GSTIN
// back-fills the party (MST-049).
func TestScreens_AddressWarning(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	p := w.create(t, party.Input{Code: "V-NEW", Name: "New Vendor", IsVendor: true})
	form := addressForm(address.Address{Title: "Bengaluru", Line1: "4 Residency Road", City: "Bengaluru", StateCode: "29", Pincode: "560001", GSTIN: gstinShree, IsBilling: true})
	form.Del("version")
	saved := b.Post(t, newAddressURL(p.ID), form)
	webtest.WantRedirect(t, saved, recordURL(p.ID))
	page := b.Get(t, saved.Location)
	wantAlert(t, page, "Party SHREE has the same PAN AABCS1429B. You may be creating a duplicate; consider adding a Karnataka address to SHREE.")
	webtest.WantBanner(t, page, "Address saved.")
	if got := w.get(t, "V-NEW"); got.PAN != "AABCS1429B" || got.GSTCategory != gstin.Regular {
		t.Errorf("party not back-filled: %+v", got)
	}
}

// The refusals the Address service names, each through the form, on create and
// on update, by exact text: next to the field when the form has it, else in the
// banner.
func TestScreens_AddressRefusals(t *testing.T) {
	const (
		create = "create"
		update = "update"
	)
	type refusal struct {
		name   string
		only   string // create or update; blank is both
		party  string // blank is SHREE
		target string // the address updated, by place; blank is Thane
		setup  func(t *testing.T, w *world)
		kv     []string
		field  string // blank is the banner
		msg    string
	}
	maharashtra := []string{"state_code", "27", "pincode", "400001"}
	cases := []refusal{
		{name: "line 1", kv: []string{"line1", ""}, field: "line1", msg: msgLine1},
		{name: "city", kv: []string{"city", ""}, field: "city", msg: msgCity},
		{name: "state", kv: []string{"state_code", "", "gstin", ""}, field: "state_code", msg: msgState},
		{name: "type", kv: []string{"is_billing", "", "is_shipping", "", "is_primary_shipping", ""}, field: "is_billing", msg: msgType},
		{name: "pincode", kv: []string{"pincode", "40001"}, field: "pincode", msg: msgPincode},
		{name: "pincode's state", kv: []string{"state_code", "27", "gstin", "", "pincode", "500001"}, field: "pincode", msg: "Pincode 500001 is not in Maharashtra. Check the pincode or the state."},
		{name: "GSTIN check digit", kv: append([]string{"gstin", "27AABCS1429B1Z5"}, maharashtra...), field: "gstin", msg: "GSTIN 27AABCS1429B1Z5 is not valid: the last character doesn't match. Check for typing mistakes."},
		{name: "GSTIN's state", kv: append([]string{"gstin", "07AABCS1429B1ZW"}, maharashtra...), field: "gstin", msg: "GSTIN 07AABCS1429B1ZW is for Delhi (07) but the address state is Maharashtra (27). Fix the GSTIN or the state."},
		{name: "MST-047 outside India", kv: []string{"country", "Singapore", "state_code", "", "pincode", "118524", "gstin", ""}, field: "country", msg: msgOverseas},
		{name: "MST-048 another PAN", kv: append([]string{"gstin", gstinMumbai}, maharashtra...), field: "gstin", msg: "GSTIN " + gstinMumbai + " belongs to PAN " + panMehta + ", but this party's PAN is AABCS1429B. Use a separate party for a different business."},
		{name: "CD-176 another party's GSTIN", kv: []string{"gstin", gstinBharat, "state_code", "29", "pincode", "560001"}, field: "gstin", msg: usedByBharat},
		{
			name: "MST-044 a disabled party's GSTIN", kv: []string{"gstin", gstinRajkot, "state_code", "24", "pincode", "360001"}, field: "gstin", msg: offRajkot,
			setup: func(t *testing.T, w *world) { t.Helper(); w.disable(t, "RAJKOT") },
		},
		{name: "MST-034 a primary billing address needs a GSTIN", kv: []string{"is_billing", "1", "is_primary_billing", "1", "gstin", ""}, field: "gstin", msg: msgNeedGSTIN},
		{name: "CD-177 unticking primary billing", only: update, target: "Mumbai", kv: []string{"is_billing", "", "is_primary_billing", "", "is_shipping", "1"}, field: "is_billing", msg: msgKeepBill},
		{name: "the primary billing address's GSTIN", only: update, target: "Mumbai", kv: []string{"gstin", ""}, field: "gstin", msg: msgNeedGSTIN},
		{
			name: "CD-180 D2 a disabled address made primary", only: update, kv: []string{"is_primary_shipping", "1"}, msg: msgEnableAddr,
			setup: func(t *testing.T, w *world) { t.Helper(); w.disableAddress(t, w.m.Addresses["Thane"]) },
		},
		{
			name: "CD-180 D1 no billing address yet", only: create, party: "SHIP", kv: nil, field: "is_billing", msg: msgNeedBill,
			setup: func(t *testing.T, w *world) {
				t.Helper()
				w.create(t, party.Input{Code: "SHIP", Name: "Ship Only", IsVendor: true, GSTCategory: gstin.Regular})
			},
		},
		{name: "MST-034 unregistered with a GSTIN", only: create, party: "LOCAL", kv: append([]string{"is_billing", "1", "gstin", gstinMumbai}, maharashtra...), field: "gstin", msg: msgUnreg},
		{
			name: "a disabled party", only: create, party: "RAJKOT", msg: "Party RAJKOT is disabled. Pick another party.",
			setup: func(t *testing.T, w *world) { t.Helper(); w.disable(t, "RAJKOT") },
		},
	}
	for _, c := range cases {
		for _, mode := range []string{create, update} {
			if c.only != "" && c.only != mode {
				continue
			}
			t.Run(c.name+" on "+mode, func(t *testing.T) {
				w := newWorld(t)
				b := w.kavya()
				if c.setup != nil {
					c.setup(t, w)
				}
				code := c.party
				if code == "" {
					code = "SHREE"
				}
				id := w.get(t, code).ID
				before := w.tally(t)
				var form url.Values
				target := newAddressURL(id)
				var stored address.Address
				if mode == create {
					form = addressForm(address.Address{Title: "Kolkata", Line1: "1 Park Street", City: "Kolkata", StateCode: "19", Pincode: "700016", IsShipping: true})
					form.Del("version")
				} else {
					place := c.target
					if place == "" {
						place = "Thane"
					}
					stored = w.address(t, w.m.Addresses[place])
					form = addressForm(stored)
					target = addressURL(id, stored.ID)
				}
				form = with(form, "title", "Typed title")
				form = with(form, c.kv...)
				r := b.Post(t, target, form)
				webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
				if c.field == "" {
					webtest.WantBanner(t, r, c.msg)
				} else {
					webtest.WantFieldErrorID(t, r, components.FieldID(c.field), c.msg)
				}
				value(t, r, "title", "Typed title")
				w.wantTally(t, before)
				if mode == update {
					if got := w.address(t, stored.ID); got != stored {
						t.Errorf("a refused save wrote: %+v", got)
					}
					if got, _ := r.FormValue(target, "version"); got != strconv.Itoa(stored.Version) {
						t.Errorf("hidden version = %q; want the posted %d", got, stored.Version)
					}
				}
			})
		}
	}
}

// The address and contact lists are Address's and Contact's to show: a Party
// reader without the list's read right gets the page without that section, its
// rows or their controls. No other right can stand without read, so a Party
// writer loses them too. The other list stays.
func TestScreens_PartyRecordHidesListsWithoutRead(t *testing.T) {
	addresses := []string{"Mumbai", "Thane", "Delhi", "27AABCS1429B1ZU", "27AABCS1429B2ZT", "07AABCS1429B1ZW"}
	contacts := []string{"Sunil Mehta", "sunil@shreesteel.test", "9820012345"}
	for _, c := range []struct {
		name, docType, heading, route string
		hidden                        []string
		otherHeading                  string
		other                         []string
	}{
		{"no Address read", address.DocType, "Addresses", "/addresses", addresses, "Contacts", contacts},
		{"no Contact read", address.ContactDocType, "Contacts", "/contacts", contacts, "Addresses", addresses},
	} {
		for _, reader := range []string{"Priya", "Kavya"} {
			t.Run(c.name+": "+reader, func(t *testing.T) {
				w := newWorld(t)
				ctx := context.Background()
				env := w.h.Env
				shree := w.get(t, "SHREE")
				w.addContact(t, shree.ID, "Sunil", "Mehta", true)

				a := env.Kit.Auth.(*auth.Auth)
				for _, role := range []string{"Masters Manager", "Purchase User", "Employee"} {
					if err := a.SetRights(ctx, env.Admin, role, c.docType, map[auth.Right]auth.Scope{}); err != nil {
						t.Fatal(err)
					}
				}
				user := env.Priya
				if reader == "Kavya" {
					user = env.Kavya
				}
				if err := env.Kit.Auth.Can(ctx, user, party.DocType, auth.RightRead, nil); err != nil {
					t.Fatalf("setup: %s can't read Party: %v", reader, err)
				}
				if err := env.Kit.Auth.Can(ctx, user, c.docType, auth.RightRead, nil); err == nil {
					t.Fatalf("setup: %s can still read %s", reader, c.docType)
				}

				page := w.h.Browser(user).Get(t, recordURL(shree.ID))
				webtest.WantStatus(t, page, http.StatusOK)
				webtest.WantText(t, page, "SHREE Shree Steel Traders")
				webtest.WantNoText(t, page, c.heading)
				for _, text := range c.hidden {
					if strings.Contains(page.Body, text) {
						t.Errorf("the page holds %q", text)
					}
				}
				if target := recordURL(shree.ID) + c.route; strings.Contains(page.Body, target) {
					t.Errorf("the page holds a control on %s\nbody: %s", target, webtest.Squash(page.Body))
				}
				webtest.WantNoText(t, page, map[string]string{"/addresses": "Add address", "/contacts": "Add contact"}[c.route])

				webtest.WantText(t, page, c.otherHeading)
				for _, text := range c.other {
					webtest.WantText(t, page, text)
				}
			})
		}
	}
}

// A kind other than billing or shipping is a bad form: 400, nothing written.
// The address is neither primary and passes for both kinds, so a kind taken
// for either one would write.
func TestScreens_AddressSetPrimaryBadKind(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	id := w.get(t, "SHREE").ID
	yard, err := w.addresses.Create(context.Background(), w.h.Env.Kavya, address.Input{
		PartyID: id, Title: "Yard", Line1: "4 Residency Road", City: "Bengaluru", StateCode: "29", Pincode: "560001",
		GSTIN: gstinShree, IsBilling: true, IsShipping: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	a := w.address(t, yard.ID)
	mumbai := w.address(t, w.m.Addresses["Mumbai"])
	thane := w.address(t, w.m.Addresses["Thane"])
	before := w.tally(t)
	target := addressURL(id, a.ID) + "/set-primary"

	for _, kind := range []string{"", " ", "both", "primary", "Billing", "SHIPPING", "billing ", "billing,shipping"} {
		t.Run(strconv.Quote(kind), func(t *testing.T) {
			webtest.WantStatus(t, b.Post(t, target, with(versionOf(a.Version), "kind", kind)), http.StatusBadRequest)
		})
	}
	t.Run("no kind", func(t *testing.T) {
		webtest.WantStatus(t, b.Post(t, target, versionOf(a.Version)), http.StatusBadRequest)
	})

	for _, want := range []address.Address{a, mumbai, thane} {
		if got := w.address(t, want.ID); got != want {
			t.Errorf("a bad kind wrote: %+v; want %+v", got, want)
		}
	}
	w.wantTally(t, before)

	// The same post with a good kind goes through.
	webtest.WantRedirect(t, b.Post(t, target, with(versionOf(a.Version), "kind", "shipping")), recordURL(id))
	if got := w.address(t, a.ID); !got.PrimaryShipping {
		t.Errorf("after a good kind %+v", got)
	}
}

// The refusals of an address's actions show on the record page.
func TestScreens_AddressActionRefusals(t *testing.T) {
	t.Run("CD-177 a registered party's primary billing address stays", func(t *testing.T) {
		w := newWorld(t)
		b := w.kavya()
		id := w.get(t, "SHREE").ID
		mumbai := w.address(t, w.m.Addresses["Mumbai"])
		for _, action := range []string{"/disable", "/delete"} {
			r := b.Post(t, addressURL(id, mumbai.ID)+action, versionOf(mumbai.Version))
			webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
			webtest.WantBanner(t, r, msgKeepBill)
			webtest.WantText(t, r, "SHREE Shree Steel Traders")
		}
		if got := w.address(t, mumbai.ID); got != mumbai {
			t.Errorf("a refused action wrote: %+v", got)
		}
	})

	t.Run("CD-180 D3 an unregistered party's may go", func(t *testing.T) {
		w := newWorld(t)
		b := w.kavya()
		id := w.get(t, "LOCAL").ID
		pune := w.address(t, w.m.Addresses["Pune"])
		webtest.WantRedirect(t, b.Post(t, addressURL(id, pune.ID)+"/disable", versionOf(pune.Version)), recordURL(id))
		if got := w.address(t, pune.ID); !got.Disabled || got.PrimaryBilling {
			t.Errorf("after disable %+v", got)
		}
	})

	t.Run("CD-180 D4 a stale page hears the conflict first", func(t *testing.T) {
		w := newWorld(t)
		b := w.kavya()
		id := w.get(t, "SHREE").ID
		mumbai := w.address(t, w.m.Addresses["Mumbai"])
		for _, action := range []string{"/disable", "/delete"} {
			r := b.Post(t, addressURL(id, mumbai.ID)+action, versionOf(mumbai.Version-1))
			webtest.WantStatus(t, r, http.StatusConflict)
			webtest.WantBanner(t, r, staleMumbai)
		}
	})

	t.Run("set-primary", func(t *testing.T) {
		w := newWorld(t)
		b := w.kavya()
		id := w.get(t, "SHREE").ID
		w.disableAddress(t, w.m.Addresses["Delhi"])
		// A billing address with no GSTIN can't be a registered party's primary.
		noGSTIN, err := w.addresses.Create(context.Background(), w.h.Env.Kavya, address.Input{
			PartyID: id, Title: "Kolkata", Line1: "1 Park Street", City: "Kolkata", StateCode: "19", Pincode: "700016", IsBilling: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range []struct {
			name, kind string
			place      int64
			msg        string
		}{
			{"Billing not ticked", "billing", w.m.Addresses["Thane"], msgTickBill},
			{"Shipping not ticked", "shipping", w.m.Addresses["Mumbai"], msgTickShip},
			{"disabled", "billing", w.m.Addresses["Delhi"], msgEnableAddr},
			{"no GSTIN", "billing", noGSTIN.ID, msgNeedGSTIN},
		} {
			t.Run(c.name, func(t *testing.T) {
				a := w.address(t, c.place)
				r := b.Post(t, addressURL(id, a.ID)+"/set-primary", with(versionOf(a.Version), "kind", c.kind))
				webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
				webtest.WantBanner(t, r, c.msg)
				if got := w.address(t, a.ID); got != a {
					t.Errorf("a refused action wrote: %+v", got)
				}
			})
		}
		thane := w.address(t, w.m.Addresses["Thane"])
		stale := b.Post(t, addressURL(id, thane.ID)+"/set-primary", with(versionOf(thane.Version-1), "kind", "shipping"))
		webtest.WantStatus(t, stale, http.StatusConflict)
		webtest.WantBanner(t, stale, staleThane)
	})

	t.Run("no version is a bad form", func(t *testing.T) {
		w := newWorld(t)
		b := w.kavya()
		id := w.get(t, "SHREE").ID
		thane := w.m.Addresses["Thane"]
		for _, action := range []string{"", "/disable", "/enable", "/delete", "/set-primary"} {
			webtest.WantStatus(t, b.Post(t, addressURL(id, thane)+action, url.Values{"kind": {"shipping"}}), http.StatusBadRequest)
		}
	})
}

// An address on a document can't be deleted, nor its party: the service's text
// on the record page (MST-014, MST-046).
func TestScreens_UsedAddressCantBeDeleted(t *testing.T) {
	const (
		usedAddress = "Cannot delete Address Thane: it is used in Draft 1. Disable it instead."
		usedParty   = "Cannot delete Party SHREE: it is used in Draft 1. Disable it instead."
	)
	w := newWorld(t)
	b := w.kavya()
	shree := w.get(t, "SHREE")
	thane := w.address(t, w.m.Addresses["Thane"])
	if _, err := mstfixture.Save(context.Background(), w.h.Env, w.h.Env.Kavya, 0, mstfixture.Header{
		VendorID: shree.ID, AddressID: thane.ID, BranchID: w.m.Branches["PUNE"], ProjectID: w.m.Projects["PRJ-METRO"],
		Date: types.Date{Year: 2026, Month: time.October, Day: 24},
	}, nil); err != nil {
		t.Fatal(err)
	}
	before := w.tally(t)
	r := b.Post(t, addressURL(shree.ID, thane.ID)+"/delete", versionOf(thane.Version))
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	webtest.WantBanner(t, r, usedAddress)
	r = b.Post(t, recordURL(shree.ID)+"/delete", versionOf(shree.Version))
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	webtest.WantBanner(t, r, usedParty)
	w.wantTally(t, before)
}

// A stale address form is DOC's 409 with the typed values kept, and an address
// keeps its own version: saving it moves neither the party's nor a sibling's.
func TestScreens_AddressKeepsItsOwnVersion(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	shree := w.get(t, "SHREE")
	thane := w.address(t, w.m.Addresses["Thane"])
	mumbai := w.address(t, w.m.Addresses["Mumbai"])

	stale := b.Post(t, addressURL(shree.ID, thane.ID), with(addressForm(thane), "version", strconv.Itoa(thane.Version-1), "line1", "Typed line"))
	webtest.WantStatus(t, stale, http.StatusConflict)
	webtest.WantBanner(t, stale, staleThane)
	value(t, stale, "line1", "Typed line")
	if got, _ := stale.FormValue(addressURL(shree.ID, thane.ID), "version"); got != strconv.Itoa(thane.Version-1) {
		t.Errorf("hidden version = %q; want the posted %d", got, thane.Version-1)
	}
	if got := w.address(t, thane.ID); got != thane {
		t.Errorf("a stale save wrote: %+v", got)
	}

	webtest.WantRedirect(t, b.Post(t, addressURL(shree.ID, thane.ID), with(addressForm(thane), "line1", "New line")), recordURL(shree.ID))
	if got := w.address(t, thane.ID); got.Line1 != "New line" || got.Version != thane.Version+1 {
		t.Errorf("after save %+v", got)
	}
	if got := w.get(t, "SHREE"); got.Version != shree.Version {
		t.Errorf("party version = %d; want %d", got.Version, shree.Version)
	}
	if got := w.address(t, mumbai.ID); got.Version != mumbai.Version {
		t.Errorf("sibling version = %d; want %d", got.Version, mumbai.Version)
	}

	// A party section's save moves no address.
	target, form := sectionForm(t, b, shree.ID, "name", "name", "Shree Steels")
	webtest.WantRedirect(t, b.Post(t, target, form), recordURL(shree.ID))
	if got := w.address(t, mumbai.ID); got.Version != mumbai.Version {
		t.Errorf("a party save moved address version to %d", got.Version)
	}
}

// Add, edit, disable, enable, set-primary and delete a contact from the record
// page. A form's save keeps every email and phone, by what the form renders.
func TestScreens_ContactActions(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	b := w.kavya()
	id := w.get(t, "SHREE").ID

	page := b.Get(t, newContactURL(id))
	webtest.WantStatus(t, page, http.StatusOK)
	for _, name := range []string{"first_name", "middle_name", "last_name", "is_primary"} {
		if !page.HasControl(name) {
			t.Errorf("the form lacks %s", name)
		}
	}
	added := b.Post(t, newContactURL(id), url.Values{
		"first_name": {"Sunil"}, "middle_name": {"K"}, "last_name": {"Mehta"}, "is_primary": {"1"},
		"emails[1].email": {"Accounts@ShreeSteel.test"}, "emails[1].primary": {"1"},
		"phones[1].number": {"98200 12345"}, "phones[1].mobile": {"1"}, "phones[1].primary": {"1"},
	})
	webtest.WantRedirect(t, added, recordURL(id))
	webtest.WantBanner(t, b.Get(t, added.Location), "Contact saved.")
	all := w.partyContacts(t, id)
	if len(all) != 1 {
		t.Fatalf("contacts = %+v", all)
	}
	sunil := all[0]
	if sunil.FirstName != "Sunil" || sunil.MiddleName != "K" || sunil.LastName != "Mehta" || !sunil.Primary ||
		len(sunil.Emails) != 1 || sunil.Emails[0].Email != "accounts@shreesteel.test" ||
		len(sunil.Phones) != 1 || sunil.Phones[0] != (address.PhoneInput{Number: "+919820012345", Mobile: true, Primary: true}) {
		t.Errorf("added %+v", sunil)
	}

	// Two emails and two phones through the service; the form keeps them all.
	saved, err := w.addresses.UpdateContact(ctx, w.h.Env.Kavya, sunil.ID, sunil.Version, address.ContactInput{
		FirstName: "Sunil", MiddleName: "K", LastName: "Mehta", Primary: true,
		Emails: []address.EmailInput{{Email: "accounts@shreesteel.test"}, {Email: "sunil@shreesteel.test", Primary: true}},
		Phones: []address.PhoneInput{{Number: "+919820012345", Mobile: true}, {Number: "022-2345678", Primary: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	sunil = w.contact(t, saved.ID)
	edit := b.Get(t, contactURL(id, sunil.ID))
	webtest.WantStatus(t, edit, http.StatusOK)
	if got, _ := edit.FormValue(contactURL(id, sunil.ID), "version"); got != strconv.Itoa(sunil.Version) {
		t.Errorf("hidden version = %q", got)
	}
	webtest.WantRedirect(t, b.Post(t, contactURL(id, sunil.ID), with(formValues(t, edit, contactURL(id, sunil.ID)), "last_name", "Shah")), recordURL(id))
	got := w.contact(t, sunil.ID)
	want := sunil
	want.LastName, want.Version = "Shah", sunil.Version+1
	if !reflect.DeepEqual(got, want) {
		t.Errorf("after edit %+v; want %+v", got, want)
	}
	webtest.WantRedirect(t, b.Post(t, contactURL(id, sunil.ID), with(contactForm(w.contact(t, sunil.ID)), "last_name", "Mehta")), recordURL(id))
	sunil = w.contact(t, sunil.ID)

	// MST-066: making Kiran primary clears Sunil.
	kiran := w.addContact(t, id, "Kiran", "Joshi", false)
	c := w.contact(t, kiran)
	webtest.WantRedirect(t, b.Post(t, contactURL(id, kiran)+"/set-primary", versionOf(c.Version)), recordURL(id))
	if got := w.contact(t, kiran); !got.Primary {
		t.Errorf("Kiran isn't primary")
	}
	if got := w.contact(t, sunil.ID); got.Primary {
		t.Errorf("Sunil is still primary")
	}
	// So does saving Sunil as primary, the other way round.
	sunil = w.contact(t, sunil.ID)
	webtest.WantRedirect(t, b.Post(t, contactURL(id, sunil.ID), with(contactForm(sunil), "is_primary", "1")), recordURL(id))
	if got := w.contact(t, kiran); got.Primary {
		t.Errorf("Kiran is still primary")
	}

	// CD-190: disabling the primary contact clears the flag and promotes nothing.
	sunil = w.contact(t, sunil.ID)
	off := b.Post(t, contactURL(id, sunil.ID)+"/disable", versionOf(sunil.Version))
	webtest.WantRedirect(t, off, recordURL(id))
	webtest.WantBanner(t, b.Get(t, off.Location), "Contact disabled.")
	sunil = w.contact(t, sunil.ID)
	if !sunil.Disabled || sunil.Primary {
		t.Errorf("after disable %+v", sunil)
	}
	if n := w.count(t, `SELECT count(*) FROM contact WHERE party_id = ? AND is_primary = 1`, id); n != 0 {
		t.Errorf("a disable promoted a contact: %d primaries", n)
	}
	on := b.Post(t, contactURL(id, sunil.ID)+"/enable", versionOf(sunil.Version))
	webtest.WantRedirect(t, on, recordURL(id))
	webtest.WantBanner(t, b.Get(t, on.Location), "Contact enabled.")

	// CD-190: so does deleting it.
	sunil = w.contact(t, sunil.ID)
	webtest.WantRedirect(t, b.Post(t, contactURL(id, sunil.ID)+"/set-primary", versionOf(sunil.Version)), recordURL(id))
	sunil = w.contact(t, sunil.ID)
	gone := b.Post(t, contactURL(id, sunil.ID)+"/delete", versionOf(sunil.Version))
	webtest.WantRedirect(t, gone, recordURL(id))
	webtest.WantBanner(t, b.Get(t, gone.Location), "Contact deleted.")
	if n := w.count(t, `SELECT count(*) FROM contact WHERE id = ?`, sunil.ID); n != 0 {
		t.Errorf("the contact is still there")
	}
	if n := w.count(t, `SELECT count(*) FROM contact WHERE party_id = ? AND is_primary = 1`, id); n != 0 {
		t.Errorf("a delete promoted a contact: %d primaries", n)
	}
}

// The refusals the Contact service names, through the form, on create and on
// update, by exact text.
func TestScreens_ContactRefusals(t *testing.T) {
	const (
		create = "create"
		update = "update"
	)
	for _, c := range []struct {
		name   string
		only   string
		code   string
		setup  func(t *testing.T, w *world, contact int64)
		kv     []string
		field  string
		banner bool
		msg    string
	}{
		{name: "MST-062 no name", kv: []string{"first_name", ""}, field: "first_name", msg: msgName},
		{name: "MST-063 email", kv: []string{"emails[1].email", "accounts@shreesteel"}, msg: msgEmail},
		{name: "MST-064 mobile", kv: []string{"phones[1].number", "5820012345"}, msg: msgMobile},
		{name: "MST-065 two primary emails", kv: []string{"emails[2].email", "two@shreesteel.test", "emails[2].primary", "1"}, msg: msgOneEmail},
		{name: "CD-190 two primary phones", kv: []string{"phones[2].number", "9820054321", "phones[2].mobile", "1", "phones[2].primary", "1"}, msg: msgOnePhone},
		{
			name: "CD-190 a disabled contact made primary", only: update, kv: []string{"is_primary", "1"}, msg: msgEnableCont, banner: true,
			setup: func(t *testing.T, w *world, contact int64) { t.Helper(); w.disableContact(t, contact) },
		},
		{
			name: "a disabled party", only: create, code: "RAJKOT", msg: "Party RAJKOT is disabled. Pick another party.", banner: true,
			setup: func(t *testing.T, w *world, _ int64) { t.Helper(); w.disable(t, "RAJKOT") },
		},
	} {
		for _, mode := range []string{create, update} {
			if c.only != "" && c.only != mode {
				continue
			}
			t.Run(c.name+" on "+mode, func(t *testing.T) {
				w := newWorld(t)
				b := w.kavya()
				code := c.code
				if code == "" {
					code = "SHREE"
				}
				id := w.get(t, code).ID
				kiran := w.addContact(t, w.get(t, "SHREE").ID, "Kiran", "Joshi", false)
				if c.setup != nil {
					c.setup(t, w, kiran)
				}
				stored := w.contact(t, kiran)
				before := w.tally(t)
				form := contactForm(stored)
				target := contactURL(id, kiran)
				if mode == create {
					form.Del("version")
					target = newContactURL(id)
				}
				form = with(form, "middle_name", "Typed")
				form = with(form, c.kv...)
				r := b.Post(t, target, form)
				webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
				switch {
				case c.field != "":
					webtest.WantFieldErrorID(t, r, components.FieldID(c.field), c.msg)
				case c.banner:
					webtest.WantBanner(t, r, c.msg)
				default:
					webtest.WantText(t, r, c.msg)
				}
				value(t, r, "middle_name", "Typed")
				w.wantTally(t, before)
				if got := w.contact(t, kiran); !reflect.DeepEqual(got, stored) {
					t.Errorf("a refused save wrote: %+v", got)
				}
			})
		}
	}
}

// Set-primary goes through the service, so its refusals show: MST-051 for
// another party's contact, CD-190 for an unknown or a disabled one. A stale
// page is DOC's 409, and a contact keeps its own version.
func TestScreens_ContactActionRefusals(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	shree := w.get(t, "SHREE")
	bharat := w.get(t, "BHARAT")
	sunil := w.addContact(t, shree.ID, "Sunil", "Mehta", true)
	kiran := w.addContact(t, shree.ID, "Kiran", "Joshi", false)
	theirs := w.addContact(t, bharat.ID, "Asha", "Rao", false)

	foreign := b.Post(t, contactURL(shree.ID, theirs)+"/set-primary", versionOf(w.contact(t, theirs).Version))
	webtest.WantStatus(t, foreign, http.StatusUnprocessableEntity)
	webtest.WantBanner(t, foreign, msgBelong)

	unknown := b.Post(t, contactURL(shree.ID, 999999)+"/set-primary", versionOf(1))
	webtest.WantStatus(t, unknown, http.StatusUnprocessableEntity)
	webtest.WantBanner(t, unknown, msgNoContact)

	k := w.contact(t, kiran)
	stale := b.Post(t, contactURL(shree.ID, kiran)+"/set-primary", versionOf(k.Version-1))
	webtest.WantStatus(t, stale, http.StatusConflict)
	webtest.WantBanner(t, stale, "Someone else changed Contact Kiran Joshi after you opened it. Reload it and make your changes again.")
	if got := w.contact(t, kiran); got.Primary {
		t.Errorf("a stale set-primary wrote")
	}

	w.disableContact(t, kiran)
	k = w.contact(t, kiran)
	off := b.Post(t, contactURL(shree.ID, kiran)+"/set-primary", versionOf(k.Version))
	webtest.WantStatus(t, off, http.StatusUnprocessableEntity)
	webtest.WantBanner(t, off, msgEnableCont)

	s := w.contact(t, sunil)
	for _, action := range []string{"", "/disable", "/delete"} {
		form := versionOf(s.Version - 1)
		if action == "" {
			form = with(contactForm(s), "version", strconv.Itoa(s.Version-1))
		}
		r := b.Post(t, contactURL(shree.ID, sunil)+action, form)
		webtest.WantStatus(t, r, http.StatusConflict)
		webtest.WantBanner(t, r, staleSunil)
	}
	for _, action := range []string{"", "/disable", "/enable", "/delete", "/set-primary"} {
		webtest.WantStatus(t, b.Post(t, contactURL(shree.ID, sunil)+action, url.Values{}), http.StatusBadRequest)
	}

	shreeNow := w.get(t, "SHREE")
	webtest.WantRedirect(t, b.Post(t, contactURL(shree.ID, sunil), with(contactForm(s), "last_name", "Shah")), recordURL(shree.ID))
	if got := w.contact(t, sunil); got.LastName != "Shah" || got.Version != s.Version+1 {
		t.Errorf("after save %+v", got)
	}
	if got := w.get(t, "SHREE"); got.Version != shreeNow.Version {
		t.Errorf("party version = %d; want %d", got.Version, shreeNow.Version)
	}
}

// A row belongs to the party in its URL: another party's or a branch's address,
// and another party's contact, are 404s on every route but set-primary's
// contact, which the service words (above). An unknown party has no add page.
func TestScreens_ChildRoutesCheckTheOwner(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	shree := w.get(t, "SHREE")
	theirs := w.m.Addresses["Bengaluru"] // BHARAT's
	branch := w.m.BranchAddresses["PUNE"]
	contact := w.addContact(t, w.get(t, "BHARAT").ID, "Asha", "Rao", false)
	stored := w.address(t, theirs)
	storedContact := w.contact(t, contact)
	form := with(addressForm(stored), "kind", "billing")

	// The party's own rows answer, so a 404 below is the owner's.
	webtest.WantStatus(t, b.Get(t, addressURL(shree.ID, w.m.Addresses["Mumbai"])), http.StatusOK)
	webtest.WantStatus(t, b.Get(t, contactURL(shree.ID, w.addContact(t, shree.ID, "Sunil", "Mehta", false))), http.StatusOK)

	for _, target := range []string{addressURL(shree.ID, theirs), addressURL(shree.ID, branch), contactURL(shree.ID, contact), contactURL(shree.ID, 999999)} {
		webtest.WantStatus(t, b.Get(t, target), http.StatusNotFound)
	}
	for _, id := range []int64{theirs, branch} {
		for _, action := range []string{"", "/disable", "/enable", "/delete", "/set-primary"} {
			webtest.WantStatus(t, b.Post(t, addressURL(shree.ID, id)+action, form), http.StatusNotFound)
		}
	}
	for _, action := range []string{"", "/disable", "/enable", "/delete"} {
		webtest.WantStatus(t, b.Post(t, contactURL(shree.ID, contact)+action, contactForm(storedContact)), http.StatusNotFound)
	}
	if got := w.address(t, theirs); got != stored {
		t.Errorf("another party's address moved: %+v", got)
	}
	if got := w.contact(t, contact); !reflect.DeepEqual(got, storedContact) {
		t.Errorf("another party's contact moved: %+v", got)
	}
	for _, target := range []string{newAddressURL(999999), newContactURL(999999)} {
		webtest.WantStatus(t, b.Get(t, target), http.StatusNotFound)
		webtest.WantStatus(t, b.Post(t, target, addressForm(stored)), http.StatusNotFound)
	}
}

// The rows have rights of their own, on Address and Contact, and the right comes
// before the form's errors. A Party writer without Address's right sees no Add
// and gets AUTH's 403.
func TestScreens_ChildActionsCheckTheirRights(t *testing.T) {
	const (
		noCreateAddress = "You don't have permission to create Address. Ask your administrator for a role that allows it."
		noWriteAddress  = "You don't have permission to change Address. Ask your administrator for a role that allows it."
		noDeleteAddress = "You don't have permission to delete Address. Ask your administrator for a role that allows it."
		noCreateContact = "You don't have permission to create Contact. Ask your administrator for a role that allows it."
		noWriteContact  = "You don't have permission to change Contact. Ask your administrator for a role that allows it."
		noDeleteContact = "You don't have permission to delete Contact. Ask your administrator for a role that allows it."
	)
	w := newWorld(t)
	ctx := context.Background()
	env := w.h.Env
	shree := w.get(t, "SHREE")
	thane := w.address(t, w.m.Addresses["Thane"])
	sunil := w.addContact(t, shree.ID, "Sunil", "Mehta", false)
	c := w.contact(t, sunil)
	before := w.tally(t)

	priya := w.h.Browser(env.Priya)
	for _, want := range []struct {
		target string
		form   url.Values
		msg    string
	}{
		{newAddressURL(shree.ID), url.Values{"line1": {""}}, noCreateAddress},
		{addressURL(shree.ID, thane.ID), versionOf(thane.Version), noWriteAddress},
		{addressURL(shree.ID, thane.ID) + "/disable", versionOf(thane.Version), noWriteAddress},
		{addressURL(shree.ID, thane.ID) + "/enable", versionOf(thane.Version), noWriteAddress},
		{addressURL(shree.ID, thane.ID) + "/set-primary", with(versionOf(thane.Version), "kind", "shipping"), noWriteAddress},
		{addressURL(shree.ID, thane.ID) + "/delete", versionOf(thane.Version), noDeleteAddress},
		{newContactURL(shree.ID), url.Values{"first_name": {""}}, noCreateContact},
		{contactURL(shree.ID, sunil), versionOf(c.Version), noWriteContact},
		{contactURL(shree.ID, sunil) + "/disable", versionOf(c.Version), noWriteContact},
		{contactURL(shree.ID, sunil) + "/enable", versionOf(c.Version), noWriteContact},
		{contactURL(shree.ID, sunil) + "/set-primary", versionOf(c.Version), noWriteContact},
		{contactURL(shree.ID, sunil) + "/delete", versionOf(c.Version), noDeleteContact},
	} {
		r := priya.Post(t, want.target, want.form)
		webtest.WantStatus(t, r, http.StatusForbidden)
		webtest.WantText(t, r, want.msg)
	}
	for _, want := range []struct{ target, msg string }{
		{newAddressURL(shree.ID), noCreateAddress},
		{addressURL(shree.ID, thane.ID), noWriteAddress},
		{newContactURL(shree.ID), noCreateContact},
		{contactURL(shree.ID, sunil), noWriteContact},
	} {
		r := priya.Get(t, want.target)
		webtest.WantStatus(t, r, http.StatusForbidden)
		webtest.WantText(t, r, want.msg)
	}
	w.wantTally(t, before)

	a := env.Kit.Auth.(*auth.Auth)
	for _, role := range []string{"Masters Manager", "Employee"} {
		if err := a.SetRights(ctx, env.Admin, role, address.DocType, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll}); err != nil {
			t.Fatal(err)
		}
	}
	b := w.kavya()
	page := b.Get(t, recordURL(shree.ID))
	webtest.WantStatus(t, page, http.StatusOK)
	if _, found := page.RowLink(newAddressURL(shree.ID)); found {
		t.Errorf("a user who can't create Address is offered Add address")
	}
	if _, found := page.RowLink(newContactURL(shree.ID)); !found {
		t.Errorf("Add contact went with Address's right")
	}
	r := b.Post(t, addressURL(shree.ID, thane.ID), addressForm(thane))
	webtest.WantStatus(t, r, http.StatusForbidden)
	webtest.WantText(t, r, noWriteAddress)
}

package partyweb_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// No case ID unless a test names one: what the Task 22 review found.

// msgNoParty is address's text for an owner that isn't there (CD-170, CD-227).
const msgNoParty = "Party not found. Pick one from the list."

// billBharat is a billing row the save refuses: BHARAT holds its GSTIN.
var billBharat = row(mumbaiRow, "title", "Bharat", "state_code", "29", "pincode", "560001", "gstin", gstinBharat)

// A row's error that holds only because an earlier row was refused is false: the
// refused row was never written, so the next row looks like the party's first.
// The refused row's own error stays, and fixing that row alone saves the draft.
func TestScreens_PartyWizardNoFollowOnErrors(t *testing.T) {
	for _, c := range []struct {
		name, code, follow string
		party              url.Values
		second             map[string]string
	}{
		{
			name: "a registered party's next billing row needs no GSTIN", code: "MEHTA", follow: msgNeedGSTIN,
			party: details("MEHTA"), second: row(blrRow, "gstin", "", "is_billing", "1", "is_shipping", ""),
		},
		{
			name: "an unregistered party's next row may hold a GSTIN", code: "MEHTA", follow: msgUnreg,
			party: with(details("MEHTA"), "gst_category", "Unregistered", "pan", ""), second: blrRow,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			before := w.tally(t)
			b := w.kavya()
			toCheck(t, b, c.party, rows(billBharat, c.second), url.Values{})
			refused := b.Post(t, stepCheck, url.Values{})
			webtest.WantStatus(t, refused, http.StatusUnprocessableEntity)
			wantPage(t, refused, "Step 2 of 4")
			webtest.WantFieldErrorID(t, refused, components.FieldID(line(1, "gstin")), usedByBharat)
			webtest.WantNoText(t, refused, c.follow)
			w.wantTally(t, before)

			webtest.WantRedirect(t, b.Post(t, stepAddresses, with(rows(mumbaiRow, c.second), "return", "check")), stepCheck)
			saved := b.Post(t, stepCheck, url.Values{})
			webtest.WantRedirect(t, saved, recordURL(w.get(t, c.code).ID))
			if got := w.tally(t); got.parties != before.parties+1 || got.addresses != before.addresses+2 {
				t.Errorf("rows = %+v; want one party and two addresses more than %+v", got, before)
			}
		})
	}
}

// The same messages are real where no refused row explains them.
func TestScreens_PartyWizardRealGSTINErrorsStay(t *testing.T) {
	blankBilling := row(blrRow, "gstin", "", "is_billing", "1", "is_shipping", "")
	unregistered := with(details("MEHTA"), "gst_category", "Unregistered", "pan", "")
	for _, c := range []struct {
		name, msg string
		party     url.Values
		rows      url.Values
		no        int
	}{
		{"the only billing row has no GSTIN", msgNeedGSTIN, details("MEHTA"), rows(blankBilling), 1},
		{"after a good shipping row", msgNeedGSTIN, details("MEHTA"), rows(blrRow, blankBilling), 2},
		{"a GSTIN on an unregistered party's shipping row", msgUnreg, unregistered, rows(blrRow), 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			before := w.tally(t)
			b := w.kavya()
			toCheck(t, b, c.party, c.rows, url.Values{})
			refused := b.Post(t, stepCheck, url.Values{})
			webtest.WantStatus(t, refused, http.StatusUnprocessableEntity)
			webtest.WantFieldErrorID(t, refused, components.FieldID(line(c.no, "gstin")), c.msg)
			w.wantTally(t, before)
		})
	}
}

// CD-180 D1 is about the draft, so several rows without billing show it once.
func TestScreens_PartyWizardNeedBillingOnce(t *testing.T) {
	w := newWorld(t)
	before := w.tally(t)
	b := w.kavya()
	ship := row(mumbaiRow, "is_billing", "", "is_shipping", "1")
	toCheck(t, b, details("MEHTA"), rows(ship, row(ship, "title", "Second"), row(ship, "title", "Third")), url.Values{})
	refused := b.Post(t, stepCheck, url.Values{})
	webtest.WantStatus(t, refused, http.StatusUnprocessableEntity)
	webtest.WantFieldErrorID(t, refused, components.FieldID(line(1, "is_billing")), msgNeedBill)
	if n := strings.Count(webtest.Squash(refused.Doc.Find(`[id$="-msg"]`).Text()), msgNeedBill); n != 1 {
		t.Errorf("D1 beside a field %d times; want 1", n)
	}
	if n := strings.Count(webtest.Squash(refused.Doc.Find(`a[href^="#f-"]`).Text()), msgNeedBill); n != 1 {
		t.Errorf("D1 in the summary %d times; want 1\nbody: %s", n, webtest.Squash(refused.Body))
	}
	w.wantTally(t, before)
}

// wantFreeText checks the control is a plain text input (MST-057, CD-229): the
// browser neither cuts what is typed nor asks for digits, and no field check
// calls a foreign postcode a bad pincode.
func wantFreeText(t *testing.T, r webtest.Response, name string) {
	t.Helper()
	in := r.Doc.Find(`input[name="` + name + `"]`)
	if in.Length() != 1 {
		t.Fatalf("%d inputs named %s; want 1\ntext: %s", in.Length(), name, r.Text())
	}
	if got, ok := in.Attr("maxlength"); ok {
		t.Errorf("%s has maxlength %s; want none", name, got)
	}
	if got, _ := in.Attr("inputmode"); got == "numeric" {
		t.Errorf("%s has a numeric inputmode", name)
	}
	if got, ok := in.Attr("hx-post"); ok {
		t.Errorf("%s posts to %s; want no field check", name, got)
	}
}

// wantRoundTrip follows a saved address through the record page and its form:
// the value shows as stored in both, and saving the form unchanged keeps it.
func wantRoundTrip(t *testing.T, w *world, b *webtest.Browser, partyID int64, a address.Address, typed, stored string) {
	t.Helper()
	if a.Pincode != stored {
		t.Errorf("%q is stored as %q; want %q", typed, a.Pincode, stored)
	}
	webtest.WantText(t, b.Get(t, recordURL(partyID)), a.City+" - "+stored)
	target := addressURL(partyID, a.ID)
	form := b.Get(t, target)
	webtest.WantStatus(t, form, http.StatusOK)
	value(t, form, "pincode", stored)
	wantFreeText(t, form, "pincode")

	webtest.WantRedirect(t, b.Post(t, target, formValues(t, form, target)), recordURL(partyID))
	got := w.address(t, a.ID)
	if got.Pincode != stored {
		t.Errorf("after an unchanged save the pincode is %q; want %q", got.Pincode, stored)
	}
	got.Version = a.Version
	if got != a {
		t.Errorf("an unchanged save moved the address:\n got %+v\nwant %+v", got, a)
	}
}

const (
	ukPostcode = "SW1A 1AA"
	inSpaced   = "400 093"
	inStored   = "400093"
)

// A pincode is free text outside India (MST-057, CD-229) and the service's rule
// inside it: SW1A 1AA saves whole, 400 093 as the service closes it up, and a
// bad Indian pincode is still refused, now by the service alone.
func TestScreens_PartyWizardPincode(t *testing.T) {
	ukRow := map[string]string{"title": "London", "line1": "1 High St", "city": "London", "state_code": "", "country": "United Kingdom", "pincode": ukPostcode, "gstin": "", "is_billing": "1"}
	overseas := with(details("UKCO"), "gst_category", "Overseas", "pan", "")

	t.Run("the control", func(t *testing.T) {
		w := newWorld(t)
		b := w.kavya()
		webtest.WantRedirect(t, b.Post(t, stepDetails, overseas), stepAddresses)
		page := b.Post(t, stepAddresses, with(url.Values{}, "_rows", "add"))
		webtest.WantStatus(t, page, http.StatusOK)
		wantFreeText(t, page, line(1, "pincode"))
	})

	for _, c := range []struct {
		name, code, typed, stored string
		party                     url.Values
		row                       map[string]string
	}{
		{"a UK postcode", "UKCO", ukPostcode, ukPostcode, overseas, ukRow},
		{"an Indian pincode with a space", "MEHTA", inSpaced, inStored, details("MEHTA"), row(mumbaiRow, "pincode", inSpaced)},
	} {
		t.Run(c.name+" round-trips", func(t *testing.T) {
			w := newWorld(t)
			b := w.kavya()
			toCheck(t, b, c.party, rows(c.row), url.Values{})
			value(t, b.Get(t, stepAddresses), line(1, "pincode"), c.typed)

			webtest.WantStatus(t, b.Post(t, stepCheck, url.Values{}), http.StatusSeeOther)
			id := w.get(t, c.code).ID
			stored := w.partyAddresses(t, id)
			if len(stored) != 1 {
				t.Fatalf("addresses = %+v", stored)
			}
			wantRoundTrip(t, w, b, id, stored[0], c.typed, c.stored)
		})
	}

	for _, bad := range []string{"012345", "4000931"} {
		t.Run("India refuses "+bad+" on Next", func(t *testing.T) {
			w := newWorld(t)
			b := w.kavya()
			webtest.WantRedirect(t, b.Post(t, stepDetails, details("MEHTA")), stepAddresses)
			r := b.Post(t, stepAddresses, rows(row(mumbaiRow, "pincode", bad)))
			webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
			webtest.WantFieldErrorID(t, r, components.FieldID(line(1, "pincode")), msgPincode)
			value(t, r, line(1, "pincode"), bad)
		})
	}
}

// The record form's half of TestScreens_PartyWizardPincode.
func TestScreens_AddressFormPincode(t *testing.T) {
	post := func(a address.Address) url.Values {
		form := addressForm(a)
		form.Del("version")
		return form
	}
	for _, c := range []struct {
		name, code, typed, stored string
		form                      address.Address
		owner                     func(w *world, t *testing.T) party.Party
	}{
		{
			"a UK postcode", "OVS", ukPostcode, ukPostcode,
			address.Address{Title: "London", Line1: "1 High St", City: "London", Country: "United Kingdom", Pincode: ukPostcode, IsBilling: true},
			func(w *world, t *testing.T) party.Party {
				t.Helper()
				return w.create(t, party.Input{Code: "OVS", Name: "Overseas Ltd", IsVendor: true, GSTCategory: gstin.Overseas})
			},
		},
		{
			"an Indian pincode with a space", "SHREE", inSpaced, inStored,
			address.Address{Title: "Andheri", Line1: "Plot 3", City: "Andheri", StateCode: "27", Country: "India", Pincode: inSpaced, IsShipping: true},
			func(w *world, t *testing.T) party.Party { t.Helper(); return w.get(t, "SHREE") },
		},
	} {
		t.Run(c.name+" round-trips", func(t *testing.T) {
			w := newWorld(t)
			b := w.kavya()
			p := c.owner(w, t)
			wantFreeText(t, b.Get(t, newAddressURL(p.ID)), "pincode")
			count := len(w.partyAddresses(t, p.ID))
			webtest.WantRedirect(t, b.Post(t, newAddressURL(p.ID), post(c.form)), recordURL(p.ID))
			all := w.partyAddresses(t, p.ID)
			if len(all) != count+1 {
				t.Fatalf("addresses = %+v; want one more than %d", all, count)
			}
			wantRoundTrip(t, w, b, p.ID, all[count], c.typed, c.stored)
		})
	}

	for _, bad := range []string{"012345", "4000931"} {
		t.Run("India refuses "+bad, func(t *testing.T) {
			w := newWorld(t)
			b := w.kavya()
			id := w.get(t, "SHREE").ID
			form := post(address.Address{Line1: "Plot 3", City: "Andheri", StateCode: "27", Country: "India", Pincode: bad, IsShipping: true})
			r := b.Post(t, newAddressURL(id), form)
			webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
			webtest.WantFieldError(t, r, "pincode", msgPincode)
			value(t, r, "pincode", bad)
		})
	}
}

// grant sets role's rights on each DocType.
func (w *world) grant(t *testing.T, role string, rights map[auth.Right]auth.Scope, docTypes ...string) {
	t.Helper()
	a := w.h.Env.Kit.Auth.(*auth.Auth)
	for _, dt := range docTypes {
		if err := a.SetRights(context.Background(), w.h.Env.Admin, role, dt, rights); err != nil {
			t.Fatal(err)
		}
	}
}

// Own scope on the real auth.Service (CD-227): a row's created_by decides, as
// in the list. The user's own row opens, offers Change and can be disabled and
// deleted; a row they didn't create isn't on the page and is 404 by URL.
func TestScreens_RowsOnOwnScope(t *testing.T) {
	own := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeOwn, auth.RightCreate: auth.ScopeOwn, auth.RightWrite: auth.ScopeOwn, auth.RightDelete: auth.ScopeOwn}
	ctx := context.Background()

	w := newWorld(t)
	env := w.h.Env
	w.grant(t, "Employee", map[auth.Right]auth.Scope{}, address.DocType, address.ContactDocType)
	w.grant(t, "Purchase User", own, address.DocType, address.ContactDocType)
	p := w.create(t, party.Input{Code: "V-NEW", Name: "New Vendor", IsVendor: true})

	addr := func(u auth.Principal, title string) int64 {
		t.Helper()
		saved, err := w.addresses.Create(ctx, u, address.Input{PartyID: p.ID, Title: title, Line1: "L1", City: "Pune", StateCode: "27", IsShipping: true})
		if err != nil {
			t.Fatal(err)
		}
		return saved.ID
	}
	cont := func(u auth.Principal, name string) int64 {
		t.Helper()
		saved, err := w.addresses.CreateContact(ctx, u, address.ContactInput{PartyID: p.ID, FirstName: name})
		if err != nil {
			t.Fatal(err)
		}
		return saved.ID
	}

	for _, c := range []struct {
		name, field string
		mine, other int64
		rowURL      func(int64, int64) string
		state       func(int64) (int, bool)
		count       string
	}{
		{
			"address", "city", addr(env.Priya, "Mine"), addr(env.Kavya, "Theirs"), addressURL,
			func(id int64) (int, bool) { a := w.address(t, id); return a.Version, a.Disabled },
			`SELECT count(*) FROM address WHERE id = ?`,
		},
		{
			"contact", "last_name", cont(env.Priya, "Mine"), cont(env.Kavya, "Theirs"), contactURL,
			func(id int64) (int, bool) { x := w.contact(t, id); return x.Version, x.Disabled },
			`SELECT count(*) FROM contact WHERE id = ?`,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := w.h.Browser(env.Priya)
			mine, other := c.rowURL(p.ID, c.mine), c.rowURL(p.ID, c.other)
			otherVersion, _ := c.state(c.other)

			page := b.Get(t, recordURL(p.ID))
			webtest.WantStatus(t, page, http.StatusOK)
			if page.Doc.Find(`a[aria-label="Change Mine"][href="`+mine+`"]`).Length() != 1 {
				t.Errorf("no Change on the user's own row\nbody: %s", webtest.Squash(page.Body))
			}
			if strings.Contains(page.Body, other) || strings.Contains(page.Body, "Theirs") {
				t.Errorf("the page holds a row the user didn't create\nbody: %s", webtest.Squash(page.Body))
			}

			form := b.Get(t, mine)
			webtest.WantStatus(t, form, http.StatusOK)
			if form.Status == http.StatusOK {
				webtest.WantRedirect(t, b.Post(t, mine, with(formValues(t, form, mine), c.field, "Changed")), recordURL(p.ID))
			}
			version, _ := c.state(c.mine)
			webtest.WantRedirect(t, b.Post(t, mine+"/disable", versionOf(version)), recordURL(p.ID))
			version, off := c.state(c.mine)
			if !off {
				t.Errorf("the user's own row isn't disabled")
			}
			webtest.WantStatus(t, b.Get(t, mine+"?delete"), http.StatusOK)
			webtest.WantRedirect(t, b.Post(t, mine+"/delete", versionOf(version)), recordURL(p.ID))
			if n := w.count(t, c.count, c.mine); n != 0 {
				t.Errorf("the user's own row is still there")
			}

			webtest.WantStatus(t, b.Get(t, other), http.StatusNotFound)
			webtest.WantStatus(t, b.Get(t, other+"?delete"), http.StatusNotFound)
			for _, action := range []string{"", "/disable", "/delete"} {
				webtest.WantStatus(t, b.Post(t, other+action, versionOf(otherVersion)), http.StatusNotFound)
			}
			if got, off := c.state(c.other); got != otherVersion || off {
				t.Errorf("a row the user didn't create moved: version %d, disabled %v", got, off)
			}
		})
	}
}

// CD-227, CD-233: an own-scope user posting a party they can't read gets the
// not-found text with nothing written, whichever way the party is posted. The
// screens take the owner from the URL: a hidden party there is a 404 that reads
// as an unknown party's, and one in the form is ignored. The service, which the
// other master screens and the import reach by id, words it for the owner field.
func TestScreens_HiddenPartyPostedIsNotFound(t *testing.T) {
	const unknown = int64(9_999_999)
	ctx := context.Background()
	w := newWorld(t)
	env := w.h.Env
	all := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightCreate: auth.ScopeAll, auth.RightWrite: auth.ScopeAll}
	w.grant(t, "Employee", map[auth.Right]auth.Scope{}, party.DocType, address.DocType, address.ContactDocType)
	w.grant(t, "Purchase User", all, address.DocType, address.ContactDocType)
	w.grant(t, "Purchase User", map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeOwn, auth.RightCreate: auth.ScopeOwn}, party.DocType)
	shree := w.get(t, "SHREE") // Kavya's
	mine, err := w.parties.Create(ctx, env.Priya, party.Input{Code: "V-MINE", Name: "My Vendor", IsVendor: true})
	if err != nil {
		t.Fatal(err)
	}
	b := w.h.Browser(env.Priya)
	posted := func(kv ...string) url.Values {
		return with(url.Values{"party_id": {strconv.FormatInt(shree.ID, 10)}}, kv...)
	}

	for _, c := range []struct {
		name  string
		url   func(int64) string
		input url.Values
		owned string
	}{
		{
			"address", newAddressURL,
			posted("title", "Posted", "line1", "L1", "city", "Pune", "state_code", "27", "country", "India", "pincode", "411001", "is_shipping", "1"),
			`SELECT count(*) FROM address WHERE party_id = ? AND title = 'Posted'`,
		},
		{"contact", newContactURL, posted("first_name", "Posted"), `SELECT count(*) FROM contact WHERE party_id = ? AND first_name = 'Posted'`},
	} {
		before := w.tally(t)
		hidden := b.Post(t, c.url(shree.ID), c.input)
		missing := b.Post(t, c.url(unknown), c.input)
		for _, r := range []webtest.Response{hidden, missing} {
			webtest.WantStatus(t, r, http.StatusNotFound)
			webtest.WantText(t, r, auth.MsgNotFound)
			webtest.WantNoText(t, r, "Shree Steel")
		}
		if h1 := hidden.Doc.Find("h1").First().Text(); h1 != missing.Doc.Find("h1").First().Text() {
			t.Errorf("%s: a hidden party's heading %q differs from an unknown one's", c.name, h1)
		}

		// In the form it lands nowhere: the row is the URL's party's.
		webtest.WantRedirect(t, b.Post(t, c.url(mine.ID), c.input), recordURL(mine.ID))
		if got, off := w.count(t, c.owned, mine.ID), w.count(t, c.owned, shree.ID); got != 1 || off != 0 {
			t.Errorf("%s: %d rows on the URL's party and %d on the posted one", c.name, got, off)
		}
		w.wantTally(t, before.plus(rowsAdded(c.name)))
	}

	before := w.tally(t)
	_, err = w.addresses.Create(ctx, env.Priya, address.Input{PartyID: shree.ID, Title: "T", Line1: "L1", City: "Pune", StateCode: "27", IsShipping: true})
	wantPartyGone(t, err)
	_, err = w.addresses.CreateContact(ctx, env.Priya, address.ContactInput{PartyID: shree.ID, FirstName: "T"})
	wantPartyGone(t, err)
	w.wantTally(t, before)
}

// rowsAdded is what one saved row of the kind adds to a tally.
func rowsAdded(kind string) tally {
	if kind == "address" {
		return tally{addresses: 1, audits: 1}
	}
	return tally{contacts: 1, audits: 1}
}

// wantPartyGone fails unless err says the owner wasn't found.
func wantPartyGone(t *testing.T, err error) {
	t.Helper()
	var he doctype.HTTPError
	if !errors.As(err, &he) || len(he.FieldErrors()) != 1 || he.FieldErrors()[0] != (doctype.FieldError{Field: "party_id", Message: msgNoParty}) {
		t.Errorf("err = %v; want %q on party_id", err, msgNoParty)
	}
}

// Rows live under their party: a user who can't read the party gets the record's
// own refusal on every row route, with nothing of the party in the body, and
// nothing is written. Both ways of not reading it are checked.
func TestScreens_RowsNeedPartyRead(t *testing.T) {
	all := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightCreate: auth.ScopeAll, auth.RightWrite: auth.ScopeAll, auth.RightDelete: auth.ScopeAll}
	for _, c := range []struct {
		name   string
		rights map[auth.Right]auth.Scope
		status int
	}{
		{"no read on Party", map[auth.Right]auth.Scope{}, http.StatusForbidden},
		{"own scope on a party the user didn't create", map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeOwn}, http.StatusNotFound},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			shree := w.get(t, "SHREE")
			thane := w.address(t, w.m.Addresses["Thane"])
			sunil := w.contact(t, w.addContact(t, shree.ID, "Sunil", "Mehta", false))
			w.grant(t, "Employee", map[auth.Right]auth.Scope{}, party.DocType, address.DocType, address.ContactDocType)
			w.grant(t, "Purchase User", all, address.DocType, address.ContactDocType)
			b := w.h.Browser(w.h.Env.Priya)
			addressAt, contactAt := addressURL(shree.ID, thane.ID), contactURL(shree.ID, sunil.ID)

			// With the party's read these pages open, so a refusal below is that read.
			webtest.WantStatus(t, b.Get(t, addressAt), http.StatusOK)
			webtest.WantStatus(t, b.Get(t, contactAt), http.StatusOK)

			w.grant(t, "Purchase User", c.rights, party.DocType)
			want := b.Get(t, recordURL(shree.ID))
			webtest.WantStatus(t, want, c.status)
			msg := webtest.Squash(want.Doc.Find("h1").First().Text())
			if msg == "" {
				t.Fatalf("the record's refusal has no heading\nbody: %s", webtest.Squash(want.Body))
			}
			before := w.tally(t)

			check := func(label string, r webtest.Response) {
				t.Helper()
				if r.Status != c.status {
					t.Errorf("%s: status = %d; want the record's %d", label, r.Status, c.status)
				}
				webtest.WantText(t, r, msg)
				for _, text := range []string{"SHREE", "Shree Steel", "Thane", "Sunil"} {
					if strings.Contains(r.Body, text) {
						t.Errorf("%s: the body holds %q", label, text)
					}
				}
			}
			for _, target := range []string{
				newAddressURL(shree.ID), addressAt, addressAt + "?delete",
				newContactURL(shree.ID), contactAt, contactAt + "?delete",
			} {
				check("GET "+target, b.Get(t, target))
			}
			addressPost := with(addressForm(thane), "kind", "shipping")
			for target, form := range map[string]url.Values{
				newAddressURL(shree.ID):    addressPost,
				addressAt:                  addressPost,
				addressAt + "/disable":     addressPost,
				addressAt + "/enable":      addressPost,
				addressAt + "/delete":      addressPost,
				addressAt + "/set-primary": addressPost,
				newContactURL(shree.ID):    contactForm(sunil),
				contactAt:                  contactForm(sunil),
				contactAt + "/disable":     contactForm(sunil),
				contactAt + "/enable":      contactForm(sunil),
				contactAt + "/delete":      contactForm(sunil),
				contactAt + "/set-primary": contactForm(sunil),
			} {
				check("POST "+target, b.Post(t, target, form))
			}
			w.wantTally(t, before)
			if got := w.address(t, thane.ID); got != thane {
				t.Errorf("a refused post moved the address: %+v", got)
			}
			if got := w.contact(t, sunil.ID); got.Version != sunil.Version || got.Primary {
				t.Errorf("a refused post moved the contact: %+v", got)
			}
		})
	}
}

// cancelHref is where the page's one Cancel link goes.
func cancelHref(t *testing.T, r webtest.Response) string {
	t.Helper()
	links := r.Doc.Find(`a:contains("Cancel")`)
	if links.Length() != 1 {
		t.Fatalf("%d Cancel links; want 1\nbody: %s", links.Length(), webtest.Squash(r.Body))
	}
	href, _ := links.Attr("href")
	return href
}

// Cancel on a row's delete step goes back to the party's record, which a user
// with delete and no write can open; the row's form would be their 403.
func TestScreens_RowDeleteCancel(t *testing.T) {
	w := newWorld(t)
	shree := w.get(t, "SHREE")
	thane := w.address(t, w.m.Addresses["Thane"]).ID
	sunil := w.addContact(t, shree.ID, "Sunil", "Mehta", false)
	targets := []string{addressURL(shree.ID, thane), contactURL(shree.ID, sunil)}

	for _, target := range targets {
		if got := cancelHref(t, w.kavya().Get(t, target+"?delete")); got != recordURL(shree.ID) {
			t.Errorf("%s: Cancel goes to %q; want the record", target, got)
		}
	}

	w.grant(t, "Employee", map[auth.Right]auth.Scope{}, address.DocType, address.ContactDocType)
	w.grant(t, "Purchase User", map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightDelete: auth.ScopeAll}, address.DocType, address.ContactDocType)
	b := w.h.Browser(w.h.Env.Priya)
	for _, target := range targets {
		confirm := b.Get(t, target+"?delete")
		webtest.WantStatus(t, confirm, http.StatusOK)
		href := cancelHref(t, confirm)
		if href != recordURL(shree.ID) {
			t.Errorf("%s: Cancel goes to %q; want the record", target, href)
		}
		webtest.WantStatus(t, b.Get(t, href), http.StatusOK)
	}
}

// Another party's contact skips the version check, whose row it isn't: a stale
// post gets the service's MST-051 refusal, not a 409, and writes nothing.
func TestScreens_ContactSetPrimaryForeignStaleVersion(t *testing.T) {
	w := newWorld(t)
	shree := w.get(t, "SHREE")
	theirs := w.contact(t, w.addContact(t, w.get(t, "BHARAT").ID, "Asha", "Rao", false))
	before := w.tally(t)
	r := w.kavya().Post(t, contactURL(shree.ID, theirs.ID)+"/set-primary", versionOf(theirs.Version+1))
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	webtest.WantBanner(t, r, msgBelong)
	w.wantTally(t, before)
	if got := w.contact(t, theirs.ID); got.Primary || got.Version != theirs.Version {
		t.Errorf("another party's contact moved: %+v", got)
	}
}

// Back drops wholly blank rows too, and the rest close up.
func TestScreens_PartyWizardBackDropsBlankRows(t *testing.T) {
	for _, c := range []struct {
		name, step, back, field, want string
		blank, kept                   map[string]string
		reach                         url.Values
	}{
		{"addresses", stepAddresses, stepDetails, "line1", "Plot 12, MIDC", blankAddress, mumbaiRow, nil},
		{"contacts", stepContacts, stepAddresses, "first_name", "Sunil", blankContact, sunilRow, url.Values{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			b := w.kavya()
			webtest.WantRedirect(t, b.Post(t, stepDetails, details("MEHTA")), stepAddresses)
			if c.reach != nil {
				webtest.WantRedirect(t, b.Post(t, stepAddresses, c.reach), stepContacts)
			}
			webtest.WantRedirect(t, b.Post(t, c.step, with(rows(c.blank, c.kept, c.blank), "nav", "back")), c.back)
			page := b.Get(t, c.step)
			value(t, page, line(1, c.field), c.want)
			noControl(t, page, line(2, c.field))
		})
	}
}

// Check and save lists what a row holds; a field left blank has no pair. The
// first case shows the labels do appear for a row that fills them.
func TestScreens_PartyWizardCheckSkipsBlankFields(t *testing.T) {
	filled := func(t *testing.T, a, c map[string]string) webtest.Response {
		t.Helper()
		w := newWorld(t)
		b := w.kavya()
		toCheck(t, b, details("MEHTA"), rows(a), rows(c))
		return b.Get(t, stepCheck)
	}
	full := filled(t, mumbaiRow, sunilRow)
	webtest.WantText(t, full, "Line 2Andheri East")
	webtest.WantText(t, full, "Middle nameK")

	blank := filled(t, row(mumbaiRow, "line2", ""), row(sunilRow, "middle_name", ""))
	webtest.WantNoText(t, blank, "Line 2")
	webtest.WantNoText(t, blank, "Middle name")
}

// A billing row's own errors all stay: only the rows after it are excused.
func TestScreens_PartyWizardRowKeepsItsOwnErrors(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	overseas := row(blrRow, "gstin", "", "is_billing", "1", "is_shipping", "", "state_code", "", "country", "United Kingdom", "pincode", "SW1A 1AA")
	toCheck(t, b, details("MEHTA"), rows(overseas), url.Values{})
	refused := b.Post(t, stepCheck, url.Values{})
	webtest.WantStatus(t, refused, http.StatusUnprocessableEntity)
	webtest.WantFieldErrorID(t, refused, components.FieldID(line(1, "country")), msgOverseas)
	webtest.WantFieldErrorID(t, refused, components.FieldID(line(1, "gstin")), msgNeedGSTIN)
}

// Rows that aren't the first billing row keep their real errors, whoever is
// refused: only a refused first billing row excuses the rows after it.
func TestScreens_PartyWizardOtherRowsKeepTheirErrors(t *testing.T) {
	unreg := with(details("MEHTA"), "gst_category", "Unregistered", "pan", "")
	bill := func(title string) map[string]string {
		return row(blrRow, "title", title, "is_billing", "1", "is_shipping", "")
	}
	blank := row(mumbaiRow, "gstin", "", "is_billing", "1", "is_shipping", "")
	for name, c := range map[string]struct {
		rows  url.Values
		lines []int
	}{
		"two shipping rows":                 {rows(blrRow, row(blrRow, "title", "Two")), []int{1, 2}},
		"two billing rows after a good one": {rows(blank, bill("B"), bill("C")), []int{2, 3}},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			b := w.kavya()
			toCheck(t, b, unreg, c.rows, url.Values{})
			r := b.Post(t, stepCheck, url.Values{})
			webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
			for _, n := range c.lines {
				webtest.WantFieldErrorID(t, r, components.FieldID(line(n, "gstin")), msgUnreg)
			}
		})
	}
}

// Set-primary on a row an own-scope user can't see answers as for a row that
// isn't there, stale version or not, and moves nothing.
func TestScreens_SetPrimaryHiddenRow(t *testing.T) {
	own := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeOwn, auth.RightCreate: auth.ScopeOwn, auth.RightWrite: auth.ScopeOwn, auth.RightDelete: auth.ScopeOwn}
	w := newWorld(t)
	env := w.h.Env
	w.grant(t, "Employee", map[auth.Right]auth.Scope{}, address.DocType, address.ContactDocType)
	w.grant(t, "Purchase User", own, address.DocType, address.ContactDocType)
	p := w.create(t, party.Input{Code: "V-NEW", Name: "New Vendor", IsVendor: true})
	ctx := context.Background()
	input := address.Input{PartyID: p.ID, Title: "Theirs", Line1: "L1", City: "Pune", StateCode: "27", IsShipping: true}
	if _, err := w.addresses.Create(ctx, env.Kavya, input); err != nil {
		t.Fatal(err)
	}
	hidden, err := w.addresses.Create(ctx, env.Kavya, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.addresses.CreateContact(ctx, env.Kavya, address.ContactInput{PartyID: p.ID, FirstName: "First"}); err != nil {
		t.Fatal(err)
	}
	theirs, err := w.addresses.CreateContact(ctx, env.Kavya, address.ContactInput{PartyID: p.ID, FirstName: "Theirs"})
	if err != nil {
		t.Fatal(err)
	}
	b := w.h.Browser(env.Priya)
	for _, version := range []int{hidden.Version, 99} {
		r := b.Post(t, addressURL(p.ID, hidden.ID)+"/set-primary", with(versionOf(version), "kind", "shipping"))
		webtest.WantStatus(t, r, http.StatusNotFound)
	}
	if w.address(t, hidden.ID).PrimaryShipping {
		t.Error("a row the user can't see was made primary")
	}
	for _, version := range []int{theirs.Version, 99} {
		r := b.Post(t, contactURL(p.ID, theirs.ID)+"/set-primary", versionOf(version))
		webtest.WantStatus(t, r, http.StatusNotFound)
	}
	if w.contact(t, theirs.ID).Primary {
		t.Error("a contact the user can't see was made primary")
	}
}

// Own read on Party: the rows of a party the user created open.
func TestScreens_RowsOfOwnParty(t *testing.T) {
	w := newWorld(t)
	env := w.h.Env
	own := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeOwn, auth.RightCreate: auth.ScopeOwn}
	w.grant(t, "Employee", map[auth.Right]auth.Scope{}, party.DocType)
	w.grant(t, "Purchase User", own, party.DocType, address.DocType, address.ContactDocType)
	saved, err := w.parties.Create(context.Background(), env.Priya, party.Input{Code: "MINE", Name: "Mine Ltd", IsVendor: true, GSTCategory: gstin.Unregistered})
	if err != nil {
		t.Fatal(err)
	}
	b := w.h.Browser(env.Priya)
	webtest.WantStatus(t, b.Get(t, recordURL(saved.ID)), http.StatusOK)
	webtest.WantStatus(t, b.Get(t, newAddressURL(saved.ID)), http.StatusOK)
}

// A pincode outside India is free text but not without limit (CD-227).
func TestScreens_OverseasPincodeLimit(t *testing.T) {
	w := newWorld(t)
	p := w.create(t, party.Input{Code: "OVS", Name: "Overseas Ltd", IsVendor: true, GSTCategory: gstin.Overseas})
	form := addressForm(address.Address{Title: "London", Line1: "1 High St", City: "London", Country: "United Kingdom", Pincode: strings.Repeat("9", 141), IsBilling: true})
	form.Del("version")
	r := w.kavya().Post(t, newAddressURL(p.ID), form)
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	webtest.WantFieldErrorID(t, r, components.FieldID("pincode"), "Pincode can be at most 140 characters.")
}

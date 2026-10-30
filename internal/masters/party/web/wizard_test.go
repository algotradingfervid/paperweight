package partyweb_test

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/employee"
	employeeweb "github.com/vardhan/internaltools/internal/masters/employee/web"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// CD-50: values stay in the session and nothing is written until Check and
// save, which writes the party, every address and every contact in one go.
func TestScreens_PartyWizardKeepsDraft(t *testing.T) {
	w := newWorld(t)
	terms := w.terms(t, "NET30", "Net 30 days")
	before := w.tally(t)
	b := w.kavya()

	d := details("MEHTA")
	d.Set("is_customer", "1")
	d.Set("default_payment_terms_id", strconv.FormatInt(terms, 10))
	a := rows(mumbaiRow, blrRow)
	c := rows(sunilRow, kiranRow)

	webtest.WantRedirect(t, b.Post(t, stepDetails, d), stepAddresses)

	// Step 2 back to step 1 shows what was typed, and what step 2 held.
	webtest.WantRedirect(t, b.Post(t, stepAddresses, with(a, "nav", "back")), stepDetails)
	first := b.Get(t, stepDetails)
	value(t, first, "code", "MEHTA")
	value(t, first, "name", "Mehta Fabricators")
	value(t, first, "pan", panMehta)
	if got := chosen(first, "gst_category"); got != "Registered Regular" {
		t.Errorf("gst_category = %q", got)
	}
	if !checked(first, "is_vendor") || !checked(first, "is_customer") {
		t.Errorf("roles lost on Back")
	}
	value(t, first, "default_payment_terms_id", strconv.FormatInt(terms, 10))
	webtest.WantText(t, first, "NET30 Net 30 days") // the picker's summary, CD-68

	second := b.Get(t, stepAddresses)
	value(t, second, line(1, "line1"), "Plot 12, MIDC")
	value(t, second, line(1, "line2"), "Andheri East")
	value(t, second, line(2, "gstin"), gstinBLR)
	if got, _ := second.Value(line(2, "state_code")); got != "29" {
		t.Errorf("row 2 state = %q", got)
	}
	if !checked(second, line(1, "is_billing")) || checked(second, line(1, "is_shipping")) || !checked(second, line(2, "is_shipping")) {
		t.Errorf("row ticks lost on Back")
	}

	webtest.WantRedirect(t, b.Post(t, stepDetails, d), stepAddresses)
	webtest.WantRedirect(t, b.Post(t, stepAddresses, a), stepContacts)
	webtest.WantRedirect(t, b.Post(t, stepContacts, c), stepCheck)

	third := b.Get(t, stepContacts)
	value(t, third, line(1, "first_name"), "Sunil")
	value(t, third, line(2, "email"), "kiran@mehtafab.test")
	if !checked(third, line(1, "is_primary")) || checked(third, line(2, "is_primary")) {
		t.Errorf("contact ticks lost")
	}
	w.wantTally(t, before)

	check := b.Get(t, stepCheck)
	webtest.WantStatus(t, check, http.StatusOK)
	wantPage(t, check, "Step 4 of 4")
	webtest.WantText(t, check, "Check and save")
	for _, text := range []string{
		"MEHTA", "Mehta Fabricators", "Registered Regular", panMehta, "NET30 Net 30 days",
		"Plot 12, MIDC", "Mumbai", gstinMumbai, "4 Residency Road", "Bengaluru", gstinBLR,
		"Sunil", "Accounts@MehtaFab.test", "Kiran", "kiran@mehtafab.test",
	} {
		webtest.WantText(t, check, text)
	}
	for _, step := range []string{stepDetails, stepAddresses, stepContacts} {
		if label, found := check.RowLink(step + "?return=check"); !found || !strings.HasPrefix(label, "Change") {
			t.Errorf("check page's Change link to %s: %q, %v", step, label, found)
		}
	}
	if got := webtest.Squash(check.Doc.Find(`button.btn-primary`).Text()); got != "Save" {
		t.Errorf("primary button = %q; want Save", got)
	}
	w.wantTally(t, before) // the check page wrote nothing

	saved := b.Post(t, stepCheck, url.Values{})
	p := w.get(t, "MEHTA")
	webtest.WantRedirect(t, saved, recordURL(p.ID))
	webtest.WantBanner(t, b.Get(t, saved.Location), "Party saved.")
	// One audit entry for each record: the party, two addresses, two contacts.
	w.wantTally(t, before.plus(tally{parties: 1, addresses: 2, contacts: 2, emails: 2, phones: 2, audits: 5}))

	if p.Name != "Mehta Fabricators" || !p.IsVendor || !p.IsCustomer || p.PAN != panMehta || p.GSTCategory != gstin.Regular || p.DefaultPaymentTermsID != terms {
		t.Errorf("saved party %+v", p)
	}
	addrs := w.partyAddresses(t, p.ID)
	if len(addrs) != 2 {
		t.Fatalf("addresses = %+v", addrs)
	}
	m, blr := addrs[0], addrs[1]
	if m.Title != "Mumbai" || m.Line1 != "Plot 12, MIDC" || m.Line2 != "Andheri East" || m.City != "Mumbai" || m.StateCode != "27" ||
		m.Country != "India" || m.Pincode != "400093" || m.GSTIN != gstinMumbai || !m.IsBilling || m.IsShipping || !m.PrimaryBilling || m.PrimaryShipping {
		t.Errorf("saved row 1 %+v", m)
	}
	if blr.Title != "Bengaluru" || blr.Line1 != "4 Residency Road" || blr.City != "Bengaluru" || blr.StateCode != "29" ||
		blr.Pincode != "560001" || blr.GSTIN != gstinBLR || blr.IsBilling || !blr.IsShipping || blr.PrimaryBilling || !blr.PrimaryShipping {
		t.Errorf("saved row 2 %+v", blr)
	}
	cs := w.partyContacts(t, p.ID)
	if len(cs) != 2 {
		t.Fatalf("contacts = %+v", cs)
	}
	s, k := cs[0], cs[1]
	if s.FirstName != "Sunil" || s.MiddleName != "K" || s.LastName != "Mehta" || !s.Primary ||
		len(s.Emails) != 1 || s.Emails[0] != (address.EmailInput{Email: "accounts@mehtafab.test", Primary: true}) ||
		len(s.Phones) != 1 || s.Phones[0] != (address.PhoneInput{Number: "+919820012345", Mobile: true, Primary: true}) {
		t.Errorf("saved contact 1 %+v", s)
	}
	if k.FirstName != "Kiran" || k.LastName != "Joshi" || k.Primary || len(k.Emails) != 1 || len(k.Phones) != 1 || k.Phones[0].Number != "+919820054321" {
		t.Errorf("saved contact 2 %+v", k)
	}
}

// A party needs neither an address nor a contact.
func TestScreens_PartyWizardNoRows(t *testing.T) {
	w := newWorld(t)
	before := w.tally(t)
	b := w.kavya()
	toCheck(t, b, details("MEHTA"), url.Values{}, url.Values{})
	webtest.WantRedirect(t, b.Post(t, stepCheck, url.Values{}), recordURL(w.get(t, "MEHTA").ID))
	w.wantTally(t, before.plus(tally{parties: 1, audits: 1}))
}

// MST-049 through the wizard: an unregistered party's first GSTIN back-fills it,
// and a row asked to be primary is.
func TestScreens_PartyWizardBackfillAndPrimary(t *testing.T) {
	w := newWorld(t)
	before := w.tally(t)
	b := w.kavya()
	d := details("MEHTA")
	d.Set("pan", "")
	d.Set("gst_category", "Unregistered")
	second := row(mumbaiRow, "title", "Mumbai 2", "gstin", gstinMumbai, "is_primary_billing", "1", "is_shipping", "1", "is_primary_shipping", "1")
	toCheck(t, b, d, rows(mumbaiRow, second), url.Values{})
	webtest.WantStatus(t, b.Post(t, stepCheck, url.Values{}), http.StatusSeeOther)
	p := w.get(t, "MEHTA")
	if p.GSTCategory != gstin.Regular || p.PAN != panMehta {
		t.Errorf("party %+v; want Registered Regular with PAN %s", p, panMehta)
	}
	addrs := w.partyAddresses(t, p.ID)
	if len(addrs) != 2 || addrs[0].PrimaryBilling || !addrs[1].PrimaryBilling || !addrs[1].PrimaryShipping {
		t.Errorf("primaries = %+v", addrs)
	}
	if got := w.tally(t); got.parties != before.parties+1 || got.addresses != before.addresses+2 {
		t.Errorf("tally = %+v; before %+v", got, before)
	}
}

// The wizard's forms post through htmx, the row buttons too.
func TestScreens_PartyWizardHTMX(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	next := b.PostHTMX(t, stepDetails, details("MEHTA"))
	webtest.WantStatus(t, next, http.StatusOK)
	if got := next.Header.Get("HX-Push-Url"); got != stepAddresses {
		t.Errorf("HX-Push-Url = %q; want %q", got, stepAddresses)
	}
	if next.Doc.Find("#wizard").Length() != 1 || strings.Contains(next.Body, "<html") {
		t.Errorf("want the addresses step's fragment\nbody: %s", webtest.Squash(next.Body))
	}

	added := b.PostHTMX(t, stepAddresses, with(rows(mumbaiRow), "_rows", "add"))
	webtest.WantStatus(t, added, http.StatusOK)
	if added.Doc.Find("#wizard").Length() != 1 || strings.Contains(added.Body, "<html") {
		t.Errorf("want the step's fragment\nbody: %s", webtest.Squash(added.Body))
	}
	value(t, added, line(1, "line1"), "Plot 12, MIDC")
	value(t, added, line(2, "line1"), "")

	bad := b.PostHTMX(t, stepAddresses, rows(row(mumbaiRow, "pincode", "012345")))
	webtest.WantStatus(t, bad, http.StatusUnprocessableEntity)
	if bad.Doc.Find("#wizard").Length() != 1 || strings.Contains(bad.Body, "<html") {
		t.Errorf("want the step's fragment with the 422\nbody: %s", webtest.Squash(bad.Body))
	}

	webtest.WantStatus(t, b.PostHTMX(t, stepAddresses, rows(mumbaiRow)), http.StatusOK)
	webtest.WantStatus(t, b.PostHTMX(t, stepContacts, rows(sunilRow)), http.StatusOK)
	saved := b.PostHTMX(t, stepCheck, url.Values{})
	webtest.WantStatus(t, saved, http.StatusOK)
	if got, want := saved.Header.Get("HX-Redirect"), recordURL(w.get(t, "MEHTA").ID); got != want {
		t.Errorf("HX-Redirect = %q; want %q", got, want)
	}
}

// /new opens the first step; the stepper names all four and links only reached
// steps; the form offers neither disabled nor a row.
func TestScreens_PartyWizardFirstPage(t *testing.T) {
	w := newWorld(t)
	page := w.kavya().Get(t, wizardMain)
	webtest.WantStatus(t, page, http.StatusOK)
	for _, text := range []string{"New party", "Step 1 of 4", "Party details", "Addresses", "Contacts", "Check and save", "Payment Terms", "GST category"} {
		webtest.WantText(t, page, text)
	}
	for _, step := range []string{stepAddresses, stepContacts, stepCheck} {
		if _, found := page.RowLink(step); found {
			t.Errorf("the stepper links %s, not reached yet", step)
		}
	}
	for _, name := range []string{"code", "name", "is_vendor", "is_customer", "pan", "gst_category", "default_payment_terms_id"} {
		if !page.HasControl(name) {
			t.Errorf("first step lacks %s", name)
		}
	}
	noControl(t, page, "disabled")
	noControl(t, page, line(1, "line1"))
	if got := page.Choices("gst_category"); len(got) != len(gstin.Categories()) {
		t.Errorf("gst_category options = %+v", got)
	}
}

// A step past the furthest reached isn't open yet, GET or POST.
func TestScreens_PartyWizardNoSkipping(t *testing.T) {
	w := newWorld(t)
	before := w.tally(t)
	b := w.kavya()
	for _, step := range []string{stepAddresses, stepContacts, stepCheck} {
		webtest.WantRedirect(t, b.Get(t, step), stepDetails)
		webtest.WantRedirect(t, b.Post(t, step, url.Values{}), stepDetails)
	}
	webtest.WantStatus(t, b.Get(t, base+"/new/nosuch"), http.StatusNotFound)
	webtest.WantStatus(t, b.Post(t, base+"/new/nosuch", url.Values{}), http.StatusNotFound)
	webtest.WantRedirect(t, b.Post(t, stepDetails, details("MEHTA")), stepAddresses)
	webtest.WantRedirect(t, b.Get(t, stepContacts), stepAddresses)
	w.wantTally(t, before)
}

// §8.3: Change on the check page returns straight to it, for the row steps too,
// and survives their Add and Remove.
func TestScreens_PartyWizardChangeReturnsToCheck(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	toCheck(t, b, details("MEHTA"), rows(mumbaiRow), rows(sunilRow))
	page := b.Get(t, stepAddresses+"?return=check")
	if got, _ := page.FormValue(stepAddresses, "return"); got != "check" {
		t.Fatalf("return = %q; want check", got)
	}
	added := b.Post(t, stepAddresses, with(rows(mumbaiRow), "_rows", "add", "return", "check"))
	webtest.WantStatus(t, added, http.StatusOK)
	if got, _ := added.FormValue(stepAddresses, "return"); got != "check" {
		t.Errorf("return after Add = %q; want check", got)
	}
	webtest.WantRedirect(t, b.Post(t, stepAddresses, with(rows(row(mumbaiRow, "city", "Andheri")), "return", "check")), stepCheck)
	webtest.WantText(t, b.Get(t, stepCheck), "Andheri")
	webtest.WantRedirect(t, b.Post(t, stepContacts, with(rows(row(sunilRow, "first_name", "Sanjay")), "return", "check")), stepCheck)
	webtest.WantText(t, b.Get(t, stepCheck), "Sanjay")
}

// Add and Remove redraw the step and don't advance; a removed row is gone from
// the page, from the session and from the save, however the user moves.
func TestScreens_PartyWizardAddRemove(t *testing.T) {
	for _, c := range []struct {
		name, step, next, field, label string
		pageNo                         string
		first, second                  map[string]string
		wantFirst, wantSecond          string
	}{
		{"addresses", stepAddresses, stepContacts, "line1", "Add address", "Step 2 of 4", mumbaiRow, blrRow, "Plot 12, MIDC", "4 Residency Road"},
		{"contacts", stepContacts, stepCheck, "first_name", "Add contact", "Step 3 of 4", sunilRow, kiranRow, "Sunil", "Kiran"},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			before := w.tally(t)
			b := w.kavya()
			webtest.WantRedirect(t, b.Post(t, stepDetails, details("MEHTA")), stepAddresses)
			if c.step == stepContacts {
				webtest.WantRedirect(t, b.Post(t, stepAddresses, url.Values{}), stepContacts)
			}
			if label := webtest.Squash(b.Get(t, c.step).Doc.Find(`button[name="_rows"][value="add"]`).Text()); !strings.Contains(label, c.label) {
				t.Errorf("Add button = %q; want %q", label, c.label)
			}

			// Add keeps what was typed, adds a blank row and stays.
			r := b.Post(t, c.step, with(rows(c.first, c.second), "_rows", "add"))
			webtest.WantStatus(t, r, http.StatusOK)
			wantPage(t, r, c.pageNo)
			value(t, r, line(1, c.field), c.wantFirst)
			value(t, r, line(2, c.field), c.wantSecond)
			value(t, r, line(3, c.field), "")
			webtest.WantRedirect(t, b.Get(t, c.next), c.step) // the next step isn't reached

			// Remove takes row 1 and the rows close up.
			r = b.Post(t, c.step, with(rows(c.first, c.second), "_rows", "remove:1"))
			webtest.WantStatus(t, r, http.StatusOK)
			wantPage(t, r, c.pageNo)
			value(t, r, line(1, c.field), c.wantSecond)
			noControl(t, r, line(2, c.field))
			webtest.WantRedirect(t, b.Get(t, c.next), c.step)

			// The stepper away and back: still one row.
			got := b.Get(t, c.step)
			value(t, got, line(1, c.field), c.wantSecond)
			noControl(t, got, line(2, c.field))

			// Three rows held, then fewer posted by Next and by Back: no ghosts.
			webtest.WantRedirect(t, b.Post(t, c.step, rows(c.first, c.second, c.first)), c.next)
			got = b.Get(t, c.step)
			value(t, got, line(3, c.field), c.wantFirst)
			webtest.WantRedirect(t, b.Post(t, c.step, rows(c.second)), c.next)
			got = b.Get(t, c.step)
			value(t, got, line(1, c.field), c.wantSecond)
			noControl(t, got, line(2, c.field))
			webtest.WantStatus(t, b.Post(t, c.step, with(rows(c.first, c.second), "nav", "back")), http.StatusSeeOther)
			webtest.WantStatus(t, b.Post(t, c.step, with(rows(c.second), "nav", "back")), http.StatusSeeOther)
			got = b.Get(t, c.step)
			noControl(t, got, line(2, c.field))

			// A row that isn't there is a bad form, and changes nothing.
			webtest.WantStatus(t, b.Post(t, c.step, with(rows(c.second), "_rows", "remove:9")), http.StatusBadRequest)
			value(t, b.Get(t, c.step), line(1, c.field), c.wantSecond)

			// The last row removed: none left on the page or in the save.
			r = b.Post(t, c.step, with(rows(c.second), "_rows", "remove:1"))
			webtest.WantStatus(t, r, http.StatusOK)
			blankRow(t, r, line(1, c.field))
			blankRow(t, b.Get(t, c.step), line(1, c.field))
			webtest.WantRedirect(t, b.Post(t, c.step, url.Values{}), c.next)
			if c.step == stepAddresses {
				webtest.WantRedirect(t, b.Post(t, stepContacts, url.Values{}), stepCheck)
			}
			check := b.Get(t, stepCheck)
			webtest.WantNoText(t, check, c.wantFirst)
			webtest.WantNoText(t, check, c.wantSecond)
			webtest.WantStatus(t, b.Post(t, stepCheck, url.Values{}), http.StatusSeeOther)
			w.wantTally(t, before.plus(tally{parties: 1, audits: 1}))
		})
	}
}

// Rows posted with a gap are numbered 1..n first, so an error's Line is the row
// on the page.
func TestScreens_PartyWizardRowGap(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	webtest.WantRedirect(t, b.Post(t, stepDetails, details("MEHTA")), stepAddresses)
	form := rows(mumbaiRow)
	for field, v := range row(blrRow, "pincode", "012345") {
		form.Set(line(3, field), v)
	}
	r := b.Post(t, stepAddresses, form)
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	webtest.WantFieldErrorID(t, r, components.FieldID(line(2, "pincode")), msgPincode)
	value(t, r, line(2, "line1"), "4 Residency Road")
	noControl(t, r, line(3, "line1"))
}

// A registered party's shipping row may come before its billing row: the screen
// saves billing rows first, whatever the order, and each row keeps its own Line.
func TestScreens_PartyWizardShippingRowFirst(t *testing.T) {
	t.Run("saves, with the billing row primary", func(t *testing.T) {
		w := newWorld(t)
		before := w.tally(t)
		b := w.kavya()
		toCheck(t, b, details("MEHTA"), rows(blrRow, mumbaiRow), url.Values{})
		saved := b.Post(t, stepCheck, url.Values{})
		p := w.get(t, "MEHTA")
		webtest.WantRedirect(t, saved, recordURL(p.ID))
		w.wantTally(t, before.plus(tally{parties: 1, addresses: 2, audits: 3}))
		stored := map[string]address.Address{}
		for _, a := range w.partyAddresses(t, p.ID) {
			stored[a.Title] = a
		}
		bill, ship := stored["Mumbai"], stored["Bengaluru"]
		if bill.GSTIN != gstinMumbai || !bill.IsBilling || bill.IsShipping || !bill.PrimaryBilling || bill.PrimaryShipping {
			t.Errorf("billing row %+v", bill)
		}
		if ship.GSTIN != gstinBLR || ship.IsBilling || !ship.IsShipping || ship.PrimaryBilling || !ship.PrimaryShipping {
			t.Errorf("shipping row %+v", ship)
		}
	})

	billBharat := row(mumbaiRow, "title", "Bharat", "state_code", "29", "pincode", "560001", "gstin", gstinBharat)
	for _, c := range []struct {
		name, field, msg string
		rows             url.Values
	}{
		{"a required field on the billing row", "city", msgCity, rows(blrRow, row(mumbaiRow, "city", ""))},
		{"a stored rule on the billing row", "gstin", usedByBharat, rows(blrRow, billBharat)},
		{"a shipping row, with the billing row after it", "gstin", usedByBharat, rows(blrRow, row(blrRow, "title", "Second Bengaluru", "gstin", gstinBharat), mumbaiRow)},
	} {
		t.Run("a refusal stays on row 2: "+c.name, func(t *testing.T) {
			w := newWorld(t)
			before := w.tally(t)
			b := w.kavya()
			toCheck(t, b, details("MEHTA"), c.rows, url.Values{})
			refused := b.Post(t, stepCheck, url.Values{})
			webtest.WantStatus(t, refused, http.StatusUnprocessableEntity)
			wantPage(t, refused, "Step 2 of 4")
			webtest.WantFieldErrorID(t, refused, components.FieldID(line(2, c.field)), c.msg)
			webtest.WantNoText(t, refused, msgNeedBill) // the draft has its billing row
			value(t, refused, line(1, "title"), "Bengaluru")
			w.wantTally(t, before)

			webtest.WantRedirect(t, b.Post(t, stepAddresses, with(rows(blrRow, mumbaiRow), "return", "check")), stepCheck)
			webtest.WantStatus(t, b.Post(t, stepCheck, url.Values{}), http.StatusSeeOther)
			w.wantTally(t, before.plus(tally{parties: 1, addresses: 2, audits: 3}))
		})
	}

	// The order is forgiven; a billing address that isn't there still isn't.
	t.Run("no billing row is still refused", func(t *testing.T) {
		w := newWorld(t)
		before := w.tally(t)
		b := w.kavya()
		toCheck(t, b, details("MEHTA"), rows(blrRow), url.Values{})
		refused := b.Post(t, stepCheck, url.Values{})
		webtest.WantStatus(t, refused, http.StatusUnprocessableEntity)
		wantPage(t, refused, "Step 2 of 4")
		webtest.WantFieldErrorID(t, refused, components.FieldID(line(1, "is_billing")), msgNeedBill)
		w.wantTally(t, before)
	})
}

// What a row posts when nothing was typed: boxes blank, the country at its
// default, nothing ticked.
var (
	blankAddress = map[string]string{
		"title": "", "line1": "", "line2": "", "city": "", "state_code": "", "country": "India", "pincode": "", "gstin": "",
		"is_billing": "", "is_shipping": "", "is_primary_billing": "", "is_primary_shipping": "",
	}
	blankContact = map[string]string{"first_name": "", "middle_name": "", "last_name": "", "email": "", "mobile": "", "is_primary": ""}
)

// A wholly blank row is dropped at Next without an error: gone from the step,
// from Check and save and from the store, and the rows after it close up, so a
// refusal reports the row as the page now numbers it. A row with anything typed
// stays and gets its errors.
func TestScreens_PartyWizardBlankRowsDrop(t *testing.T) {
	for _, c := range []struct {
		name, step, next, pageNo, field string
		blank, first, second            map[string]string
		wantFirst, wantSecond           string
		onNext, onSave                  map[string]string // refused there, as row 3 of 3 posted
		onNextField, onNextMsg          string
		onSaveField, onSaveMsg          string
		saved                           tally
	}{
		{
			name: "addresses", step: stepAddresses, next: stepContacts, pageNo: "Step 2 of 4", field: "line1",
			blank: blankAddress, first: mumbaiRow, second: blrRow, wantFirst: "Plot 12, MIDC", wantSecond: "4 Residency Road",
			onNext: row(blrRow, "pincode", "012345"), onNextField: "pincode", onNextMsg: msgPincode,
			onSave: row(blrRow, "gstin", gstinBharat), onSaveField: "gstin", onSaveMsg: usedByBharat,
			saved: tally{parties: 1, addresses: 2, audits: 3},
		},
		{
			name: "contacts", step: stepContacts, next: stepCheck, pageNo: "Step 3 of 4", field: "first_name",
			blank: blankContact, first: sunilRow, second: kiranRow, wantFirst: "Sunil", wantSecond: "Kiran",
			onNext: row(kiranRow, "mobile", "5820012345"), onNextField: "mobile", onNextMsg: msgMobile,
			onSave: row(kiranRow, "first_name", ""), onSaveField: "first_name", onSaveMsg: msgName,
			saved: tally{parties: 1, contacts: 2, emails: 2, phones: 2, audits: 3},
		},
	} {
		reach := func(t *testing.T, b *webtest.Browser) {
			t.Helper()
			webtest.WantRedirect(t, b.Post(t, stepDetails, details("MEHTA")), stepAddresses)
			if c.step == stepContacts {
				webtest.WantRedirect(t, b.Post(t, stepAddresses, url.Values{}), stepContacts)
			}
		}
		// checkText posts the step, walks to Check and save and is that page's text.
		checkText := func(t *testing.T, b *webtest.Browser, form url.Values) string {
			t.Helper()
			reach(t, b)
			webtest.WantRedirect(t, b.Post(t, c.step, form), c.next)
			if c.step == stepAddresses {
				webtest.WantRedirect(t, b.Post(t, stepContacts, url.Values{}), stepCheck)
			}
			return b.Get(t, stepCheck).Text()
		}

		t.Run(c.name+": dropped wherever it stands", func(t *testing.T) {
			w := newWorld(t)
			before := w.tally(t)
			b := w.kavya()
			got := checkText(t, b, rows(c.blank, c.first, c.blank, c.second, c.blank))
			if want := checkText(t, w.kavya(), rows(c.first, c.second)); got != want {
				t.Errorf("Check and save differs with blank rows:\n got %s\nwant %s", got, want)
			}
			page := b.Get(t, c.step)
			value(t, page, line(1, c.field), c.wantFirst)
			value(t, page, line(2, c.field), c.wantSecond)
			noControl(t, page, line(3, c.field))
			w.wantTally(t, before)
			webtest.WantRedirect(t, b.Post(t, stepCheck, url.Values{}), recordURL(w.get(t, "MEHTA").ID))
			w.wantTally(t, before.plus(c.saved))
		})

		t.Run(c.name+": only blank rows", func(t *testing.T) {
			w := newWorld(t)
			before := w.tally(t)
			b := w.kavya()
			got := checkText(t, b, rows(c.blank, c.blank))
			if want := checkText(t, w.kavya(), url.Values{}); got != want {
				t.Errorf("Check and save differs with blank rows:\n got %s\nwant %s", got, want)
			}
			webtest.WantRedirect(t, b.Post(t, stepCheck, url.Values{}), recordURL(w.get(t, "MEHTA").ID))
			w.wantTally(t, before.plus(tally{parties: 1, audits: 1}))
		})

		t.Run(c.name+": a refusal on Next reports the closed-up row", func(t *testing.T) {
			w := newWorld(t)
			b := w.kavya()
			reach(t, b)
			r := b.Post(t, c.step, rows(c.first, c.blank, c.onNext))
			webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
			wantPage(t, r, c.pageNo)
			webtest.WantFieldErrorID(t, r, components.FieldID(line(2, c.onNextField)), c.onNextMsg)
			value(t, r, line(2, c.field), c.wantSecond)
			noControl(t, r, line(3, c.field))
			held := b.Get(t, c.step)
			value(t, held, line(2, c.field), c.wantSecond)
			noControl(t, held, line(3, c.field))
		})

		t.Run(c.name+": a refusal at Check and save reports the closed-up row", func(t *testing.T) {
			w := newWorld(t)
			before := w.tally(t)
			b := w.kavya()
			checkText(t, b, rows(c.first, c.blank, c.onSave))
			r := b.Post(t, stepCheck, url.Values{})
			webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
			wantPage(t, r, c.pageNo)
			webtest.WantFieldErrorID(t, r, components.FieldID(line(2, c.onSaveField)), c.onSaveMsg)
			noControl(t, r, line(3, c.field))
			w.wantTally(t, before)
		})
	}

	for field, typed := range map[string]string{
		"title": "Godown", "line1": "Plot 9", "line2": "Phase 2", "city": "Thane", "state_code": "27",
		"pincode": "400601", "gstin": gstinMumbai, "country": "Singapore",
	} {
		t.Run("an address row with only "+field+" stays", func(t *testing.T) {
			w := newWorld(t)
			b := w.kavya()
			webtest.WantRedirect(t, b.Post(t, stepDetails, details("MEHTA")), stepAddresses)
			r := b.Post(t, stepAddresses, rows(mumbaiRow, row(blankAddress, field, typed)))
			webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
			webtest.WantFieldErrorID(t, r, components.FieldID(line(2, "is_billing")), msgType)
			value(t, r, line(2, field), typed)
		})
	}

	for field, typed := range map[string]string{
		"middle_name": "K", "last_name": "Joshi", "email": "kiran@mehtafab.test", "mobile": "9820054321",
	} {
		t.Run("a contact row with only "+field+" stays", func(t *testing.T) {
			w := newWorld(t)
			before := w.tally(t)
			b := w.kavya()
			toCheck(t, b, details("MEHTA"), url.Values{}, rows(sunilRow, row(blankContact, field, typed)))
			value(t, b.Get(t, stepContacts), line(2, field), typed)
			refused := b.Post(t, stepCheck, url.Values{})
			webtest.WantStatus(t, refused, http.StatusUnprocessableEntity)
			wantPage(t, refused, "Step 3 of 4")
			webtest.WantFieldErrorID(t, refused, components.FieldID(line(2, "first_name")), msgName)
			w.wantTally(t, before)
		})
	}

	t.Run("a contact row with only first_name is saved", func(t *testing.T) {
		w := newWorld(t)
		before := w.tally(t)
		b := w.kavya()
		toCheck(t, b, details("MEHTA"), url.Values{}, rows(sunilRow, row(blankContact, "first_name", "Kiran")))
		webtest.WantStatus(t, b.Post(t, stepCheck, url.Values{}), http.StatusSeeOther)
		w.wantTally(t, before.plus(tally{parties: 1, contacts: 2, emails: 1, phones: 1, audits: 3}))
	})
}

// A step hears its own format rules on Next, a row's in that row. Required
// fields and rules that need the database wait for Check and save.
func TestScreens_PartyWizardStepErrors(t *testing.T) {
	w := newWorld(t)
	before := w.tally(t)
	b := w.kavya()

	for _, c := range []struct {
		name, field, value, id, msg string
	}{
		{"no role", "is_vendor", "", "is_vendor", msgRole},
		{"PAN", "pan", "AABCS1429", "pan", msgPAN},
		{"code", "code", strings.Repeat("C", 21), "code", msgCodeForm},
		{"category", "gst_category", "Registered", "gst_category", msgCategory},
		{"terms", "default_payment_terms_id", "abc", "default_payment_terms_id", msgNoTerms},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := b.Post(t, stepDetails, with(details("MEHTA"), c.field, c.value))
			webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
			webtest.WantFieldErrorID(t, r, components.FieldID(c.id), c.msg)
			wantPage(t, r, "Step 1 of 4")
		})
	}

	// Blank required fields pass Next.
	webtest.WantRedirect(t, b.Post(t, stepDetails, with(details(""), "name", "")), stepAddresses)
	webtest.WantRedirect(t, b.Post(t, stepDetails, details("MEHTA")), stepAddresses)

	for _, c := range []struct {
		name string
		rows url.Values
		id   string
		msg  string
	}{
		{"pincode on row 2", rows(mumbaiRow, row(blrRow, "pincode", "40001")), line(2, "pincode"), msgPincode},
		{"pincode's state", rows(row(mumbaiRow, "pincode", "500001")), line(1, "pincode"), "Pincode 500001 is not in Maharashtra. Check the pincode or the state."},
		{"GSTIN", rows(row(mumbaiRow, "gstin", "27AABCS1429B1Z5")), line(1, "gstin"), "GSTIN 27AABCS1429B1Z5 is not valid: the last character doesn't match. Check for typing mistakes."},
		{"GSTIN's state", rows(row(mumbaiRow, "state_code", "29", "pincode", "560001")), line(1, "gstin"), "GSTIN " + gstinMumbai + " is for Maharashtra (27) but the address state is Karnataka (29). Fix the GSTIN or the state."},
		{"type", rows(row(mumbaiRow, "is_billing", "")), line(1, "is_billing"), msgType},
		{"state", rows(row(mumbaiRow, "state_code", "", "gstin", "")), line(1, "state_code"), msgState},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := b.Post(t, stepAddresses, c.rows)
			webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
			webtest.WantFieldErrorID(t, r, components.FieldID(c.id), c.msg)
			webtest.WantNoText(t, r, msgOwner) // the party doesn't exist yet: it isn't the row's fault
			wantPage(t, r, "Step 2 of 4")
		})
	}
	webtest.WantRedirect(t, b.Post(t, stepAddresses, rows(row(mumbaiRow, "line1", ""), row(blrRow, "city", ""))), stepContacts)

	for _, c := range []struct {
		name, id, msg string
		rows          url.Values
	}{
		{"email", line(1, "email"), msgEmail, rows(row(sunilRow, "email", "accounts@shreesteel"))},
		{"mobile on row 2", line(2, "mobile"), msgMobile, rows(sunilRow, row(kiranRow, "mobile", "5820012345"))},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := b.Post(t, stepContacts, c.rows)
			webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
			webtest.WantFieldErrorID(t, r, components.FieldID(c.id), c.msg)
			webtest.WantNoText(t, r, "A contact must belong to one party or one branch.")
			wantPage(t, r, "Step 3 of 4")
		})
	}
	webtest.WantRedirect(t, b.Post(t, stepContacts, rows(row(sunilRow, "first_name", ""))), stepCheck)

	w.wantTally(t, before)
}

// All rows' errors show together on their own step, each in its row, with the
// rules the database decides next to the pure ones, not just the first.
func TestScreens_PartyWizardAllRowErrors(t *testing.T) {
	w := newWorld(t)
	before := w.tally(t)
	b := w.kavya()
	a := rows(
		row(mumbaiRow, "line1", ""),
		row(blrRow, "city", ""),
		row(blrRow, "title", "Second Bengaluru", "gstin", gstinBharat),
		row(blrRow, "title", "Rajkot", "city", "Rajkot", "state_code", "24", "pincode", "360001", "gstin", gstinRajkot),
	)
	c := rows(row(sunilRow, "first_name", ""), row(kiranRow, "first_name", ""))
	toCheck(t, b, details("MEHTA"), a, c)

	refused := b.Post(t, stepCheck, url.Values{})
	webtest.WantStatus(t, refused, http.StatusUnprocessableEntity)
	wantPage(t, refused, "Step 2 of 4")
	webtest.WantFieldErrorID(t, refused, components.FieldID(line(1, "line1")), msgLine1)
	webtest.WantFieldErrorID(t, refused, components.FieldID(line(2, "city")), msgCity)
	webtest.WantFieldErrorID(t, refused, components.FieldID(line(3, "gstin")), usedByBharat)
	webtest.WantFieldErrorID(t, refused, components.FieldID(line(4, "gstin")), usedByRajkot)
	webtest.WantNoText(t, refused, msgName) // a later step's errors wait for it
	value(t, refused, line(2, "title"), "Bengaluru")
	if got := refused.Header.Get("HX-Push-Url"); got != stepAddresses {
		t.Errorf("HX-Push-Url = %q; want %q", got, stepAddresses)
	}
	if got, _ := refused.FormValue(stepAddresses, "return"); got != "check" {
		t.Errorf("return = %q; want check", got)
	}
	w.wantTally(t, before)
	if _, ok := w.byCode(t, "MEHTA"); ok {
		t.Errorf("a refused save wrote the party")
	}

	// Fixed, the save goes on to the next step's errors, all of them.
	webtest.WantRedirect(t, b.Post(t, stepAddresses, with(rows(mumbaiRow, blrRow), "return", "check")), stepCheck)
	refused = b.Post(t, stepCheck, url.Values{})
	webtest.WantStatus(t, refused, http.StatusUnprocessableEntity)
	wantPage(t, refused, "Step 3 of 4")
	webtest.WantFieldErrorID(t, refused, components.FieldID(line(1, "first_name")), msgName)
	webtest.WantFieldErrorID(t, refused, components.FieldID(line(2, "first_name")), msgName)
	w.wantTally(t, before)
}

// Errors on two steps show on the first, alone; a stored rule on the party's
// own step comes first.
func TestScreens_PartyWizardErrorsOnFirstStep(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	toCheck(t, b, with(details(""), "name", ""), rows(row(mumbaiRow, "city", "")), url.Values{})
	refused := b.Post(t, stepCheck, url.Values{})
	webtest.WantStatus(t, refused, http.StatusUnprocessableEntity)
	wantPage(t, refused, "Step 1 of 4")
	webtest.WantFieldErrorID(t, refused, components.FieldID("code"), msgCodeReq)
	webtest.WantFieldErrorID(t, refused, components.FieldID("name"), msgNameReq)
	webtest.WantNoText(t, refused, msgCity)

	webtest.WantRedirect(t, b.Post(t, stepDetails, with(details("BHARAT"), "return", "check")), stepCheck)
	webtest.WantRedirect(t, b.Post(t, stepAddresses, with(rows(mumbaiRow), "return", "check")), stepCheck)
	dup := b.Post(t, stepCheck, url.Values{})
	webtest.WantStatus(t, dup, http.StatusUnprocessableEntity)
	webtest.WantFieldErrorID(t, dup, components.FieldID("code"), "Code BHARAT is already used by Party Bharat Tools. Choose another code.")
}

// A rule whose cause is on another step waits for Check and save, and never
// strands the user on Next; there it shows in its row, and a fix on the other
// step clears it.
func TestScreens_PartyWizardCrossStepRules(t *testing.T) {
	for _, c := range []struct {
		name string
		d    url.Values
		row  map[string]string
		id   string
		msg  string
		fix  url.Values
	}{
		{
			"a registered party's billing address needs a GSTIN", details("MEHTA"),
			row(mumbaiRow, "gstin", ""), line(1, "gstin"), msgNeedGSTIN,
			with(details("MEHTA"), "gst_category", "Unregistered"),
		},
		{
			"an address outside India needs category Overseas", details("MEHTA"),
			row(mumbaiRow, "country", "Singapore", "state_code", "", "pincode", "118524", "gstin", ""), line(1, "country"), msgOverseas,
			with(details("MEHTA"), "gst_category", "Overseas", "pan", ""),
		},
		{
			"a GSTIN must carry the party's PAN", details("MEHTA"),
			row(mumbaiRow, "state_code", "29", "pincode", "560001", "gstin", gstinShree), line(1, "gstin"), otherPAN,
			with(details("MEHTA"), "pan", "AABCS1429B"),
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			before := w.tally(t)
			b := w.kavya()
			toCheck(t, b, c.d, rows(c.row), url.Values{}) // no Next refused anything
			refused := b.Post(t, stepCheck, url.Values{})
			webtest.WantStatus(t, refused, http.StatusUnprocessableEntity)
			wantPage(t, refused, "Step 2 of 4")
			webtest.WantFieldErrorID(t, refused, components.FieldID(c.id), c.msg)
			w.wantTally(t, before)

			webtest.WantRedirect(t, b.Post(t, stepDetails, with(c.fix, "return", "check")), stepCheck)
			webtest.WantStatus(t, b.Post(t, stepCheck, url.Values{}), http.StatusSeeOther)
			if got := w.tally(t); got.parties != before.parties+1 || got.addresses != before.addresses+1 {
				t.Errorf("tally = %+v; before %+v", got, before)
			}
		})
	}
}

// Back saves a row without validating it, and the steps after it still move:
// its error waits for Check and save, in its row on its step.
func TestScreens_PartyWizardBackNeverStrands(t *testing.T) {
	w := newWorld(t)
	before := w.tally(t)
	b := w.kavya()
	toCheck(t, b, details("MEHTA"), rows(mumbaiRow), rows(sunilRow))

	webtest.WantRedirect(t, b.Post(t, stepAddresses, with(rows(row(mumbaiRow, "pincode", "012345")), "nav", "back")), stepDetails)
	value(t, b.Get(t, stepAddresses), line(1, "pincode"), "012345")
	webtest.WantRedirect(t, b.Post(t, stepDetails, details("MEHTA")), stepAddresses)
	webtest.WantRedirect(t, b.Post(t, stepContacts, rows(sunilRow)), stepCheck)

	refused := b.Post(t, stepCheck, url.Values{})
	webtest.WantStatus(t, refused, http.StatusUnprocessableEntity)
	wantPage(t, refused, "Step 2 of 4")
	webtest.WantFieldErrorID(t, refused, components.FieldID(line(1, "pincode")), msgPincode)
	value(t, refused, line(1, "pincode"), "012345")

	webtest.WantRedirect(t, b.Post(t, stepAddresses, with(rows(mumbaiRow), "return", "check")), stepCheck)
	webtest.WantStatus(t, b.Post(t, stepContacts, with(rows(row(sunilRow, "email", "bad@mail")), "nav", "back")), http.StatusSeeOther)
	refused = b.Post(t, stepCheck, url.Values{})
	webtest.WantStatus(t, refused, http.StatusUnprocessableEntity)
	wantPage(t, refused, "Step 3 of 4")
	webtest.WantFieldErrorID(t, refused, components.FieldID(line(1, "email")), msgEmail)
	w.wantTally(t, before)
}

// The link on the details step: not numeric on Next; unknown or disabled at
// Check and save, on its step; blank is none, and Clear (CD-217) posts blank.
func TestScreens_PartyWizardTermsLink(t *testing.T) {
	w := newWorld(t)
	terms := w.terms(t, "NET30", "Net 30 days")
	b := w.kavya()
	id := strconv.FormatInt(terms, 10)

	toCheck(t, b, with(details("MEHTA"), "default_payment_terms_id", "999999"), url.Values{}, url.Values{})
	check := b.Get(t, stepCheck)
	webtest.WantStatus(t, check, http.StatusOK)
	webtest.WantText(t, check, "999999") // can't be looked up, and still shows
	refused := b.Post(t, stepCheck, url.Values{})
	webtest.WantStatus(t, refused, http.StatusUnprocessableEntity)
	wantPage(t, refused, "Step 1 of 4")
	webtest.WantFieldErrorID(t, refused, components.FieldID("default_payment_terms_id"), msgNoTerms)

	// A chosen value offers Clear, and a blank post clears the draft.
	webtest.WantRedirect(t, b.Post(t, stepDetails, with(details("MEHTA"), "default_payment_terms_id", id, "return", "check")), stepCheck)
	page := b.Get(t, stepDetails)
	if n := page.Doc.Find(`[data-picker-clear][aria-label="Clear Payment Terms"]`).Length(); n != 1 {
		t.Errorf("Clear controls on Payment Terms = %d; want 1", n)
	}
	webtest.WantRedirect(t, b.Post(t, stepDetails, with(details("MEHTA"), "default_payment_terms_id", "", "return", "check")), stepCheck)
	webtest.WantNoText(t, b.Get(t, stepCheck), "NET30")
	webtest.WantRedirect(t, b.Post(t, stepCheck, url.Values{}), recordURL(w.get(t, "MEHTA").ID))
	if got := w.get(t, "MEHTA").DefaultPaymentTermsID; got != 0 {
		t.Errorf("terms = %d; want none", got)
	}

	second := details("MEHTA2")
	second.Set("default_payment_terms_id", id)
	toCheck(t, b, second, url.Values{}, url.Values{})
	w.exec(t, `UPDATE payment_terms SET disabled = 1 WHERE id = ?`, terms)
	off := b.Post(t, stepCheck, url.Values{})
	webtest.WantStatus(t, off, http.StatusUnprocessableEntity)
	webtest.WantFieldErrorID(t, off, components.FieldID("default_payment_terms_id"), msgOffTerms)
	if _, ok := w.byCode(t, "MEHTA2"); ok {
		t.Errorf("a refused save wrote the party")
	}
}

// Tests with exact row counts: a refusal on the last contact or address leaves
// no partial records, the session keeps the draft, and the fix then saves all
// of it.
func TestScreens_PartyWizardRefusalWritesNothing(t *testing.T) {
	t.Run("on the last contact", func(t *testing.T) {
		w := newWorld(t)
		before := w.tally(t)
		b := w.kavya()
		toCheck(t, b, details("MEHTA"), rows(mumbaiRow, blrRow), rows(sunilRow, row(kiranRow, "first_name", "")))
		refused := b.Post(t, stepCheck, url.Values{})
		webtest.WantStatus(t, refused, http.StatusUnprocessableEntity)
		wantPage(t, refused, "Step 3 of 4")
		webtest.WantFieldErrorID(t, refused, components.FieldID(line(2, "first_name")), msgName)
		w.wantTally(t, before)
		value(t, b.Get(t, stepDetails), "code", "MEHTA")
		value(t, b.Get(t, stepAddresses), line(2, "gstin"), gstinBLR)

		webtest.WantRedirect(t, b.Post(t, stepContacts, with(rows(sunilRow, kiranRow), "return", "check")), stepCheck)
		webtest.WantStatus(t, b.Post(t, stepCheck, url.Values{}), http.StatusSeeOther)
		w.wantTally(t, before.plus(tally{parties: 1, addresses: 2, contacts: 2, emails: 2, phones: 2, audits: 5}))
	})

	// The party and the first address were good, so a write that stops here has
	// something to roll back.
	t.Run("on the last address, with contacts to come", func(t *testing.T) {
		w := newWorld(t)
		before := w.tally(t)
		b := w.kavya()
		toCheck(t, b, details("MEHTA"), rows(mumbaiRow, row(blrRow, "gstin", gstinBharat)), rows(sunilRow))
		refused := b.Post(t, stepCheck, url.Values{})
		webtest.WantStatus(t, refused, http.StatusUnprocessableEntity)
		wantPage(t, refused, "Step 2 of 4")
		webtest.WantFieldErrorID(t, refused, components.FieldID(line(2, "gstin")), usedByBharat)
		w.wantTally(t, before)
		webtest.WantRedirect(t, b.Post(t, stepAddresses, with(rows(mumbaiRow), "return", "check")), stepCheck)
		webtest.WantStatus(t, b.Post(t, stepCheck, url.Values{}), http.StatusSeeOther)
		w.wantTally(t, before.plus(tally{parties: 1, addresses: 1, contacts: 1, emails: 1, phones: 1, audits: 3}))
	})
}

// A finished wizard can't be replayed, and the next one starts empty, rows too.
func TestScreens_PartyWizardNoReplay(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	toCheck(t, b, details("MEHTA"), rows(mumbaiRow, blrRow), rows(sunilRow))
	webtest.WantStatus(t, b.Post(t, stepCheck, url.Values{}), http.StatusSeeOther)
	after := w.tally(t)

	webtest.WantRedirect(t, b.Post(t, stepCheck, url.Values{}), stepDetails)
	w.wantTally(t, after)

	value(t, b.Get(t, stepDetails), "code", "")
	webtest.WantRedirect(t, b.Get(t, stepAddresses), stepDetails)
	webtest.WantRedirect(t, b.Post(t, stepDetails, details("MEHTA2")), stepAddresses)
	noControl(t, b.Get(t, stepAddresses), line(2, "line1"))
	blankRow(t, b.Get(t, stepAddresses), line(1, "line1"))
}

// Two tabs press Save at once: one party, every row once, and the session can't
// make a second.
func TestScreens_PartyWizardDoubleSubmit(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	toCheck(t, b, details("MEHTA"), rows(mumbaiRow, blrRow), rows(sunilRow, kiranRow))
	before := w.tally(t)
	const tabs = 4
	statuses := make([]int, tabs)
	var wg sync.WaitGroup
	for i := range tabs {
		tab := b.Clone()
		wg.Go(func() { statuses[i] = tab.Post(t, stepCheck, url.Values{}).Status })
	}
	wg.Wait()
	saved := 0
	for _, s := range statuses {
		switch s {
		case http.StatusSeeOther:
			saved++
		case http.StatusUnprocessableEntity:
		default:
			t.Errorf("status %d; want 303 or 422", s)
		}
	}
	if saved < 1 {
		t.Errorf("no tab saved: %v", statuses)
	}
	w.wantTally(t, before.plus(tally{parties: 1, addresses: 2, contacts: 2, emails: 2, phones: 2, audits: 5}))
}

// Warnings from the party's save and each address's are merged, and the two PAN
// warnings are one (AD-5). After the redirect the record page shows exactly one.
func TestScreens_PartyWizardWarnings(t *testing.T) {
	panBanners := func(t *testing.T, r webtest.Response) []string {
		t.Helper()
		var out []string
		for _, s := range r.Banners() {
			if strings.Contains(s, shreePANNote) {
				out = append(out, s)
			}
		}
		return out
	}
	shreeRow := row(blrRow, "gstin", gstinShree, "is_billing", "1", "is_shipping", "")

	t.Run("party and address both warn", func(t *testing.T) {
		w := newWorld(t)
		b := w.kavya()
		d := details("SHREE2")
		d.Set("name", "Shree Steel Traders")
		d.Set("pan", "AABCS1429B")
		toCheck(t, b, d, rows(shreeRow), url.Values{})
		saved := b.Post(t, stepCheck, url.Values{})
		webtest.WantRedirect(t, saved, recordURL(w.get(t, "SHREE2").ID))
		page := b.Get(t, saved.Location)
		if got := panBanners(t, page); len(got) != 1 {
			t.Errorf("PAN warnings = %q; want exactly one", got)
		}
		webtest.WantBanner(t, page, "A party named Shree Steel Traders already exists (SHREE).")
		webtest.WantBanner(t, page, "Party saved.")
	})

	t.Run("two addresses with one GSTIN", func(t *testing.T) {
		w := newWorld(t)
		b := w.kavya()
		d := details("SHREE2")
		d.Set("pan", "AABCS1429B")
		toCheck(t, b, d, rows(shreeRow, row(shreeRow, "title", "Godown")), url.Values{})
		saved := b.Post(t, stepCheck, url.Values{})
		webtest.WantStatus(t, saved, http.StatusSeeOther)
		if got := panBanners(t, b.Get(t, saved.Location)); len(got) != 1 {
			t.Errorf("PAN warnings = %q; want exactly one", got)
		}
	})

	t.Run("the address's alone keeps its text", func(t *testing.T) {
		w := newWorld(t)
		b := w.kavya()
		toCheck(t, b, with(details("SHREE2"), "pan", ""), rows(shreeRow), url.Values{})
		saved := b.Post(t, stepCheck, url.Values{})
		webtest.WantStatus(t, saved, http.StatusSeeOther)
		page := b.Get(t, saved.Location)
		if got := panBanners(t, page); len(got) != 1 {
			t.Errorf("PAN warnings = %q; want exactly one", got)
		}
		webtest.WantBanner(t, page, "Party SHREE has the same PAN AABCS1429B. You may be creating a duplicate; consider adding a Karnataka address to SHREE.")
	})
}

// Every step, GET and POST, refuses a user without create with AUTH's 403, the
// row buttons too.
func TestScreens_PartyWizardRefusesWithoutCreate(t *testing.T) {
	w := newWorld(t)
	before := w.tally(t)
	priya := w.h.Browser(w.h.Env.Priya)
	for _, target := range []string{wizardMain, stepDetails, stepAddresses, stepContacts, stepCheck} {
		r := priya.Get(t, target)
		webtest.WantStatus(t, r, http.StatusForbidden)
		webtest.WantText(t, r, msgNoCreate)
	}
	for _, target := range []string{stepDetails, stepAddresses, stepContacts, stepCheck, startOver} {
		r := priya.Post(t, target, details("MEHTA"))
		webtest.WantStatus(t, r, http.StatusForbidden)
		webtest.WantText(t, r, msgNoCreate)
	}
	r := priya.Post(t, stepAddresses, with(rows(mumbaiRow), "_rows", "add"))
	webtest.WantStatus(t, r, http.StatusForbidden)
	webtest.WantText(t, r, msgNoCreate)
	if _, found := priya.Get(t, base).RowLink(wizardMain); found {
		t.Errorf("the list offers New to a user without create")
	}
	if _, found := w.kavya().Get(t, base).RowLink(wizardMain); !found {
		t.Errorf("the list offers no New to Kavya")
	}
	w.wantTally(t, before)
}

// A user who can create Party but not Address or Contact learns at entry, with
// AUTH's 403, on every step, not at the last one.
func TestScreens_PartyWizardEntryRights(t *testing.T) {
	for _, c := range []struct{ docType, msg string }{
		{address.DocType, "You don't have permission to create Address. Ask your administrator for a role that allows it."},
		{address.ContactDocType, "You don't have permission to create Contact. Ask your administrator for a role that allows it."},
	} {
		t.Run(c.docType, func(t *testing.T) {
			w := newWorld(t)
			ctx := context.Background()
			env := w.h.Env
			a := env.Kit.Auth.(*auth.Auth)
			for _, role := range []string{"Masters Manager", "Employee"} {
				if err := a.SetRights(ctx, env.Admin, role, c.docType, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll}); err != nil {
					t.Fatal(err)
				}
			}
			if err := env.Kit.Auth.Can(ctx, env.Kavya, party.DocType, auth.RightCreate, nil); err != nil {
				t.Fatalf("setup: Kavya can't create Party: %v", err)
			}
			if err := env.Kit.Auth.Can(ctx, env.Kavya, c.docType, auth.RightCreate, nil); err == nil {
				t.Fatalf("setup: Kavya can still create %s", c.docType)
			}
			b := w.kavya()
			for _, target := range []string{wizardMain, stepDetails, stepAddresses, stepContacts, stepCheck} {
				r := b.Get(t, target)
				webtest.WantStatus(t, r, http.StatusForbidden)
				webtest.WantText(t, r, c.msg)
			}
			r := b.Post(t, stepDetails, details("MEHTA"))
			webtest.WantStatus(t, r, http.StatusForbidden)
			webtest.WantText(t, r, c.msg)
			if _, found := b.Get(t, base).RowLink(wizardMain); found {
				t.Errorf("the list offers New to a user who can't finish it")
			}
		})
	}
}

// bigRows is n rows of the given fields, each value about size bytes.
func bigRows(n, size int, fixed map[string]string, fields ...string) url.Values {
	v := url.Values{}
	for i := 1; i <= n; i++ {
		for field, x := range fixed {
			v.Set(line(i, field), x)
		}
		for _, field := range fields {
			v.Set(line(i, field), strings.Repeat("x", size))
		}
	}
	return v
}

// The row limit's rows fit in the session (the kit's comment: 50 addresses and
// 50 contacts at about 100 bytes a field), and save.
func TestScreens_PartyWizardDraftFits(t *testing.T) {
	w := newWorld(t)
	before := w.tally(t)
	b := w.kavya()
	webtest.WantRedirect(t, b.Post(t, stepDetails, with(details("MEHTA"), "pan", "", "gst_category", "Unregistered")), stepAddresses)
	a := bigRows(50, 100, map[string]string{"state_code": "27", "pincode": "400001", "is_billing": "1"}, "title", "line1", "line2", "city")
	webtest.WantRedirect(t, b.Post(t, stepAddresses, a), stepContacts)
	c := bigRows(50, 100, map[string]string{"email": strings.Repeat("a", 86) + "@mehtafab.test", "mobile": "9820012345"}, "first_name", "middle_name", "last_name")
	webtest.WantRedirect(t, b.Post(t, stepContacts, c), stepCheck)
	value(t, b.Get(t, stepAddresses), line(50, "city"), strings.Repeat("x", 100))
	value(t, b.Get(t, stepContacts), line(50, "first_name"), strings.Repeat("x", 100))
	webtest.WantStatus(t, b.Post(t, stepCheck, url.Values{}), http.StatusSeeOther)
	w.wantTally(t, before.plus(tally{parties: 1, addresses: 50, contacts: 50, emails: 50, phones: 50, audits: 101}))
}

// A bigger draft is refused with the kit's message, by Next, Back and Add, and
// none of it reaches the session; the cap is on the whole draft, not one step.
func TestScreens_PartyWizardDraftCap(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	webtest.WantRedirect(t, b.Post(t, stepDetails, details("MEHTA")), stepAddresses)
	held := rows(mumbaiRow)
	webtest.WantRedirect(t, b.Post(t, stepAddresses, held), stepContacts)

	huge := bigRows(300, 900, map[string]string{"state_code": "27", "pincode": "400001", "is_billing": "1"}, "title", "line1", "line2", "city")
	for name, form := range map[string]url.Values{
		"next": huge,
		"back": with(huge, "nav", "back"),
		"add":  with(huge, "_rows", "add"),
	} {
		t.Run(name, func(t *testing.T) {
			r := b.Post(t, stepAddresses, form)
			webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
			webtest.WantText(t, r, msgTooBig)
			page := b.Get(t, stepAddresses)
			value(t, page, line(1, "line1"), "Plot 12, MIDC")
			noControl(t, page, line(2, "line1"))
		})
	}

	// Each step alone is under the cap; both together are over it.
	a := bigRows(50, 450, map[string]string{"state_code": "27", "pincode": "400001", "is_shipping": "1"}, "title", "line1", "line2", "city")
	webtest.WantRedirect(t, b.Post(t, stepAddresses, a), stepContacts)
	c := bigRows(50, 800, map[string]string{"email": "a@mehtafab.test", "mobile": "9820012345"}, "first_name", "middle_name", "last_name")
	r := b.Post(t, stepContacts, c)
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	webtest.WantText(t, r, msgTooBig)
	blankRow(t, b.Get(t, stepContacts), line(1, "first_name"))
}

// Start over drops the whole draft, rows included, writes nothing, and only
// shows with a draft; another user's press leaves it alone.
func TestScreens_PartyWizardStartOver(t *testing.T) {
	w := newWorld(t)
	before := w.tally(t)
	b := w.kavya()
	offered := func(r webtest.Response) bool {
		return r.Doc.Find(`form[action="`+startOver+`"] button`).Length() == 1
	}
	if offered(b.Get(t, wizardMain)) {
		t.Errorf("Start over offered with no draft")
	}
	toCheck(t, b, details("MEHTA"), rows(mumbaiRow, blrRow), rows(sunilRow))
	for _, step := range []string{stepDetails, stepAddresses, stepContacts, stepCheck} {
		if !offered(b.Get(t, step)) {
			t.Errorf("%s: Start over not offered with a draft", step)
		}
	}

	webtest.WantRedirect(t, b.As(w.h.Env.Admin).Post(t, startOver, url.Values{}), stepDetails)
	value(t, b.Get(t, stepDetails), "code", "MEHTA")

	webtest.WantRedirect(t, b.Post(t, startOver, url.Values{}), stepDetails)
	value(t, b.Get(t, stepDetails), "code", "")
	webtest.WantRedirect(t, b.Get(t, stepAddresses), stepDetails)
	webtest.WantRedirect(t, b.Get(t, stepCheck), stepDetails)
	w.wantTally(t, before)

	// The next draft has no ghost of the rows.
	webtest.WantRedirect(t, b.Post(t, stepDetails, details("MEHTA2")), stepAddresses)
	page := b.Get(t, stepAddresses)
	noControl(t, page, line(2, "line1"))
	blankRow(t, page, line(1, "line1"))
}

// One session holds a Party and an Employee draft apart, and Start over on one
// leaves the other.
func TestScreens_PartyAndEmployeeDraftsDontMix(t *testing.T) {
	h := webtest.NewSessions(t, func(k mstkit.Env, sm *scs.SessionManager) func(chi.Router) {
		return func(r chi.Router) {
			mount(k, sm)(r)
			employeeweb.Routes(employee.New(k), k, sm)(r)
		}
	})
	const employeeStep = "/m/employee/new/details"
	b := h.Browser(h.Env.Kavya)
	webtest.WantRedirect(t, b.Post(t, stepDetails, details("MEHTA")), stepAddresses)
	webtest.WantRedirect(t, b.Post(t, employeeStep, url.Values{
		"code": {"EMP-101"}, "name": {"Nisha Menon"}, "joining_date": {"01-04-2024"}, "status": {"Active"},
	}), "/m/employee/new/reporting")

	value(t, b.Get(t, stepDetails), "code", "MEHTA")
	value(t, b.Get(t, employeeStep), "code", "EMP-101")
	webtest.WantRedirect(t, b.Post(t, stepAddresses, rows(mumbaiRow)), stepContacts)
	value(t, b.Get(t, employeeStep), "code", "EMP-101")

	webtest.WantRedirect(t, b.Post(t, startOver, url.Values{}), stepDetails)
	value(t, b.Get(t, stepDetails), "code", "")
	value(t, b.Get(t, employeeStep), "code", "EMP-101")
}

// The session key names the user: two users on one browser don't see each
// other's wizard.
func TestScreens_PartyWizardUsersDontMix(t *testing.T) {
	w := newWorld(t)
	kavya := w.kavya()
	webtest.WantRedirect(t, kavya.Post(t, stepDetails, details("MEHTA")), stepAddresses)
	shared := kavya.As(w.h.Env.Admin)
	value(t, shared.Get(t, stepDetails), "code", "")
	webtest.WantRedirect(t, shared.Post(t, stepDetails, details("OTHER")), stepAddresses)
	value(t, kavya.Get(t, stepDetails), "code", "MEHTA")
}

// The inputs: GSTIN, pincode and PAN are CodeInputs, the state is a Select of
// the GST states, and the labels are Line 1, Line 2 and Payment Terms (CD-199).
func TestScreens_PartyWizardInputs(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	wantCode(t, b.Get(t, stepDetails), "pan", "pan")
	webtest.WantRedirect(t, b.Post(t, stepDetails, details("MEHTA")), stepAddresses)
	page := b.Post(t, stepAddresses, with(url.Values{}, "_rows", "add"))
	webtest.WantStatus(t, page, http.StatusOK)
	wantAddressInputs(t, page, line(1, "gstin"), line(1, "pincode"), line(1, "state_code"))
}

// wantCode checks the control named name checks itself at its CodeInput kind.
func wantCode(t *testing.T, r webtest.Response, name, kind string) {
	t.Helper()
	got, _ := r.Doc.Find(`input[name="` + name + `"]`).Attr("hx-post")
	if got != "/ui/validate/"+kind {
		t.Errorf("%s posts to %q; want the %s CodeInput", name, got, kind)
	}
}

func wantAddressInputs(t *testing.T, r webtest.Response, gstinName, pincodeName, stateName string) {
	t.Helper()
	wantCode(t, r, gstinName, "gstin")
	wantCode(t, r, pincodeName, "pincode")
	if r.Doc.Find(`select[name="`+stateName+`"]`).Length() != 1 {
		t.Errorf("%s is not a Select\nbody: %s", stateName, webtest.Squash(r.Body))
	}
	var found bool
	for _, c := range r.Choices(stateName) {
		found = found || c.Value == "27" && strings.Contains(c.Label, "Maharashtra")
	}
	if !found {
		t.Errorf("%s options lack Maharashtra (27): %+v", stateName, r.Choices(stateName))
	}
	webtest.WantText(t, r, "Line 1")
	webtest.WantText(t, r, "Line 2")
}

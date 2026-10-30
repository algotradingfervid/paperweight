package partyweb_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// No case ID: what the implementer added beyond the committed tests' contract.

// alertsHolding is the notices in #alerts holding text (CD-80: warnings show
// there, not in the main region).
func alertsHolding(r webtest.Response, text string) int {
	return strings.Count(webtest.Squash(r.Doc.Find("#alerts").Text()), text)
}

// The two PAN warnings are one on the next page, and the address's longer text
// stays; the name's warning is its own.
func TestScreens_PartyWizardWarningsInAlerts(t *testing.T) {
	const karnataka = "consider adding a Karnataka address to SHREE."
	shreeRow := row(blrRow, "gstin", gstinShree, "is_billing", "1", "is_shipping", "")
	w := newWorld(t)
	b := w.kavya()
	d := details("SHREE2")
	d.Set("name", "Shree Steel Traders")
	d.Set("pan", "AABCS1429B")
	toCheck(t, b, d, rows(shreeRow, row(shreeRow, "title", "Godown")), url.Values{})
	saved := b.Post(t, stepCheck, url.Values{})
	webtest.WantRedirect(t, saved, recordURL(w.get(t, "SHREE2").ID))
	page := b.Get(t, saved.Location)
	if n := alertsHolding(page, shreePANNote); n != 1 {
		t.Errorf("PAN warnings in #alerts = %d; want 1\n%s", n, page.Doc.Find("#alerts").Text())
	}
	if n := alertsHolding(page, karnataka); n != 1 {
		t.Errorf("the address's text in #alerts = %d; want 1", n)
	}
	if n := alertsHolding(page, "A party named Shree Steel Traders already exists (SHREE)."); n != 1 {
		t.Errorf("name warnings in #alerts = %d; want 1", n)
	}
}

// An address saved on its own page warns the same way.
func TestScreens_AddressWarningInAlerts(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	p := w.create(t, party.Input{Code: "V-NEW", Name: "New Vendor", IsVendor: true})
	form := addressForm(address.Address{Title: "Bengaluru", Line1: "4 Residency Road", City: "Bengaluru", StateCode: "29", Pincode: "560001", GSTIN: gstinShree, IsBilling: true})
	form.Del("version")
	saved := b.Post(t, newAddressURL(p.ID), form)
	webtest.WantRedirect(t, saved, recordURL(p.ID))
	if n := alertsHolding(b.Get(t, saved.Location), "consider adding a Karnataka address to SHREE."); n != 1 {
		t.Errorf("the address's warning in #alerts = %d; want 1", n)
	}
}

// Check and save reads as words: a tick is Yes and a state its name, on a row
// and on the party.
func TestScreens_PartyWizardCheckWords(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	toCheck(t, b, details("MEHTA"), rows(mumbaiRow), rows(sunilRow))
	check := b.Get(t, stepCheck)
	webtest.WantText(t, check, "VendorYes")
	webtest.WantText(t, check, "BillingYes")
	webtest.WantText(t, check, "Maharashtra (27)")
	webtest.WantText(t, check, "PrimaryYes")
}

// What counts as typed: spaces and the default don't, a tick does.
func TestScreens_PartyWizardBlankEdges(t *testing.T) {
	t.Run("spaces only", func(t *testing.T) {
		w := newWorld(t)
		b := w.kavya()
		webtest.WantRedirect(t, b.Post(t, stepDetails, details("MEHTA")), stepAddresses)
		spaces := row(blankAddress, "title", "   ", "city", " ", "country", " India ")
		webtest.WantRedirect(t, b.Post(t, stepAddresses, rows(spaces, mumbaiRow)), stepContacts)
		page := b.Get(t, stepAddresses)
		value(t, page, line(1, "line1"), "Plot 12, MIDC")
		noControl(t, page, line(2, "line1"))
	})

	t.Run("a tick alone keeps the row", func(t *testing.T) {
		w := newWorld(t)
		b := w.kavya()
		webtest.WantRedirect(t, b.Post(t, stepDetails, details("MEHTA")), stepAddresses)
		r := b.Post(t, stepAddresses, rows(mumbaiRow, row(blankAddress, "is_primary_billing", "1")))
		webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
		webtest.WantFieldErrorID(t, r, components.FieldID(line(2, "is_billing")), msgType)
	})

	// Add leaves a blank row in the session. A stepper link past Next still
	// reaches Check and save, which must not count or save it.
	t.Run("a blank row held by Add", func(t *testing.T) {
		w := newWorld(t)
		before := w.tally(t)
		b := w.kavya()
		toCheck(t, b, details("MEHTA"), rows(mumbaiRow), url.Values{})
		webtest.WantStatus(t, b.Post(t, stepAddresses, with(rows(blankAddress, mumbaiRow), "_rows", "add")), http.StatusOK)
		check := b.Get(t, stepCheck)
		webtest.WantText(t, check, "Row1")
		webtest.WantNoText(t, check, "Row2")
		webtest.WantRedirect(t, b.Post(t, stepCheck, url.Values{}), recordURL(w.get(t, "MEHTA").ID))
		w.wantTally(t, before.plus(tally{parties: 1, addresses: 1, audits: 2}))
	})

	t.Run("a blank row held by Add closes up on a refusal", func(t *testing.T) {
		w := newWorld(t)
		b := w.kavya()
		toCheck(t, b, details("MEHTA"), rows(mumbaiRow), url.Values{})
		webtest.WantStatus(t, b.Post(t, stepAddresses, with(rows(blankAddress, row(mumbaiRow, "city", "")), "_rows", "add")), http.StatusOK)
		r := b.Post(t, stepCheck, url.Values{})
		webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
		webtest.WantFieldErrorID(t, r, components.FieldID(line(1, "city")), msgCity)
		noControl(t, r, line(2, "line1"))
	})
}

// A new row starts from its defaults; Add keeps the rest as typed.
func TestScreens_PartyWizardAddUsesDefaults(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	webtest.WantRedirect(t, b.Post(t, stepDetails, details("MEHTA")), stepAddresses)
	r := b.Post(t, stepAddresses, with(url.Values{}, "_rows", "add"))
	value(t, r, line(1, "country"), "India")
}

// A value past the limit never reaches the session, on a row as on a field.
func TestScreens_PartyWizardRowValueTooLong(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	webtest.WantRedirect(t, b.Post(t, stepDetails, details("MEHTA")), stepAddresses)
	long := strings.Repeat("x", 1001)
	r := b.Post(t, stepAddresses, rows(row(mumbaiRow, "title", long)))
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	webtest.WantFieldErrorID(t, r, components.FieldID(line(1, "title")), "Title can be at most 1,000 characters.")
	blankRow(t, b.Get(t, stepAddresses), line(1, "title"))
}

// The summary reads in row order though billing rows save first.
func TestScreens_PartyWizardErrorsInRowOrder(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	toCheck(t, b, details("MEHTA"), rows(row(blrRow, "city", ""), row(mumbaiRow, "line1", "")), url.Values{})
	r := b.Post(t, stepCheck, url.Values{})
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	text := r.Text()
	if city, line1 := strings.Index(text, msgCity), strings.Index(text, msgLine1); city < 0 || line1 < city {
		t.Errorf("summary order: city at %d, line 1 at %d; want row 1's first", city, line1)
	}
}

// A role no form asks for is a bad URL.
func TestScreens_PartyPickerBadRole(t *testing.T) {
	w := newWorld(t)
	webtest.WantStatus(t, w.h.GetHTMX(t, w.h.Env.Kavya, "/pick/Party?role=supplier"), http.StatusBadRequest)
}

// The contact form's spare phone is ticked Mobile, and a spare row left blank
// isn't saved or refused.
func TestScreens_ContactFormSpareRows(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	id := w.get(t, "SHREE").ID
	page := b.Get(t, newContactURL(id))
	if !checked(page, "phones[1].mobile") {
		t.Errorf("the spare phone isn't ticked Mobile")
	}
	webtest.WantRedirect(t, b.Post(t, newContactURL(id), formValuesWith(t, page, newContactURL(id), "first_name", "Sunil")), recordURL(id))
	cs := w.partyContacts(t, id)
	if len(cs) != 1 || len(cs[0].Emails) != 0 || len(cs[0].Phones) != 0 {
		t.Errorf("contacts = %+v", cs)
	}

	// A refused form offers one spare row, however many blank ones it posted.
	r := b.Post(t, newContactURL(id), url.Values{"emails[1].email": {""}, "emails[2].email": {""}, "phones[7].number": {""}})
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	noControl(t, r, "emails[2].email")
	noControl(t, r, "phones[2].number")
	if !checked(r, "phones[1].mobile") {
		t.Errorf("the spare phone isn't ticked Mobile after a refusal")
	}
}

func formValuesWith(t *testing.T, r webtest.Response, action string, kv ...string) url.Values {
	t.Helper()
	return with(formValues(t, r, action), kv...)
}

// Delete from a row goes through its confirm step, which needs delete, not
// write.
func TestScreens_RowDeleteConfirm(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	shree := w.get(t, "SHREE")
	thane := w.address(t, w.m.Addresses["Thane"])
	sunil := w.addContact(t, shree.ID, "Sunil", "Mehta", false)
	page := b.Get(t, recordURL(shree.ID))
	for _, target := range []string{addressURL(shree.ID, thane.ID), contactURL(shree.ID, sunil)} {
		if _, found := page.RowLink(target + "?delete"); !found {
			t.Errorf("no Delete on %s", target)
		}
		confirm := b.Get(t, target+"?delete")
		webtest.WantStatus(t, confirm, http.StatusOK)
		webtest.WantText(t, confirm, "This can't be undone.")
		if _, ok := confirm.FormValue(target+"/delete", "version"); !ok {
			t.Errorf("the confirm step at %s posts no version", target)
		}
	}
	r := w.h.Browser(w.h.Env.Priya).Get(t, addressURL(shree.ID, thane.ID)+"?delete")
	webtest.WantStatus(t, r, http.StatusForbidden)
	webtest.WantText(t, r, "You don't have permission to delete Address.")

	// Delete alone, without write, still reaches the step.
	ctx := context.Background()
	a := w.h.Env.Kit.Auth.(*auth.Auth)
	for _, role := range []string{"Masters Manager", "Employee"} {
		if err := a.SetRights(ctx, w.h.Env.Admin, role, address.DocType, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightDelete: auth.ScopeAll}); err != nil {
			t.Fatal(err)
		}
	}
	webtest.WantStatus(t, w.kavya().Get(t, addressURL(shree.ID, thane.ID)+"?delete"), http.StatusOK)
}

// A row offers set-primary only where it would do something, and never on a
// disabled row.
func TestScreens_RowPrimaryActions(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	shree := w.get(t, "SHREE")
	id := shree.ID
	sunil := w.addContact(t, id, "Sunil", "Mehta", true)
	kiran := w.addContact(t, id, "Kiran", "Joshi", false)
	offers := func(action string) bool {
		_, found := b.Get(t, recordURL(id)).FormValue(action, "version")
		return found
	}
	if offers(addressURL(id, w.m.Addresses["Mumbai"]) + "/set-primary") {
		t.Errorf("the primary billing address offers set-primary")
	}
	if !offers(addressURL(id, w.m.Addresses["Delhi"]) + "/set-primary") {
		t.Errorf("Delhi offers no set-primary")
	}
	if offers(contactURL(id, sunil) + "/set-primary") {
		t.Errorf("the primary contact offers set-primary")
	}
	if !offers(contactURL(id, kiran) + "/set-primary") {
		t.Errorf("Kiran offers no set-primary")
	}
	w.disableContact(t, kiran)
	if offers(contactURL(id, kiran) + "/set-primary") {
		t.Errorf("a disabled contact offers set-primary")
	}
}

// A party with no rows says so.
func TestScreens_PartyRecordEmptyLists(t *testing.T) {
	w := newWorld(t)
	p := w.create(t, party.Input{Code: "V-NEW", Name: "New Vendor", IsVendor: true})
	page := w.kavya().Get(t, recordURL(p.ID))
	webtest.WantText(t, page, "No addresses yet.")
	webtest.WantText(t, page, "No contacts yet.")
}

// More rows than one page of the list still all show.
func TestScreens_PartyRecordShowsEveryRow(t *testing.T) {
	w := newWorld(t)
	shree := w.get(t, "SHREE")
	w.exec(t, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 501)
		INSERT INTO contact (party_id, name, first_name, created_at, updated_at) SELECT ?, 'Bulk' || i, 'Bulk' || i, '2026-01-01', '2026-01-01' FROM n`, shree.ID)
	page := w.kavya().Get(t, recordURL(shree.ID))
	webtest.WantStatus(t, page, http.StatusOK)
	if n := strings.Count(page.Text(), "NameBulk"); n != 501 {
		t.Errorf("contacts shown = %d; want 501", n)
	}
}

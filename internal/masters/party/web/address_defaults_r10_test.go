package partyweb_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/party"
)

// bareParty is a party with no address, which an unregistered one may be.
func (w *world) bareParty(t *testing.T) int64 {
	t.Helper()
	saved, err := w.parties.Create(context.Background(), w.h.Env.Kavya, party.Input{Code: "MEHTA", Name: "Mehta Fabrication", IsVendor: true, GSTCategory: gstin.Unregistered})
	if err != nil {
		t.Fatal(err)
	}
	return saved.ID
}

func switchOn(r webtest.Response, name string) bool {
	_, ok := r.Doc.Find(`input[type="checkbox"][name="` + name + `"]`).Attr("checked")
	return ok
}

// UI v2 R10 (shell M14): the wizard's first address opens as billing and primary
// billing, a later one doesn't, and each switch says what it does.
func TestAddressDefaults_Wizard(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	webtest.WantRedirect(t, b.Post(t, stepDetails, details("MEHTA")), stepAddresses)
	first := b.Post(t, stepAddresses, with(url.Values{}, "_rows", "add"))
	webtest.WantStatus(t, first, http.StatusOK)
	for name, want := range map[string]bool{"is_billing": true, "is_primary_billing": true, "is_shipping": false, "is_primary_shipping": false} {
		if got := switchOn(first, line(1, name)); got != want {
			t.Errorf("first address %s = %v; want %v", name, got, want)
		}
	}
	webtest.WantText(t, first, "Orders use this billing address unless you pick another.")
	webtest.WantText(t, first, "Orders use this delivery address unless you pick another.")

	second := b.Post(t, stepAddresses, with(rows(mumbaiRow), "_rows", "add"))
	webtest.WantStatus(t, second, http.StatusOK)
	if switchOn(second, line(2, "is_billing")) || switchOn(second, line(2, "is_primary_billing")) {
		t.Error("second address opens as billing")
	}
}

// A first address left at its defaults holds nothing typed, so Continue drops
// it rather than refusing it; the party saves with no address, as before.
func TestAddressDefaults_WizardBlankRowStaysBlank(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	webtest.WantRedirect(t, b.Post(t, stepDetails, details("MEHTA")), stepAddresses)
	blank := url.Values{line(1, "country"): {"India"}, line(1, "is_billing"): {"1"}, line(1, "is_primary_billing"): {"1"}}
	webtest.WantRedirect(t, b.Post(t, stepAddresses, blank), stepContacts)
}

// A party's first address page opens with the same defaults, on the party's
// own page too; with an enabled address stored it doesn't, and with only a
// disabled one it does again (a disabled address bills nothing).
func TestAddressDefaults_RecordPage(t *testing.T) {
	w := newWorld(t)
	id := w.bareParty(t)
	path := recordURL(id) + "/addresses" + "/new"
	r := w.h.Get(t, w.h.Env.Kavya, path)
	webtest.WantStatus(t, r, http.StatusOK)
	if !switchOn(r, "is_billing") || !switchOn(r, "is_primary_billing") {
		t.Errorf("first address page isn't billing and primary\nbody: %s", webtest.Squash(r.Body))
	}
	if switchOn(r, "is_shipping") || switchOn(r, "is_primary_shipping") {
		t.Error("first address page is shipping")
	}
	if v, _ := r.Value("country"); v != "India" {
		t.Errorf("country = %q", v)
	}
	saved := w.h.Post(t, w.h.Env.Kavya, path, url.Values{
		"line1": {"Plot 12, MIDC"}, "city": {"Mumbai"}, "state_code": {"27"}, "country": {"India"}, "is_billing": {"1"}, "is_primary_billing": {"1"},
	})
	if saved.Status != http.StatusSeeOther {
		t.Fatalf("save = %d\nbody: %s", saved.Status, webtest.Squash(saved.Body))
	}
	rows := w.partyAddresses(t, id)
	if len(rows) != 1 || !rows[0].PrimaryBilling {
		t.Fatalf("addresses = %+v; want one primary billing", rows)
	}
	if next := w.h.Get(t, w.h.Env.Kavya, path); switchOn(next, "is_billing") || switchOn(next, "is_primary_billing") {
		t.Error("second address page opens as billing, with an enabled address stored")
	}
	if err := w.addresses.SetDisabled(context.Background(), w.h.Env.Kavya, rows[0].ID, rows[0].Version, true); err != nil {
		t.Fatal(err)
	}
	if next := w.h.Get(t, w.h.Env.Kavya, path); !switchOn(next, "is_billing") || !switchOn(next, "is_primary_billing") {
		t.Error("with only a disabled address stored, the page doesn't open as the first address")
	}
}

// The address and contact forms are section cards.
func TestPartyRows_Cards(t *testing.T) {
	w := newWorld(t)
	id := w.bareParty(t)
	for path, want := range map[string]int{recordURL(id) + "/addresses/new": 3, recordURL(id) + "/contacts/new": 2} {
		r := w.h.Get(t, w.h.Env.Kavya, path)
		webtest.WantStatus(t, r, http.StatusOK)
		if n := r.Doc.Find("form section.card .card-desc").Length(); n != want {
			t.Errorf("%s: %d cards, want %d", path, n, want)
		}
		if n := r.Doc.Find("form input:not([type=hidden])").Not("section.card input").Length(); n != 0 {
			t.Errorf("%s: %d inputs outside the cards", path, n)
		}
	}
}

// UI v2 R10 (M14 ruling): an address or contact row the user never filled in
// is left out on Continue, and the next page says so; a partly filled one is
// kept and validates as before.
func TestPartyWizard_LeftOutEmptyRows(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	webtest.WantRedirect(t, b.Post(t, stepDetails, details("MEHTA")), stepAddresses)
	blank := url.Values{line(1, "country"): {"India"}, line(1, "is_billing"): {"1"}, line(1, "is_primary_billing"): {"1"}}
	webtest.WantRedirect(t, b.Post(t, stepAddresses, blank), stepContacts)
	webtest.WantText(t, b.Get(t, stepContacts), "Left out the empty address.")
	webtest.WantRedirect(t, b.Post(t, stepContacts, url.Values{line(1, "first_name"): {""}}), stepCheck)
	check := b.Get(t, stepCheck)
	webtest.WantText(t, check, "Left out the empty contact.")
	webtest.WantNoText(t, check, "Left out the empty address.")

	partly := url.Values{line(1, "country"): {"India"}, line(1, "city"): {"Mumbai"}, line(1, "is_billing"): {"1"}, line(1, "is_primary_billing"): {"1"}}
	r := w.kavya()
	webtest.WantRedirect(t, r.Post(t, stepDetails, details("MEHTA")), stepAddresses)
	stay := r.Post(t, stepAddresses, partly)
	webtest.WantStatus(t, stay, http.StatusUnprocessableEntity)
	webtest.WantNoText(t, stay, "Left out")
	if v, _ := stay.Value(line(1, "city")); v != "Mumbai" {
		t.Errorf("the partly filled row's city = %q; want it kept", v)
	}
}

// Check's Addresses card with addresses shows them, never Not set; with no
// contact the Contacts card says Not set.
func TestPartyWizard_CheckAddressesNotSet(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	toCheck(t, b, details("MEHTA"), rows(mumbaiRow), url.Values{})
	card := func(r webtest.Response, title string) string {
		var out string
		r.Doc.Find("section.card").Each(func(_ int, c *goquery.Selection) {
			if strings.Contains(c.Find(".card-title").Text(), title) {
				out = webtest.Squash(c.Text())
			}
		})
		return out
	}
	check := b.Get(t, stepCheck)
	if got := card(check, "Addresses"); strings.Contains(got, "Not set") || !strings.Contains(got, "Mumbai") {
		t.Errorf("Addresses card = %q", got)
	}
	if got := card(check, "Contacts"); !strings.Contains(got, "Not set") {
		t.Errorf("Contacts card = %q; want Not set", got)
	}
}

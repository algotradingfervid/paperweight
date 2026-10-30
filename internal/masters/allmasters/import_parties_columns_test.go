package allmasters_test

import (
	"context"
	"errors"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/csvio"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/party"
)

// partiesCoromPAN is partiesCoromGSTIN's PAN.
const partiesCoromPAN = "AAACT2727Q"

// Each cell comes back on the record it was imported to, and a role left blank
// or No stays off.
func TestImport_PartyColumnsStored(t *testing.T) {
	w := partiesSetup(t)
	partiesWantImported(t, w.importFile(t, party.DocType, partiesCSV(partiesPartyHeader,
		"SHREE,Shree Steel Traders,Yes,No,"+partiesShreePAN+",,,No",
		"COROM,Coromandel Cements,No,Yes,"+partiesCoromPAN+",,,No")), 2)

	cases := []struct {
		code, name, pan string
		vendor, cust    bool
	}{
		{"SHREE", "Shree Steel Traders", partiesShreePAN, true, false},
		{"COROM", "Coromandel Cements", partiesCoromPAN, false, true},
	}
	for _, c := range cases {
		p := w.partyByCode(t, c.code)
		if p.Name != c.name {
			t.Errorf("%s: name = %q, want %q", c.code, p.Name, c.name)
		}
		if p.IsVendor != c.vendor || p.IsCustomer != c.cust {
			t.Errorf("%s: vendor = %v, customer = %v; want %v, %v", c.code, p.IsVendor, p.IsCustomer, c.vendor, c.cust)
		}
		if p.PAN != c.pan {
			t.Errorf("%s: PAN = %q, want %q", c.code, p.PAN, c.pan)
		}
	}
}

// The foreign row is what Country is proved on: a blank country is India.
func TestImport_AddressColumnsStored(t *testing.T) {
	w := partiesSetup(t)
	local := w.createVendor(t, "LOCAL")
	foreign := w.createParty(t, party.Input{Code: "FOREIGN", Name: "Foreign Vendor", IsVendor: true, GSTCategory: gstin.Overseas})
	partiesWantImported(t, w.importFile(t, address.DocType, partiesCSV(partiesAddressHeader,
		"LOCAL,Wagle Godown,12 Steel Market,Masjid Bunder,Thane,27,India,400601,,No,Yes,No",
		"LOCAL,Head Office,7 Anna Salai,Second Floor,Chennai,33,India,600002,,Yes,No,No",
		"FOREIGN,Raffles Office,1 Raffles Place,Tower Two,Singapore,,Singapore,048616,,Yes,No,No")), 3)

	cases := []struct {
		partyID int64
		want    []address.Address
	}{
		{local, []address.Address{
			{
				PartyID: local, Title: "Wagle Godown", Line1: "12 Steel Market", Line2: "Masjid Bunder", City: "Thane",
				StateCode: "27", Country: "India", Pincode: "400601", IsShipping: true, PrimaryShipping: true, Version: 1,
			},
			{
				PartyID: local, Title: "Head Office", Line1: "7 Anna Salai", Line2: "Second Floor", City: "Chennai",
				StateCode: "33", Country: "India", Pincode: "600002", IsBilling: true, PrimaryBilling: true, Version: 1,
			},
		}},
		{foreign, []address.Address{
			{
				PartyID: foreign, Title: "Raffles Office", Line1: "1 Raffles Place", Line2: "Tower Two", City: "Singapore",
				Country: "Singapore", Pincode: "048616", IsBilling: true, PrimaryBilling: true, Version: 1,
			},
		}},
	}
	for _, c := range cases {
		stored := w.addresses(t, c.partyID)
		if len(stored) != len(c.want) {
			t.Fatalf("party %d: %d addresses stored, want %d", c.partyID, len(stored), len(c.want))
		}
		for i, got := range stored {
			got.ID = 0 // AUTOINCREMENT, not a cell
			if got != c.want[i] {
				t.Errorf("party %d, address %d:\n got %+v\nwant %+v", c.partyID, i, got, c.want[i])
			}
		}
	}
}

func TestImport_ContactColumnsStored(t *testing.T) {
	w := partiesSetup(t)
	w.createVendor(t, "SHREE")
	partiesWantImported(t, w.importFile(t, address.ContactDocType, partiesCSV(partiesContactHeader,
		"SHREE,Sunil,Kumar,Mehta,,,No")), 1)

	c := w.contact(t, "Sunil")
	if c.FirstName != "Sunil" || c.MiddleName != "Kumar" || c.LastName != "Mehta" {
		t.Errorf("name = %q %q %q, want Sunil Kumar Mehta", c.FirstName, c.MiddleName, c.LastName)
	}
}

// Plan Task 20 Step 3: an error that isn't a validation error aborts the import
// instead of becoming a row error. Purchase User may create and import but not
// write, so only the Disabled cell's SetDisabledTx is refused, after the row's
// CreateTx has run. On the real auth service, as a non-admin.
func TestImport_WriteDeniedAborts(t *testing.T) {
	w := partiesSetup(t)
	ctx := context.Background()
	if err := w.env.Kit.Auth.(*auth.Auth).SetRights(ctx, w.env.Admin, "Purchase User", party.DocType, map[auth.Right]auth.Scope{
		auth.RightRead:   auth.ScopeAll,
		auth.RightCreate: auth.ScopeAll,
		auth.RightImport: auth.ScopeAll,
	}); err != nil {
		t.Fatal(err)
	}
	dt := registered(t, party.DocType)
	auditBefore := partiesAuditCount(t, w.env)
	file := partiesCSV(partiesPartyHeader, "V-OFF,Vendor Off,Yes,No,,,,Yes")

	runs := []struct {
		name string
		run  func(context.Context, auth.Principal, *doctype.DocType, io.Reader) (csvio.Report, error)
	}{
		{"Preview", w.im.Preview},
		{"Import", w.im.Import},
	}
	for _, r := range runs {
		rep, err := r.run(ctx, w.env.Priya, dt, strings.NewReader(file))
		var denied *auth.DeniedError
		if !errors.As(err, &denied) {
			t.Fatalf("%s: err = %v, want a *auth.DeniedError", r.name, err)
		}
		if denied.Right != auth.RightWrite {
			t.Errorf("%s: denied right = %q, want %q", r.name, denied.Right, auth.RightWrite)
		}
		if !reflect.DeepEqual(rep, csvio.Report{}) {
			t.Errorf("%s: report = %+v, want empty", r.name, rep)
		}
		if n := partiesCount(t, w.env, "party"); n != 0 {
			t.Errorf("%s stored %d parties, want 0", r.name, n)
		}
		if n := partiesAuditCount(t, w.env); n != auditBefore {
			t.Errorf("%s left %d audit entries, want %d", r.name, n, auditBefore)
		}
	}

	rep, err := w.im.Preview(ctx, w.env.Priya, dt, strings.NewReader(partiesCSV(partiesPartyHeader, "V-ON,Vendor On,Yes,No,,,,No")))
	if err != nil {
		t.Fatalf("Preview with Disabled No: %v", err)
	}
	partiesWantErrors(t, rep)
	partiesWantNotImported(t, rep, 1, 1)
}

// A GSTIN fills in a blank state, so an unknown state must not reach the master
// as blank: the GSTIN's state would silently replace it.
func TestImport_UnknownStateWithGSTIN(t *testing.T) {
	w := partiesSetup(t)
	id := w.createVendor(t, "LOCAL")
	auditBefore := partiesAuditCount(t, w.env)

	rep := w.importFile(t, address.DocType, partiesCSV(partiesAddressHeader,
		partiesAddressRow("LOCAL", "Mumbai", "Atlantis", "400001", partiesShreeGSTIN, "Yes", "No", "No")))

	const want = "Row 2: State is required for an Indian address."
	if !slices.ContainsFunc(rep.Errors, func(e csvio.RowError) bool { return e.String() == want }) {
		t.Errorf("row errors = %q, want them to include %q", rep.Errors, want)
	}
	partiesWantNotImported(t, rep, 1, 0)
	if n := partiesCount(t, w.env, "address"); n != 0 {
		t.Errorf("%d addresses stored, want 0", n)
	}
	if n := partiesAuditCount(t, w.env); n != auditBefore {
		t.Errorf("audit entries %d, want %d", n, auditBefore)
	}
	if p := getParty(t, w.env, w.svc, id); p.PAN != "" || p.GSTCategory != gstin.Unregistered {
		t.Errorf("party changed: PAN %q, category %q", p.PAN, p.GSTCategory)
	}
}

func TestImport_OnePrimaryEachType(t *testing.T) {
	w := partiesSetup(t)
	id := w.createVendor(t, "LOCAL")
	partiesWantImported(t, w.importFile(t, address.DocType, partiesCSV(partiesAddressHeader,
		partiesAddressRow("LOCAL", "Mumbai", "27", "400001", "", "Yes", "Yes", "No"),
		partiesAddressRow("LOCAL", "Pune", "27", "411001", "", "Yes", "Yes", "No"),
		partiesAddressRow("LOCAL", "Thane", "27", "400601", "", "Yes", "Yes", "No"))), 3)

	stored := w.addresses(t, id)
	if len(stored) != 3 {
		t.Fatalf("%d addresses stored, want 3", len(stored))
	}
	if stored[0].City != "Mumbai" {
		t.Fatalf("first address = %s, want Mumbai", stored[0].City)
	}
	for i, a := range stored {
		first := i == 0
		if a.PrimaryBilling != first || a.PrimaryShipping != first {
			t.Errorf("%s: primary billing = %v, shipping = %v; want %v for both", a.City, a.PrimaryBilling, a.PrimaryShipping, first)
		}
	}
}

package address_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/mstfixture"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Literal values from 06-masters.md. docNewGSTIN is also Thane's, which the
// same party may share (MST-044).
const (
	docNotVendorMsg = "Only Customer Ltd is not marked as a vendor. Tick Vendor on the party first." // MST-042
	docNewGSTIN     = "27AABCS1429B2ZT"                                                              // MST-052
)

const docQty types.Qty = 1000 // 1.000

var docDate = types.Date{Year: 2026, Month: time.October, Day: 24}

// docOrder is a PO header for vendor at branch PUNE on project PRJ-METRO, the
// one project Priya's restriction allows.
func docOrder(m msttest.Masters, vendorID, addressID int64) mstfixture.Header {
	return mstfixture.Header{
		VendorID: vendorID, AddressID: addressID, BranchID: m.Branches["PUNE"], ProjectID: m.Projects["PRJ-METRO"], Date: docDate,
	}
}

func docOrderCount(t *testing.T, d *db.DB) int {
	t.Helper()
	ctx := context.Background()
	var n int
	if err := d.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, "SELECT count(*) FROM mst_order").Scan(&n)
	}); err != nil {
		t.Fatalf("count orders: %v", err)
	}
	return n
}

func TestParty_VendorPickerFilter(t *testing.T) { // MST-042
	env := msttest.New(t)
	ctx := context.Background()
	m := msttest.SeedMasters(t, env)
	ps := party.New(env.Kit)

	only, err := ps.Create(ctx, env.Kavya, party.Input{Code: "C-ONLY", Name: "Only Customer Ltd", IsCustomer: true})
	if err != nil {
		t.Fatalf("create C-ONLY: %v", err)
	}
	mix, err := ps.Create(ctx, env.Kavya, party.Input{Code: "B-MIX", Name: "Mixed Party Ltd", IsVendor: true, IsCustomer: true})
	if err != nil {
		t.Fatalf("create B-MIX: %v", err)
	}

	choices, err := ps.Search(ctx, env.Priya, party.Vendor, "", false)
	if err != nil {
		t.Fatalf("Search(Vendor): %v", err)
	}
	codes := make([]string, len(choices))
	for i, c := range choices {
		codes[i] = c.Code
	}
	for _, want := range []string{"SHREE", "B-MIX"} {
		if !slices.Contains(codes, want) {
			t.Errorf("vendor picker = %v; want %s in it", codes, want)
		}
	}
	if slices.Contains(codes, "C-ONLY") {
		t.Errorf("vendor picker = %v; want C-ONLY left out", codes)
	}

	_, err = mstfixture.Save(ctx, env, env.Priya, 0, docOrder(m, only.ID, 0), nil)
	var he doctype.HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("save with C-ONLY = %v (%T); want field errors", err, err)
	}
	if fes := he.FieldErrors(); len(fes) != 1 || fes[0].Message != docNotVendorMsg {
		t.Errorf("save with C-ONLY: field errors = %+v; want only %q", fes, docNotVendorMsg)
	}
	if n := docOrderCount(t, env.DB); n != 0 {
		t.Errorf("after refusal: %d orders; want 0", n)
	}

	for code, id := range map[string]int64{"SHREE": m.Parties["SHREE"], "B-MIX": mix.ID} {
		if _, err := mstfixture.Save(ctx, env, env.Priya, 0, docOrder(m, id, 0), nil); err != nil {
			t.Errorf("save with %s: %v", code, err)
		}
	}
}

func TestParty_GSTINChangeKeepsSnapshots(t *testing.T) { // MST-052
	env := msttest.New(t)
	ctx := context.Background()
	m := msttest.SeedMasters(t, env)
	s := address.New(env.Kit)
	mumbai := m.Addresses["Mumbai"]
	h := docOrder(m, m.Parties["SHREE"], mumbai)

	lines := []mstfixture.Line{{ItemID: m.Items["MS-ANGLE-50"], UoMID: m.UoMs["KGS"], Qty: docQty, Rate: 62_5000}}

	saved, err := mstfixture.Save(ctx, env, env.Priya, 0, h, lines)
	if err != nil {
		t.Fatalf("save order: %v", err)
	}
	if got, want := mstfixture.Approve(t, env, saved.ID), "PO/26-27/00001"; got != want {
		t.Fatalf("approved number = %q; want %q", got, want)
	}

	a := addrGet(t, env.DB, s, mumbai)
	if _, err := s.Update(ctx, env.Kavya, mumbai, a.Version, address.Input{
		PartyID: a.PartyID, Title: a.Title, Line1: a.Line1, City: a.City, StateCode: a.StateCode, Country: a.Country,
		Pincode: a.Pincode, GSTIN: docNewGSTIN, IsBilling: a.IsBilling, IsShipping: a.IsShipping,
		PrimaryBilling: a.PrimaryBilling, PrimaryShipping: a.PrimaryShipping,
	}); err != nil {
		t.Fatalf("change Mumbai GSTIN: %v", err)
	}

	if got := mstfixture.Snapshot(t, env, saved.ID).VendorGSTIN; got != addrMumbaiGSTIN {
		t.Errorf("approved order GSTIN = %q; want %q", got, addrMumbaiGSTIN)
	}
	next, err := mstfixture.Save(ctx, env, env.Priya, 0, h, lines)
	if err != nil {
		t.Fatalf("save new order: %v", err)
	}
	if got := mstfixture.Snapshot(t, env, next.ID).VendorGSTIN; got != docNewGSTIN {
		t.Errorf("new order GSTIN = %q; want %q", got, docNewGSTIN)
	}

	var entries []audit.Entry
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		entries, err = audit.ForDoc(ctx, tx, address.DocType, mumbai)
		return err
	}); err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no audit entries for the Mumbai address")
	}
	last := entries[len(entries)-1]
	want := audit.Change{Field: "gstin", Before: addrMumbaiGSTIN, After: docNewGSTIN, Op: "set"}
	if last.Action != "save" || last.By != env.Kavya.ID || len(last.Changes) != 1 || last.Changes[0] != want {
		t.Errorf("last audit entry = %+v; want save by Kavya with only %+v", last, want)
	}
}

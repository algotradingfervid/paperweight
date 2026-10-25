package address_test

import (
	"context"
	"testing"

	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/party"
)

// Messages copied verbatim from 06-masters.md MST-044, MST-047 and MST-048
// (06 wins over the plan's SHREE-OLD wording for MST-044).
const (
	addrOverseasMsg = "Addresses outside India must have GST category Overseas."
	addrDupMsg      = "GSTIN 27AABCS1429B1ZU is already used by party SHREE (disabled). Enable and use that party instead."
	addrOtherPANMsg = "GSTIN 27AAGCM1234F1ZS belongs to PAN AAGCM1234F, but this party's PAN is AABCS1429B. Use a separate party for a different business."
	// CD-167: a party update whose PAN differs from its address GSTIN's PAN
	// gets MST-048's message; the GSTIN-derived PAN wins (MST-040).
	addrPostedPANMsg = "GSTIN 27AABCS1429B1ZU belongs to PAN AABCS1429B, but this party's PAN is AAGCM1234F. Use a separate party for a different business."

	addrOtherGSTIN = "27AAGCM1234F1ZS"
	addrOtherPAN   = "AAGCM1234F"
	addrCoromGSTIN = "33AAACT2727Q1Z3"
	addrCoromPAN   = "AAACT2727Q"
)

// MST-047 has no party DB-free rule (the category lives on the party), so it
// runs through the service with 06's literal values.
func TestParty_OverseasCountry(t *testing.T) { // MST-047
	env := msttest.New(t)
	s := address.New(env.Kit)

	reg := addrParty(t, env, "SG-STEEL", "Singapore Steel Pte Ltd", "", gstin.Regular)
	_, err := s.Create(context.Background(), env.Kavya, address.Input{
		PartyID: reg, Title: "Singapore", Line1: "1 Raffles Place", City: "Singapore",
		Country: "Singapore", Pincode: "118524", IsBilling: true,
	})
	addrWantHas(t, err, addrOverseasMsg)
	if as := addrForParty(t, env.DB, s, reg); len(as) != 0 {
		t.Errorf("after refusal: ForParty = %+v; want none", as)
	}

	// An Indian address on an Overseas party is allowed (high-seas sales).
	ovs := addrParty(t, env, "HIGHSEAS", "High Seas Traders", "", gstin.Overseas)
	in := addrMumbai(ovs)
	in.GSTIN = ""
	if _, err := s.Create(context.Background(), env.Kavya, in); err != nil {
		t.Errorf("Create(India address, party Overseas) = %v; want nil", err)
	}
}

func TestParty_DuplicateGSTINRefused(t *testing.T) { // MST-044
	env := msttest.New(t)
	ctx := context.Background()
	s := address.New(env.Kit)
	ps := party.New(env.Kit)
	shree := addrShree(t, env)

	// Two addresses of the same party may share a GSTIN.
	addrCreate(t, env, s, addrMumbai(shree))
	ship := addrMumbai(shree)
	ship.Title, ship.IsBilling, ship.IsShipping = "Mumbai godown", false, true
	if _, err := s.Create(ctx, env.Kavya, ship); err != nil {
		t.Fatalf("Create(second SHREE address with %s) = %v; want nil", addrMumbaiGSTIN, err)
	}

	if err := ps.SetDisabled(ctx, env.Kavya, shree, addrPartyGet(t, env.DB, ps, shree).Version, true); err != nil {
		t.Fatalf("disable SHREE: %v", err)
	}

	newID := addrParty(t, env, "SHREE-NEW", "Shree Steel New", "", gstin.Regular)
	_, err := s.Create(ctx, env.Kavya, addrMumbai(newID))
	addrWantHas(t, err, addrDupMsg)
	if as := addrForParty(t, env.DB, s, newID); len(as) != 0 {
		t.Errorf("after refusal: ForParty(SHREE-NEW) = %+v; want none", as)
	}
	if p := addrPartyGet(t, env.DB, ps, newID); p.PAN != "" {
		t.Errorf("after refusal: SHREE-NEW PAN = %q; want blank (nothing back-filled)", p.PAN)
	}
}

func TestParty_GSTINsSharePAN(t *testing.T) { // MST-048
	env := msttest.New(t)
	s := address.New(env.Kit)
	ps := party.New(env.Kit)
	shree := addrShree(t, env)

	in := addrMumbai(shree)
	in.GSTIN = addrOtherGSTIN
	_, err := s.Create(context.Background(), env.Kavya, in)
	addrWantHas(t, err, addrOtherPANMsg)
	if as := addrForParty(t, env.DB, s, shree); len(as) != 0 {
		t.Errorf("after refusal: ForParty(SHREE) = %+v; want none", as)
	}
	if p := addrPartyGet(t, env.DB, ps, shree); p.PAN != addrShreePAN {
		t.Errorf("after refusal: SHREE PAN = %q; want %q", p.PAN, addrShreePAN)
	}
}

func TestParty_GSTINBackfillsParty(t *testing.T) { // MST-049
	env := msttest.New(t)
	s := address.New(env.Kit)
	ps := party.New(env.Kit)
	corom := addrParty(t, env, "COROM", "Coromandel Cables", "", gstin.Unregistered)

	addrCreate(t, env, s, address.Input{
		PartyID: corom, Title: "Chennai", Line1: "22 Guindy Industrial Estate", City: "Chennai",
		StateCode: "33", Country: "India", Pincode: "600001", GSTIN: addrCoromGSTIN, IsBilling: true,
	})
	p := addrPartyGet(t, env.DB, ps, corom)
	if p.GSTCategory != gstin.Regular || p.PAN != addrCoromPAN {
		t.Errorf("COROM after billing %s: {GSTCategory %q, PAN %q}; want {%q, %q}",
			addrCoromGSTIN, p.GSTCategory, p.PAN, gstin.Regular, addrCoromPAN)
	}
}

// TestParty_PANMatchesAddressGSTIN pins CD-167: a party update posting a PAN
// that differs from its addresses' GSTIN PAN is refused (MST-040: the
// GSTIN-derived PAN wins) with MST-048's message.
func TestParty_PANMatchesAddressGSTIN(t *testing.T) {
	env := msttest.New(t)
	ctx := context.Background()
	s := address.New(env.Kit)
	ps := party.New(env.Kit)
	shree := addrShree(t, env)
	addrCreate(t, env, s, addrMumbai(shree))

	in := party.Input{Code: addrShreeCode, Name: addrShreeName, IsVendor: true, PAN: addrOtherPAN, GSTCategory: gstin.Regular}
	_, err := ps.Update(ctx, env.Kavya, shree, addrPartyGet(t, env.DB, ps, shree).Version, in)
	addrWantHas(t, err, addrPostedPANMsg)
	if p := addrPartyGet(t, env.DB, ps, shree); p.PAN != addrShreePAN {
		t.Errorf("after refusal: SHREE PAN = %q; want %q", p.PAN, addrShreePAN)
	}

	// The same PAN in another case saves.
	in.PAN = "aabcs1429b"
	if _, err := ps.Update(ctx, env.Kavya, shree, addrPartyGet(t, env.DB, ps, shree).Version, in); err != nil {
		t.Errorf("Update(SHREE, PAN aabcs1429b) = %v; want nil", err)
	}

	// A party without addresses may change its PAN.
	corom := addrParty(t, env, "COROM", "Coromandel Cables", addrCoromPAN, gstin.Unregistered)
	cin := party.Input{Code: "COROM", Name: "Coromandel Cables", IsVendor: true, PAN: addrOtherPAN, GSTCategory: gstin.Unregistered}
	if _, err := ps.Update(ctx, env.Kavya, corom, addrPartyGet(t, env.DB, ps, corom).Version, cin); err != nil {
		t.Errorf("Update(COROM without addresses, new PAN) = %v; want nil", err)
	}
	if p := addrPartyGet(t, env.DB, ps, corom); p.PAN != addrOtherPAN {
		t.Errorf("COROM PAN = %q; want %q", p.PAN, addrOtherPAN)
	}
}

// TestParty_BlankCategoryGuessedFromPrimaryBilling pins M5 / MST-035: a party
// saved with a blank category takes the category guessed from its primary
// billing address's GSTIN (a blank category is not "keep the old one").
// A party without addresses keeps its category (TestParty_BlankCategoryKeptOnUpdate).
func TestParty_BlankCategoryGuessedFromPrimaryBilling(t *testing.T) {
	env := msttest.New(t)
	ctx := context.Background()
	s := address.New(env.Kit)
	ps := party.New(env.Kit)
	shree := addrParty(t, env, addrShreeCode, addrShreeName, addrShreePAN, gstin.Composition)
	addrCreate(t, env, s, addrMumbai(shree))
	before := addrPartyGet(t, env.DB, ps, shree)
	if before.GSTCategory != gstin.Composition {
		t.Fatalf("after Mumbai address: GSTCategory = %q; want %q (a user's registered category is kept)", before.GSTCategory, gstin.Composition)
	}

	in := party.Input{Code: addrShreeCode, Name: addrShreeName, IsVendor: true, PAN: addrShreePAN}
	if _, err := ps.Update(ctx, env.Kavya, shree, before.Version, in); err != nil {
		t.Fatalf("Update(SHREE, blank category) = %v; want nil", err)
	}
	if p := addrPartyGet(t, env.DB, ps, shree); p.GSTCategory != gstin.Regular {
		t.Errorf("after Update(blank category): GSTCategory = %q; want %q (guessed from %s)", p.GSTCategory, gstin.Regular, addrMumbaiGSTIN)
	}
}

package address_test

import (
	"context"
	"testing"

	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/platform/db"
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

// MST-034's messages, verbatim from 06-masters.md.
const (
	addrNeedGSTINMsg  = "Enter a GSTIN, or set GST category to Unregistered or Overseas."
	addrUnregGSTINMsg = "GST category can't be Unregistered when a GSTIN is entered."
	addrCoromCode     = "COROM"
	addrCoromName     = "Coromandel Cables"
)

// addrChennai is COROM's Chennai billing address without a GSTIN.
func addrChennai(partyID int64) address.Input {
	return address.Input{PartyID: partyID, Title: "Chennai", Line1: "22 Guindy Industrial Estate", City: "Chennai",
		StateCode: "33", Country: "India", Pincode: "600001", IsBilling: true}
}

// addrPrimaryBillingMsg is CD-177 (1): a registered party's primary billing
// address can't be unticked, disabled or deleted.
const addrPrimaryBillingMsg = "This is the party's primary billing address. Make another billing address primary first."

// addrM5Holds checks M5 over the whole database: every Registered Regular,
// Registered Composition or SEZ party with any address has exactly one
// primary billing address, and it has a GSTIN; no Unregistered party has any
// address with a GSTIN.
func addrM5Holds(t *testing.T, env *msttest.Env, when string) {
	t.Helper()
	ctx := context.Background()
	var registeredNoGSTIN, registeredNotOnePrimary, unregisteredWithGSTIN []string
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		q := func(sqlText string, args []any, out *[]string) error {
			rows, err := tx.SQL().QueryContext(ctx, sqlText, args...)
			if err != nil {
				return err
			}
			defer func() { _ = rows.Close() }()
			for rows.Next() {
				var code string
				if err := rows.Scan(&code); err != nil {
					return err
				}
				*out = append(*out, code)
			}
			return rows.Err()
		}
		if err := q(`SELECT p.code FROM party p JOIN address a ON a.party_id = p.id
			WHERE p.gst_category IN (?, ?, ?) AND a.is_primary_billing = 1 AND a.gstin = ''`,
			[]any{string(gstin.Regular), string(gstin.Composition), string(gstin.SEZ)}, &registeredNoGSTIN); err != nil {
			return err
		}
		if err := q(`SELECT p.code FROM party p
			WHERE p.gst_category IN (?, ?, ?)
			AND EXISTS (SELECT 1 FROM address a WHERE a.party_id = p.id)
			AND (SELECT count(*) FROM address a WHERE a.party_id = p.id AND a.is_primary_billing = 1) <> 1`,
			[]any{string(gstin.Regular), string(gstin.Composition), string(gstin.SEZ)}, &registeredNotOnePrimary); err != nil {
			return err
		}
		return q(`SELECT p.code FROM party p JOIN address a ON a.party_id = p.id
			WHERE p.gst_category = ? AND a.gstin <> ''`,
			[]any{string(gstin.Unregistered)}, &unregisteredWithGSTIN)
	}); err != nil {
		t.Fatalf("%s: M5 check: %v", when, err)
	}
	if len(registeredNoGSTIN) > 0 {
		t.Errorf("%s: registered parties %v have a primary billing address without a GSTIN (M5)", when, registeredNoGSTIN)
	}
	if len(registeredNotOnePrimary) > 0 {
		t.Errorf("%s: registered parties %v have addresses but not exactly one primary billing address (M5, CD-177)", when, registeredNotOnePrimary)
	}
	if len(unregisteredWithGSTIN) > 0 {
		t.Errorf("%s: Unregistered parties %v have an address with a GSTIN (MST-034)", when, unregisteredWithGSTIN)
	}
}

// TestParty_M5HoldsOnEveryWritePath pins M5 and MST-034 on every write that
// can break them: promoting an address, a party update, and a shipping-only
// address with a GSTIN, which is checked against the stored category and
// never upgrades it (only a primary billing address drives the guess and the
// upgrade, MST-049).
func TestParty_M5HoldsOnEveryWritePath(t *testing.T) {
	for _, tc := range []struct {
		name string
		// step sets up the world (checked against M5 too) and returns the
		// write that must be refused.
		step func(t *testing.T, env *msttest.Env) error
		msg  string
	}{
		{
			name: "SetPrimary on a billing address without a GSTIN",
			step: func(t *testing.T, env *msttest.Env) error {
				s := address.New(env.Kit)
				shree := addrShree(t, env)
				addrCreate(t, env, s, addrMumbai(shree))
				office := addrMumbai(shree)
				office.Title, office.Line1, office.GSTIN = "Mumbai office", "Unit 5, Andheri East", ""
				id := addrCreate(t, env, s, office)
				addrM5Holds(t, env, "after the second billing address")
				return addrSetPrimary(env, s, id, true, addrGet(t, env.DB, s, id).Version)
			},
			msg: addrNeedGSTINMsg,
		},
		{
			name: "party Update to Unregistered while an address has a GSTIN",
			step: func(t *testing.T, env *msttest.Env) error {
				s := address.New(env.Kit)
				ps := party.New(env.Kit)
				shree := addrShree(t, env)
				addrCreate(t, env, s, addrMumbai(shree))
				addrM5Holds(t, env, "after Mumbai")
				in := party.Input{Code: addrShreeCode, Name: addrShreeName, IsVendor: true, PAN: addrShreePAN, GSTCategory: gstin.Unregistered}
				_, err := ps.Update(context.Background(), env.Kavya, shree, addrPartyGet(t, env.DB, ps, shree).Version, in)
				return err
			},
			msg: addrUnregGSTINMsg,
		},
		{
			name: "party Update to Registered Regular while the primary billing address has no GSTIN",
			step: func(t *testing.T, env *msttest.Env) error {
				s := address.New(env.Kit)
				ps := party.New(env.Kit)
				corom := addrParty(t, env, addrCoromCode, addrCoromName, "", gstin.Unregistered)
				addrCreate(t, env, s, addrChennai(corom))
				addrM5Holds(t, env, "after Chennai")
				in := party.Input{Code: addrCoromCode, Name: addrCoromName, IsVendor: true, GSTCategory: gstin.Regular}
				_, err := ps.Update(context.Background(), env.Kavya, corom, addrPartyGet(t, env.DB, ps, corom).Version, in)
				return err
			},
			msg: addrNeedGSTINMsg,
		},
		{
			name: "shipping-only address with a GSTIN on an Unregistered party",
			step: func(t *testing.T, env *msttest.Env) error {
				s := address.New(env.Kit)
				corom := addrParty(t, env, addrCoromCode, addrCoromName, "", gstin.Unregistered)
				addrCreate(t, env, s, addrChennai(corom))
				addrM5Holds(t, env, "after Chennai")
				ship := addrChennai(corom)
				ship.Title, ship.Line1, ship.GSTIN = "Chennai godown", "8 Ambattur Industrial Estate", addrCoromGSTIN
				ship.IsBilling, ship.IsShipping = false, true
				_, err := s.Create(context.Background(), env.Kavya, ship)
				return err
			},
			msg: addrUnregGSTINMsg,
		},
		{
			name: "Update unticking Billing on a registered party's primary billing address",
			step: func(t *testing.T, env *msttest.Env) error {
				s := address.New(env.Kit)
				id := addrShreeMumbai(t, env, s)
				in := addrMumbai(addrOwnerOf(t, env, s, id))
				in.IsBilling, in.IsShipping = false, true
				_, err := s.Update(context.Background(), env.Kavya, id, addrGet(t, env.DB, s, id).Version, in)
				return err
			},
			msg: addrPrimaryBillingMsg,
		},
		{
			name: "SetDisabled on a registered party's only primary billing address",
			step: func(t *testing.T, env *msttest.Env) error {
				s := address.New(env.Kit)
				id := addrShreeMumbai(t, env, s)
				return s.SetDisabled(context.Background(), env.Kavya, id, addrGet(t, env.DB, s, id).Version, true)
			},
			msg: addrPrimaryBillingMsg,
		},
		{
			name: "Delete of a registered party's only primary billing address",
			step: func(t *testing.T, env *msttest.Env) error {
				s := address.New(env.Kit)
				id := addrShreeMumbai(t, env, s)
				return s.Delete(context.Background(), env.Kavya, id, addrGet(t, env.DB, s, id).Version)
			},
			msg: addrPrimaryBillingMsg,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := msttest.New(t)
			addrM5Holds(t, env, "at start")
			err := tc.step(t, env)
			addrM5Holds(t, env, "after the refused write")
			addrWantHas(t, err, tc.msg)
		})
	}

}

// addrShreeMumbai creates SHREE with Mumbai as its only (primary billing)
// address, checks M5, and returns Mumbai's id.
func addrShreeMumbai(t *testing.T, env *msttest.Env, s *address.Service) int64 {
	t.Helper()
	id := addrCreate(t, env, s, addrMumbai(addrShree(t, env)))
	if !addrGet(t, env.DB, s, id).PrimaryBilling {
		t.Fatal("Mumbai: PrimaryBilling = false; want true (first billing address)")
	}
	addrM5Holds(t, env, "after Mumbai")
	return id
}

// addrOwnerOf is the party of address id.
func addrOwnerOf(t *testing.T, env *msttest.Env, s *address.Service, id int64) int64 {
	t.Helper()
	return addrGet(t, env.DB, s, id).PartyID
}

// TestParty_UnregisteredMayLosePrimaryBilling: CD-177 (1) lets an
// Unregistered party's primary billing address be disabled or deleted.
func TestParty_UnregisteredMayLosePrimaryBilling(t *testing.T) {
	env := msttest.New(t)
	ctx := context.Background()
	s := address.New(env.Kit)

	corom := addrParty(t, env, addrCoromCode, addrCoromName, "", gstin.Unregistered)
	chennai := addrCreate(t, env, s, addrChennai(corom))
	if !addrGet(t, env.DB, s, chennai).PrimaryBilling {
		t.Fatal("Chennai: PrimaryBilling = false; want true (first billing address)")
	}
	if err := s.SetDisabled(ctx, env.Kavya, chennai, addrGet(t, env.DB, s, chennai).Version, true); err != nil {
		t.Errorf("SetDisabled(COROM's primary billing Chennai) = %v; want nil", err)
	}

	tata := addrParty(t, env, "C-TATA", "Tata Projects", "", gstin.Unregistered)
	mumbai := addrMumbai(tata)
	mumbai.GSTIN = ""
	id := addrCreate(t, env, s, mumbai)
	if !addrGet(t, env.DB, s, id).PrimaryBilling {
		t.Fatal("C-TATA Mumbai: PrimaryBilling = false; want true (first billing address)")
	}
	if err := s.Delete(ctx, env.Kavya, id, addrGet(t, env.DB, s, id).Version); err != nil {
		t.Errorf("Delete(C-TATA's primary billing Mumbai) = %v; want nil", err)
	}
	if as := addrForParty(t, env.DB, s, tata); len(as) != 0 {
		t.Errorf("after Delete: ForParty(C-TATA) = %+v; want none", as)
	}
	addrM5Holds(t, env, "after disable and delete")
}

// TestParty_OverseasCountryOnPartyUpdate pins CD-177 (2): a party with an
// address outside India can't be updated to any category but Overseas
// (MST-047's text).
func TestParty_OverseasCountryOnPartyUpdate(t *testing.T) {
	env := msttest.New(t)
	s := address.New(env.Kit)
	ps := party.New(env.Kit)
	sg := addrParty(t, env, "SG-STEEL", "Singapore Steel Pte Ltd", "", gstin.Overseas)
	addrCreate(t, env, s, address.Input{
		PartyID: sg, Title: "Singapore", Line1: "1 Raffles Place", City: "Singapore",
		Country: "Singapore", Pincode: "118524", IsBilling: true,
	})

	in := party.Input{Code: "SG-STEEL", Name: "Singapore Steel Pte Ltd", IsVendor: true, GSTCategory: gstin.Unregistered}
	_, err := ps.Update(context.Background(), env.Kavya, sg, addrPartyGet(t, env.DB, ps, sg).Version, in)
	addrWantHas(t, err, addrOverseasMsg)
	if p := addrPartyGet(t, env.DB, ps, sg); p.GSTCategory != gstin.Overseas {
		t.Errorf("after refusal: SG-STEEL GSTCategory = %q; want %q", p.GSTCategory, gstin.Overseas)
	}
}

// TestParty_CategoryCheckedAgainstAllGSTINs pins CD-177 (2): a party update
// checks the category against every address GSTIN, not only the primary
// billing one. A TCS GSTIN fits Registered Regular only (MST-033, CD-39).
func TestParty_CategoryCheckedAgainstAllGSTINs(t *testing.T) {
	const (
		fkCode     = "FKART"
		fkName     = "Flipkart Internet"
		fkPAN      = "AABCF8078M"
		fkGSTIN    = "29AABCF8078M1ZX" // regular GSTIN of the TCS GSTIN's PAN (check digit by gstin.CheckDigit)
		fkTCS      = "29AABCF8078M1C8" // MST-033's e-commerce TCS GSTIN
		fkMismatch = "GSTIN 29AABCF8078M1C8 doesn't match GST category Registered Composition. Check the GSTIN and the category."
	)
	env := msttest.New(t)
	s := address.New(env.Kit)
	ps := party.New(env.Kit)
	fk := addrParty(t, env, fkCode, fkName, fkPAN, gstin.Regular)
	addrCreate(t, env, s, address.Input{PartyID: fk, Title: "Bengaluru", Line1: "Embassy Tech Village", City: "Bengaluru",
		StateCode: "29", Country: "India", Pincode: "560103", GSTIN: fkGSTIN, IsBilling: true})
	addrCreate(t, env, s, address.Input{PartyID: fk, Title: "Bengaluru TCS", Line1: "Embassy Tech Village", City: "Bengaluru",
		StateCode: "29", Country: "India", Pincode: "560103", GSTIN: fkTCS, IsShipping: true})
	addrM5Holds(t, env, "after both addresses")

	in := party.Input{Code: fkCode, Name: fkName, IsVendor: true, PAN: fkPAN, GSTCategory: gstin.Composition}
	_, err := ps.Update(context.Background(), env.Kavya, fk, addrPartyGet(t, env.DB, ps, fk).Version, in)
	addrWantHas(t, err, fkMismatch)
	if p := addrPartyGet(t, env.DB, ps, fk); p.GSTCategory != gstin.Regular {
		t.Errorf("after refusal: FKART GSTCategory = %q; want %q", p.GSTCategory, gstin.Regular)
	}
}

// TestParty_OverseasKeptWithoutGSTIN: an Overseas party with an Indian
// address and no GSTIN (MST-047 allows it) keeps Overseas when saved with a
// blank category; the guess only fills a category the GSTIN decides.
func TestParty_OverseasKeptWithoutGSTIN(t *testing.T) {
	env := msttest.New(t)
	s := address.New(env.Kit)
	ps := party.New(env.Kit)
	ovs := addrParty(t, env, "HIGHSEAS", "High Seas Traders", "", gstin.Overseas)
	in := addrMumbai(ovs)
	in.GSTIN = ""
	addrCreate(t, env, s, in)

	pin := party.Input{Code: "HIGHSEAS", Name: "High Seas Traders", IsVendor: true}
	if _, err := ps.Update(context.Background(), env.Kavya, ovs, addrPartyGet(t, env.DB, ps, ovs).Version, pin); err != nil {
		t.Fatalf("Update(HIGHSEAS, blank category) = %v; want nil", err)
	}
	if p := addrPartyGet(t, env.DB, ps, ovs); p.GSTCategory != gstin.Overseas {
		t.Errorf("after Update(blank category): GSTCategory = %q; want %q", p.GSTCategory, gstin.Overseas)
	}
}

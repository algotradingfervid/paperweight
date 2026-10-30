package address_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doc/docfake"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/mstfixture"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/masters/paymentterms"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// p2Mix is MST-043's B-MIX, vendor and customer, created in the test.
func p2Mix(t *testing.T, env *msttest.Env, kit mstkit.Env) mstkit.Saved {
	t.Helper()
	saved, err := party.New(kit).Create(context.Background(), env.Kavya, p2MixInput())
	if err != nil {
		t.Fatalf("party Create(B-MIX): %v", err)
	}
	return saved
}

func p2MixInput() party.Input {
	return party.Input{Code: "B-MIX", Name: "Mixed Party Ltd", IsVendor: true, IsCustomer: true, GSTCategory: gstin.Unregistered}
}

func p2Lines(m msttest.Masters) []mstfixture.Line {
	return []mstfixture.Line{{ItemID: m.Items["MS-ANGLE-50"], UoMID: m.UoMs["KGS"], Qty: docQty, Rate: 62_5000}}
}

func TestParty_RoleInUse(t *testing.T) { // MST-043
	ctx := context.Background()
	untick := func(in party.Input, vendor bool) party.Input {
		if vendor {
			in.IsVendor = false
		} else {
			in.IsCustomer = false
		}
		return in
	}

	t.Run("approved order", func(t *testing.T) {
		env := msttest.New(t)
		m := msttest.SeedMasters(t, env)
		ps := party.New(env.Kit)
		mix := p2Mix(t, env, env.Kit)
		saved, err := mstfixture.Save(ctx, env, env.Priya, 0, docOrder(m, mix.ID, 0), p2Lines(m))
		if err != nil {
			t.Fatalf("save order: %v", err)
		}
		number := mstfixture.Approve(t, env, saved.ID)
		if number != "PO/26-27/00001" {
			t.Fatalf("approved number = %q; want PO/26-27/00001", number)
		}

		_, err = ps.Update(ctx, env.Kavya, mix.ID, mix.Version, untick(p2MixInput(), true))
		m5WantField(t, err, "is_vendor", "B-MIX is used as a vendor on PO/26-27/00001. Disable the party instead.")
		if got := addrPartyGet(t, env.DB, ps, mix.ID); !got.IsVendor || got.Version != mix.Version {
			t.Errorf("after refusal: IsVendor %v, Version %d; want true, %d", got.IsVendor, got.Version, mix.Version)
		}

		// Only the role in use is locked.
		if _, err := ps.Update(ctx, env.Kavya, mix.ID, mix.Version, untick(p2MixInput(), false)); err != nil {
			t.Fatalf("untick customer = %v; want nil", err)
		}
	})

	t.Run("draft counts", func(t *testing.T) {
		env := msttest.New(t)
		m := msttest.SeedMasters(t, env)
		mix := p2Mix(t, env, env.Kit)
		saved, err := mstfixture.Save(ctx, env, env.Priya, 0, docOrder(m, mix.ID, 0), p2Lines(m))
		if err != nil {
			t.Fatalf("save order: %v", err)
		}
		_, err = party.New(env.Kit).Update(ctx, env.Kavya, mix.ID, mix.Version, untick(p2MixInput(), true))
		m5WantField(t, err, "is_vendor", fmt.Sprintf("B-MIX is used as a vendor on Draft %d. Disable the party instead.", saved.ID))
	})

	// The fixture has no customer link and the DOC test fixture calls its
	// vendor link supplier_id: docfake supplies those references.
	for _, tc := range []struct {
		name, field, want string
		vendor            bool
	}{
		{"supplier link", "supplier_id", "B-MIX is used as a vendor on PO/26-27/00007. Disable the party instead.", true},
		{"customer link", "customer_id", "B-MIX is used as a customer on PO/26-27/00007. Disable the party instead.", false},
		{"vendor line link", "lines.vendor_id", "B-MIX is used as a vendor on PO/26-27/00007. Disable the party instead.", true},
		{"customer line link", "lines.customer_id", "B-MIX is used as a customer on PO/26-27/00007. Disable the party instead.", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := msttest.New(t)
			docs := docfake.New()
			kit := env.Kit
			kit.Engine = docs
			mix := p2Mix(t, env, kit)
			docs.AddReference("party", mix.ID, doc.Reference{DocType: "Purchase Order", Field: tc.field, ID: 7, Display: "PO/26-27/00007", State: doc.StateApproved})
			field := "is_customer"
			if tc.vendor {
				field = "is_vendor"
			}
			_, err := party.New(kit).Update(ctx, env.Kavya, mix.ID, mix.Version, untick(p2MixInput(), tc.vendor))
			m5WantField(t, err, field, tc.want)
		})
	}

	t.Run("reference by another field stays free", func(t *testing.T) {
		env := msttest.New(t)
		docs := docfake.New()
		kit := env.Kit
		kit.Engine = docs
		mix := p2Mix(t, env, kit)
		docs.AddReference("party", mix.ID, doc.Reference{DocType: "Purchase Order", Field: "customer_id", ID: 7, Display: "PO/26-27/00007", State: doc.StateApproved})
		if _, err := party.New(kit).Update(ctx, env.Kavya, mix.ID, mix.Version, untick(p2MixInput(), true)); err != nil {
			t.Fatalf("untick vendor with only a customer use = %v; want nil", err)
		}
	})

	// MST-103 needs a project's customer to be one, so the untick is refused
	// from the party's side too (CD-167: References covers documents only).
	t.Run("project customer", func(t *testing.T) {
		env := msttest.New(t)
		m := msttest.SeedMasters(t, env)
		mix := p2Mix(t, env, env.Kit)
		if _, err := project.New(env.Kit).Create(ctx, env.Kavya, project.Input{Code: "PRJ-MIX", Name: "Mixed", BranchID: m.Branches["PUNE"], CustomerID: mix.ID}); err != nil {
			t.Fatalf("project Create: %v", err)
		}
		_, err := party.New(env.Kit).Update(ctx, env.Kavya, mix.ID, mix.Version, untick(p2MixInput(), false))
		m5WantField(t, err, "is_customer", "B-MIX is used as a customer on Project PRJ-MIX. Disable the party instead.")
	})

	t.Run("a stale page gets the conflict", func(t *testing.T) {
		env := msttest.New(t)
		m := msttest.SeedMasters(t, env)
		mix := p2Mix(t, env, env.Kit)
		if _, err := mstfixture.Save(ctx, env, env.Priya, 0, docOrder(m, mix.ID, 0), p2Lines(m)); err != nil {
			t.Fatalf("save order: %v", err)
		}
		_, err := party.New(env.Kit).Update(ctx, env.Kavya, mix.ID, mix.Version+5, untick(p2MixInput(), true))
		var he doctype.HTTPError
		if !errors.As(err, &he) || he.Status() != 409 {
			t.Errorf("err = %v; want a 409", err)
		}
	})
}

func TestParty_SamePANOrNameWarns(t *testing.T) { // MST-045
	const (
		panWarn  = "Party SHREE has the same PAN AABCS1429B. You may be creating a duplicate; consider adding a Delhi address to SHREE."
		nameWarn = "A party named Shree Steel Traders already exists (SHREE)."
	)
	ctx := context.Background()

	t.Run("new party with the same name and PAN", func(t *testing.T) {
		env := msttest.New(t)
		m := msttest.SeedMasters(t, env)
		ps, as := party.New(env.Kit), address.New(env.Kit)
		delhi := m.Addresses["Delhi"]
		if err := as.Delete(ctx, env.Kavya, delhi, addrGet(t, env.DB, as, delhi).Version); err != nil {
			t.Fatalf("delete SHREE's Delhi address: %v", err)
		}

		in := party.Input{Code: "SHREE-DEL", Name: addrShreeName, IsVendor: true}
		saved, err := ps.Create(ctx, env.Kavya, in)
		if err != nil {
			t.Fatalf("party Create: %v", err)
		}
		if !slices.Equal(saved.Warnings, []string{nameWarn}) {
			t.Errorf("party Warnings = %q; want %q", saved.Warnings, nameWarn)
		}

		a := addrDelhi(saved.ID)
		created, err := as.Create(ctx, env.Kavya, a)
		if err != nil {
			t.Fatalf("address Create: %v", err)
		}
		if !slices.Equal(created.Warnings, []string{panWarn}) {
			t.Errorf("address Warnings = %q; want %q", created.Warnings, panWarn)
		}
		if got := addrPartyGet(t, env.DB, ps, saved.ID).PAN; got != addrShreePAN {
			t.Errorf("SHREE-DEL PAN = %q; want %q (it saves)", got, addrShreePAN)
		}

		// Saving again with nothing new warns no more, and writes nothing.
		in.GSTCategory = gstin.Regular
		stored := addrPartyGet(t, env.DB, ps, saved.ID).Version
		again, err := ps.Update(ctx, env.Kavya, saved.ID, stored, in)
		if err != nil {
			t.Fatalf("party Update: %v", err)
		}
		if again.Version != stored {
			t.Errorf("party no-op Update Version = %d; want %d", again.Version, stored)
		}
		if len(again.Warnings) != 0 {
			t.Errorf("party Update Warnings = %q; want none", again.Warnings)
		}
		version := addrGet(t, env.DB, as, created.ID).Version
		noop, err := as.Update(ctx, env.Kavya, created.ID, version, a)
		if err != nil {
			t.Fatalf("address no-op Update: %v", err)
		}
		if len(noop.Warnings) != 0 || noop.Version != version {
			t.Errorf("address no-op Update = {Warnings %q, Version %d}; want {none, %d}", noop.Warnings, noop.Version, version)
		}
		a.Line2 = "Near the metro"
		upd, err := as.Update(ctx, env.Kavya, created.ID, addrGet(t, env.DB, as, created.ID).Version, a)
		if err != nil {
			t.Fatalf("address Update: %v", err)
		}
		if len(upd.Warnings) != 0 {
			t.Errorf("address Update Warnings = %q; want none", upd.Warnings)
		}
	})

	t.Run("GSTIN changed on an update", func(t *testing.T) {
		env := msttest.New(t)
		msttest.SeedMasters(t, env)
		as := address.New(env.Kit)
		// A GSTIN SHREE doesn't hold, with SHREE's PAN.
		other := addrParty(t, env, "SHREE-NEW", "Shree New", "", gstin.Unregistered)
		in := address.Input{PartyID: other, Title: "Kolkata", Line1: "1 Park Street", City: "Kolkata", StateCode: "19", Pincode: "700016", IsBilling: true}
		in.GSTIN = "19AABCS1429B1ZR"
		created, err := as.Create(ctx, env.Kavya, in)
		if err != nil {
			t.Fatalf("address Create: %v", err)
		}
		want := "Party SHREE has the same PAN AABCS1429B. You may be creating a duplicate; consider adding a West Bengal address to SHREE."
		if !slices.Equal(created.Warnings, []string{want}) {
			t.Errorf("Warnings = %q; want %q", created.Warnings, want)
		}
		in.GSTIN = "19AABCS1429B2ZQ"
		upd, err := as.Update(ctx, env.Kavya, created.ID, addrGet(t, env.DB, as, created.ID).Version, in)
		if err != nil {
			t.Fatalf("address Update: %v", err)
		}
		if !slices.Equal(upd.Warnings, []string{want}) {
			t.Errorf("Update Warnings = %q; want %q", upd.Warnings, want)
		}
	})

	t.Run("typed PAN with no address", func(t *testing.T) {
		const typedWarn = "Party SHREE has the same PAN AABCS1429B. You may be creating a duplicate."
		env := msttest.New(t)
		msttest.SeedMasters(t, env)
		ps := party.New(env.Kit)

		in := party.Input{Code: "SHREE-TWO", Name: "Second Shree", IsVendor: true, PAN: addrShreePAN}
		saved, err := ps.Create(ctx, env.Kavya, in)
		if err != nil {
			t.Fatalf("party Create: %v", err)
		}
		if !slices.Equal(saved.Warnings, []string{typedWarn}) {
			t.Errorf("Create Warnings = %q; want %q", saved.Warnings, typedWarn)
		}

		edit := party.Input{Code: "EDITED", Name: "Edited Ltd", IsVendor: true, PAN: "AAFCV2449G"}
		other, err := ps.Create(ctx, env.Kavya, edit)
		if err != nil {
			t.Fatalf("party Create: %v", err)
		}
		if len(other.Warnings) != 0 {
			t.Errorf("Create with a free PAN: Warnings = %q; want none", other.Warnings)
		}
		upd, err := ps.Update(ctx, env.Kavya, other.ID, other.Version, edit)
		if err != nil {
			t.Fatalf("party Update: %v", err)
		}
		if len(upd.Warnings) != 0 {
			t.Errorf("Update with the PAN unchanged: Warnings = %q; want none", upd.Warnings)
		}
		edit.PAN = addrShreePAN
		upd, err = ps.Update(ctx, env.Kavya, other.ID, upd.Version, edit)
		if err != nil {
			t.Fatalf("party Update: %v", err)
		}
		// SHREE-TWO holds the PAN as well.
		want := []string{typedWarn, "Party SHREE-TWO has the same PAN AABCS1429B. You may be creating a duplicate."}
		if !slices.Equal(upd.Warnings, want) {
			t.Errorf("PAN edit Warnings = %q; want %q", upd.Warnings, want)
		}
	})

	t.Run("rename onto another party in another case", func(t *testing.T) {
		env := msttest.New(t)
		msttest.SeedMasters(t, env)
		ps := party.New(env.Kit)
		in := party.Input{Code: "NEWCO", Name: "New Traders", IsVendor: true}
		saved, err := ps.Create(ctx, env.Kavya, in)
		if err != nil {
			t.Fatalf("party Create: %v", err)
		}
		if len(saved.Warnings) != 0 {
			t.Errorf("Create Warnings = %q; want none", saved.Warnings)
		}
		in.Name = "SHREE STEEL TRADERS"
		upd, err := ps.Update(ctx, env.Kavya, saved.ID, saved.Version, in)
		if err != nil {
			t.Fatalf("party Update: %v", err)
		}
		if !slices.Equal(upd.Warnings, []string{nameWarn}) {
			t.Errorf("Update Warnings = %q; want %q", upd.Warnings, nameWarn)
		}
	})

	t.Run("a branch address is no duplicate party", func(t *testing.T) {
		env := msttest.New(t)
		m := msttest.SeedMasters(t, env)
		addrParty(t, env, "VARDHAN", "Vardhan as a vendor", "AAFCV2449G", gstin.Unregistered)
		saved, err := address.New(env.Kit).Create(ctx, env.Kavya, address.Input{
			BranchID: m.Branches["PUNE"], Title: "Bhosari", Line1: "Plot 12", City: "Pune", StateCode: "27", Pincode: "411026",
			GSTIN: "27AAFCV2449G1Z7", IsShipping: true,
		})
		if err != nil {
			t.Fatalf("address Create: %v", err)
		}
		if len(saved.Warnings) != 0 {
			t.Errorf("Warnings = %q; want none", saved.Warnings)
		}
	})
}

func TestParty_DefaultPaymentTerms(t *testing.T) { // MST-050
	ctx := context.Background()
	env := msttest.New(t)
	m := msttest.SeedMasters(t, env)
	ps := party.New(env.Kit)
	ts := paymentterms.New(env.Kit)
	terms, err := ts.Create(ctx, env.Kavya, paymentterms.Input{Code: "NET30", Name: "Net 30", Rows: []paymentterms.Row{{Portion: 100_000, Days: 30}}})
	if err != nil {
		t.Fatalf("create NET30: %v", err)
	}
	shree := addrPartyGet(t, env.DB, ps, m.Parties["SHREE"])
	if _, err := ps.Update(ctx, env.Kavya, shree.ID, shree.Version, party.Input{
		Code: shree.Code, Name: shree.Name, IsVendor: shree.IsVendor, PAN: shree.PAN, GSTCategory: shree.GSTCategory, DefaultPaymentTermsID: terms.ID,
	}); err != nil {
		t.Fatalf("set default terms: %v", err)
	}

	defaults := func() party.Defaults {
		t.Helper()
		var got party.Defaults
		if err := env.DB.Read(ctx, func(tx *db.Tx) error {
			var err error
			got, err = ps.Defaults(ctx, tx, shree.ID)
			return err
		}); err != nil {
			t.Fatalf("Defaults: %v", err)
		}
		return got
	}
	if got := defaults().PaymentTermsID; got != terms.ID {
		t.Errorf("PaymentTermsID = %d; want NET30 (%d)", got, terms.ID)
	}
	if err := ts.SetDisabled(ctx, env.Kavya, terms.ID, terms.Version, true); err != nil {
		t.Fatalf("disable NET30: %v", err)
	}
	if got := defaults().PaymentTermsID; got != 0 {
		t.Errorf("PaymentTermsID with NET30 disabled = %d; want 0", got)
	}
}

// TestParty_DefaultPaymentTermsChecked: the link is re-checked on every save,
// through Create and Update, also when the terms went off after being set.
func TestParty_DefaultPaymentTermsChecked(t *testing.T) {
	const (
		field       = "default_payment_terms_id"
		notFound    = "Default payment terms not found. Pick one from the list."
		disabledMsg = "Payment terms NET30 is disabled. Pick another payment terms."
		noTerms     = 9999
	)
	ctx := context.Background()
	env := msttest.New(t)
	ps := party.New(env.Kit)
	ts := paymentterms.New(env.Kit)
	terms, err := ts.Create(ctx, env.Kavya, paymentterms.Input{Code: "NET30", Name: "Net 30", Rows: []paymentterms.Row{{Portion: 100_000, Days: 30}}})
	if err != nil {
		t.Fatalf("create NET30: %v", err)
	}
	in := party.Input{Code: "TERMS", Name: "Terms Ltd", IsVendor: true}
	in.DefaultPaymentTermsID = noTerms
	_, err = ps.Create(ctx, env.Kavya, in)
	m5WantField(t, err, field, notFound)

	in.DefaultPaymentTermsID = terms.ID
	saved, err := ps.Create(ctx, env.Kavya, in)
	if err != nil {
		t.Fatalf("party Create: %v", err)
	}

	in.DefaultPaymentTermsID = noTerms
	_, err = ps.Update(ctx, env.Kavya, saved.ID, saved.Version, in)
	m5WantField(t, err, field, notFound)

	if err := ts.SetDisabled(ctx, env.Kavya, terms.ID, terms.Version, true); err != nil {
		t.Fatalf("disable NET30: %v", err)
	}
	// An edit of another field with the stored link is refused too.
	in.DefaultPaymentTermsID, in.Name = terms.ID, "Terms Ltd Renamed"
	_, err = ps.Update(ctx, env.Kavya, saved.ID, saved.Version, in)
	m5WantField(t, err, field, disabledMsg)
	if got := addrPartyGet(t, env.DB, ps, saved.ID); got.Name != "Terms Ltd" || got.Version != saved.Version {
		t.Errorf("after refusal: Name %q, Version %d; want Terms Ltd, %d", got.Name, got.Version, saved.Version)
	}

	in.Code = "TERMS-2"
	_, err = ps.Create(ctx, env.Kavya, in)
	m5WantField(t, err, field, disabledMsg)

	in.Code, in.DefaultPaymentTermsID = "TERMS", 0
	if _, err := ps.Update(ctx, env.Kavya, saved.ID, saved.Version, in); err != nil {
		t.Fatalf("Update clearing the terms = %v; want nil", err)
	}
}

func TestAddress_PincodeState(t *testing.T) { // MST-056
	for _, tc := range []struct {
		name, state, pin, want string
	}{
		{"Karnataka refuses a Hyderabad pin", "29", "500001", "Pincode 500001 is not in Karnataka. Check the pincode or the state."},
		{"Telangana", "36", "500001", ""},
		{"Maharashtra", "27", "400093", ""},
		{"Ladakh", "38", "194101", ""},
		{"Jammu and Kashmir", "01", "194101", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := addrValid()
			in.StateCode, in.Pincode = tc.state, tc.pin
			_, err := address.Validate(in)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("Validate = %v; want nil", err)
				}
				return
			}
			m5WantField(t, err, "pincode", tc.want)
		})
	}

	// The state a GSTIN gives is the one checked.
	in := addrValid()
	in.StateCode, in.GSTIN, in.Pincode = "", addrMumbaiGSTIN, "560001"
	_, err := address.Validate(in)
	m5WantField(t, err, "pincode", "Pincode 560001 is not in Maharashtra. Check the pincode or the state.")
}

func TestAddress_Foreign(t *testing.T) { // MST-057
	ctx := context.Background()
	env := msttest.New(t)
	s := address.New(env.Kit)
	// Another party with a blank PAN: a GSTIN with no PAN warns of nothing.
	addrParty(t, env, "BLANK", "Blank PAN Ltd", "", gstin.Unregistered)
	abroad := addrParty(t, env, "SG-SUPPLY", "Singapore Supply Pte", "", gstin.Overseas)

	saved, err := s.Create(ctx, env.Kavya, address.Input{
		PartyID: abroad, Title: "Singapore", Line1: "1 Raffles Place", City: "Singapore", Country: "Singapore",
		Pincode: "118524", GSTIN: "9917SGP29001OST", IsBilling: true,
	})
	if err != nil {
		t.Fatalf("Create = %v; want nil", err)
	}
	if len(saved.Warnings) != 0 {
		t.Errorf("Warnings = %q; want none", saved.Warnings)
	}
	a := addrGet(t, env.DB, s, saved.ID)
	if a.StateCode != "" || a.Pincode != "118524" || a.GSTIN != "9917SGP29001OST" {
		t.Errorf("address = {StateCode %q, Pincode %q, GSTIN %q}; want {\"\", 118524, 9917SGP29001OST}", a.StateCode, a.Pincode, a.GSTIN)
	}
	if got := address.PlaceOfSupply(a); got != "96" {
		t.Errorf("PlaceOfSupply = %q; want 96", got)
	}
}

func TestAddress_Disabled(t *testing.T) { // MST-061, CD-180 D2
	const msg = "Address Thane is disabled. Pick an active address, or ask an admin to enable it." // CD-38
	ctx := context.Background()
	env := msttest.New(t)
	m := msttest.SeedMasters(t, env)
	s := address.New(env.Kit)
	shree, thane, delhi := m.Parties["SHREE"], m.Addresses["Thane"], m.Addresses["Delhi"]
	if !addrGet(t, env.DB, s, thane).PrimaryShipping || addrGet(t, env.DB, s, delhi).PrimaryShipping {
		t.Fatal("seed: want Thane primary shipping and Delhi not")
	}
	h := docOrder(m, shree, thane)
	draft, err := mstfixture.Save(ctx, env, env.Priya, 0, h, p2Lines(m))
	if err != nil {
		t.Fatalf("save draft: %v", err)
	}

	if err := s.SetDisabled(ctx, env.Kavya, thane, addrGet(t, env.DB, s, thane).Version, true); err != nil {
		t.Fatalf("disable Thane: %v", err)
	}

	// Not offered, and primary no more; nothing was promoted.
	for _, a := range addrForParty(t, env.DB, s, shree) {
		if a.ID == thane {
			t.Error("ForParty offers the disabled Thane address")
		}
	}
	if a := addrGet(t, env.DB, s, thane); a.PrimaryShipping || !a.Disabled {
		t.Errorf("Thane = {PrimaryShipping %v, Disabled %v}; want {false, true}", a.PrimaryShipping, a.Disabled)
	}
	if addrPrimaryCount(t, env.DB, shree, "is_primary_shipping") != 0 {
		t.Error("a primary shipping address remains; want none (nothing promoted)")
	}

	// A draft already using it fails on save.
	_, err = mstfixture.Save(ctx, env, env.Priya, draft.ID, h, p2Lines(m))
	var de *doc.Error
	if !errors.As(err, &de) || len(de.FieldErrors()) != 1 || de.FieldErrors()[0] != (doctype.FieldError{Field: "address_id", Message: msg}) {
		t.Fatalf("save draft = %+v; want one error on address_id %q", err, msg)
	}

	// Enabled again, it saves; it isn't primary until someone makes it so.
	if err := s.SetDisabled(ctx, env.Kavya, thane, addrGet(t, env.DB, s, thane).Version, false); err != nil {
		t.Fatalf("enable Thane: %v", err)
	}
	if _, err := mstfixture.Save(ctx, env, env.Priya, draft.ID, h, p2Lines(m)); err != nil {
		t.Fatalf("save draft after enabling = %v; want nil", err)
	}
	if addrGet(t, env.DB, s, thane).PrimaryShipping {
		t.Error("enabling Thane made it primary again; want not")
	}
}

func TestContact_OnePrimaryEmail(t *testing.T) { // MST-065
	emails := func(primary ...bool) []address.EmailInput {
		out := make([]address.EmailInput, len(primary))
		for i, p := range primary {
			out[i] = address.EmailInput{Email: fmt.Sprintf("a%d@shreesteel.test", i), Primary: p}
		}
		return out
	}
	phones := func(primary ...bool) []address.PhoneInput {
		out := make([]address.PhoneInput, len(primary))
		for i, p := range primary {
			out[i] = address.PhoneInput{Number: fmt.Sprintf("022 2345 000%d", i), Primary: p}
		}
		return out
	}

	t.Run("none marked: the first", func(t *testing.T) {
		in := contactInput("Sunil", "", "Mehta")
		in.Emails, in.Phones = emails(false, false, false), phones(false, false)
		got, err := address.ValidateContact(in)
		if err != nil {
			t.Fatalf("ValidateContact = %v; want nil", err)
		}
		if !got.Emails[0].Primary || got.Emails[1].Primary || got.Emails[2].Primary {
			t.Errorf("Emails = %+v; want only the first primary", got.Emails)
		}
		if !got.Phones[0].Primary || got.Phones[1].Primary {
			t.Errorf("Phones = %+v; want only the first primary", got.Phones)
		}
	})

	t.Run("one marked stays", func(t *testing.T) {
		in := contactInput("Sunil", "", "Mehta")
		in.Emails, in.Phones = emails(false, true), phones(false, true)
		got, err := address.ValidateContact(in)
		if err != nil {
			t.Fatalf("ValidateContact = %v; want nil", err)
		}
		if got.Emails[0].Primary || !got.Emails[1].Primary {
			t.Errorf("Emails = %+v; want only the second primary", got.Emails)
		}
		if got.Phones[0].Primary || !got.Phones[1].Primary {
			t.Errorf("Phones = %+v; want only the second primary", got.Phones)
		}
	})

	t.Run("two marked", func(t *testing.T) {
		in := contactInput("Sunil", "", "Mehta")
		in.Emails = emails(true, true, false)
		_, err := address.ValidateContact(in)
		contactWantOnly(t, err, "Only one email can be primary.")
	})

	t.Run("saved", func(t *testing.T) {
		env := msttest.New(t)
		s := address.New(env.Kit)
		ctx := context.Background()
		shree := addrShree(t, env)
		in := address.ContactInput{PartyID: shree, FirstName: "Sunil", Emails: emails(false, false, false)}
		saved, err := s.CreateContact(ctx, env.Kavya, in)
		if err != nil {
			t.Fatalf("CreateContact: %v", err)
		}
		var c address.Contact
		if err := env.DB.Read(ctx, func(tx *db.Tx) error {
			var err error
			c, err = s.GetContact(ctx, tx, saved.ID)
			return err
		}); err != nil {
			t.Fatalf("GetContact: %v", err)
		}
		if len(c.Emails) != 3 || !c.Emails[0].Primary || c.Emails[1].Primary || c.Emails[2].Primary {
			t.Errorf("stored Emails = %+v; want three, only the first primary", c.Emails)
		}
	})
}

func TestAddress_DisplayText(t *testing.T) { // MST-068
	a := address.Address{
		Title: "Mumbai", Line1: "Plot 12, MIDC", Line2: "Andheri East", City: "Mumbai",
		StateCode: "27", Country: "India", Pincode: "400093", GSTIN: addrMumbaiGSTIN,
	}
	const want = "Plot 12, MIDC\nAndheri East\nMumbai - 400093\nMaharashtra (27)\nGSTIN: 27AABCS1429B1ZU"
	if got := address.DisplayText(a); got != want {
		t.Errorf("DisplayText = %q; want %q", got, want)
	}
}

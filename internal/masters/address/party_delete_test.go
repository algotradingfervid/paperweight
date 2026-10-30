package address_test

import (
	"context"
	"errors"
	"maps"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doc/docfake"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// MST-046's party is created in the test (M7: C-TATA Tata Projects); its
// refusal is MST-014's wording with the reference PO's number.
const (
	deleteCode   = "C-TATA"
	deleteRefuse = "Cannot delete Party C-TATA: it is used in PO/26-27/00001. Disable it instead."
)

// deleteParty is MST-046's party with two addresses and one contact (one
// email, one mobile), never used.
type deleteParty struct {
	party, mumbai, thane, contact int64
}

func newDeleteParty(t *testing.T, env *msttest.Env, kit mstkit.Env) deleteParty {
	t.Helper()
	ctx := context.Background()
	saved, err := party.New(kit).Create(ctx, env.Kavya, party.Input{
		Code: deleteCode, Name: "Tata Projects", IsCustomer: true, GSTCategory: gstin.Unregistered,
	})
	if err != nil {
		t.Fatalf("party Create(%s): %v", deleteCode, err)
	}
	dp := deleteParty{party: saved.ID}
	svc := address.New(kit)
	for _, a := range []struct {
		id  *int64
		in  address.Input
		lbl string
	}{
		{&dp.mumbai, address.Input{PartyID: dp.party, Title: "Mumbai", Line1: "14 Ballard Estate", City: "Mumbai", StateCode: "27", Pincode: "400001", IsBilling: true}, "Mumbai"},
		{&dp.thane, address.Input{PartyID: dp.party, Title: "Thane", Line1: "3 Wagle Estate", City: "Thane", StateCode: "27", Pincode: "400601", IsShipping: true}, "Thane"},
	} {
		s, err := svc.Create(ctx, env.Kavya, a.in)
		if err != nil {
			t.Fatalf("address Create(%s): %v", a.lbl, err)
		}
		*a.id = s.ID
	}
	c, err := svc.CreateContact(ctx, env.Kavya, address.ContactInput{
		PartyID: dp.party, FirstName: "Sunil", LastName: "Mehta", Primary: true,
		Emails: []address.EmailInput{{Email: "accounts@shreesteel.test", Primary: true}},
		Phones: []address.PhoneInput{{Number: "9820012345", Mobile: true, Primary: true}},
	})
	if err != nil {
		t.Fatalf("CreateContact(Sunil Mehta): %v", err)
	}
	dp.contact = c.ID
	return dp
}

// deleteCount runs a count(*) query with one id argument.
func deleteCount(t *testing.T, d *db.DB, query string, id int64) int {
	t.Helper()
	var n int
	if err := d.Read(context.Background(), func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(context.Background(), query, id).Scan(&n)
	}); err != nil {
		t.Fatalf("%s [%d]: %v", query, id, err)
	}
	return n
}

// rows counts what is left of dp: party, its addresses, its contact and the
// contact's emails and phones.
func (dp deleteParty) rows(t *testing.T, d *db.DB) map[string]int {
	t.Helper()
	return map[string]int{
		"party":         deleteCount(t, d, "SELECT count(*) FROM party WHERE id = ?", dp.party),
		"address":       deleteCount(t, d, "SELECT count(*) FROM address WHERE party_id = ?", dp.party),
		"mumbai":        deleteCount(t, d, "SELECT count(*) FROM address WHERE id = ?", dp.mumbai),
		"thane":         deleteCount(t, d, "SELECT count(*) FROM address WHERE id = ?", dp.thane),
		"contact":       deleteCount(t, d, "SELECT count(*) FROM contact WHERE id = ?", dp.contact),
		"contact_email": deleteCount(t, d, "SELECT count(*) FROM contact_email WHERE contact_id = ?", dp.contact),
		"contact_phone": deleteCount(t, d, "SELECT count(*) FROM contact_phone WHERE contact_id = ?", dp.contact),
	}
}

func partyVersion(t *testing.T, d *db.DB, s *party.Service, id int64) int {
	t.Helper()
	var p party.Party
	if err := d.Read(context.Background(), func(tx *db.Tx) error {
		var err error
		p, err = s.Get(context.Background(), tx, id)
		return err
	}); err != nil {
		t.Fatalf("party Get(%d): %v", id, err)
	}
	return p.Version
}

func TestParty_DeleteCascadesOwnAddresses(t *testing.T) { // MST-046
	t.Run("unreferenced party goes with its addresses and contact", func(t *testing.T) {
		env := msttest.New(t)
		dp := newDeleteParty(t, env, env.Kit)
		want := map[string]int{"party": 1, "address": 2, "mumbai": 1, "thane": 1, "contact": 1, "contact_email": 1, "contact_phone": 1}
		if got := dp.rows(t, env.DB); !maps.Equal(got, want) {
			t.Fatalf("before delete: rows = %v; want %v", got, want)
		}

		ps := party.New(env.Kit)
		if err := ps.Delete(context.Background(), env.Kavya, dp.party, partyVersion(t, env.DB, ps, dp.party)); err != nil {
			t.Fatalf("Delete(%s) = %v; want nil", deleteCode, err)
		}
		want = map[string]int{"party": 0, "address": 0, "mumbai": 0, "thane": 0, "contact": 0, "contact_email": 0, "contact_phone": 0}
		if got := dp.rows(t, env.DB); !maps.Equal(got, want) {
			t.Errorf("after delete: rows = %v; want %v", got, want)
		}
	})

	for _, tc := range []struct {
		name  string
		table string
		row   func(deleteParty) int64
		field string
	}{
		{"address on a PO refuses the delete", "address", func(dp deleteParty) int64 { return dp.thane }, "shipping_address_id"},
		{"contact on a PO refuses the delete", "contact", func(dp deleteParty) int64 { return dp.contact }, "contact_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := msttest.New(t)
			docs := docfake.New()
			kit := env.Kit
			kit.Engine = docs
			dp := newDeleteParty(t, env, kit)
			docs.AddReference(tc.table, tc.row(dp), doc.Reference{
				DocType: "Purchase Order", Field: tc.field, ID: 1, Display: "PO/26-27/00001", State: doc.StateApproved,
			})

			ps := party.New(kit)
			err := ps.Delete(context.Background(), env.Kavya, dp.party, partyVersion(t, env.DB, ps, dp.party))
			var refused *mstkit.DeleteRefusedError
			if !errors.As(err, &refused) || err.Error() != deleteRefuse {
				t.Fatalf("Delete(%s) = %v (%T); want *mstkit.DeleteRefusedError %q", deleteCode, err, err, deleteRefuse)
			}
			if refused.Status() != 422 || refused.FieldErrors() != nil {
				t.Errorf("refusal Status() = %d, FieldErrors() = %v; want 422, nil", refused.Status(), refused.FieldErrors())
			}
			want := map[string]int{"party": 1, "address": 2, "mumbai": 1, "thane": 1, "contact": 1, "contact_email": 1, "contact_phone": 1}
			if got := dp.rows(t, env.DB); !maps.Equal(got, want) {
				t.Errorf("after refused delete: rows = %v; want %v (nothing deleted)", got, want)
			}
		})
	}
}

package address_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Messages copied verbatim from 06-masters.md MST-062, MST-063, MST-064 and
// MST-051. Unit tests keep 06's literal values (M7).
const (
	contactNameMsg    = "Contact name is required."
	contactEmailMsg   = "Enter a valid email address, e.g. accounts@shreesteel.test."
	contactMobileMsg  = "Enter a 10-digit Indian mobile number starting 6–9, optionally with +91."
	contactBelongsMsg = "That contact belongs to another party. Pick one of this party's contacts."
)

// contactInput is a valid contact of party 1 with only the given names set.
func contactInput(first, middle, last string) address.ContactInput {
	return address.ContactInput{PartyID: 1, FirstName: first, MiddleName: middle, LastName: last}
}

// contactWantOnly checks that err is a 422 *mstkit.ValidationError holding
// exactly one field error with message msg, and returns that error's field.
func contactWantOnly(t *testing.T, err error, msg string) string {
	t.Helper()
	var ve *mstkit.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v (%T); want *mstkit.ValidationError %q", err, err, msg)
	}
	if ve.Status() != 422 {
		t.Errorf("Status() = %d; want 422", ve.Status())
	}
	got := ve.FieldErrors()
	if len(got) != 1 || got[0].Message != msg {
		t.Fatalf("FieldErrors() = %+v; want one error %q", got, msg)
	}
	return got[0].Field
}

func TestContact_NameRequired(t *testing.T) { // MST-062
	for _, first := range []string{"", "   "} {
		_, err := address.ValidateContact(contactInput(first, "K", "Sharma"))
		if f := contactWantOnly(t, err, contactNameMsg); f != "first_name" {
			t.Errorf("ValidateContact(first name %q): field = %q; want %q", first, f, "first_name")
		}
	}

	if _, err := address.ValidateContact(contactInput("Ravi", "", "")); err != nil {
		t.Errorf("ValidateContact(first name only) = %v; want nil", err)
	}

	for _, tc := range []struct {
		c    address.Contact
		want string
	}{
		{address.Contact{FirstName: "Ravi", MiddleName: "K", LastName: "Sharma"}, "Ravi K Sharma"},
		{address.Contact{FirstName: "Ravi", LastName: "Sharma"}, "Ravi Sharma"},
		{address.Contact{FirstName: "Ravi"}, "Ravi"},
	} {
		if got := address.FullName(tc.c); got != tc.want {
			t.Errorf("FullName(%q, %q, %q) = %q; want %q", tc.c.FirstName, tc.c.MiddleName, tc.c.LastName, got, tc.want)
		}
	}
}

func TestContact_EmailFormat(t *testing.T) { // MST-063
	for _, bad := range []string{"accounts@shreesteel", "accounts shree@shreesteel.test"} {
		got, err := address.NormalizeEmail(bad)
		if err == nil || err.Error() != contactEmailMsg {
			t.Errorf("NormalizeEmail(%q) = %q, %v; want error %q", bad, got, err, contactEmailMsg)
		}

		in := contactInput("Sunil", "", "Mehta")
		in.Emails = []address.EmailInput{{Email: bad, Primary: true}}
		_, err = address.ValidateContact(in)
		contactWantOnly(t, err, contactEmailMsg)
	}

	for _, raw := range []string{"Accounts@ShreeSteel.test", "  ACCOUNTS@shreesteel.TEST "} {
		got, err := address.NormalizeEmail(raw)
		if err != nil || got != "accounts@shreesteel.test" {
			t.Errorf("NormalizeEmail(%q) = %q, %v; want %q, nil", raw, got, err, "accounts@shreesteel.test")
		}
	}

	in := contactInput("Sunil", "", "Mehta")
	in.Emails = []address.EmailInput{{Email: "Accounts@ShreeSteel.test", Primary: true}}
	out, err := address.ValidateContact(in)
	if err != nil {
		t.Fatalf("ValidateContact(email Accounts@ShreeSteel.test) = %v; want nil", err)
	}
	if len(out.Emails) != 1 || out.Emails[0].Email != "accounts@shreesteel.test" {
		t.Errorf("ValidateContact(email Accounts@ShreeSteel.test).Emails = %+v; want [accounts@shreesteel.test]", out.Emails)
	}
}

func TestContact_MobileFormat(t *testing.T) { // MST-064
	const want = "+919820012345"
	for _, raw := range []string{"98200 12345", "+91-9820012345", "09820012345"} {
		got, err := address.NormalizeMobile(raw)
		if err != nil || got != want {
			t.Errorf("NormalizeMobile(%q) = %q, %v; want %q, nil", raw, got, err, want)
		}

		in := contactInput("Sunil", "", "Mehta")
		in.Phones = []address.PhoneInput{{Number: raw, Mobile: true, Primary: true}}
		out, err := address.ValidateContact(in)
		if err != nil {
			t.Errorf("ValidateContact(mobile %q) = %v; want nil", raw, err)
			continue
		}
		if len(out.Phones) != 1 || out.Phones[0].Number != want {
			t.Errorf("ValidateContact(mobile %q).Phones = %+v; want [%s]", raw, out.Phones, want)
		}
	}

	for _, bad := range []string{"5820012345", "98200123"} {
		got, err := address.NormalizeMobile(bad)
		if err == nil || err.Error() != contactMobileMsg {
			t.Errorf("NormalizeMobile(%q) = %q, %v; want error %q", bad, got, err, contactMobileMsg)
		}

		in := contactInput("Sunil", "", "Mehta")
		in.Phones = []address.PhoneInput{{Number: bad, Mobile: true, Primary: true}}
		_, err = address.ValidateContact(in)
		contactWantOnly(t, err, contactMobileMsg)
	}
}

// contactParty creates a vendor party (Unregistered, so it needs no GSTIN,
// M5) and returns its id.
func contactParty(t *testing.T, env *msttest.Env, code, name string) int64 {
	t.Helper()
	saved, err := party.New(env.Kit).Create(context.Background(), env.Kavya, party.Input{
		Code: code, Name: name, IsVendor: true, GSTCategory: gstin.Unregistered,
	})
	if err != nil {
		t.Fatalf("party Create(%s): %v", code, err)
	}
	return saved.ID
}

// createContact saves a contact of partyID, primary or not, and returns its id.
func createContact(t *testing.T, env *msttest.Env, s *address.Service, partyID int64, first, last string, primary bool) int64 {
	t.Helper()
	saved, err := s.CreateContact(context.Background(), env.Kavya, address.ContactInput{
		PartyID: partyID, FirstName: first, LastName: last, Primary: primary,
	})
	if err != nil {
		t.Fatalf("CreateContact(%s %s, primary %v): %v", first, last, primary, err)
	}
	return saved.ID
}

// primaryContacts lists the ids of partyID's contacts marked primary.
func primaryContacts(t *testing.T, d *db.DB, partyID int64) []int64 {
	t.Helper()
	var ids []int64
	if err := d.Read(context.Background(), func(tx *db.Tx) error {
		rows, err := tx.SQL().QueryContext(context.Background(),
			"SELECT id FROM contact WHERE party_id = ? AND is_primary = 1 ORDER BY id", partyID)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("primary contacts of party %d: %v", partyID, err)
	}
	return ids
}

func TestContact_OnePrimaryPerParty(t *testing.T) { // MST-066
	env := msttest.New(t)
	ctx := context.Background()
	svc := address.New(env.Kit)
	shree := contactParty(t, env, "SHREE", "Shree Steel Traders")

	sunil := createContact(t, env, svc, shree, "Sunil", "Mehta", true)
	if got := primaryContacts(t, env.DB, shree); len(got) != 1 || got[0] != sunil {
		t.Fatalf("after Sunil Mehta saved as primary: primary contacts = %v; want [%d]", got, sunil)
	}

	kiran := createContact(t, env, svc, shree, "Kiran", "Joshi", true)
	if got := primaryContacts(t, env.DB, shree); len(got) != 1 || got[0] != kiran {
		t.Fatalf("after Kiran Joshi saved as primary: primary contacts = %v; want only Kiran Joshi [%d] (Sunil Mehta %d no longer primary)", got, kiran, sunil)
	}

	if err := env.DB.Write(ctx, func(tx *db.Tx) error {
		return svc.SetPrimaryContactTx(ctx, tx, env.Kavya, shree, sunil)
	}); err != nil {
		t.Fatalf("SetPrimaryContactTx(SHREE, Sunil Mehta) = %v; want nil", err)
	}
	if got := primaryContacts(t, env.DB, shree); len(got) != 1 || got[0] != sunil {
		t.Errorf("after SetPrimaryContactTx(Sunil Mehta): primary contacts = %v; want only Sunil Mehta [%d]", got, sunil)
	}
}

func TestParty_PrimaryMustBelong(t *testing.T) { // MST-051
	env := msttest.New(t)
	ctx := context.Background()
	svc := address.New(env.Kit)
	shree := contactParty(t, env, "SHREE", "Shree Steel Traders")
	bharat := contactParty(t, env, "BHARAT", "Bharat Tools")
	bharatContact := createContact(t, env, svc, bharat, "Kiran", "Joshi", true)

	err := env.DB.Write(ctx, func(tx *db.Tx) error {
		return svc.SetPrimaryContactTx(ctx, tx, env.Kavya, shree, bharatContact)
	})
	contactWantOnly(t, err, contactBelongsMsg)

	if got := primaryContacts(t, env.DB, shree); len(got) != 0 {
		t.Errorf("after refused SetPrimaryContactTx: SHREE primary contacts = %v; want none", got)
	}
	if got := primaryContacts(t, env.DB, bharat); len(got) != 1 || got[0] != bharatContact {
		t.Errorf("after refused SetPrimaryContactTx: BHARAT primary contacts = %v; want [%d]", got, bharatContact)
	}
}

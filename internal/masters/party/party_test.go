package party_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Messages and values copied verbatim from 06-masters.md MST-040 and MST-041.
// SHREE keeps its PAN and GSTINs under the fixture mapping (M7), so the unit
// and integration tests use the same literals.
const (
	shreeCode  = "SHREE"
	shreeName  = "Shree Steel Traders"
	shreePAN   = "AABCS1429B"
	shreeGSTIN = "27AABCS1429B1ZU"

	panMsg  = "PAN must be 10 characters: 5 letters, 4 digits, 1 letter (e.g. AAFCV2449G)."
	roleMsg = "Tick Vendor, Customer or both."
)

// shree is MST-040's party: SHREE, vendor only (M9), category Unregistered,
// no addresses, with the given PAN text.
func shree(pan string) party.Input {
	return party.Input{
		Code:        shreeCode,
		Name:        shreeName,
		IsVendor:    true,
		PAN:         pan,
		GSTCategory: gstin.Unregistered,
	}
}

// wantOnlyMessage checks that err is a 422 *mstkit.ValidationError holding
// exactly one header field error with message msg, and returns its field.
func wantOnlyMessage(t *testing.T, err error, msg string) string {
	t.Helper()
	var ve *mstkit.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v (%T); want *mstkit.ValidationError %q", err, err, msg)
	}
	if ve.Status() != 422 {
		t.Errorf("Status() = %d; want 422", ve.Status())
	}
	got := ve.FieldErrors()
	if len(got) != 1 || got[0].Message != msg || got[0].Line != 0 {
		t.Fatalf("FieldErrors() = %+v; want one header error %q", got, msg)
	}
	return got[0].Field
}

// wantFieldError is wantOnlyMessage with the field checked too.
func wantFieldError(t *testing.T, err error, field, msg string) {
	t.Helper()
	if got := wantOnlyMessage(t, err, msg); got != field {
		t.Errorf("field = %q; want %q", got, field)
	}
}

func TestParty_RoleFlags(t *testing.T) { // MST-041
	neither := shree(shreePAN)
	neither.IsVendor, neither.IsCustomer = false, false
	_, err := party.Validate(neither)
	if f := wantOnlyMessage(t, err, roleMsg); f == "" {
		t.Error("role error has no field; want it on a role flag")
	}

	both := shree(shreePAN)
	both.IsCustomer = true
	out, err := party.Validate(both)
	if err != nil {
		t.Fatalf("Validate(vendor and customer) = %v; want nil", err)
	}
	if !out.IsVendor || !out.IsCustomer {
		t.Errorf("Validate(vendor and customer) = {IsVendor %v, IsCustomer %v}; want both true", out.IsVendor, out.IsCustomer)
	}

	for _, in := range []party.Input{
		{Code: "V-ONLY", Name: "Vendor Only", IsVendor: true, GSTCategory: gstin.Unregistered},
		{Code: "C-ONLY", Name: "Only Customer Ltd", IsCustomer: true, GSTCategory: gstin.Unregistered},
	} {
		if _, err := party.Validate(in); err != nil {
			t.Errorf("Validate(%s, vendor %v, customer %v) = %v; want nil", in.Code, in.IsVendor, in.IsCustomer, err)
		}
	}
}

func TestParty_PANValidation(t *testing.T) { // MST-040
	t.Run("unit", func(t *testing.T) {
		out, err := party.Validate(shree("aabcs1429b"))
		if err != nil {
			t.Fatalf("Validate(PAN aabcs1429b) = %v; want nil", err)
		}
		if out.PAN != shreePAN {
			t.Errorf("Validate(PAN aabcs1429b).PAN = %q; want %q", out.PAN, shreePAN)
		}

		_, err = party.Validate(shree("AABCS1429"))
		wantFieldError(t, err, "pan", panMsg)
	})

	t.Run("integration", func(t *testing.T) {
		env := msttest.New(t)
		ctx := context.Background()
		svc := party.New(env.Kit)

		// A bad PAN is refused and nothing is stored.
		_, err := svc.Create(ctx, env.Kavya, shree("AABCS1429"))
		wantFieldError(t, err, "pan", panMsg)
		if p, ok := byCode(t, env.DB, svc, shreeCode); ok {
			t.Fatalf("after PAN AABCS1429: ByCode(%s) = %+v; want none", shreeCode, p)
		}

		// A lower-case PAN is stored upper-case.
		saved, err := svc.Create(ctx, env.Kavya, shree("aabcs1429b"))
		if err != nil {
			t.Fatalf("Create(SHREE, PAN aabcs1429b) = %v; want nil", err)
		}
		got := get(t, env.DB, svc, saved.ID)
		if got.Code != shreeCode || got.Name != shreeName || got.PAN != shreePAN {
			t.Errorf("Get = {Code %q, Name %q, PAN %q}; want {%q, %q, %q}", got.Code, got.Name, got.PAN, shreeCode, shreeName, shreePAN)
		}

		// The Mumbai address's GSTIN carries the same PAN: it stays.
		if err := env.DB.Write(ctx, func(tx *db.Tx) error {
			return svc.ApplyGSTINTx(ctx, tx, env.Kavya, saved.ID, shreeGSTIN)
		}); err != nil {
			t.Fatalf("ApplyGSTINTx(%s) = %v; want nil", shreeGSTIN, err)
		}
		if after := get(t, env.DB, svc, saved.ID); after.PAN != shreePAN {
			t.Errorf("after ApplyGSTINTx(%s): PAN = %q; want %q", shreeGSTIN, after.PAN, shreePAN)
		}
	})
}

// get reads party id, failing the test on error.
func get(t *testing.T, d *db.DB, s *party.Service, id int64) party.Party {
	t.Helper()
	var p party.Party
	if err := d.Read(context.Background(), func(tx *db.Tx) error {
		var err error
		p, err = s.Get(context.Background(), tx, id)
		return err
	}); err != nil {
		t.Fatalf("Get(%d): %v", id, err)
	}
	return p
}

// byCode looks a party up by code, failing the test on error.
func byCode(t *testing.T, d *db.DB, s *party.Service, code string) (party.Party, bool) {
	t.Helper()
	var (
		p  party.Party
		ok bool
	)
	if err := d.Read(context.Background(), func(tx *db.Tx) error {
		var err error
		p, ok, err = s.ByCode(context.Background(), tx, code)
		return err
	}); err != nil {
		t.Fatalf("ByCode(%s): %v", code, err)
	}
	return p, ok
}

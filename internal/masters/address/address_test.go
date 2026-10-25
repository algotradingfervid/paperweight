package address_test

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Messages copied verbatim from 06-masters.md MST-053, MST-054, MST-055 and
// MST-067. Unit tests keep 06's literal values (M7).
const (
	addrLine1Msg = "Address line 1 is required."
	addrCityMsg  = "City is required."
	addrStateMsg = "State is required for an Indian address."
	addrTypeMsg  = "Tick Billing, Shipping or both."
	addrPinMsg   = "Pincode must be 6 digits and can't start with 0."
	addrOwnerMsg = "An address must belong to one party or one branch."
)

// SHREE's fixture values (00-fixtures.md; unchanged by M7) and M9's pincodes.
const (
	addrShreeCode   = "SHREE"
	addrShreeName   = "Shree Steel Traders"
	addrShreePAN    = "AABCS1429B"
	addrMumbaiGSTIN = "27AABCS1429B1ZU"
	addrThaneGSTIN  = "27AABCS1429B2ZT"
	addrDelhiGSTIN  = "07AABCS1429B1ZW"
)

// addrValid is a valid Indian billing address of party 1 (pure Validate).
func addrValid() address.Input {
	return address.Input{
		PartyID: 1, Title: "Mumbai", Line1: "Plot 12, MIDC", City: "Mumbai",
		StateCode: "27", Country: "India", Pincode: "400001", IsBilling: true,
	}
}

// addrWantOnly checks that err is a 422 *mstkit.ValidationError holding
// exactly one field error with message msg.
func addrWantOnly(t *testing.T, err error, msg string) {
	t.Helper()
	var ve *mstkit.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v (%T); want *mstkit.ValidationError %q", err, err, msg)
	}
	if ve.Status() != 422 {
		t.Errorf("Status() = %d; want 422", ve.Status())
	}
	if got := ve.FieldErrors(); len(got) != 1 || got[0].Message != msg {
		t.Fatalf("FieldErrors() = %+v; want one error %q", got, msg)
	}
}

// addrWantHas checks that err is a 422 *mstkit.ValidationError with a field
// error whose message is msg (other errors may come with it).
func addrWantHas(t *testing.T, err error, msg string) {
	t.Helper()
	var ve *mstkit.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v (%T); want *mstkit.ValidationError with %q", err, err, msg)
	}
	if ve.Status() != 422 {
		t.Errorf("Status() = %d; want 422", ve.Status())
	}
	for _, fe := range ve.FieldErrors() {
		if fe.Message == msg {
			return
		}
	}
	t.Fatalf("FieldErrors() = %+v; want one with %q", ve.FieldErrors(), msg)
}

func TestAddress_RequiredFields(t *testing.T) { // MST-053
	for _, tc := range []struct {
		name string
		edit func(*address.Input)
		msg  string
	}{
		{"no line 1", func(in *address.Input) { in.Line1 = "" }, addrLine1Msg},
		{"blank line 1", func(in *address.Input) { in.Line1 = "   " }, addrLine1Msg},
		{"no city", func(in *address.Input) { in.City = "" }, addrCityMsg},
		{"no state", func(in *address.Input) { in.StateCode = "" }, addrStateMsg},
		{"no state, blank country (India)", func(in *address.Input) { in.StateCode, in.Country = "", "" }, addrStateMsg},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := addrValid()
			tc.edit(&in)
			_, err := address.Validate(in)
			addrWantOnly(t, err, tc.msg)
		})
	}

	in := addrValid()
	in.Country = ""
	out, err := address.Validate(in)
	if err != nil {
		t.Fatalf("Validate(blank country) = %v; want nil", err)
	}
	if out.Country != "India" {
		t.Errorf("Validate(blank country).Country = %q; want %q", out.Country, "India")
	}
}

func TestAddress_Type(t *testing.T) { // MST-054
	in := addrValid()
	in.IsBilling, in.IsShipping = false, false
	_, err := address.Validate(in)
	addrWantOnly(t, err, addrTypeMsg)

	for _, tc := range []struct{ billing, shipping bool }{{true, false}, {false, true}, {true, true}} {
		in := addrValid()
		in.IsBilling, in.IsShipping = tc.billing, tc.shipping
		if _, err := address.Validate(in); err != nil {
			t.Errorf("Validate(billing %v, shipping %v) = %v; want nil", tc.billing, tc.shipping, err)
		}
	}
}

func TestAddress_PincodeFormat(t *testing.T) { // MST-055
	for _, pin := range []string{"012345", "40001", "4000011", "40O001"} {
		in := addrValid()
		in.Pincode = pin
		_, err := address.Validate(in)
		addrWantOnly(t, err, addrPinMsg)
	}
	for _, pin := range []string{"", "400001"} {
		in := addrValid()
		in.Pincode = pin
		if _, err := address.Validate(in); err != nil {
			t.Errorf("Validate(pincode %q) = %v; want nil", pin, err)
		}
	}
}

func TestAddress_SingleOwner(t *testing.T) { // MST-067
	for _, tc := range []struct {
		name            string
		partyID, branch int64
	}{
		{"no owner", 0, 0},
		{"party and branch", 1, 1},
	} {
		in := addrValid()
		in.PartyID, in.BranchID = tc.partyID, tc.branch
		_, err := address.Validate(in)
		addrWantOnly(t, err, addrOwnerMsg)
	}
	for _, tc := range []struct{ partyID, branch int64 }{{1, 0}, {0, 1}} {
		in := addrValid()
		in.PartyID, in.BranchID = tc.partyID, tc.branch
		if _, err := address.Validate(in); err != nil {
			t.Errorf("Validate(party %d, branch %d) = %v; want nil", tc.partyID, tc.branch, err)
		}
	}
}

// --- integration helpers (canonical fixtures, M7) ---

// addrParty creates a vendor party and returns its id.
func addrParty(t *testing.T, env *msttest.Env, code, name, pan string, cat gstin.Category) int64 {
	t.Helper()
	saved, err := party.New(env.Kit).Create(context.Background(), env.Kavya, party.Input{
		Code: code, Name: name, IsVendor: true, PAN: pan, GSTCategory: cat,
	})
	if err != nil {
		t.Fatalf("party Create(%s): %v", code, err)
	}
	return saved.ID
}

// addrShree creates SHREE as in 00-fixtures.md (Registered Regular, PAN
// AABCS1429B) with no addresses yet (M5 allows it).
func addrShree(t *testing.T, env *msttest.Env) int64 {
	t.Helper()
	return addrParty(t, env, addrShreeCode, addrShreeName, addrShreePAN, gstin.Regular)
}

func addrMumbai(partyID int64) address.Input {
	return address.Input{PartyID: partyID, Title: "Mumbai", Line1: "Plot 12, MIDC", City: "Mumbai",
		StateCode: "27", Country: "India", Pincode: "400001", GSTIN: addrMumbaiGSTIN, IsBilling: true}
}

func addrThane(partyID int64) address.Input {
	return address.Input{PartyID: partyID, Title: "Thane", Line1: "Gala 4, Wagle Estate", City: "Thane",
		StateCode: "27", Country: "India", Pincode: "400601", GSTIN: addrThaneGSTIN, IsShipping: true}
}

func addrDelhi(partyID int64) address.Input {
	return address.Input{PartyID: partyID, Title: "Delhi", Line1: "14 Naraina Industrial Area", City: "Delhi",
		StateCode: "07", Country: "India", Pincode: "110001", GSTIN: addrDelhiGSTIN, IsBilling: true, IsShipping: true}
}

// addrCreate saves in and returns its id, failing the test on error.
func addrCreate(t *testing.T, env *msttest.Env, s *address.Service, in address.Input) int64 {
	t.Helper()
	saved, err := s.Create(context.Background(), env.Kavya, in)
	if err != nil {
		t.Fatalf("address Create(%s): %v", in.Title, err)
	}
	return saved.ID
}

func addrGet(t *testing.T, d *db.DB, s *address.Service, id int64) address.Address {
	t.Helper()
	var a address.Address
	if err := d.Read(context.Background(), func(tx *db.Tx) error {
		var err error
		a, err = s.Get(context.Background(), tx, id)
		return err
	}); err != nil {
		t.Fatalf("address Get(%d): %v", id, err)
	}
	return a
}

func addrForParty(t *testing.T, d *db.DB, s *address.Service, partyID int64) []address.Address {
	t.Helper()
	var as []address.Address
	if err := d.Read(context.Background(), func(tx *db.Tx) error {
		var err error
		as, err = s.ForParty(context.Background(), tx, partyID, false)
		return err
	}); err != nil {
		t.Fatalf("ForParty(%d): %v", partyID, err)
	}
	return as
}

func addrPartyGet(t *testing.T, d *db.DB, s *party.Service, id int64) party.Party {
	t.Helper()
	var p party.Party
	if err := d.Read(context.Background(), func(tx *db.Tx) error {
		var err error
		p, err = s.Get(context.Background(), tx, id)
		return err
	}); err != nil {
		t.Fatalf("party Get(%d): %v", id, err)
	}
	return p
}

// addrPrimaryCount counts partyID's addresses flagged primary for column
// (is_primary_billing or is_primary_shipping, plan Task 6 Step 3).
func addrPrimaryCount(t *testing.T, d *db.DB, partyID int64, column string) int {
	t.Helper()
	var n int
	if err := d.Read(context.Background(), func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(context.Background(),
			"SELECT count(*) FROM address WHERE party_id = ? AND "+column+" = 1", partyID).Scan(&n)
	}); err != nil {
		t.Fatalf("count %s of party %d: %v", column, partyID, err)
	}
	return n
}

// addrSetPrimary promotes id to primary billing (or shipping) at its current
// version.
func addrSetPrimary(env *msttest.Env, s *address.Service, id int64, billing bool, version int) error {
	ctx := context.Background()
	return env.DB.Write(ctx, func(tx *db.Tx) error {
		return s.SetPrimaryTx(ctx, tx, env.Kavya, id, billing, version)
	})
}

func TestAddress_ManyPerParty(t *testing.T) { // MST-058
	env := msttest.New(t)
	s := address.New(env.Kit)
	shree := addrShree(t, env)
	for _, in := range []address.Input{addrMumbai(shree), addrThane(shree), addrDelhi(shree)} {
		addrCreate(t, env, s, in)
	}

	got := addrForParty(t, env.DB, s, shree)
	if len(got) != 3 {
		t.Fatalf("ForParty(SHREE) = %d addresses %+v; want 3", len(got), got)
	}
	type row struct{ Title, StateCode, GSTIN string }
	var rows []row
	for _, a := range got {
		if a.PartyID != shree {
			t.Errorf("address %q: PartyID = %d; want %d", a.Title, a.PartyID, shree)
		}
		rows = append(rows, row{a.Title, a.StateCode, a.GSTIN})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Title < rows[j].Title })
	want := []row{
		{"Delhi", "07", addrDelhiGSTIN},
		{"Mumbai", "27", addrMumbaiGSTIN},
		{"Thane", "27", addrThaneGSTIN},
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Errorf("ForParty(SHREE) = %+v; want %+v", rows, want)
			break
		}
	}
}

func TestAddress_FirstIsPrimary(t *testing.T) { // MST-059
	env := msttest.New(t)
	ctx := context.Background()
	s := address.New(env.Kit)
	shree := addrShree(t, env)

	mumbai := addrCreate(t, env, s, addrMumbai(shree)) // PrimaryBilling not asked for
	if a := addrGet(t, env.DB, s, mumbai); !a.PrimaryBilling || a.PrimaryShipping {
		t.Errorf("first billing address: PrimaryBilling %v, PrimaryShipping %v; want true, false", a.PrimaryBilling, a.PrimaryShipping)
	}
	var g string
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		g, err = s.PrimaryGSTIN(ctx, tx, shree)
		return err
	}); err != nil {
		t.Fatalf("PrimaryGSTIN(SHREE): %v", err)
	}
	if g != addrMumbaiGSTIN {
		t.Errorf("PrimaryGSTIN(SHREE) = %q; want %q", g, addrMumbaiGSTIN)
	}

	// A second billing address is not primary billing; it is the first
	// shipping address, so it becomes primary shipping.
	delhi := addrCreate(t, env, s, addrDelhi(shree))
	if a := addrGet(t, env.DB, s, delhi); a.PrimaryBilling || !a.PrimaryShipping {
		t.Errorf("Delhi: PrimaryBilling %v, PrimaryShipping %v; want false, true", a.PrimaryBilling, a.PrimaryShipping)
	}
	if a := addrGet(t, env.DB, s, mumbai); !a.PrimaryBilling {
		t.Error("after Delhi: Mumbai PrimaryBilling = false; want true")
	}
}

func TestAddress_OnePrimaryPerType(t *testing.T) { // MST-060
	env := msttest.New(t)
	s := address.New(env.Kit)
	shree := addrShree(t, env)
	mumbai := addrCreate(t, env, s, addrMumbai(shree))
	thane := addrCreate(t, env, s, addrThane(shree))
	delhi := addrCreate(t, env, s, addrDelhi(shree))
	if !addrGet(t, env.DB, s, mumbai).PrimaryBilling || !addrGet(t, env.DB, s, thane).PrimaryShipping {
		t.Fatal("before: want Mumbai primary billing and Thane primary shipping")
	}

	if err := addrSetPrimary(env, s, delhi, true, addrGet(t, env.DB, s, delhi).Version); err != nil {
		t.Fatalf("SetPrimaryTx(Delhi, billing) = %v; want nil", err)
	}
	if addrGet(t, env.DB, s, mumbai).PrimaryBilling {
		t.Error("after promoting Delhi: Mumbai PrimaryBilling = true; want false")
	}
	if !addrGet(t, env.DB, s, delhi).PrimaryBilling {
		t.Error("after promoting Delhi: Delhi PrimaryBilling = false; want true")
	}
	if n := addrPrimaryCount(t, env.DB, shree, "is_primary_billing"); n != 1 {
		t.Errorf("primary billing addresses = %d; want 1", n)
	}

	if err := addrSetPrimary(env, s, delhi, false, addrGet(t, env.DB, s, delhi).Version); err != nil {
		t.Fatalf("SetPrimaryTx(Delhi, shipping) = %v; want nil", err)
	}
	if addrGet(t, env.DB, s, thane).PrimaryShipping {
		t.Error("after promoting Delhi: Thane PrimaryShipping = true; want false")
	}
	d := addrGet(t, env.DB, s, delhi)
	if !d.PrimaryShipping || !d.PrimaryBilling {
		t.Errorf("Delhi: PrimaryBilling %v, PrimaryShipping %v; want both true", d.PrimaryBilling, d.PrimaryShipping)
	}
	if n := addrPrimaryCount(t, env.DB, shree, "is_primary_shipping"); n != 1 {
		t.Errorf("primary shipping addresses = %d; want 1", n)
	}
	if n := addrPrimaryCount(t, env.DB, shree, "is_primary_billing"); n != 1 {
		t.Errorf("primary billing addresses after shipping promotion = %d; want 1", n)
	}
}

func TestAddress_PrimaryUniqueUnderRace(t *testing.T) { // Review Focus 3
	env := msttest.New(t)
	s := address.New(env.Kit)
	shree := addrShree(t, env)
	mumbai := addrCreate(t, env, s, addrMumbai(shree))
	delhi := addrCreate(t, env, s, addrDelhi(shree))
	pune := addrMumbai(shree)
	pune.Title, pune.Line1, pune.City, pune.Pincode = "Pune", "Survey 21, Bhosari MIDC", "Pune", "411001"
	puneID := addrCreate(t, env, s, pune)

	ids := []int64{delhi, puneID}
	versions := []int{addrGet(t, env.DB, s, delhi).Version, addrGet(t, env.DB, s, puneID).Version}
	errs := make([]error, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = addrSetPrimary(env, s, ids[i], true, versions[i])
		}(i)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("SetPrimaryTx(address %d, billing) = %v; want nil", ids[i], err)
		}
	}

	if n := addrPrimaryCount(t, env.DB, shree, "is_primary_billing"); n != 1 {
		t.Fatalf("primary billing addresses after the race = %d; want exactly 1", n)
	}
	if addrGet(t, env.DB, s, mumbai).PrimaryBilling {
		t.Error("after the race: Mumbai is still primary billing; want Delhi or Pune")
	}

	// The database itself refuses a second primary (partial unique index).
	err := env.DB.Write(context.Background(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(context.Background(),
			"UPDATE address SET is_primary_billing = 1 WHERE party_id = ? AND is_primary_billing = 0", shree)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "UNIQUE constraint failed") {
		t.Errorf("second primary billing by SQL = %v; want a UNIQUE constraint failure", err)
	}
}

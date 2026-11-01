package site_test

import (
	"context"
	"errors"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// M0-58 (CD-337 (1)): a site's optional delivery address and contact.
const (
	msgDeliveryState  = "Delivery address state must match the branch state."
	msgDeliveryLine1  = "Address line 1 is required."
	msgDeliveryCity   = "City is required."
	msgDeliveryNoSt   = "State is required for the delivery address."
	msgContactName    = "Contact name is required."
	msgSiteAddrState  = "A site of this branch has its delivery address in another state. Change that address first."
	msgMobileFormat   = "Enter a 10-digit Indian mobile number starting 6–9, optionally with +91."
	msgPinFormat      = "Pincode must be 6 digits and can't start with 0."
	msgPinNotInDelhi  = "Pincode 411057 is not in Delhi. Check the pincode or the state."
	puneDeliveryText  = "Gate 2, Hinjewadi\nPhase 1\nPune - 411057\nMaharashtra (27)"
	delhiDeliveryText = "Plot 4\nNew Delhi - 110001\nDelhi (07)"
)

// puneDelivery is a delivery address in Maharashtra, as typed.
func puneDelivery() *site.Delivery {
	return &site.Delivery{
		Line1: " Gate 2, Hinjewadi ", Line2: "Phase 1", City: "Pune", StateCode: "27", Pincode: "411 057",
		ContactName: " Raj Patil ", ContactMobile: "98765 43210",
	}
}

// puneStored is puneDelivery as stored.
var puneStored = site.Delivery{
	Line1: "Gate 2, Hinjewadi", Line2: "Phase 1", City: "Pune", StateCode: "27", Pincode: "411057",
	ContactName: "Raj Patil", ContactMobile: "+919876543210",
}

func delhiDelivery() *site.Delivery {
	return &site.Delivery{Line1: "Plot 4", City: "New Delhi", StateCode: "07", Pincode: "110001"}
}

// delhiBranch adds branch DELHI (state 07, no GSTIN).
func (w *world) delhiBranch(t *testing.T) int64 {
	t.Helper()
	b, err := company.New(w.env.Kit).Create(context.Background(), w.env.Kavya, company.BranchInput{Code: "DELHI", Name: "Delhi Office", StateCode: "07"})
	if err != nil {
		t.Fatalf("create branch DELHI: %v", err)
	}
	return b.ID
}

// delivery is DeliveryAddress for id.
func (w *world) delivery(t *testing.T, id int64) (site.Delivery, bool) {
	t.Helper()
	var d site.Delivery
	var ok bool
	if err := w.env.DB.Read(context.Background(), func(tx *db.Tx) error {
		var err error
		d, ok, err = site.DeliveryAddress(context.Background(), tx, id)
		return err
	}); err != nil {
		t.Fatalf("DeliveryAddress(%d): %v", id, err)
	}
	return d, ok
}

// count is a one-number query.
func (w *world) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := w.env.DB.Read(context.Background(), func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(context.Background(), q, args...).Scan(&n)
	}); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func (w *world) siteInput(code string, branch int64, d *site.Delivery) site.Input {
	return site.Input{Code: code, Name: code + " site", BranchID: branch, Type: site.SiteKind, Delivery: d}
}

func wantFieldError(t *testing.T, err error, field, msg string) {
	t.Helper()
	var ve *mstkit.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v (%T); want a field error %q on %s", err, err, msg, field)
	}
	for _, f := range ve.FieldErrors() {
		if f.Field == field && f.Message == msg {
			return
		}
	}
	t.Errorf("FieldErrors() = %+v; want %q on %s", ve.FieldErrors(), msg, field)
}

func TestSiteDelivery_CreateSavesAndReads(t *testing.T) {
	w := newWorld(t)
	saved := w.create(t, w.siteInput("HINJ", w.branchID, puneDelivery()))
	st := w.get(t, saved.ID)
	if st.Delivery != puneStored {
		t.Errorf("Get().Delivery = %+v; want %+v", st.Delivery, puneStored)
	}
	d, ok := w.delivery(t, saved.ID)
	if !ok || d != puneStored {
		t.Errorf("DeliveryAddress = %+v, %v; want %+v, true", d, ok, puneStored)
	}
	if got := d.Text(); got != puneDeliveryText {
		t.Errorf("Text() = %q; want %q", got, puneDeliveryText)
	}
	// One address and one contact owned by the site, the mobile a primary phone.
	if n := w.count(t, `SELECT count(*) FROM address WHERE site_id = ? AND party_id IS NULL AND branch_id IS NULL AND name = 'Pune' AND is_shipping = 1 AND country = 'India'`, saved.ID); n != 1 {
		t.Errorf("site addresses = %d; want 1", n)
	}
	if n := w.count(t, `SELECT count(*) FROM contact c JOIN contact_phone f ON f.contact_id = c.id
		WHERE c.site_id = ? AND c.name = 'Raj Patil' AND c.first_name = 'Raj Patil' AND f.number = '+919876543210' AND f.kind = 'mobile' AND f.is_primary = 1`, saved.ID); n != 1 {
		t.Errorf("site contacts with the mobile = %d; want 1", n)
	}
}

func TestSiteDelivery_NoneAndContactOnly(t *testing.T) {
	w := newWorld(t)
	none := w.create(t, w.siteInput("NONE", w.branchID, nil))
	if d, ok := w.delivery(t, none.ID); ok || d != (site.Delivery{}) {
		t.Errorf("DeliveryAddress(no delivery) = %+v, %v; want zero, false", d, ok)
	}
	blank := w.create(t, w.siteInput("BLANK", w.branchID, &site.Delivery{City: " "}))
	if d, ok := w.delivery(t, blank.ID); ok || d != (site.Delivery{}) {
		t.Errorf("DeliveryAddress(blank delivery) = %+v, %v; want zero, false", d, ok)
	}
	who := w.create(t, w.siteInput("WHO", w.branchID, &site.Delivery{ContactName: "Raj"}))
	d, ok := w.delivery(t, who.ID)
	if !ok || d != (site.Delivery{ContactName: "Raj"}) || d.Text() != "" {
		t.Errorf("DeliveryAddress(contact only) = %+v, %v, text %q; want the contact, true, no text", d, ok, d.Text())
	}
	if n := w.count(t, `SELECT count(*) FROM address WHERE site_id IS NOT NULL`); n != 0 {
		t.Errorf("site addresses = %d; want 0", n)
	}
	if n := w.count(t, `SELECT count(*) FROM contact_phone`); n != 0 {
		t.Errorf("phones = %d; want 0 (no mobile given)", n)
	}
	addrOnly := w.create(t, w.siteInput("ADDR", w.branchID, &site.Delivery{Line1: "Gate 1", City: "Pune", StateCode: "27"}))
	if n := w.count(t, `SELECT count(*) FROM contact WHERE site_id = ?`, addrOnly.ID); n != 0 {
		t.Errorf("contacts of a site with an address only = %d; want 0", n)
	}
}

func TestSiteDelivery_Validate(t *testing.T) {
	tooLong := strings.Repeat("x", mstkit.MaxNameLen+1)
	overMax := " can be at most " + strconv.Itoa(mstkit.MaxNameLen) + " characters."
	cases := []struct {
		name         string
		d            site.Delivery
		field, msg   string
		wantNoErrors bool
	}{
		{name: "line 1 missing", d: site.Delivery{City: "Pune", StateCode: "27"}, field: "delivery_line1", msg: msgDeliveryLine1},
		{name: "city missing", d: site.Delivery{Line1: "Gate 2", StateCode: "27"}, field: "delivery_city", msg: msgDeliveryCity},
		{name: "state missing", d: site.Delivery{Line1: "Gate 2", City: "Pune"}, field: "delivery_state_code", msg: msgDeliveryNoSt},
		{name: "state unknown", d: site.Delivery{Line1: "Gate 2", City: "Pune", StateCode: "99"}, field: "delivery_state_code", msg: msgDeliveryNoSt},
		{name: "only line 1", d: site.Delivery{Line1: "Gate 2"}, field: "delivery_city", msg: msgDeliveryCity},
		{name: "only line 2", d: site.Delivery{Line2: "Phase 1"}, field: "delivery_line1", msg: msgDeliveryLine1},
		{name: "only city", d: site.Delivery{City: "Pune"}, field: "delivery_line1", msg: msgDeliveryLine1},
		{name: "only state", d: site.Delivery{StateCode: "27"}, field: "delivery_city", msg: msgDeliveryCity},
		{name: "only pincode", d: site.Delivery{Pincode: "411057"}, field: "delivery_city", msg: msgDeliveryCity},
		{name: "pincode format", d: site.Delivery{Line1: "Gate 2", City: "Pune", StateCode: "27", Pincode: "01234"}, field: "delivery_pincode", msg: msgPinFormat},
		{name: "pincode not in state", d: site.Delivery{Line1: "Gate 2", City: "Delhi", StateCode: "07", Pincode: "411057"}, field: "delivery_pincode", msg: msgPinNotInDelhi},
		{name: "mobile format", d: site.Delivery{ContactName: "Raj", ContactMobile: "12345"}, field: "contact_mobile", msg: msgMobileFormat},
		{name: "mobile without name", d: site.Delivery{ContactMobile: "9876543210"}, field: "contact_name", msg: msgContactName},
		{name: "line 1 too long", d: site.Delivery{Line1: tooLong, City: "Pune", StateCode: "27"}, field: "delivery_line1", msg: "Address line 1" + overMax},
		{name: "line 2 too long", d: site.Delivery{Line1: "Gate 2", Line2: tooLong, City: "Pune", StateCode: "27"}, field: "delivery_line2", msg: "Address line 2" + overMax},
		{name: "city too long", d: site.Delivery{Line1: "Gate 2", City: tooLong, StateCode: "27"}, field: "delivery_city", msg: "City" + overMax},
		{name: "contact name too long", d: site.Delivery{ContactName: tooLong}, field: "contact_name", msg: "Contact name" + overMax},
		{name: "no pincode is fine", d: site.Delivery{Line1: "Gate 2", City: "Pune", StateCode: "27"}, wantNoErrors: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := site.Input{Code: "S1", Name: "S1", BranchID: 1, Type: site.SiteKind, Delivery: &c.d}
			_, err := site.Validate(in)
			if c.wantNoErrors {
				if err != nil {
					t.Fatalf("Validate = %v; want nil", err)
				}
				return
			}
			wantFieldError(t, err, c.field, c.msg)
		})
	}
	// Through the service: a refused delivery saves no site.
	w := newWorld(t)
	_, err := w.svc.Create(context.Background(), w.env.Kavya, w.siteInput("BAD", w.branchID, &site.Delivery{City: "Pune", StateCode: "27"}))
	wantFieldError(t, err, "delivery_line1", msgDeliveryLine1)
	if n := w.count(t, `SELECT count(*) FROM site`); n != 0 {
		t.Errorf("sites after a refused create = %d; want 0", n)
	}
}

// The address's state is its branch's on every write path: create, update
// with a delivery, and a branch change that keeps the stored one (CD-234).
func TestSiteDelivery_StateFollowsBranch(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	delhi := w.delhiBranch(t)

	_, err := w.svc.Create(ctx, w.env.Kavya, w.siteInput("WRONG", w.branchID, delhiDelivery()))
	wantFieldError(t, err, "delivery_state_code", msgDeliveryState)

	// An unknown branch is the branch's refusal alone.
	_, err = w.svc.Create(ctx, w.env.Kavya, w.siteInput("NOBR", 9999, puneDelivery()))
	var ve *mstkit.ValidationError
	if !errors.As(err, &ve) || len(ve.FieldErrors()) != 1 || ve.FieldErrors()[0].Field != "branch_id" {
		t.Errorf("Create at an unknown branch = %v; want only the branch_id refusal", err)
	}

	saved := w.create(t, w.siteInput("HINJ", w.branchID, puneDelivery()))
	st := w.get(t, saved.ID)

	// Update posting an address in another state.
	in := w.siteInput("HINJ", w.branchID, delhiDelivery())
	_, err = w.svc.Update(ctx, w.env.Kavya, st.ID, st.Version, in)
	wantFieldError(t, err, "delivery_state_code", msgDeliveryState)

	// Branch change keeping the stored Maharashtra address (no delivery posted).
	in = w.siteInput("HINJ", delhi, nil)
	_, err = w.svc.Update(ctx, w.env.Kavya, st.ID, st.Version, in)
	wantFieldError(t, err, "delivery_state_code", msgDeliveryState)
	if got := w.get(t, st.ID); got != st {
		t.Errorf("site after refused saves = %+v; want unchanged %+v", got, st)
	}

	// Branch change with an address in the new branch's state.
	in = w.siteInput("HINJ", delhi, delhiDelivery())
	if _, err := w.svc.Update(ctx, w.env.Kavya, st.ID, st.Version, in); err != nil {
		t.Fatalf("Update to DELHI with a Delhi address = %v; want saved", err)
	}
	got := w.get(t, st.ID)
	if got.BranchID != delhi || got.Delivery.Text() != delhiDeliveryText || got.Delivery.ContactName != "" {
		t.Errorf("after the move = %+v (text %q); want DELHI, the Delhi address, no contact", got, got.Delivery.Text())
	}
	if n := w.count(t, `SELECT count(*) FROM contact WHERE site_id = ?`, st.ID); n != 0 {
		t.Errorf("contacts after clearing the contact = %d; want 0", n)
	}

	// A branch change with no address is free.
	plain := w.create(t, w.siteInput("PLAIN", w.branchID, nil))
	if _, err := w.svc.Update(ctx, w.env.Kavya, plain.ID, plain.Version, w.siteInput("PLAIN", delhi, nil)); err != nil {
		t.Errorf("branch change of a site with no address = %v; want saved", err)
	}
}

// Update without a delivery keeps the stored one (the site's own form); an
// empty one removes it, with its contact and phone; a change bumps the version
// and is audited on the site.
func TestSiteDelivery_KeepChangeRemove(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	saved := w.create(t, w.siteInput("HINJ", w.branchID, puneDelivery()))
	st := w.get(t, saved.ID)

	addressID := func() int {
		return w.count(t, `SELECT id FROM address WHERE site_id = ?`, st.ID)
	}
	firstAddress := addressID()
	in := w.siteInput("HINJ", w.branchID, nil)
	in.Name = "Hinjewadi"
	s2, err := w.svc.Update(ctx, w.env.Kavya, st.ID, st.Version, in)
	if err != nil {
		t.Fatal(err)
	}
	if got := addressID(); got != firstAddress {
		t.Errorf("address id after a save that kept it = %d; want %d (not rewritten)", got, firstAddress)
	}
	if got := w.get(t, st.ID); got.Delivery != puneStored || got.Name != "Hinjewadi" {
		t.Errorf("after a save without delivery = %+v; want the name changed and the delivery kept", got)
	}

	// The same delivery again changes nothing.
	in.Delivery = puneDelivery()
	s3, err := w.svc.Update(ctx, w.env.Kavya, st.ID, s2.Version, in)
	if err != nil || s3.Version != s2.Version {
		t.Errorf("unchanged save = %+v, %v; want version %d kept", s3, err, s2.Version)
	}

	// A new mobile: one contact, one phone, version bumped, audited.
	audits := w.count(t, `SELECT count(*) FROM audit_log WHERE doc_type = 'Site' AND doc_id = ?`, st.ID)
	d := puneDelivery()
	d.ContactMobile = "9123456789"
	in.Delivery = d
	s4, err := w.svc.Update(ctx, w.env.Kavya, st.ID, s3.Version, in)
	if err != nil || s4.Version != s3.Version+1 {
		t.Fatalf("mobile change = %+v, %v; want version %d", s4, err, s3.Version+1)
	}
	if got := w.get(t, st.ID).Delivery.ContactMobile; got != "+919123456789" {
		t.Errorf("mobile = %q; want +919123456789", got)
	}
	if n := w.count(t, `SELECT count(*) FROM contact_phone`); n != 1 {
		t.Errorf("phones = %d; want 1", n)
	}
	if n := w.count(t, `SELECT count(*) FROM audit_log WHERE doc_type = 'Site' AND doc_id = ? AND changes LIKE '%+919123456789%'`, st.ID); n != 1 ||
		w.count(t, `SELECT count(*) FROM audit_log WHERE doc_type = 'Site' AND doc_id = ?`, st.ID) != audits+1 {
		t.Errorf("audit entries naming the new mobile = %d; want 1, and one more entry", n)
	}

	// Empty removes both.
	in.Delivery = &site.Delivery{}
	if _, err := w.svc.Update(ctx, w.env.Kavya, st.ID, s4.Version, in); err != nil {
		t.Fatal(err)
	}
	if d, ok := w.delivery(t, st.ID); ok {
		t.Errorf("DeliveryAddress after removal = %+v, true; want none", d)
	}
	for _, q := range []string{`SELECT count(*) FROM address WHERE site_id IS NOT NULL`, `SELECT count(*) FROM contact WHERE site_id IS NOT NULL`, `SELECT count(*) FROM contact_phone`} {
		if n := w.count(t, q); n != 0 {
			t.Errorf("%s = %d; want 0", q, n)
		}
	}
}

// A save at a stale version changes neither the site nor its delivery (CD-94).
func TestSiteDelivery_StaleVersion(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	saved := w.create(t, w.siteInput("HINJ", w.branchID, puneDelivery()))
	st := w.get(t, saved.ID)
	in := w.siteInput("HINJ", w.branchID, nil)
	in.Name = "Moved on"
	if _, err := w.svc.Update(ctx, w.env.Kavya, st.ID, st.Version, in); err != nil {
		t.Fatal(err)
	}
	moved := w.get(t, st.ID)
	in = w.siteInput("HINJ", w.branchID, &site.Delivery{Line1: "Other gate", City: "Pune", StateCode: "27"})
	_, err := w.svc.Update(ctx, w.env.Kavya, st.ID, st.Version, in)
	var he doctype.HTTPError
	if !errors.As(err, &he) || he.Status() != 409 {
		t.Fatalf("stale delivery save = %v; want 409", err)
	}
	if got := w.get(t, st.ID); got != moved {
		t.Errorf("after stale save = %+v; want %+v", got, moved)
	}
}

// A site's address and contact go with it.
func TestSiteDelivery_DeletedWithSite(t *testing.T) {
	w := newWorld(t)
	saved := w.create(t, w.siteInput("HINJ", w.branchID, puneDelivery()))
	if err := w.svc.Delete(context.Background(), w.env.Kavya, saved.ID, saved.Version); err != nil {
		t.Fatalf("Delete = %v", err)
	}
	for _, q := range []string{`SELECT count(*) FROM address`, `SELECT count(*) FROM contact`, `SELECT count(*) FROM contact_phone`} {
		if n := w.count(t, q); n != 0 {
			t.Errorf("%s = %d after deleting the site; want 0", q, n)
		}
	}
}

// The branch side of the invariant: a branch's state can't move away from a
// site address under it (CD-180).
func TestSiteDelivery_BranchStateChange(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	co := company.New(w.env.Kit)
	nag, err := co.Create(ctx, w.env.Kavya, company.BranchInput{Code: "NAG", Name: "Nagpur", StateCode: "27"})
	if err != nil {
		t.Fatal(err)
	}
	saved := w.create(t, w.siteInput("NAG-S", nag.ID, &site.Delivery{Line1: "Wardha Road", City: "Nagpur", StateCode: "27"}))
	// A save that keeps the state isn't blocked by the site's address.
	renamed, err := co.Update(ctx, w.env.Kavya, nag.ID, nag.Version, company.BranchInput{Code: "NAG", Name: "Nagpur Works", StateCode: "27"})
	if err != nil {
		t.Fatalf("rename keeping the state = %v; want saved", err)
	}
	nag.Version = renamed.Version
	_, err = co.Update(ctx, w.env.Kavya, nag.ID, nag.Version, company.BranchInput{Code: "NAG", Name: "Nagpur", StateCode: "07"})
	wantFieldError(t, err, "state_code", msgSiteAddrState)

	// Without the address the state may change, even beside another branch's
	// site address in the state NAG leaves (HINJ at PUNE, 27): only NAG's own
	// sites count.
	w.create(t, w.siteInput("HINJ", w.branchID, puneDelivery()))
	if _, err := w.svc.Update(ctx, w.env.Kavya, saved.ID, saved.Version, w.siteInput("NAG-S", nag.ID, &site.Delivery{})); err != nil {
		t.Fatal(err)
	}
	if _, err := co.Update(ctx, w.env.Kavya, nag.ID, nag.Version, company.BranchInput{Code: "NAG", Name: "Nagpur", StateCode: "07"}); err != nil {
		t.Errorf("state change of NAG beside PUNE's site address in 27 = %v; want saved", err)
	}
}

// The Address master can't change a site's address: its owner is neither a
// party nor a branch, so Update refuses it and the state stays.
func TestSiteDelivery_AddressMasterRefuses(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	saved := w.create(t, w.siteInput("HINJ", w.branchID, puneDelivery()))
	var id int64
	var version int
	if err := w.env.DB.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, `SELECT id, version FROM address WHERE site_id = ?`, saved.ID).Scan(&id, &version)
	}); err != nil {
		t.Fatal(err)
	}
	_, err := address.New(w.env.Kit).Update(ctx, w.env.Kavya, id, version, address.Input{
		Line1: "Plot 4", City: "New Delhi", StateCode: "07", IsShipping: true,
	})
	if err == nil {
		t.Fatal("Address Update of a site's address = nil; want refused")
	}
	if got := w.get(t, saved.ID).Delivery; got != puneStored {
		t.Errorf("delivery after the refused Address Update = %+v; want %+v", got, puneStored)
	}
}

// A random run of site saves, branch moves, removals and branch state changes
// never leaves a site address outside its branch's state, nor two addresses
// or contacts on one site (CD-180).
func TestSiteDelivery_RandomSequences(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	co := company.New(w.env.Kit)
	branches := []int64{w.branchID, w.delhiBranch(t)}
	nag, err := co.Create(ctx, w.env.Kavya, company.BranchInput{Code: "NAG", Name: "Nagpur", StateCode: "27"})
	if err != nil {
		t.Fatal(err)
	}
	branches = append(branches, nag.ID)
	deliveries := []*site.Delivery{nil, {}, puneDelivery(), delhiDelivery(), {ContactName: "Raj", ContactMobile: "9876543210"}}
	var sites []int64
	for i := range 3 {
		sites = append(sites, w.create(t, w.siteInput("R"+strconv.Itoa(i), w.branchID, nil)).ID)
	}
	rng := rand.New(rand.NewPCG(58, 58))
	for step := range 120 {
		switch rng.IntN(4) {
		case 0, 1, 2:
			id := sites[rng.IntN(len(sites))]
			st := w.get(t, id)
			in := w.siteInput(st.Code, branches[rng.IntN(len(branches))], deliveries[rng.IntN(len(deliveries))])
			_, _ = w.svc.Update(ctx, w.env.Kavya, id, st.Version, in)
		default:
			var b company.Branch
			if err := w.env.DB.Read(ctx, func(tx *db.Tx) error { var err error; b, err = co.Get(ctx, tx, nag.ID); return err }); err != nil {
				t.Fatal(err)
			}
			state := []string{"27", "07"}[rng.IntN(2)]
			_, _ = co.Update(ctx, w.env.Kavya, nag.ID, b.Version, company.BranchInput{Code: "NAG", Name: "Nagpur", StateCode: state})
		}
		if n := w.count(t, `SELECT count(*) FROM address a JOIN site s ON s.id = a.site_id JOIN branch b ON b.id = s.branch_id WHERE a.state_code <> b.state_code`); n != 0 {
			t.Fatalf("step %d: %d site addresses outside their branch's state", step, n)
		}
		if n := w.count(t, `SELECT count(*) FROM (SELECT site_id FROM address WHERE site_id IS NOT NULL GROUP BY site_id HAVING count(*) > 1)`); n != 0 {
			t.Fatalf("step %d: %d sites with two addresses", step, n)
		}
	}
	if n := w.count(t, `SELECT count(*) FROM address WHERE site_id IS NOT NULL`); n == 0 {
		t.Error("the run never left a site address: it tested nothing")
	}
}

// A change of any one delivery field alone is saved.
func TestSiteDelivery_EachFieldSaves(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	saved := w.create(t, w.siteInput("HINJ", w.branchID, puneDelivery()))
	version := saved.Version
	changes := []struct {
		name string
		set  func(d *site.Delivery)
		want func(d site.Delivery) bool
	}{
		{"line 1", func(d *site.Delivery) { d.Line1 = "Gate 3" }, func(d site.Delivery) bool { return d.Line1 == "Gate 3" }},
		{"line 2", func(d *site.Delivery) { d.Line2 = "Phase 2" }, func(d site.Delivery) bool { return d.Line2 == "Phase 2" }},
		{"city", func(d *site.Delivery) { d.City = "Pimpri" }, func(d site.Delivery) bool { return d.City == "Pimpri" }},
		{"pincode", func(d *site.Delivery) { d.Pincode = "411001" }, func(d site.Delivery) bool { return d.Pincode == "411001" }},
		{"contact name", func(d *site.Delivery) { d.ContactName = "Asha" }, func(d site.Delivery) bool { return d.ContactName == "Asha" }},
		{"contact mobile", func(d *site.Delivery) { d.ContactMobile = "9000000001" }, func(d site.Delivery) bool { return d.ContactMobile == "+919000000001" }},
	}
	for _, c := range changes {
		d := w.get(t, saved.ID).Delivery
		c.set(&d)
		s, err := w.svc.Update(ctx, w.env.Kavya, saved.ID, version, w.siteInput("HINJ", w.branchID, &d))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if s.Version != version+1 || !c.want(w.get(t, saved.ID).Delivery) {
			t.Errorf("%s: version %d, delivery %+v; want version %d and the change stored", c.name, s.Version, w.get(t, saved.ID).Delivery, version+1)
		}
		version = s.Version
	}
	// The state alone, with a branch in the new state.
	delhi := w.delhiBranch(t)
	d := w.get(t, saved.ID).Delivery
	d.StateCode, d.Pincode = "07", ""
	if _, err := w.svc.Update(ctx, w.env.Kavya, saved.ID, version, w.siteInput("HINJ", delhi, &d)); err != nil || w.get(t, saved.ID).Delivery.StateCode != "07" {
		t.Errorf("state change = %v; want the state stored", err)
	}
}

// CD-356 (3): the Address and Contact masters' Delete, SetPrimary and
// SetDisabled refuse a site's own address and contact; only the site's form
// changes them.
func TestSiteDelivery_AddressContactMastersRefuseSiteRows(t *testing.T) {
	const (
		msgSiteAddress = "This address is a site's delivery address. Change it on the site."
		msgSiteContact = "This contact is a site's delivery contact. Change it on the site."
	)
	w := newWorld(t)
	ctx := context.Background()
	saved := w.create(t, w.siteInput("HINJ", w.branchID, puneDelivery()))
	var addrID, contactID int64
	var addrVersion, contactVersion int
	if err := w.env.DB.Read(ctx, func(tx *db.Tx) error {
		if err := tx.SQL().QueryRowContext(ctx, `SELECT id, version FROM address WHERE site_id = ?`, saved.ID).Scan(&addrID, &addrVersion); err != nil {
			return err
		}
		return tx.SQL().QueryRowContext(ctx, `SELECT id, version FROM contact WHERE site_id = ?`, saved.ID).Scan(&contactID, &contactVersion)
	}); err != nil {
		t.Fatal(err)
	}
	a := address.New(w.env.Kit)
	kavya := w.env.Kavya
	inTx := func(fn func(tx *db.Tx) error) error { return w.env.DB.Write(ctx, fn) }
	cases := []struct {
		name, msg string
		call      func() error
	}{
		{"address delete", msgSiteAddress, func() error { return a.Delete(ctx, kavya, addrID, addrVersion) }},
		{"address disable", msgSiteAddress, func() error { return a.SetDisabled(ctx, kavya, addrID, addrVersion, true) }},
		{"address set primary shipping", msgSiteAddress, func() error {
			return inTx(func(tx *db.Tx) error { return a.SetPrimaryTx(ctx, tx, kavya, addrID, false, addrVersion) })
		}},
		{"contact delete", msgSiteContact, func() error { return a.DeleteContact(ctx, kavya, contactID, contactVersion) }},
		{"contact disable", msgSiteContact, func() error { return a.SetContactDisabled(ctx, kavya, contactID, contactVersion, true) }},
		{"contact set primary", msgSiteContact, func() error {
			return inTx(func(tx *db.Tx) error { return a.SetPrimaryContactTx(ctx, tx, kavya, 0, contactID) })
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wantFieldError(t, c.call(), "site_id", c.msg)
			if n := w.count(t, `SELECT count(*) FROM address WHERE id = ? AND version = ? AND disabled = 0 AND is_primary_shipping = 0`, addrID, addrVersion); n != 1 {
				t.Error("the site's address changed")
			}
			if n := w.count(t, `SELECT count(*) FROM contact WHERE id = ? AND version = ? AND disabled = 0 AND is_primary = 0`, contactID, contactVersion); n != 1 {
				t.Error("the site's contact changed")
			}
		})
	}
	if got := w.get(t, saved.ID).Delivery; got != puneStored {
		t.Errorf("delivery after the refused calls = %+v; want %+v", got, puneStored)
	}
}

package address_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Messages for MST Task 6 attempt 3 (CD-180), CD-176 and CD-170, verbatim.
const (
	m5NeedPrimaryMsg    = "A registered party needs a primary billing address with a GSTIN. Add the billing address first." // CD-180 D1
	m5EnableFirstMsg    = "Enable this address before making it primary."                                                   // CD-177 (3), CD-180 D2
	m5DupEnabledMsg     = "GSTIN 27AABCS1429B1ZU is already used by party SHREE. Use that party instead."                   // CD-176
	m5TickBillingMsg    = "Tick Billing on this address before making it the primary billing address."                      // CD-176
	m5TickShippingMsg   = "Tick Shipping on this address before making it the primary shipping address."                    // CD-176
	m5PartyNotFoundMsg  = "Party not found. Pick one from the list."                                                        // CD-170
	m5PartyDisabledMsg  = "Party SHREE is disabled. Pick another party."                                                    // CD-170
	m5BranchNotFoundMsg = "Branch not found. Pick one from the list."                                                       // CD-170
	m5BranchDisabledMsg = "Branch BLR is disabled. Pick another branch."                                                    // CD-170
	m5StaleMumbaiMsg    = "Someone else changed Address Mumbai after you opened it. Reload it and make your changes again." // CD-94, CD-180 D4
)

// m5WantField checks that err is a 422 *mstkit.ValidationError with a field
// error on field with message msg (other errors may come with it).
func m5WantField(t *testing.T, err error, field, msg string) {
	t.Helper()
	var ve *mstkit.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v (%T); want *mstkit.ValidationError with %s: %q", err, err, field, msg)
	}
	if ve.Status() != 422 {
		t.Errorf("Status() = %d; want 422", ve.Status())
	}
	for _, fe := range ve.FieldErrors() {
		if fe.Field == field && fe.Message == msg {
			return
		}
	}
	t.Fatalf("FieldErrors() = %+v; want one on %s with %q", ve.FieldErrors(), field, msg)
}

// m5WantStale checks that err is DOC's stale error (409) with message msg.
func m5WantStale(t *testing.T, err error, msg string) {
	t.Helper()
	var de *doc.Error
	if !errors.As(err, &de) {
		t.Fatalf("err = %v (%T); want *doc.Error stale %q", err, err, msg)
	}
	if de.Kind != doc.KindStale || de.Status() != 409 || de.Message != msg {
		t.Fatalf("err = {Kind %q, Status %d, Message %q}; want {%q, 409, %q}", de.Kind, de.Status(), de.Message, doc.KindStale, msg)
	}
}

// m5HoldsAll is addrM5Holds (R1, R2) plus R3 (every address GSTIN fits the
// party's category, CD-177 (2)), R4 (a party with an address outside India
// is Overseas, MST-047) and CD-180 D2 (no disabled address is primary).
func m5HoldsAll(t *testing.T, env *msttest.Env, when string) {
	t.Helper()
	addrM5Holds(t, env, when)
	ctx := context.Background()
	type row struct {
		code, cat, country, g string
		id                    int64
	}
	var rows []row
	var disabledPrimary []int64
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		rs, err := tx.SQL().QueryContext(ctx, `SELECT p.code, p.gst_category, a.country, a.gstin, a.id
			FROM address a JOIN party p ON p.id = a.party_id ORDER BY a.id`)
		if err != nil {
			return err
		}
		defer func() { _ = rs.Close() }()
		for rs.Next() {
			var r row
			if err := rs.Scan(&r.code, &r.cat, &r.country, &r.g, &r.id); err != nil {
				return err
			}
			rows = append(rows, r)
		}
		if err := rs.Err(); err != nil {
			return err
		}
		ds, err := tx.SQL().QueryContext(ctx, `SELECT id FROM address
			WHERE disabled = 1 AND (is_primary_billing = 1 OR is_primary_shipping = 1) ORDER BY id`)
		if err != nil {
			return err
		}
		defer func() { _ = ds.Close() }()
		for ds.Next() {
			var id int64
			if err := ds.Scan(&id); err != nil {
				return err
			}
			disabledPrimary = append(disabledPrimary, id)
		}
		return ds.Err()
	}); err != nil {
		t.Fatalf("%s: M5 check: %v", when, err)
	}
	for _, r := range rows {
		if r.g != "" {
			if err := gstin.ValidateCategory(gstin.Category(r.cat), r.g); err != nil {
				t.Errorf("%s: party %s (%s) address %d GSTIN %s: %v (R3)", when, r.code, r.cat, r.id, r.g, err)
			}
		}
		if r.country != "India" && r.cat != string(gstin.Overseas) {
			t.Errorf("%s: party %s is %s with address %d in %s (R4, MST-047)", when, r.code, r.cat, r.id, r.country)
		}
	}
	if len(disabledPrimary) > 0 {
		t.Errorf("%s: disabled addresses %v are primary (CD-180 D2)", when, disabledPrimary)
	}
}

// m5Snapshot dumps every party and address row, all columns.
func m5Snapshot(t *testing.T, env *msttest.Env) string {
	t.Helper()
	ctx := context.Background()
	var b strings.Builder
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		for _, table := range []string{"party", "address"} {
			rs, err := tx.SQL().QueryContext(ctx, "SELECT * FROM "+table+" ORDER BY id")
			if err != nil {
				return err
			}
			cols, err := rs.Columns()
			if err != nil {
				_ = rs.Close()
				return err
			}
			for rs.Next() {
				vals := make([]any, len(cols))
				ptrs := make([]any, len(cols))
				for i := range vals {
					ptrs[i] = &vals[i]
				}
				if err := rs.Scan(ptrs...); err != nil {
					_ = rs.Close()
					return err
				}
				fmt.Fprintf(&b, "%s %v\n", table, vals)
			}
			if err := rs.Err(); err != nil {
				_ = rs.Close()
				return err
			}
			if err := rs.Close(); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return b.String()
}

// m5Run drives public writes, logging each, and after every write checks
// M5 (success) or that nothing changed (refusal).
type m5Run struct {
	t   *testing.T
	env *msttest.Env
	s   *address.Service
	ps  *party.Service
	log []string
}

func newM5Run(t *testing.T) *m5Run {
	env := msttest.New(t)
	return &m5Run{t: t, env: env, s: address.New(env.Kit), ps: party.New(env.Kit)}
}

func (r *m5Run) fail(format string, args ...any) {
	r.t.Helper()
	r.t.Fatalf(format+"\nstep sequence:\n%s", append(args, strings.Join(r.log, "\n"))...)
}

// do runs write. A refusal must be a 422 (or a non-stale 409 refusal) and
// leave the database as it was; a success must leave M5 holding.
func (r *m5Run) do(desc string, write func() error) error {
	r.t.Helper()
	before := m5Snapshot(r.t, r.env)
	err := write()
	res := "ok"
	if err != nil {
		res = "refused: " + err.Error()
	}
	r.log = append(r.log, fmt.Sprintf("%3d. %s -> %s", len(r.log)+1, desc, res))
	if err != nil {
		var he interface{ Status() int }
		var de *doc.Error
		stale := errors.As(err, &de) && de.Kind == doc.KindStale
		if !errors.As(err, &he) || (he.Status() != 422 && he.Status() != 409) || stale {
			r.fail("step %d: error %v (%T); want a 422 or 409 refusal", len(r.log), err, err)
		}
		if after := m5Snapshot(r.t, r.env); after != before {
			r.fail("step %d: refused write changed the database\nbefore:\n%s\nafter:\n%s", len(r.log), before, after)
		}
		return err
	}
	m5HoldsAll(r.t, r.env, fmt.Sprintf("after step %d (%s)", len(r.log), desc))
	if r.t.Failed() {
		r.fail("step %d: M5 broken after a successful write", len(r.log))
	}
	return nil
}

// must is do for a set-up write that has to succeed.
func (r *m5Run) must(desc string, write func() error) {
	r.t.Helper()
	if err := r.do(desc, write); err != nil {
		r.fail("set-up step %d: %v; want nil", len(r.log), err)
	}
}

func (r *m5Run) create(id *int64, in address.Input) func() error {
	return func() error {
		saved, err := r.s.Create(context.Background(), r.env.Kavya, in)
		if id != nil {
			*id = saved.ID
		}
		return err
	}
}

func (r *m5Run) update(id int64, edit func(*address.Input)) func() error {
	return func() error {
		a := addrGet(r.t, r.env.DB, r.s, id)
		in := address.Input{PartyID: a.PartyID, BranchID: a.BranchID, Title: a.Title, Line1: a.Line1, Line2: a.Line2,
			City: a.City, StateCode: a.StateCode, Country: a.Country, Pincode: a.Pincode, GSTIN: a.GSTIN,
			IsBilling: a.IsBilling, IsShipping: a.IsShipping}
		edit(&in)
		_, err := r.s.Update(context.Background(), r.env.Kavya, id, a.Version, in)
		return err
	}
}

func (r *m5Run) setPrimary(id int64, billing bool) func() error {
	return func() error { return addrSetPrimary(r.env, r.s, id, billing, addrGet(r.t, r.env.DB, r.s, id).Version) }
}

func (r *m5Run) setDisabled(id int64, disabled bool) func() error {
	return func() error {
		return r.s.SetDisabled(context.Background(), r.env.Kavya, id, addrGet(r.t, r.env.DB, r.s, id).Version, disabled)
	}
}

func (r *m5Run) del(id int64) func() error {
	return func() error {
		return r.s.Delete(context.Background(), r.env.Kavya, id, addrGet(r.t, r.env.DB, r.s, id).Version)
	}
}

func (r *m5Run) partyUpdate(id int64, cat gstin.Category, blankPAN bool) func() error {
	return func() error {
		p := addrPartyGet(r.t, r.env.DB, r.ps, id)
		in := party.Input{Code: p.Code, Name: p.Name, IsVendor: p.IsVendor, IsCustomer: p.IsCustomer,
			PAN: p.PAN, GSTCategory: cat, DefaultPaymentTermsID: p.DefaultPaymentTermsID}
		if blankPAN {
			in.PAN = ""
		}
		_, err := r.ps.Update(context.Background(), r.env.Kavya, id, p.Version, in)
		return err
	}
}

// m5Pune is a second SHREE billing-only address sharing Mumbai's GSTIN.
func m5Pune(partyID int64) address.Input {
	in := addrMumbai(partyID)
	in.Title, in.Line1, in.City, in.Pincode = "Pune", "Survey 21, Bhosari MIDC", "Pune", "411001"
	return in
}

// m5Godown is a second SHREE shipping-only address sharing Thane's GSTIN.
func m5Godown(partyID int64) address.Input {
	in := addrThane(partyID)
	in.Title, in.Line1 = "Thane godown", "Gala 9, Wagle Estate"
	return in
}

// TestParty_M5PropertyRandomWrites pins CD-180: after every successful
// public address or party write M5 holds (R1–R4 and D2), and a refused write
// changes nothing. Fixed sequences hit the three holes the attempt-2 review
// found; then a fixed-seed random sequence runs.
func TestParty_M5PropertyRandomWrites(t *testing.T) {
	t.Run("hole 1: registered party's first address is shipping-only", func(t *testing.T) {
		r := newM5Run(t)
		shree := addrShree(t, r.env)
		m5HoldsAll(t, r.env, "at start")
		_ = r.do("SHREE Create Thane (shipping only, GSTIN)", r.create(nil, addrThane(shree)))
		ship := addrThane(shree)
		ship.GSTIN = ""
		_ = r.do("SHREE Create Thane (shipping only, no GSTIN)", r.create(nil, ship))
	})
	t.Run("hole 2: party update to registered without a primary billing address", func(t *testing.T) {
		r := newM5Run(t)
		corom := addrParty(t, r.env, addrCoromCode, addrCoromName, "", gstin.Unregistered)
		ship := addrChennai(corom)
		ship.IsBilling, ship.IsShipping = false, true
		r.must("COROM Create Chennai (shipping only)", r.create(nil, ship))
		for _, c := range []gstin.Category{gstin.Regular, gstin.Composition, gstin.SEZ} {
			_ = r.do("COROM Update category "+string(c), r.partyUpdate(corom, c, false))
		}
	})
	t.Run("hole 3: Update makes a disabled address primary", func(t *testing.T) {
		r := newM5Run(t)
		shree := addrShree(t, r.env)
		var pune, godown int64
		r.must("SHREE Create Mumbai", r.create(nil, addrMumbai(shree)))
		r.must("SHREE Create Thane", r.create(nil, addrThane(shree)))
		r.must("SHREE Create Pune (billing)", r.create(&pune, m5Pune(shree)))
		r.must("SHREE Create Thane godown (shipping)", r.create(&godown, m5Godown(shree)))
		r.must("SetDisabled(Pune, true)", r.setDisabled(pune, true))
		r.must("SetDisabled(Thane godown, true)", r.setDisabled(godown, true))
		_ = r.do("Update disabled Pune with PrimaryBilling", r.update(pune, func(in *address.Input) { in.PrimaryBilling = true }))
		_ = r.do("Update disabled Thane godown with PrimaryShipping", r.update(godown, func(in *address.Input) { in.PrimaryShipping = true }))
	})
	t.Run("D2: disabling a primary address", func(t *testing.T) {
		r := newM5Run(t)
		corom := addrParty(t, r.env, addrCoromCode, addrCoromName, "", gstin.Unregistered)
		shree := addrShree(t, r.env)
		both := addrChennai(corom)
		both.IsShipping = true
		var chennai, thane int64
		r.must("COROM Create Chennai (billing+shipping)", r.create(&chennai, both))
		r.must("SHREE Create Mumbai", r.create(nil, addrMumbai(shree)))
		r.must("SHREE Create Thane", r.create(&thane, addrThane(shree)))
		_ = r.do("SetDisabled(COROM Chennai, true)", r.setDisabled(chennai, true))
		_ = r.do("SetDisabled(SHREE Thane, true)", r.setDisabled(thane, true))
	})
	t.Run("random seed 20261025", func(t *testing.T) {
		m5Random(t, rand.New(rand.NewPCG(20261025, 180)), 300)
	})
}

// m5Party is one party of the random run with the GSTINs that fit its PAN.
type m5Party struct {
	id   int64
	code string
	pool []string
}

func m5Random(t *testing.T, rng *rand.Rand, steps int) {
	r := newM5Run(t)
	coromSecond := "29AAACT2727Q1Z" // a second COROM GSTIN (Karnataka), check digit below
	coromSecond += string(gstin.CheckDigit(coromSecond))
	parties := []m5Party{
		{addrShree(t, r.env), addrShreeCode, []string{addrMumbaiGSTIN, addrThaneGSTIN, addrDelhiGSTIN}},
		{addrParty(t, r.env, addrCoromCode, addrCoromName, "", gstin.Unregistered), addrCoromCode, []string{addrCoromGSTIN, coromSecond}},
		{addrParty(t, r.env, "SG-STEEL", "Singapore Steel Pte Ltd", "", gstin.Overseas), "SG-STEEL", []string{"9917SGP29001OST"}},
	}
	m5HoldsAll(t, r.env, "at start")
	categories := append([]gstin.Category{""}, gstin.Categories()...)
	chance := func(pct int) bool { return rng.IntN(100) < pct }
	n := 0

	// fill sets the location and GSTIN of in at random.
	fill := func(in *address.Input, pt m5Party) {
		in.GSTIN = ""
		if chance(60) {
			in.GSTIN = pt.pool[rng.IntN(len(pt.pool))]
		}
		switch {
		case strings.HasPrefix(in.GSTIN, "99") || chance(20): // the OIDAR GSTIN has no Indian state
			in.Country, in.StateCode, in.City, in.Pincode = "Singapore", "", "Singapore", "118524"
		case in.GSTIN != "":
			in.Country, in.StateCode, in.City, in.Pincode = "India", "", "Mumbai", ""
		default:
			in.Country, in.StateCode, in.City, in.Pincode = "India", "27", "Mumbai", "400001"
		}
		in.IsBilling, in.IsShipping = true, true
		switch rng.IntN(3) {
		case 0:
			in.IsShipping = false
		case 1:
			in.IsBilling = false
		}
		in.PrimaryBilling, in.PrimaryShipping = chance(30), chance(30)
	}
	desc := func(in address.Input) string {
		return fmt.Sprintf("{%s %s state %q GSTIN %q billing %v shipping %v primary %v/%v}",
			in.Title, in.Country, in.StateCode, in.GSTIN, in.IsBilling, in.IsShipping, in.PrimaryBilling, in.PrimaryShipping)
	}

	for range steps {
		pt := parties[rng.IntN(len(parties))]
		var as []address.Address
		if err := r.env.DB.Read(context.Background(), func(tx *db.Tx) error {
			var err error
			as, err = r.s.ForParty(context.Background(), tx, pt.id, true)
			return err
		}); err != nil {
			r.fail("ForParty(%s): %v", pt.code, err)
		}
		k := rng.IntN(100)
		if len(as) == 0 && k < 86 {
			k = 0
		}
		var a address.Address
		if len(as) > 0 {
			a = as[rng.IntN(len(as))]
		}
		switch {
		case k < 30:
			n++
			in := address.Input{PartyID: pt.id, Title: fmt.Sprintf("A%d", n), Line1: "1 Main Road"}
			fill(&in, pt)
			_ = r.do(pt.code+" Create "+desc(in), r.create(nil, in))
		case k < 50:
			in := address.Input{PartyID: a.PartyID, Title: a.Title, Line1: a.Line1, Line2: a.Line2, City: a.City,
				StateCode: a.StateCode, Country: a.Country, Pincode: a.Pincode, GSTIN: a.GSTIN,
				IsBilling: a.IsBilling, IsShipping: a.IsShipping}
			if chance(50) {
				fill(&in, pt)
			} else {
				// Only the flags change: untick, retick and primary flags.
				in.IsBilling, in.IsShipping = chance(60), chance(60)
				if !in.IsBilling && !in.IsShipping {
					in.IsBilling = true
				}
				in.PrimaryBilling, in.PrimaryShipping = chance(30), chance(30)
			}
			_ = r.do(fmt.Sprintf("%s Update(%d, %s)", pt.code, a.ID, desc(in)), r.update(a.ID, func(got *address.Input) { *got = in }))
		case k < 63:
			billing := chance(50)
			_ = r.do(fmt.Sprintf("%s SetPrimary(%d (%s), billing %v)", pt.code, a.ID, a.Title, billing), r.setPrimary(a.ID, billing))
		case k < 78:
			disabled := chance(60)
			_ = r.do(fmt.Sprintf("%s SetDisabled(%d (%s), %v)", pt.code, a.ID, a.Title, disabled), r.setDisabled(a.ID, disabled))
		case k < 86:
			_ = r.do(fmt.Sprintf("%s Delete(%d (%s))", pt.code, a.ID, a.Title), r.del(a.ID))
		default:
			c := categories[rng.IntN(len(categories))]
			blankPAN := chance(30)
			_ = r.do(fmt.Sprintf("%s party Update(category %q, blank PAN %v)", pt.code, c, blankPAN), r.partyUpdate(pt.id, c, blankPAN))
		}
	}
	t.Logf("%d random steps; log:\n%s", steps, strings.Join(r.log, "\n"))
}

// TestParty_M5RegisteredFirstAddressShippingOnly pins CD-180 D1 on address
// create: a registered party's first address can't be shipping-only.
func TestParty_M5RegisteredFirstAddressShippingOnly(t *testing.T) {
	for _, g := range []string{addrThaneGSTIN, ""} {
		t.Run("GSTIN "+g, func(t *testing.T) {
			env := msttest.New(t)
			s := address.New(env.Kit)
			shree := addrShree(t, env)
			in := addrThane(shree)
			in.GSTIN = g
			_, err := s.Create(context.Background(), env.Kavya, in)
			m5WantField(t, err, "is_billing", m5NeedPrimaryMsg)
			if as := addrForParty(t, env.DB, s, shree); len(as) != 0 {
				t.Errorf("after refusal: ForParty(SHREE) = %+v; want none", as)
			}
			m5HoldsAll(t, env, "after the refused create")
		})
	}
}

// TestParty_M5UpdateToRegisteredWithoutPrimaryBilling pins CD-180 D1 on a
// party update: a party with addresses but no primary billing address
// can't become Regular, Composition or SEZ.
func TestParty_M5UpdateToRegisteredWithoutPrimaryBilling(t *testing.T) {
	for _, c := range []gstin.Category{gstin.Regular, gstin.Composition, gstin.SEZ} {
		t.Run(string(c), func(t *testing.T) {
			env := msttest.New(t)
			s := address.New(env.Kit)
			ps := party.New(env.Kit)
			corom := addrParty(t, env, addrCoromCode, addrCoromName, "", gstin.Unregistered)
			ship := addrChennai(corom)
			ship.IsBilling, ship.IsShipping = false, true
			addrCreate(t, env, s, ship)

			in := party.Input{Code: addrCoromCode, Name: addrCoromName, IsVendor: true, GSTCategory: c}
			_, err := ps.Update(context.Background(), env.Kavya, corom, addrPartyGet(t, env.DB, ps, corom).Version, in)
			m5WantField(t, err, "gst_category", m5NeedPrimaryMsg)
			if p := addrPartyGet(t, env.DB, ps, corom); p.GSTCategory != gstin.Unregistered {
				t.Errorf("after refusal: COROM GSTCategory = %q; want %q", p.GSTCategory, gstin.Unregistered)
			}
			m5HoldsAll(t, env, "after the refused update")
		})
	}
}

// TestAddress_UpdateRefusesPrimaryOnDisabled pins CD-180 D2 on Update: asking
// for PrimaryBilling or PrimaryShipping on a disabled address is refused.
func TestAddress_UpdateRefusesPrimaryOnDisabled(t *testing.T) {
	for _, billing := range []bool{true, false} {
		t.Run(fmt.Sprintf("billing %v", billing), func(t *testing.T) {
			env := msttest.New(t)
			ctx := context.Background()
			s := address.New(env.Kit)
			shree := addrShree(t, env)
			mumbai := addrCreate(t, env, s, addrMumbai(shree))
			thane := addrCreate(t, env, s, addrThane(shree))
			in := m5Pune(shree)
			if !billing {
				in = m5Godown(shree)
			}
			id := addrCreate(t, env, s, in)
			if err := s.SetDisabled(ctx, env.Kavya, id, addrGet(t, env.DB, s, id).Version, true); err != nil {
				t.Fatalf("SetDisabled(%s) = %v; want nil (not primary)", in.Title, err)
			}

			in.PrimaryBilling, in.PrimaryShipping = billing, !billing
			_, err := s.Update(ctx, env.Kavya, id, addrGet(t, env.DB, s, id).Version, in)
			m5WantField(t, err, "disabled", m5EnableFirstMsg)
			if a := addrGet(t, env.DB, s, id); a.PrimaryBilling || a.PrimaryShipping || !a.Disabled {
				t.Errorf("after refusal: %s {PrimaryBilling %v, PrimaryShipping %v, Disabled %v}; want {false, false, true}",
					in.Title, a.PrimaryBilling, a.PrimaryShipping, a.Disabled)
			}
			if !addrGet(t, env.DB, s, mumbai).PrimaryBilling || !addrGet(t, env.DB, s, thane).PrimaryShipping {
				t.Error("after refusal: want Mumbai still primary billing and Thane still primary shipping")
			}
		})
	}
}

// TestAddress_DisabledNotPromotedAutomatically pins CD-180 D2 on the
// first-of-type promotion. Shipping: ticking Shipping on a disabled address
// of a party with no shipping address saves without making it primary
// shipping. Billing: disabling an Unregistered party's primary billing
// address clears the flag and promotes nothing, and a later edit of the
// disabled address saves without making it primary billing again.
func TestAddress_DisabledNotPromotedAutomatically(t *testing.T) {
	t.Run("shipping", func(t *testing.T) {
		env := msttest.New(t)
		ctx := context.Background()
		s := address.New(env.Kit)
		corom := addrParty(t, env, addrCoromCode, addrCoromName, "", gstin.Unregistered)
		addrCreate(t, env, s, addrChennai(corom))
		godown := addrChennai(corom)
		godown.Title, godown.Line1 = "Chennai godown", "8 Ambattur Industrial Estate"
		id := addrCreate(t, env, s, godown)
		if a := addrGet(t, env.DB, s, id); a.PrimaryBilling {
			t.Fatal("Chennai godown: PrimaryBilling = true; want false (second billing address)")
		}
		if err := s.SetDisabled(ctx, env.Kavya, id, addrGet(t, env.DB, s, id).Version, true); err != nil {
			t.Fatalf("SetDisabled(Chennai godown) = %v; want nil", err)
		}

		godown.IsShipping = true
		if _, err := s.Update(ctx, env.Kavya, id, addrGet(t, env.DB, s, id).Version, godown); err != nil {
			t.Fatalf("Update(disabled Chennai godown, tick Shipping) = %v; want nil", err)
		}
		a := addrGet(t, env.DB, s, id)
		if !a.IsShipping || a.PrimaryShipping || a.PrimaryBilling || !a.Disabled {
			t.Errorf("Chennai godown {IsShipping %v, PrimaryShipping %v, PrimaryBilling %v, Disabled %v}; want {true, false, false, true}",
				a.IsShipping, a.PrimaryShipping, a.PrimaryBilling, a.Disabled)
		}
		m5HoldsAll(t, env, "after the update")
	})
	t.Run("billing", func(t *testing.T) {
		env := msttest.New(t)
		ctx := context.Background()
		s := address.New(env.Kit)
		corom := addrParty(t, env, addrCoromCode, addrCoromName, "", gstin.Unregistered)
		chennaiIn := addrChennai(corom)
		chennai := addrCreate(t, env, s, chennaiIn)
		godownIn := addrChennai(corom)
		godownIn.Title, godownIn.Line1 = "Chennai godown", "8 Ambattur Industrial Estate"
		godown := addrCreate(t, env, s, godownIn)
		if !addrGet(t, env.DB, s, chennai).PrimaryBilling || addrGet(t, env.DB, s, godown).PrimaryBilling {
			t.Fatal("want Chennai primary billing and Chennai godown not (second billing address)")
		}

		if err := s.SetDisabled(ctx, env.Kavya, chennai, addrGet(t, env.DB, s, chennai).Version, true); err != nil {
			t.Fatalf("SetDisabled(Chennai, COROM Unregistered) = %v; want nil", err)
		}
		if a := addrGet(t, env.DB, s, chennai); a.PrimaryBilling || !a.Disabled {
			t.Errorf("after disable: Chennai {PrimaryBilling %v, Disabled %v}; want {false, true} (flag cleared)", a.PrimaryBilling, a.Disabled)
		}
		if addrGet(t, env.DB, s, godown).PrimaryBilling {
			t.Error("after disable: Chennai godown PrimaryBilling = true; want false (nothing promoted)")
		}
		m5HoldsAll(t, env, "after disabling Chennai")

		chennaiIn.Line2 = "Near Kathipara Junction"
		if _, err := s.Update(ctx, env.Kavya, chennai, addrGet(t, env.DB, s, chennai).Version, chennaiIn); err != nil {
			t.Fatalf("Update(disabled Chennai, change Line2) = %v; want nil", err)
		}
		a := addrGet(t, env.DB, s, chennai)
		if a.Line2 != chennaiIn.Line2 || a.PrimaryBilling || !a.Disabled {
			t.Errorf("Chennai {Line2 %q, PrimaryBilling %v, Disabled %v}; want {%q, false, true}",
				a.Line2, a.PrimaryBilling, a.Disabled, chennaiIn.Line2)
		}
		if addrGet(t, env.DB, s, godown).PrimaryBilling {
			t.Error("after the update: Chennai godown PrimaryBilling = true; want false (nothing promoted)")
		}
		if n := addrPrimaryCount(t, env.DB, corom, "is_primary_billing"); n != 0 {
			t.Errorf("after the update: COROM has %d primary billing addresses; want 0", n)
		}
		m5HoldsAll(t, env, "after the update")
	})
}

// m5Branch sets up the company with one branch, PUNE, and returns its id.
func m5Branch(t *testing.T, env *msttest.Env) int64 {
	t.Helper()
	ctx := context.Background()
	cs := company.New(env.Kit)
	if err := cs.SaveCompany(ctx, env.Kavya, company.CompanyInput{LegalName: "Vardhan Infra Projects Pvt Ltd", PAN: "AAFCV2449G"}, 0); err != nil {
		t.Fatalf("SaveCompany: %v", err)
	}
	b, err := cs.Create(ctx, env.Kavya, company.BranchInput{Code: "PUNE", Name: "Pune Works", StateCode: "27", GSTIN: "27AAFCV2449G1Z7", IsHeadOffice: true})
	if err != nil {
		t.Fatalf("create branch PUNE: %v", err)
	}
	return b.ID
}

// TestAddress_BranchDisabledNeverPrimary pins CD-180 D2 on branch addresses,
// where the party post-state check doesn't run: SetPrimary and Update asking
// PrimaryBilling or PrimaryShipping on a disabled branch address are refused
// on disabled, and the first-of-type promotion skips a disabled one.
func TestAddress_BranchDisabledNeverPrimary(t *testing.T) {
	env := msttest.New(t)
	ctx := context.Background()
	s := address.New(env.Kit)
	branch := m5Branch(t, env)
	worksIn := address.Input{BranchID: branch, Title: "Pune works", Line1: "Plot 7, Chakan MIDC", City: "Pune",
		StateCode: "27", Country: "India", Pincode: "410501", IsBilling: true, IsShipping: true}
	godownIn := worksIn
	godownIn.Title, godownIn.Line1, godownIn.Pincode = "Pune godown", "Gala 3, Bhosari MIDC", "411026"
	works := addrCreate(t, env, s, worksIn)
	godown := addrCreate(t, env, s, godownIn)
	if a := addrGet(t, env.DB, s, works); !a.PrimaryBilling || !a.PrimaryShipping {
		t.Fatalf("Pune works {PrimaryBilling %v, PrimaryShipping %v}; want {true, true} (first of each type)", a.PrimaryBilling, a.PrimaryShipping)
	}
	if err := s.SetDisabled(ctx, env.Kavya, godown, addrGet(t, env.DB, s, godown).Version, true); err != nil {
		t.Fatalf("SetDisabled(Pune godown) = %v; want nil (not primary)", err)
	}

	// want checks both addresses' primary flags and that no disabled
	// address is primary.
	want := func(when string, worksPrimary bool) {
		t.Helper()
		if a := addrGet(t, env.DB, s, godown); a.PrimaryBilling || a.PrimaryShipping || !a.Disabled {
			t.Errorf("%s: Pune godown {PrimaryBilling %v, PrimaryShipping %v, Disabled %v}; want {false, false, true}",
				when, a.PrimaryBilling, a.PrimaryShipping, a.Disabled)
		}
		if a := addrGet(t, env.DB, s, works); a.PrimaryBilling != worksPrimary || a.PrimaryShipping != worksPrimary {
			t.Errorf("%s: Pune works {PrimaryBilling %v, PrimaryShipping %v}; want {%v, %v}",
				when, a.PrimaryBilling, a.PrimaryShipping, worksPrimary, worksPrimary)
		}
		m5HoldsAll(t, env, when)
	}

	for _, billing := range []bool{true, false} {
		err := addrSetPrimary(env, s, godown, billing, addrGet(t, env.DB, s, godown).Version)
		m5WantField(t, err, "disabled", m5EnableFirstMsg)
		want(fmt.Sprintf("after SetPrimary(disabled Pune godown, billing %v)", billing), true)

		in := godownIn
		in.PrimaryBilling, in.PrimaryShipping = billing, !billing
		_, err = s.Update(ctx, env.Kavya, godown, addrGet(t, env.DB, s, godown).Version, in)
		m5WantField(t, err, "disabled", m5EnableFirstMsg)
		want(fmt.Sprintf("after Update(disabled Pune godown, PrimaryBilling %v, PrimaryShipping %v)", billing, !billing), true)
	}

	if err := s.SetDisabled(ctx, env.Kavya, works, addrGet(t, env.DB, s, works).Version, true); err != nil {
		t.Fatalf("SetDisabled(Pune works) = %v; want nil", err)
	}
	if a := addrGet(t, env.DB, s, works); !a.Disabled {
		t.Error("after SetDisabled: Pune works Disabled = false; want true")
	}
	want("after disabling Pune works", false)

	godownIn.Line2 = "Behind the weighbridge"
	if _, err := s.Update(ctx, env.Kavya, godown, addrGet(t, env.DB, s, godown).Version, godownIn); err != nil {
		t.Fatalf("Update(disabled Pune godown, change Line2) = %v; want nil", err)
	}
	if a := addrGet(t, env.DB, s, godown); a.Line2 != godownIn.Line2 {
		t.Errorf("after Update: Pune godown Line2 = %q; want %q", a.Line2, godownIn.Line2)
	}
	want("after Update(disabled Pune godown) with no primary of either type", false)

	worksIn.Line2 = "Gate 2"
	if _, err := s.Update(ctx, env.Kavya, works, addrGet(t, env.DB, s, works).Version, worksIn); err != nil {
		t.Fatalf("Update(disabled Pune works, change Line2) = %v; want nil", err)
	}
	want("after Update(disabled Pune works) with no primary of either type", false)
}

// TestAddress_StaleVersionBeforeM5 pins CD-180 D4: SetDisabled and Delete
// at a stale version of a registered party's primary billing address get
// the stale error, not the M5 refusal.
func TestAddress_StaleVersionBeforeM5(t *testing.T) {
	for _, op := range []string{"SetDisabled", "Delete"} {
		t.Run(op, func(t *testing.T) {
			env := msttest.New(t)
			ctx := context.Background()
			s := address.New(env.Kit)
			id := addrShreeMumbai(t, env, s)
			stale := addrGet(t, env.DB, s, id).Version
			in := addrMumbai(0)
			in.Line2 = "Andheri East"
			if _, err := s.Update(ctx, env.Kavya, id, stale, in); err != nil {
				t.Fatalf("Update(Mumbai) = %v; want nil", err)
			}
			before := m5Snapshot(t, env)

			var err error
			if op == "SetDisabled" {
				err = s.SetDisabled(ctx, env.Kavya, id, stale, true)
			} else {
				err = s.Delete(ctx, env.Kavya, id, stale)
			}
			m5WantStale(t, err, m5StaleMumbaiMsg)
			if after := m5Snapshot(t, env); after != before {
				t.Errorf("stale %s changed the database:\nbefore:\n%s\nafter:\n%s", op, before, after)
			}
		})
	}
}

// TestParty_DuplicateGSTINOnEnabledParty pins CD-176: a GSTIN already used
// by an enabled party.
func TestParty_DuplicateGSTINOnEnabledParty(t *testing.T) {
	env := msttest.New(t)
	s := address.New(env.Kit)
	ps := party.New(env.Kit)
	addrShreeMumbai(t, env, s)

	newID := addrParty(t, env, "SHREE-NEW", "Shree Steel New", "", gstin.Regular)
	_, err := s.Create(context.Background(), env.Kavya, addrMumbai(newID))
	addrWantHas(t, err, m5DupEnabledMsg)
	if as := addrForParty(t, env.DB, s, newID); len(as) != 0 {
		t.Errorf("after refusal: ForParty(SHREE-NEW) = %+v; want none", as)
	}
	if p := addrPartyGet(t, env.DB, ps, newID); p.PAN != "" {
		t.Errorf("after refusal: SHREE-NEW PAN = %q; want blank", p.PAN)
	}
}

// TestAddress_SetPrimaryNeedsTick pins CD-176: an address is made primary
// billing (shipping) only when it is ticked Billing (Shipping).
func TestAddress_SetPrimaryNeedsTick(t *testing.T) {
	env := msttest.New(t)
	s := address.New(env.Kit)
	shree := addrShree(t, env)
	mumbai := addrCreate(t, env, s, addrMumbai(shree)) // billing only
	thane := addrCreate(t, env, s, addrThane(shree))   // shipping only

	err := addrSetPrimary(env, s, thane, true, addrGet(t, env.DB, s, thane).Version)
	addrWantHas(t, err, m5TickBillingMsg)
	err = addrSetPrimary(env, s, mumbai, false, addrGet(t, env.DB, s, mumbai).Version)
	addrWantHas(t, err, m5TickShippingMsg)
	if a := addrGet(t, env.DB, s, thane); a.PrimaryBilling || !a.PrimaryShipping {
		t.Errorf("Thane {PrimaryBilling %v, PrimaryShipping %v}; want {false, true}", a.PrimaryBilling, a.PrimaryShipping)
	}
	if a := addrGet(t, env.DB, s, mumbai); !a.PrimaryBilling || a.PrimaryShipping {
		t.Errorf("Mumbai {PrimaryBilling %v, PrimaryShipping %v}; want {true, false}", a.PrimaryBilling, a.PrimaryShipping)
	}
}

// TestAddress_OwnerLinkChecked pins CD-170 for the owner links: a missing
// or disabled party or branch is refused on its own field.
func TestAddress_OwnerLinkChecked(t *testing.T) {
	env := msttest.New(t)
	ctx := context.Background()
	s := address.New(env.Kit)
	ps := party.New(env.Kit)
	cs := company.New(env.Kit)

	_, err := s.Create(ctx, env.Kavya, addrMumbai(999999))
	m5WantField(t, err, "party_id", m5PartyNotFoundMsg)

	shree := addrShree(t, env)
	if err := ps.SetDisabled(ctx, env.Kavya, shree, addrPartyGet(t, env.DB, ps, shree).Version, true); err != nil {
		t.Fatalf("disable SHREE: %v", err)
	}
	_, err = s.Create(ctx, env.Kavya, addrMumbai(shree))
	m5WantField(t, err, "party_id", m5PartyDisabledMsg)

	if err := cs.SaveCompany(ctx, env.Kavya, company.CompanyInput{LegalName: "Vardhan Infra Projects Pvt Ltd", PAN: "AAFCV2449G"}, 0); err != nil {
		t.Fatalf("SaveCompany: %v", err)
	}
	if _, err := cs.Create(ctx, env.Kavya, company.BranchInput{Code: "PUNE", Name: "Pune Works", StateCode: "27", GSTIN: "27AAFCV2449G1Z7", IsHeadOffice: true}); err != nil {
		t.Fatalf("create branch PUNE: %v", err)
	}
	blr, err := cs.Create(ctx, env.Kavya, company.BranchInput{Code: "BLR", Name: "Bengaluru Site Office", StateCode: "29", GSTIN: "29AAFCV2449G1Z3"})
	if err != nil {
		t.Fatalf("create branch BLR: %v", err)
	}
	if err := cs.SetDisabled(ctx, env.Kavya, blr.ID, blr.Version, true); err != nil {
		t.Fatalf("disable BLR: %v", err)
	}
	branchAddr := func(branchID int64) address.Input {
		return address.Input{BranchID: branchID, Title: "Bengaluru", Line1: "Whitefield Main Road", City: "Bengaluru",
			StateCode: "29", Country: "India", Pincode: "560066", IsBilling: true}
	}
	_, err = s.Create(ctx, env.Kavya, branchAddr(999999))
	m5WantField(t, err, "branch_id", m5BranchNotFoundMsg)
	_, err = s.Create(ctx, env.Kavya, branchAddr(blr.ID))
	m5WantField(t, err, "branch_id", m5BranchDisabledMsg)

	var n int
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, "SELECT count(*) FROM address").Scan(&n)
	}); err != nil {
		t.Fatalf("count addresses: %v", err)
	}
	if n != 0 {
		t.Errorf("after the refusals: %d addresses; want 0", n)
	}
}

// TestParty_BlankPANFromAddressGSTIN pins MST-040 on a party update: a blank
// posted PAN takes the PAN of the address GSTINs.
func TestParty_BlankPANFromAddressGSTIN(t *testing.T) {
	env := msttest.New(t)
	s := address.New(env.Kit)
	ps := party.New(env.Kit)
	shree := addrShree(t, env)
	addrCreate(t, env, s, addrMumbai(shree))

	in := party.Input{Code: addrShreeCode, Name: addrShreeName, IsVendor: true, GSTCategory: gstin.Regular}
	if _, err := ps.Update(context.Background(), env.Kavya, shree, addrPartyGet(t, env.DB, ps, shree).Version, in); err != nil {
		t.Fatalf("Update(SHREE, blank PAN) = %v; want nil", err)
	}
	if p := addrPartyGet(t, env.DB, ps, shree); p.PAN != addrShreePAN {
		t.Errorf("after Update(blank PAN): PAN = %q; want %q (from %s)", p.PAN, addrShreePAN, addrMumbaiGSTIN)
	}
}

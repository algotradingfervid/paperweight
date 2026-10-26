package address_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Contact invariants (CD-180 lessons applied to contacts; engineering-lessons
// §1 item 6, §8): at most one primary contact per party and per branch; a
// primary contact belongs to the party it is primary for (MST-051); at most
// one primary email and one primary phone per contact (MST-065); a disabled
// contact is never primary (CD-180 D2's rule for addresses).
const (
	ctOneEmailMsg    = "Only one email can be primary."                    // MST-065
	ctOnePhoneMsg    = "Only one phone can be primary."                    // proposed (MST-065 pattern)
	ctEnableFirstMsg = "Enable this contact before making it primary."     // proposed (CD-177 (3) pattern)
	ctOwnerMsg       = "The owner of a contact can't be changed."          // proposed (CD-176 pattern)
	ctSingleOwnerMsg = "A contact must belong to one party or one branch." // proposed (MST-067 pattern)
	ctNotFoundMsg    = "Contact not found. Pick one from the list."        // CD-170 pattern
	ctEmailDomain    = "@shreesteel.test"                                  // MST-063's domain
	ctMobilePrefix   = "+9198200"                                          // MST-064's stored form
	ctSeedA, ctSeedB = uint64(20261026), uint64(7)                         // random run seed
	ctRandomSteps    = 300                                                 // random run length
	ctPartyKind      = "party"                                             // owner kinds
	ctBranchKind     = "branch"                                            // owner kinds
	ctSetPrimaryDesc = "SetPrimaryContactTx"                               // log label
	ctUnknownContact = int64(9999)                                         // an id no contact has
)

func TestContact_SingleOwner(t *testing.T) { // MST-067 for contacts
	for _, tc := range []struct{ partyID, branchID int64 }{{0, 0}, {1, 1}} {
		in := contactInput("Sunil", "", "Mehta")
		in.PartyID, in.BranchID = tc.partyID, tc.branchID
		_, err := address.ValidateContact(in)
		contactWantOnly(t, err, ctSingleOwnerMsg)
	}
	for _, tc := range []struct{ partyID, branchID int64 }{{1, 0}, {0, 1}} {
		in := contactInput("Sunil", "", "Mehta")
		in.PartyID, in.BranchID = tc.partyID, tc.branchID
		if _, err := address.ValidateContact(in); err != nil {
			t.Errorf("ValidateContact(party %d, branch %d) = %v; want nil", tc.partyID, tc.branchID, err)
		}
	}
}

// ctOwner is a contact owner: a party or a branch.
type ctOwner struct {
	name              string
	partyID, branchID int64
}

func (o ctOwner) kind() string {
	if o.branchID != 0 {
		return ctBranchKind
	}
	return ctPartyKind
}

// ctState is one contact row as stored.
type ctState struct {
	id, partyID, branchID int64
	primary, disabled     bool
	version               int
}

// ctQueryIDs runs query and returns its first column as ids.
func ctQueryIDs(t *testing.T, d *db.DB, query string, args ...any) []int64 {
	t.Helper()
	ctx := context.Background()
	var ids []int64
	if err := d.Read(ctx, func(tx *db.Tx) error {
		rs, err := tx.SQL().QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer func() { _ = rs.Close() }()
		for rs.Next() {
			var id int64
			if err := rs.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return rs.Err()
	}); err != nil {
		t.Fatalf("%s %v: %v", query, args, err)
	}
	return ids
}

// ctContacts lists the stored contacts of o in id order.
func ctContacts(t *testing.T, d *db.DB, o ctOwner) []ctState {
	t.Helper()
	ctx := context.Background()
	var cs []ctState
	if err := d.Read(ctx, func(tx *db.Tx) error {
		rs, err := tx.SQL().QueryContext(ctx, `SELECT id, coalesce(party_id, 0), coalesce(branch_id, 0), is_primary, disabled, version
			FROM contact WHERE coalesce(party_id, 0) = ? AND coalesce(branch_id, 0) = ? ORDER BY id`, o.partyID, o.branchID)
		if err != nil {
			return err
		}
		defer func() { _ = rs.Close() }()
		for rs.Next() {
			var c ctState
			if err := rs.Scan(&c.id, &c.partyID, &c.branchID, &c.primary, &c.disabled, &c.version); err != nil {
				return err
			}
			cs = append(cs, c)
		}
		return rs.Err()
	}); err != nil {
		t.Fatalf("contacts of %s: %v", o.name, err)
	}
	return cs
}

// ctGet is contact id as stored; ok is false when it doesn't exist.
func ctGet(t *testing.T, d *db.DB, id int64) (c ctState, ok bool) {
	t.Helper()
	ctx := context.Background()
	if err := d.Read(ctx, func(tx *db.Tx) error {
		err := tx.SQL().QueryRowContext(ctx, `SELECT id, coalesce(party_id, 0), coalesce(branch_id, 0), is_primary, disabled, version
			FROM contact WHERE id = ?`, id).Scan(&c.id, &c.partyID, &c.branchID, &c.primary, &c.disabled, &c.version)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		ok = err == nil
		return err
	}); err != nil {
		t.Fatalf("contact %d: %v", id, err)
	}
	return c, ok
}

// ctPrimaries lists the ids of o's contacts marked primary.
func ctPrimaries(t *testing.T, d *db.DB, o ctOwner) []int64 {
	t.Helper()
	var ids []int64
	for _, c := range ctContacts(t, d, o) {
		if c.primary {
			ids = append(ids, c.id)
		}
	}
	return ids
}

// ctHolds checks every contact invariant over the whole database.
func ctHolds(t *testing.T, env *msttest.Env, when string) {
	t.Helper()
	for _, c := range []struct{ rule, query string }{
		{"two primary contacts on one party (MST-066)",
			"SELECT party_id FROM contact WHERE is_primary = 1 AND party_id IS NOT NULL GROUP BY party_id HAVING count(*) > 1"},
		{"two primary contacts on one branch",
			"SELECT branch_id FROM contact WHERE is_primary = 1 AND branch_id IS NOT NULL GROUP BY branch_id HAVING count(*) > 1"},
		{"disabled contacts are primary (CD-180 D2 for contacts)",
			"SELECT id FROM contact WHERE disabled = 1 AND is_primary = 1"},
		{"contacts with two primary emails (MST-065)",
			"SELECT contact_id FROM contact_email WHERE is_primary = 1 GROUP BY contact_id HAVING count(*) > 1"},
		{"contacts with two primary phones",
			"SELECT contact_id FROM contact_phone WHERE is_primary = 1 GROUP BY contact_id HAVING count(*) > 1"},
		{"contacts without exactly one owner (MST-067)",
			"SELECT id FROM contact WHERE (party_id IS NULL) = (branch_id IS NULL)"},
	} {
		if ids := ctQueryIDs(t, env.DB, c.query); len(ids) > 0 {
			t.Errorf("%s: %s: %v", when, c.rule, ids)
		}
	}
}

// ctSnapshot dumps every contact, contact_email and contact_phone row.
func ctSnapshot(t *testing.T, env *msttest.Env) string {
	t.Helper()
	ctx := context.Background()
	var b strings.Builder
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		for _, table := range []string{"contact", "contact_email", "contact_phone"} {
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

// ctRun drives public contact writes, logging each; after every write it
// checks the invariants (success) or that nothing changed (refusal).
type ctRun struct {
	t   *testing.T
	env *msttest.Env
	s   *address.Service
	log []string
}

func newCtRun(t *testing.T) *ctRun {
	env := msttest.New(t)
	return &ctRun{t: t, env: env, s: address.New(env.Kit)}
}

func (r *ctRun) fail(format string, args ...any) {
	r.t.Helper()
	r.t.Fatalf(format+"\nstep sequence:\n%s", append(args, strings.Join(r.log, "\n"))...)
}

// do runs write. A refusal must be a 422 (or a non-stale 409 refusal) and
// leave the contact tables as they were; a success must leave every contact
// invariant holding.
func (r *ctRun) do(desc string, write func() error) error {
	r.t.Helper()
	before := ctSnapshot(r.t, r.env)
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
		if after := ctSnapshot(r.t, r.env); after != before {
			r.fail("step %d: refused write changed the contact tables\nbefore:\n%s\nafter:\n%s", len(r.log), before, after)
		}
		return err
	}
	ctHolds(r.t, r.env, fmt.Sprintf("after step %d (%s)", len(r.log), desc))
	if r.t.Failed() {
		r.fail("step %d: a contact invariant broke after a successful write", len(r.log))
	}
	return nil
}

// must is do for a write that has to succeed.
func (r *ctRun) must(desc string, write func() error) {
	r.t.Helper()
	if err := r.do(desc, write); err != nil {
		r.fail("step %d: %v; want nil", len(r.log), err)
	}
}

// refused is do for a write that has to be refused with msg on field (any
// field when field is "").
func (r *ctRun) refused(desc, field, msg string, write func() error) {
	r.t.Helper()
	err := r.do(desc, write)
	if err == nil {
		r.fail("step %d: succeeded; want refused with %q", len(r.log), msg)
	}
	if field == "" {
		addrWantHas(r.t, err, msg)
		return
	}
	m5WantField(r.t, err, field, msg)
}

func (r *ctRun) version(id int64) int {
	r.t.Helper()
	c, ok := ctGet(r.t, r.env.DB, id)
	if !ok {
		r.fail("contact %d not found", id)
	}
	return c.version
}

func (r *ctRun) create(id *int64, in address.ContactInput) func() error {
	return func() error {
		saved, err := r.s.CreateContact(context.Background(), r.env.Kavya, in)
		if id != nil {
			*id = saved.ID
		}
		return err
	}
}

func (r *ctRun) update(id int64, in address.ContactInput) func() error {
	return func() error {
		_, err := r.s.UpdateContact(context.Background(), r.env.Kavya, id, r.version(id), in)
		return err
	}
}

func (r *ctRun) setPrimary(partyID, id int64) func() error {
	return func() error {
		ctx := context.Background()
		return r.env.DB.Write(ctx, func(tx *db.Tx) error {
			return r.s.SetPrimaryContactTx(ctx, tx, r.env.Kavya, partyID, id)
		})
	}
}

func (r *ctRun) setDisabled(id int64, disabled bool) func() error {
	return func() error {
		return r.s.SetContactDisabled(context.Background(), r.env.Kavya, id, r.version(id), disabled)
	}
}

func (r *ctRun) del(id int64) func() error {
	return func() error {
		return r.s.DeleteContact(context.Background(), r.env.Kavya, id, r.version(id))
	}
}

// wantPrimaries checks o's primary contacts are exactly want.
func (r *ctRun) wantPrimaries(when string, o ctOwner, want ...int64) {
	r.t.Helper()
	if got := ctPrimaries(r.t, r.env.DB, o); !slices.Equal(got, want) {
		r.fail("%s: %s primary contacts = %v; want %v", when, o.name, got, want)
	}
}

// ctIn is a contact of o with one primary email and one primary mobile.
func ctIn(o ctOwner, first, last, mobile string, primary bool) address.ContactInput {
	return address.ContactInput{
		PartyID: o.partyID, BranchID: o.branchID, FirstName: first, LastName: last, Primary: primary,
		Emails: []address.EmailInput{{Email: strings.ToLower(first) + ctEmailDomain, Primary: true}},
		Phones: []address.PhoneInput{{Number: mobile, Mobile: true, Primary: true}},
	}
}

// ctSetup is an owner with Sunil Mehta (primary) and Kiran Joshi (not).
type ctSetup struct {
	r            *ctRun
	o, other     ctOwner
	sunil, kiran int64
	sunilIn      address.ContactInput
	kiranIn      address.ContactInput
}

// newCtSetup makes the owner of the given kind (party SHREE or branch PUNE)
// and another party BHARAT, then Sunil Mehta (primary) and Kiran Joshi.
func newCtSetup(t *testing.T, kind string) *ctSetup {
	r := newCtRun(t)
	cs := &ctSetup{r: r}
	cs.other = ctOwner{name: "party BHARAT", partyID: contactParty(t, r.env, "BHARAT", "Bharat Tools")}
	if kind == ctBranchKind {
		cs.o = ctOwner{name: "branch PUNE", branchID: m5Branch(t, r.env)}
	} else {
		cs.o = ctOwner{name: "party SHREE", partyID: contactParty(t, r.env, "SHREE", "Shree Steel Traders")}
	}
	cs.sunilIn = ctIn(cs.o, "Sunil", "Mehta", "+919820012345", true)
	cs.kiranIn = ctIn(cs.o, "Kiran", "Joshi", "+919820054321", false)
	r.must("Create Sunil Mehta (primary)", r.create(&cs.sunil, cs.sunilIn))
	r.must("Create Kiran Joshi", r.create(&cs.kiran, cs.kiranIn))
	r.wantPrimaries("after set-up", cs.o, cs.sunil)
	return cs
}

// twoPrimaryEmails and twoPrimaryPhones return in with a second primary
// email (phone).
func twoPrimaryEmails(in address.ContactInput) address.ContactInput {
	in.Emails = append(slices.Clone(in.Emails), address.EmailInput{Email: "purchase" + ctEmailDomain, Primary: true})
	return in
}

func twoPrimaryPhones(in address.ContactInput) address.ContactInput {
	in.Phones = append(slices.Clone(in.Phones), address.PhoneInput{Number: "+919820011111", Mobile: true, Primary: true})
	return in
}

// TestContact_InvariantsOnEveryWritePath runs every contact write path
// (create, update, set-primary, disable, enable, delete) for a party-owned
// and a branch-owned contact, and checks after each write that every contact
// invariant holds (ctHolds), plus the path's own outcome.
func TestContact_InvariantsOnEveryWritePath(t *testing.T) {
	type path struct {
		name      string
		partyOnly bool // SetPrimaryContactTx takes a party (plan Task 7)
		run       func(cs *ctSetup)
	}
	paths := []path{
		{name: "create primary clears the old primary", run: func(cs *ctSetup) {
			r := cs.r
			var asha int64
			r.must("Create Asha Rao (primary)", r.create(&asha, ctIn(cs.o, "Asha", "Rao", "+919820022222", true)))
			r.wantPrimaries("after Create Asha Rao (primary)", cs.o, asha)
		}},
		{name: "create refuses two primary emails", run: func(cs *ctSetup) {
			cs.r.refused("Create Asha Rao with two primary emails", "", ctOneEmailMsg,
				cs.r.create(nil, twoPrimaryEmails(ctIn(cs.o, "Asha", "Rao", "+919820022222", true))))
			cs.r.wantPrimaries("after the refused create", cs.o, cs.sunil)
		}},
		{name: "create refuses two primary phones", run: func(cs *ctSetup) {
			cs.r.refused("Create Asha Rao with two primary phones", "", ctOnePhoneMsg,
				cs.r.create(nil, twoPrimaryPhones(ctIn(cs.o, "Asha", "Rao", "+919820022222", true))))
			cs.r.wantPrimaries("after the refused create", cs.o, cs.sunil)
		}},
		{name: "update makes a contact primary and clears the old one", run: func(cs *ctSetup) {
			in := cs.kiranIn
			in.Primary = true
			cs.r.must("Update Kiran Joshi (Primary)", cs.r.update(cs.kiran, in))
			cs.r.wantPrimaries("after Update Kiran Joshi (Primary)", cs.o, cs.kiran)
		}},
		{name: "update refuses two primary emails", run: func(cs *ctSetup) {
			cs.r.refused("Update Kiran Joshi with two primary emails", "", ctOneEmailMsg, cs.r.update(cs.kiran, twoPrimaryEmails(cs.kiranIn)))
		}},
		{name: "update refuses two primary phones", run: func(cs *ctSetup) {
			cs.r.refused("Update Kiran Joshi with two primary phones", "", ctOnePhoneMsg, cs.r.update(cs.kiran, twoPrimaryPhones(cs.kiranIn)))
		}},
		{name: "update refuses primary on a disabled contact", run: func(cs *ctSetup) {
			cs.r.must("SetContactDisabled(Kiran Joshi, true)", cs.r.setDisabled(cs.kiran, true))
			in := cs.kiranIn
			in.Primary = true
			cs.r.refused("Update disabled Kiran Joshi (Primary)", "disabled", ctEnableFirstMsg, cs.r.update(cs.kiran, in))
			cs.r.wantPrimaries("after the refused update", cs.o, cs.sunil)
		}},
		{name: "update refuses another owner", run: func(cs *ctSetup) {
			in := cs.sunilIn
			in.PartyID, in.BranchID = cs.other.partyID, 0
			cs.r.refused("Update Sunil Mehta to "+cs.other.name, "", ctOwnerMsg, cs.r.update(cs.sunil, in))
			cs.r.wantPrimaries("after the refused update", cs.o, cs.sunil)
			cs.r.wantPrimaries("after the refused update", cs.other)
		}},
		{name: "set-primary moves the primary", partyOnly: true, run: func(cs *ctSetup) {
			cs.r.must(ctSetPrimaryDesc+"(Kiran Joshi)", cs.r.setPrimary(cs.o.partyID, cs.kiran))
			cs.r.wantPrimaries("after "+ctSetPrimaryDesc+"(Kiran Joshi)", cs.o, cs.kiran)
		}},
		{name: "set-primary refuses a disabled contact", partyOnly: true, run: func(cs *ctSetup) {
			cs.r.must("SetContactDisabled(Kiran Joshi, true)", cs.r.setDisabled(cs.kiran, true))
			cs.r.refused(ctSetPrimaryDesc+"(disabled Kiran Joshi)", "disabled", ctEnableFirstMsg, cs.r.setPrimary(cs.o.partyID, cs.kiran))
			cs.r.wantPrimaries("after the refused set-primary", cs.o, cs.sunil)
		}},
		{name: "set-primary refuses another party's contact", partyOnly: true, run: func(cs *ctSetup) {
			var ravi int64
			cs.r.must("Create Ravi Sharma (BHARAT)", cs.r.create(&ravi, ctIn(cs.other, "Ravi", "Sharma", "+919820033333", false)))
			cs.r.refused(ctSetPrimaryDesc+"(SHREE, BHARAT's Ravi Sharma)", "", contactBelongsMsg, cs.r.setPrimary(cs.o.partyID, ravi))
			cs.r.wantPrimaries("after the refused set-primary", cs.o, cs.sunil)
			cs.r.wantPrimaries("after the refused set-primary", cs.other)
		}},
		{name: "set-primary refuses an unknown contact", partyOnly: true, run: func(cs *ctSetup) {
			cs.r.refused(ctSetPrimaryDesc+"(unknown contact)", "", ctNotFoundMsg, cs.r.setPrimary(cs.o.partyID, ctUnknownContact))
			cs.r.wantPrimaries("after the refused set-primary", cs.o, cs.sunil)
		}},
		{name: "disabling the primary clears it and promotes nothing", run: func(cs *ctSetup) {
			cs.r.must("SetContactDisabled(Sunil Mehta, true)", cs.r.setDisabled(cs.sunil, true))
			if c, _ := ctGet(cs.r.t, cs.r.env.DB, cs.sunil); !c.disabled || c.primary {
				cs.r.fail("after disabling Sunil Mehta: {disabled %v, primary %v}; want {true, false}", c.disabled, c.primary)
			}
			cs.r.wantPrimaries("after disabling Sunil Mehta", cs.o)
		}},
		{name: "disable and enable a contact", run: func(cs *ctSetup) {
			cs.r.must("SetContactDisabled(Kiran Joshi, true)", cs.r.setDisabled(cs.kiran, true))
			cs.r.wantPrimaries("after disabling Kiran Joshi", cs.o, cs.sunil)
			cs.r.must("SetContactDisabled(Kiran Joshi, false)", cs.r.setDisabled(cs.kiran, false))
			if c, _ := ctGet(cs.r.t, cs.r.env.DB, cs.kiran); c.disabled {
				cs.r.fail("after enabling Kiran Joshi: disabled = true; want false")
			}
			cs.r.wantPrimaries("after enabling Kiran Joshi", cs.o, cs.sunil)
			cs.r.must("SetContactDisabled(Sunil Mehta, true)", cs.r.setDisabled(cs.sunil, true))
			cs.r.must("SetContactDisabled(Sunil Mehta, false)", cs.r.setDisabled(cs.sunil, false))
			in := cs.sunilIn
			in.Primary = true
			cs.r.must("Update enabled Sunil Mehta (Primary)", cs.r.update(cs.sunil, in))
			cs.r.wantPrimaries("after re-enabled Sunil Mehta is made primary", cs.o, cs.sunil)
		}},
		{name: "deleting the primary promotes nothing", run: func(cs *ctSetup) {
			cs.r.must("DeleteContact(Sunil Mehta)", cs.r.del(cs.sunil))
			if _, ok := ctGet(cs.r.t, cs.r.env.DB, cs.sunil); ok {
				cs.r.fail("after DeleteContact(Sunil Mehta): the contact is still there")
			}
			for _, table := range []string{"contact_email", "contact_phone"} {
				if ids := ctQueryIDs(cs.r.t, cs.r.env.DB, "SELECT id FROM "+table+" WHERE contact_id = ?", cs.sunil); len(ids) > 0 {
					cs.r.fail("after DeleteContact(Sunil Mehta): %s rows %v left", table, ids)
				}
			}
			cs.r.wantPrimaries("after DeleteContact(Sunil Mehta)", cs.o)
		}},
	}
	for _, kind := range []string{ctPartyKind, ctBranchKind} {
		t.Run(kind, func(t *testing.T) {
			for _, p := range paths {
				if p.partyOnly && kind == ctBranchKind {
					continue
				}
				t.Run(p.name, func(t *testing.T) {
					p.run(newCtSetup(t, kind))
				})
			}
		})
	}
}

// TestContact_PropertyRandomWrites runs a fixed-seed random sequence of the
// public contact writes over two parties and a branch. After every write it
// checks the invariants (success) or that nothing changed (refusal), and that
// the outcome is the one the rules give: refused exactly when a rule names
// the input, the new primary is the only one, and a disable or delete of the
// primary promotes nothing.
func TestContact_PropertyRandomWrites(t *testing.T) {
	ctRandom(t, rand.New(rand.NewPCG(ctSeedA, ctSeedB)), ctRandomSteps)
}

func ctRandom(t *testing.T, rng *rand.Rand, steps int) {
	r := newCtRun(t)
	owners := []ctOwner{
		{name: "party SHREE", partyID: contactParty(t, r.env, "SHREE", "Shree Steel Traders")},
		{name: "party BHARAT", partyID: contactParty(t, r.env, "BHARAT", "Bharat Tools")},
		{name: "branch PUNE", branchID: m5Branch(t, r.env)},
	}
	chance := func(pct int) bool { return rng.IntN(100) < pct }
	inputs := map[int64]address.ContactInput{} // last saved input of each contact
	n := 0

	// lists returns 0–3 emails and phones with random primary flags, and the
	// reasons a save of them is refused.
	lists := func() ([]address.EmailInput, []address.PhoneInput, []string) {
		n++
		var es []address.EmailInput
		var ps []address.PhoneInput
		ep, pp := 0, 0
		for i := range rng.IntN(4) {
			e := address.EmailInput{Email: fmt.Sprintf("c%d.%d%s", n, i, ctEmailDomain), Primary: chance(35)}
			if e.Primary {
				ep++
			}
			es = append(es, e)
		}
		for i := range rng.IntN(4) {
			p := address.PhoneInput{Number: fmt.Sprintf("%s%05d", ctMobilePrefix, n*10+i), Mobile: true, Primary: chance(35)}
			if p.Primary {
				pp++
			}
			ps = append(ps, p)
		}
		var why []string
		if ep > 1 {
			why = append(why, ctOneEmailMsg)
		}
		if pp > 1 {
			why = append(why, ctOnePhoneMsg)
		}
		return es, ps, why
	}
	// expect checks err against the reasons the write must be refused.
	expect := func(err error, why []string) {
		t.Helper()
		switch {
		case len(why) == 0 && err != nil:
			r.fail("step %d: refused; want success", len(r.log))
		case len(why) > 0 && err == nil:
			r.fail("step %d: succeeded; want refused (%s)", len(r.log), strings.Join(why, " "))
		case len(why) == 1:
			var ve *mstkit.ValidationError
			found := false
			if errors.As(err, &ve) {
				for _, fe := range ve.FieldErrors() {
					found = found || fe.Message == why[0]
				}
			}
			if !found {
				r.fail("step %d: refused with %v; want %q", len(r.log), err, why[0])
			}
		}
	}

	for range steps {
		o := owners[rng.IntN(len(owners))]
		cs := ctContacts(t, r.env.DB, o)
		before := ctPrimaries(t, r.env.DB, o)
		k := rng.IntN(100)
		if len(cs) == 0 {
			k = 0
		}
		var c ctState
		if len(cs) > 0 {
			c = cs[rng.IntN(len(cs))]
		}
		switch {
		case k < 30: // create
			es, ps, why := lists()
			in := address.ContactInput{PartyID: o.partyID, BranchID: o.branchID, FirstName: fmt.Sprintf("C%d", n),
				Emails: es, Phones: ps, Primary: chance(40)}
			var id int64
			err := r.do(fmt.Sprintf("%s Create %+v", o.name, in), r.create(&id, in))
			expect(err, why)
			if err == nil {
				inputs[id] = in
				if in.Primary {
					r.wantPrimaries("after a primary create", o, id)
				}
			}
		case k < 50: // update
			in := inputs[c.id]
			in.Primary = c.primary || chance(40)
			var why []string
			if chance(50) {
				in.Emails, in.Phones, why = lists()
			}
			if in.Primary && !c.primary && c.disabled {
				why = append(why, ctEnableFirstMsg)
			}
			if chance(10) {
				to := owners[rng.IntN(len(owners))]
				if to != o {
					in.PartyID, in.BranchID = to.partyID, to.branchID
					why = append(why, ctOwnerMsg)
				}
			}
			err := r.do(fmt.Sprintf("%s Update(%d, %+v)", o.name, c.id, in), r.update(c.id, in))
			expect(err, why)
			if err == nil {
				inputs[c.id] = in
				if in.Primary {
					r.wantPrimaries("after a primary update", o, c.id)
				}
			}
		case k < 65 && o.kind() == ctPartyKind: // set-primary, sometimes with another party's contact
			target, owner := c, o
			if chance(20) {
				other := owners[1-slices.Index(owners, o)]
				if ocs := ctContacts(t, r.env.DB, other); len(ocs) > 0 {
					target, owner = ocs[rng.IntN(len(ocs))], other
				}
			}
			var why []string
			if owner != o {
				why = append(why, contactBelongsMsg)
			}
			if target.disabled {
				why = append(why, ctEnableFirstMsg)
			}
			err := r.do(fmt.Sprintf("%s %s(%d of %s)", o.name, ctSetPrimaryDesc, target.id, owner.name), r.setPrimary(o.partyID, target.id))
			expect(err, why)
			if err == nil {
				r.wantPrimaries("after set-primary", o, target.id)
			}
		case k < 85: // disable or enable
			disabled := chance(60)
			err := r.do(fmt.Sprintf("%s SetContactDisabled(%d, %v)", o.name, c.id, disabled), r.setDisabled(c.id, disabled))
			expect(err, nil)
			if disabled {
				want := before
				if c.primary {
					want = nil // the flag is cleared and nothing is promoted (CD-180 D2)
				}
				r.wantPrimaries("after a disable", o, want...)
			}
		default: // delete
			err := r.do(fmt.Sprintf("%s DeleteContact(%d)", o.name, c.id), r.del(c.id))
			expect(err, nil)
			want := before
			if c.primary {
				want = nil // nothing is promoted (CD-180 D3)
			}
			r.wantPrimaries("after a delete", o, want...)
			delete(inputs, c.id)
		}
	}
	t.Logf("%d random steps; log:\n%s", steps, strings.Join(r.log, "\n"))
}

package address_test

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// CD-170 owner-link texts for contacts (the same as for addresses, m5_test.go).
const (
	coPartyNotFoundMsg  = "Party not found. Pick one from the list."
	coPartyDisabledMsg  = "Party SHREE is disabled. Pick another party."
	coBranchNotFoundMsg = "Branch not found. Pick one from the list."
	coBranchDisabledMsg = "Branch BLR is disabled. Pick another branch."
	coUnknownOwner      = int64(999999) // an id no party or branch has
	coContactDocType    = "Contact"
)

// coOwners is the CD-170 world: SHREE (disabled, primary contact Sunil
// Mehta), BHARAT (enabled), branch PUNE (enabled) and branch BLR (disabled,
// primary contact Anil Kumar).
type coOwners struct {
	env                       *msttest.Env
	s                         *address.Service
	shree, bharat, pune, blr  int64
	sunil, anil               int64
	sunilVersion, anilVersion int
}

func newCoOwners(t *testing.T) *coOwners {
	t.Helper()
	ctx := context.Background()
	env := msttest.New(t)
	o := &coOwners{env: env, s: address.New(env.Kit)}
	ps := party.New(env.Kit)
	cs := company.New(env.Kit)

	o.shree = contactParty(t, env, "SHREE", "Shree Steel Traders")
	o.bharat = contactParty(t, env, "BHARAT", "Bharat Tools")
	o.sunil = createContact(t, env, o.s, o.shree, "Sunil", "Mehta", true)
	if err := ps.SetDisabled(ctx, env.Kavya, o.shree, addrPartyGet(t, env.DB, ps, o.shree).Version, true); err != nil {
		t.Fatalf("disable SHREE: %v", err)
	}

	o.pune = m5Branch(t, env)
	blr, err := cs.Create(ctx, env.Kavya, company.BranchInput{Code: "BLR", Name: "Bengaluru Site Office", StateCode: "29", GSTIN: "29AAFCV2449G1Z3"})
	if err != nil {
		t.Fatalf("create branch BLR: %v", err)
	}
	o.blr = blr.ID
	saved, err := o.s.CreateContact(ctx, env.Kavya, address.ContactInput{BranchID: o.blr, FirstName: "Anil", LastName: "Kumar", Primary: true})
	if err != nil {
		t.Fatalf("CreateContact(Anil Kumar, BLR): %v", err)
	}
	o.anil = saved.ID
	if err := cs.SetDisabled(ctx, env.Kavya, o.blr, blr.Version, true); err != nil {
		t.Fatalf("disable BLR: %v", err)
	}
	o.sunilVersion, o.anilVersion = 1, 1
	return o
}

// coIn is Asha Rao, primary, with a primary email and mobile, owned by
// partyID or branchID.
func coIn(partyID, branchID int64) address.ContactInput {
	return address.ContactInput{
		PartyID: partyID, BranchID: branchID, FirstName: "Asha", LastName: "Rao", Primary: true,
		Emails: []address.EmailInput{{Email: "asha@shreesteel.test", Primary: true}},
		Phones: []address.PhoneInput{{Number: "+919820022222", Mobile: true, Primary: true}},
	}
}

// coAuditCount is the number of Contact audit entries.
func coAuditCount(t *testing.T, env *msttest.Env) int {
	t.Helper()
	ctx := context.Background()
	var n int
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, "SELECT count(*) FROM audit_log WHERE doc_type = ?", coContactDocType).Scan(&n)
	}); err != nil {
		t.Fatalf("count Contact audit entries: %v", err)
	}
	return n
}

// strconvID is id as audited.
func strconvID(id int64) string { return strconv.FormatInt(id, 10) }

// coGet is contact id read through GetContact.
func coGet(t *testing.T, env *msttest.Env, s *address.Service, id int64) address.Contact {
	t.Helper()
	var c address.Contact
	if err := env.DB.Read(context.Background(), func(tx *db.Tx) error {
		var err error
		c, err = s.GetContact(context.Background(), tx, id)
		return err
	}); err != nil {
		t.Fatalf("GetContact(%d): %v", id, err)
	}
	return c
}

// coStoredName is contact id's stored name column (the full name).
func coStoredName(t *testing.T, env *msttest.Env, id int64) string {
	t.Helper()
	ctx := context.Background()
	var name string
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, "SELECT name FROM contact WHERE id = ?", id).Scan(&name)
	}); err != nil {
		t.Fatalf("name of contact %d: %v", id, err)
	}
	return name
}

// TestContact_OwnerLinkChecked pins CD-170 on CreateContact: an unknown or
// disabled party or branch is refused with its own text on its own field,
// before the FK, and nothing is written (no contact, email, phone or audit
// entry, and the owner's primary contact keeps its flag).
func TestContact_OwnerLinkChecked(t *testing.T) {
	o := newCoOwners(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name              string
		partyID, branchID int64
		field, msg        string // empty: saved
	}{
		{"unknown party", coUnknownOwner, 0, "party_id", coPartyNotFoundMsg},
		{"disabled party SHREE", o.shree, 0, "party_id", coPartyDisabledMsg},
		{"unknown branch", 0, coUnknownOwner, "branch_id", coBranchNotFoundMsg},
		{"disabled branch BLR", 0, o.blr, "branch_id", coBranchDisabledMsg},
		{"enabled party BHARAT", o.bharat, 0, "", ""},
		{"enabled branch PUNE", 0, o.pune, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, audits := ctSnapshot(t, o.env), coAuditCount(t, o.env)
			saved, err := o.s.CreateContact(ctx, o.env.Kavya, coIn(tc.partyID, tc.branchID))
			if tc.msg == "" {
				if err != nil {
					t.Fatalf("CreateContact(Asha Rao, %s) = %v; want nil", tc.name, err)
				}
				if c := coGet(t, o.env, o.s, saved.ID); c.PartyID != tc.partyID || c.BranchID != tc.branchID || !c.Primary {
					t.Errorf("saved contact {PartyID %d, BranchID %d, Primary %v}; want {%d, %d, true}",
						c.PartyID, c.BranchID, c.Primary, tc.partyID, tc.branchID)
				}
				return
			}
			if f := contactWantOnly(t, err, tc.msg); f != tc.field {
				t.Errorf("CreateContact(Asha Rao, %s): field = %q; want %q", tc.name, f, tc.field)
			}
			if after := ctSnapshot(t, o.env); after != before {
				t.Errorf("refused create changed the contact tables\nbefore:\n%s\nafter:\n%s", before, after)
			}
			if n := coAuditCount(t, o.env); n != audits {
				t.Errorf("refused create: Contact audit entries = %d; want %d", n, audits)
			}
		})
	}
	if c := coGet(t, o.env, o.s, o.sunil); !c.Primary {
		t.Errorf("after the refusals: Sunil Mehta (SHREE) Primary = false; want true")
	}
	if c := coGet(t, o.env, o.s, o.anil); !c.Primary {
		t.Errorf("after the refusals: Anil Kumar (BLR) Primary = false; want true")
	}
}

// TestContact_EditUnderDisabledOwner: CD-170's disabled-owner check applies
// when a contact is created (its owner never changes), not when an existing
// contact of a disabled party or branch is edited, as for addresses.
func TestContact_EditUnderDisabledOwner(t *testing.T) {
	o := newCoOwners(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name              string
		id                int64
		version           int
		partyID, branchID int64
		first             string
	}{
		{"Sunil Mehta of disabled SHREE", o.sunil, o.sunilVersion, o.shree, 0, "Sunil"},
		{"Anil Kumar of disabled BLR", o.anil, o.anilVersion, 0, o.blr, "Anil"},
	} {
		in := address.ContactInput{PartyID: tc.partyID, BranchID: tc.branchID, FirstName: tc.first, LastName: "Rao"}
		if _, err := o.s.UpdateContact(ctx, o.env.Kavya, tc.id, tc.version, in); err != nil {
			t.Errorf("UpdateContact(%s, last name Rao) = %v; want nil", tc.name, err)
			continue
		}
		if c := coGet(t, o.env, o.s, tc.id); c.LastName != "Rao" {
			t.Errorf("after UpdateContact(%s): LastName = %q; want %q", tc.name, c.LastName, "Rao")
		}
	}
}

// TestContact_OwnerChangeField: a refused owner change (CD-190) is reported
// on the stored owner's field, party_id or branch_id, as for addresses.
func TestContact_OwnerChangeField(t *testing.T) {
	o := newCoOwners(t)
	ctx := context.Background()
	puneContact, err := o.s.CreateContact(ctx, o.env.Kavya, coIn(0, o.pune))
	if err != nil {
		t.Fatalf("CreateContact(Asha Rao, PUNE): %v", err)
	}
	bharatContact, err := o.s.CreateContact(ctx, o.env.Kavya, coIn(o.bharat, 0))
	if err != nil {
		t.Fatalf("CreateContact(Asha Rao, BHARAT): %v", err)
	}
	for _, tc := range []struct {
		name              string
		id                int64
		partyID, branchID int64
		field             string
	}{
		{"PUNE's contact to branch BLR", puneContact.ID, 0, o.blr, "branch_id"},
		{"PUNE's contact to party BHARAT", puneContact.ID, o.bharat, 0, "branch_id"},
		{"BHARAT's contact to party SHREE", bharatContact.ID, o.shree, 0, "party_id"},
		{"BHARAT's contact to branch PUNE", bharatContact.ID, 0, o.pune, "party_id"},
	} {
		before := ctSnapshot(t, o.env)
		_, err := o.s.UpdateContact(ctx, o.env.Kavya, tc.id, 1, coIn(tc.partyID, tc.branchID))
		if f := contactWantOnly(t, err, ctOwnerMsg); f != tc.field {
			t.Errorf("UpdateContact(%s): field = %q; want %q", tc.name, f, tc.field)
		}
		if after := ctSnapshot(t, o.env); after != before {
			t.Errorf("UpdateContact(%s) refused but changed the contact tables", tc.name)
		}
	}
}

// TestContact_TxMethodsCheckRights: each Tx write checks the caller's right
// itself, since a Tx caller has no Write wrapper to do it. Priya holds only
// read on Contact (msttest), so each is refused with auth.ErrForbidden and
// nothing changes.
func TestContact_TxMethodsCheckRights(t *testing.T) {
	env := msttest.New(t)
	ctx := context.Background()
	s := address.New(env.Kit)
	shree := contactParty(t, env, "SHREE", "Shree Steel Traders")
	sunil := createContact(t, env, s, shree, "Sunil", "Mehta", true)
	kiran := createContact(t, env, s, shree, "Kiran", "Joshi", false)

	for _, tc := range []struct {
		name string
		run  func(tx *db.Tx) error
	}{
		{"CreateContactTx", func(tx *db.Tx) error {
			_, err := s.CreateContactTx(ctx, tx, env.Priya, coIn(shree, 0))
			return err
		}},
		{"UpdateContactTx", func(tx *db.Tx) error {
			_, err := s.UpdateContactTx(ctx, tx, env.Priya, kiran, 1,
				address.ContactInput{PartyID: shree, FirstName: "Kiran", LastName: "Rao", Primary: true})
			return err
		}},
		{"SetContactDisabledTx", func(tx *db.Tx) error {
			return s.SetContactDisabledTx(ctx, tx, env.Priya, sunil, 1, true)
		}},
		{"DeleteContactTx", func(tx *db.Tx) error {
			return s.DeleteContactTx(ctx, tx, env.Priya, kiran, 1)
		}},
		{"SetPrimaryContactTx", func(tx *db.Tx) error {
			return s.SetPrimaryContactTx(ctx, tx, env.Priya, shree, kiran)
		}},
	} {
		before, audits := ctSnapshot(t, env), coAuditCount(t, env)
		err := env.DB.Write(ctx, tc.run)
		if !errors.Is(err, auth.ErrForbidden) {
			t.Errorf("%s(Priya) = %v; want auth.ErrForbidden", tc.name, err)
		}
		if after := ctSnapshot(t, env); after != before {
			t.Errorf("%s(Priya) refused but changed the contact tables\nbefore:\n%s\nafter:\n%s", tc.name, before, after)
		}
		if n := coAuditCount(t, env); n != audits {
			t.Errorf("%s(Priya): Contact audit entries = %d; want %d", tc.name, n, audits)
		}
	}
}

// TestContact_SavedAsPosted: CreateContact and UpdateContact store what was
// posted, normalised (names trimmed, mobile as +91 and ten digits, landline
// trimmed, emails lower-cased), with the full name in the name column, and
// GetContact reads it all back; Update replaces the emails and phones and
// returns the next version.
func TestContact_SavedAsPosted(t *testing.T) {
	env := msttest.New(t)
	ctx := context.Background()
	s := address.New(env.Kit)
	shree := contactParty(t, env, "SHREE", "Shree Steel Traders")

	saved, err := s.CreateContact(ctx, env.Kavya, address.ContactInput{
		PartyID: shree, FirstName: " Ravi ", MiddleName: " K ", LastName: " Sharma ",
		Emails: []address.EmailInput{{Email: "Ravi@ShreeSteel.test", Primary: true}, {Email: "accounts@shreesteel.test"}},
		Phones: []address.PhoneInput{{Number: "98200 12345", Mobile: true, Primary: true}, {Number: " 022 2345 6789 "}},
	})
	if err != nil {
		t.Fatalf("CreateContact(Ravi K Sharma): %v", err)
	}
	if saved.Version != 1 {
		t.Errorf("CreateContact: Version = %d; want 1", saved.Version)
	}
	want := address.Contact{
		ID: saved.ID, PartyID: shree, FirstName: "Ravi", MiddleName: "K", LastName: "Sharma",
		Emails:  []address.EmailInput{{Email: "ravi@shreesteel.test", Primary: true}, {Email: "accounts@shreesteel.test"}},
		Phones:  []address.PhoneInput{{Number: "+919820012345", Mobile: true, Primary: true}, {Number: "022 2345 6789"}},
		Version: 1,
	}
	if got := coGet(t, env, s, saved.ID); !reflect.DeepEqual(got, want) {
		t.Errorf("after CreateContact: GetContact = %+v; want %+v", got, want)
	}
	if got := coStoredName(t, env, saved.ID); got != "Ravi K Sharma" {
		t.Errorf("after CreateContact: name column = %q; want %q", got, "Ravi K Sharma")
	}

	upd, err := s.UpdateContact(ctx, env.Kavya, saved.ID, 1, address.ContactInput{
		PartyID: shree, FirstName: "Ravi", LastName: " Verma ",
		Emails: []address.EmailInput{{Email: "ravi.verma@shreesteel.test", Primary: true}},
		Phones: []address.PhoneInput{{Number: "022 2345 0000", Primary: true}},
	})
	if err != nil {
		t.Fatalf("UpdateContact(Ravi Verma): %v", err)
	}
	if upd.ID != saved.ID || upd.Version != 2 {
		t.Errorf("UpdateContact = {ID %d, Version %d}; want {%d, 2}", upd.ID, upd.Version, saved.ID)
	}
	want = address.Contact{
		ID: saved.ID, PartyID: shree, FirstName: "Ravi", LastName: "Verma",
		Emails:  []address.EmailInput{{Email: "ravi.verma@shreesteel.test", Primary: true}},
		Phones:  []address.PhoneInput{{Number: "022 2345 0000", Primary: true}},
		Version: 2,
	}
	if got := coGet(t, env, s, saved.ID); !reflect.DeepEqual(got, want) {
		t.Errorf("after UpdateContact: GetContact = %+v; want %+v", got, want)
	}
	if got := coStoredName(t, env, saved.ID); got != "Ravi Verma" {
		t.Errorf("after UpdateContact: name column = %q; want %q", got, "Ravi Verma")
	}
}

// coWantAudit checks contact id's audit entries: actions and changes in
// order, each by Kavya at the test clock.
func coWantAudit(t *testing.T, env *msttest.Env, who string, id int64, want []audit.Entry) {
	t.Helper()
	ctx := context.Background()
	var got []audit.Entry
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		got, err = audit.ForDoc(ctx, tx, coContactDocType, id)
		return err
	}); err != nil {
		t.Fatalf("audit.ForDoc(%s): %v", who, err)
	}
	if len(got) != len(want) {
		t.Fatalf("%s: audit entries = %+v; want %d: %+v", who, got, len(want), want)
	}
	for i, w := range want {
		e := got[i]
		if e.Action != w.Action || e.By != env.Kavya.ID || !e.At.Equal(env.Clock.Now()) {
			t.Errorf("%s entry %d = {Action %s, By %d, At %v}; want {%s, %d, %v}", who, i, e.Action, e.By, e.At, w.Action, env.Kavya.ID, env.Clock.Now())
		}
		if !reflect.DeepEqual(e.Changes, w.Changes) {
			t.Errorf("%s entry %d (%s) changes = %+v; want %+v", who, i, w.Action, e.Changes, w.Changes)
		}
	}
}

// TestContact_Audited: every contact write is audited with what changed:
// create, update (emails and phones listed, the primary marked), set-primary
// on the new and the old primary, and disabling the primary (the disable and
// the cleared flag). Making the primary contact primary again is a no-op: no
// version bump, no entry.
func TestContact_Audited(t *testing.T) {
	env := msttest.New(t)
	ctx := context.Background()
	s := address.New(env.Kit)
	shree := contactParty(t, env, "SHREE", "Shree Steel Traders")
	shreeID := strconvID(shree)
	sunil := createContact(t, env, s, shree, "Sunil", "Mehta", true)

	saved, err := s.CreateContact(ctx, env.Kavya, address.ContactInput{
		PartyID: shree, FirstName: "Ravi", MiddleName: "K", LastName: "Sharma",
		Emails: []address.EmailInput{{Email: "ravi@shreesteel.test", Primary: true}, {Email: "accounts@shreesteel.test"}},
		Phones: []address.PhoneInput{{Number: "+919820012345", Mobile: true, Primary: true}, {Number: "022 2345 6789"}},
	})
	if err != nil {
		t.Fatalf("CreateContact(Ravi K Sharma): %v", err)
	}
	ravi := saved.ID
	if _, err := s.UpdateContact(ctx, env.Kavya, ravi, 1, address.ContactInput{
		PartyID: shree, FirstName: "Ravi", MiddleName: "K", LastName: "Verma",
		Emails: []address.EmailInput{{Email: "ravi@shreesteel.test", Primary: true}},
		Phones: []address.PhoneInput{{Number: "+919820012345", Mobile: true, Primary: true}, {Number: "022 2345 6789"}},
	}); err != nil {
		t.Fatalf("UpdateContact(Ravi K Verma): %v", err)
	}
	setPrimary := func() error {
		return env.DB.Write(ctx, func(tx *db.Tx) error { return s.SetPrimaryContactTx(ctx, tx, env.Kavya, shree, ravi) })
	}
	if err := setPrimary(); err != nil {
		t.Fatalf("SetPrimaryContactTx(Ravi): %v", err)
	}
	audits := coAuditCount(t, env)
	if err := setPrimary(); err != nil {
		t.Fatalf("SetPrimaryContactTx(Ravi) again: %v", err)
	}
	if v := coGet(t, env, s, ravi).Version; v != 3 {
		t.Errorf("after SetPrimaryContactTx on the primary contact: version = %d; want 3 (unchanged)", v)
	}
	if n := coAuditCount(t, env); n != audits {
		t.Errorf("after SetPrimaryContactTx on the primary contact: Contact audit entries = %d; want %d", n, audits)
	}
	if err := s.SetContactDisabled(ctx, env.Kavya, ravi, 3, true); err != nil {
		t.Fatalf("SetContactDisabled(Ravi): %v", err)
	}

	set := func(field, before, after string) audit.Change {
		return audit.Change{Field: field, Before: before, After: after, Op: audit.OpSet}
	}
	coWantAudit(t, env, "Ravi", ravi, []audit.Entry{
		{Action: "create", Changes: []audit.Change{
			set("emails", "", "ravi@shreesteel.test (primary), accounts@shreesteel.test"),
			set("first_name", "", "Ravi"), set("is_primary", "", "false"), set("last_name", "", "Sharma"),
			set("middle_name", "", "K"), set("party_id", "", shreeID),
			set("phones", "", "+919820012345 (mobile) (primary), 022 2345 6789"),
		}},
		{Action: "save", Changes: []audit.Change{
			set("emails", "ravi@shreesteel.test (primary), accounts@shreesteel.test", "ravi@shreesteel.test (primary)"),
			set("last_name", "Sharma", "Verma"),
		}},
		{Action: "save", Changes: []audit.Change{set("is_primary", "false", "true")}},
		{Action: "disable", Changes: []audit.Change{set("disabled", "false", "true")}},
		{Action: "save", Changes: []audit.Change{set("is_primary", "true", "false")}},
	})
	coWantAudit(t, env, "Sunil", sunil, []audit.Entry{
		{Action: "create", Changes: []audit.Change{
			set("first_name", "", "Sunil"), set("is_primary", "", "true"), set("last_name", "", "Mehta"), set("party_id", "", shreeID),
		}},
		{Action: "save", Changes: []audit.Change{set("is_primary", "true", "false")}},
	})
}

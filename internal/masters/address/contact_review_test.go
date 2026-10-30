package address_test

import (
	"context"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// MST Task 7 review tests: blank-owner updates (AD ruling under CD-190),
// stale versions on every contact write path (CD-94, CD-180 D4) and blank
// phone entries (AD ruling on landlines).
const (
	crStaleSunilMsg = "Someone else changed Contact Sunil Mehta after you opened it. Reload it and make your changes again." // CD-94
	crOtherEmail    = "sunil.mehta" + ctEmailDomain                                                                          // another user's edit
	crSunilMobile   = "+919820012345"                                                                                        // Sunil's stored mobile
)

// TestContact_UpdateKeepsOwner mirrors TestAddress_UpdateKeepsOwner (AD
// ruling under CD-190): an UpdateContact that posts no owner (PartyID 0 and
// BranchID 0) keeps the stored one, for a party-owned and a branch-owned
// contact; with Primary set it makes the contact primary and clears the
// owner's old primary.
func TestContact_UpdateKeepsOwner(t *testing.T) {
	for _, kind := range []string{ctPartyKind, ctBranchKind} {
		t.Run(kind, func(t *testing.T) {
			cs := newCtSetup(t, kind)
			r := cs.r
			wantOwner := func(when string, id int64) {
				t.Helper()
				c, ok := ctGet(t, r.env.DB, id)
				if !ok {
					t.Fatalf("%s: contact %d not found", when, id)
				}
				if c.partyID != cs.o.partyID || c.branchID != cs.o.branchID {
					t.Errorf("%s: contact %d {party_id %d, branch_id %d}; want {%d, %d}",
						when, id, c.partyID, c.branchID, cs.o.partyID, cs.o.branchID)
				}
			}

			sunil := cs.sunilIn
			sunil.PartyID, sunil.BranchID = 0, 0
			sunil.MiddleName = "K"
			r.must("Update Sunil Mehta (no owner, middle name K)", r.update(cs.sunil, sunil))
			wantOwner("after blank-owner Update(Sunil)", cs.sunil)
			if got := coStoredName(t, r.env, cs.sunil); got != "Sunil K Mehta" {
				t.Errorf("after blank-owner Update(Sunil): name = %q; want %q", got, "Sunil K Mehta")
			}
			r.wantPrimaries("after blank-owner Update(Sunil, primary)", cs.o, cs.sunil)

			kiran := cs.kiranIn
			kiran.PartyID, kiran.BranchID = 0, 0
			kiran.Primary = true
			r.must("Update Kiran Joshi (no owner, primary)", r.update(cs.kiran, kiran))
			wantOwner("after blank-owner Update(Kiran, primary)", cs.kiran)
			wantOwner("after blank-owner Update(Kiran, primary)", cs.sunil)
			r.wantPrimaries("after blank-owner Update(Kiran, primary)", cs.o, cs.kiran)
			if got := ctContacts(t, r.env.DB, cs.other); len(got) != 0 {
				t.Errorf("after blank-owner updates: %s contacts = %+v; want none", cs.other.name, got)
			}
		})
	}
}

// TestContact_StaleVersionOnEveryWritePath pins CD-94 and CD-180 D4 on
// contacts: a write posting Sunil Mehta's version 1 after another user
// changed him gets DOC's stale 409 and changes nothing. UpdateContact with
// Primary after another user disabled him is stale, not the enable-first
// refusal. SetPrimaryContactTx takes no version, so it has no stale case.
func TestContact_StaleVersionOnEveryWritePath(t *testing.T) {
	type path struct {
		name  string
		other func(cs *ctSetup) error // another user's change to Sunil (v1 -> v2)
		stale func(cs *ctSetup) error // the write at version 1
	}
	ctx := context.Background()
	otherEdit := func(cs *ctSetup) error {
		in := cs.sunilIn
		in.Emails = []address.EmailInput{{Email: crOtherEmail, Primary: true}}
		_, err := cs.r.s.UpdateContact(ctx, cs.r.env.Admin, cs.sunil, 1, in)
		return err
	}
	paths := []path{
		{
			name: "UpdateContact primary after disable",
			other: func(cs *ctSetup) error {
				return cs.r.s.SetContactDisabled(ctx, cs.r.env.Admin, cs.sunil, 1, true)
			},
			stale: func(cs *ctSetup) error {
				in := cs.sunilIn
				in.Primary = true
				_, err := cs.r.s.UpdateContact(ctx, cs.r.env.Kavya, cs.sunil, 1, in)
				return err
			},
		},
		{
			name:  "UpdateContact",
			other: otherEdit,
			stale: func(cs *ctSetup) error {
				in := cs.sunilIn
				in.MiddleName = "K"
				_, err := cs.r.s.UpdateContact(ctx, cs.r.env.Kavya, cs.sunil, 1, in)
				return err
			},
		},
		{
			name:  "SetContactDisabled",
			other: otherEdit,
			stale: func(cs *ctSetup) error {
				return cs.r.s.SetContactDisabled(ctx, cs.r.env.Kavya, cs.sunil, 1, true)
			},
		},
		{
			name:  "DeleteContact primary",
			other: otherEdit,
			stale: func(cs *ctSetup) error {
				return cs.r.s.DeleteContact(ctx, cs.r.env.Kavya, cs.sunil, 1)
			},
		},
	}
	for _, kind := range []string{ctPartyKind, ctBranchKind} {
		for _, p := range paths {
			t.Run(kind+"/"+p.name, func(t *testing.T) {
				cs := newCtSetup(t, kind)
				if c, ok := ctGet(t, cs.r.env.DB, cs.sunil); !ok || c.version != 1 || !c.primary {
					t.Fatalf("set-up: Sunil Mehta = %+v (found %v); want version 1, primary", c, ok)
				}
				if err := p.other(cs); err != nil {
					t.Fatalf("another user's change to Sunil Mehta: %v", err)
				}
				before := ctSnapshot(t, cs.r.env)
				err := p.stale(cs)
				m5WantStale(t, err, crStaleSunilMsg)
				if after := ctSnapshot(t, cs.r.env); after != before {
					t.Errorf("stale %s changed the contact tables:\nbefore:\n%s\nafter:\n%s", p.name, before, after)
				}
			})
		}
	}
}

// crBlankPhones counts contact id's stored phones whose number is blank.
func crBlankPhones(t *testing.T, r *ctRun, id int64) int {
	t.Helper()
	return len(ctQueryIDs(t, r.env.DB, "SELECT id FROM contact_phone WHERE contact_id = ? AND trim(number) = ''", id))
}

// crWithBlanks is in with an empty and a spaces-only phone entry (mobile or
// landline) added after its real mobile.
func crWithBlanks(in address.ContactInput, mobile bool) address.ContactInput {
	in.Phones = append(slices.Clone(in.Phones),
		address.PhoneInput{Number: "", Mobile: mobile},
		address.PhoneInput{Number: "   ", Mobile: mobile},
	)
	return in
}

// TestContact_BlankPhoneDropped pins the AD ruling on landlines: a phone
// entry with a blank number (landline or mobile, empty or spaces) is
// dropped on CreateContact and UpdateContact: not stored and not refused.
func TestContact_BlankPhoneDropped(t *testing.T) {
	for _, tc := range []struct {
		name, kind string
		mobile     bool
	}{
		{"party/landline", ctPartyKind, false},
		{"party/mobile", ctPartyKind, true},
		{"branch/landline", ctBranchKind, false},
		{"branch/mobile", ctBranchKind, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kind := tc.kind
			r := newCtRun(t)
			o := ctOwner{name: "party SHREE"}
			if kind == ctBranchKind {
				o = ctOwner{name: "branch PUNE", branchID: m5Branch(t, r.env)}
			} else {
				o.partyID = contactParty(t, r.env, "SHREE", "Shree Steel Traders")
			}
			want := []address.PhoneInput{{Number: crSunilMobile, Mobile: true, Primary: true}}
			wantPhones := func(when string, id int64) {
				t.Helper()
				if n := crBlankPhones(t, r, id); n != 0 {
					t.Fatalf("%s: %d contact_phone rows with a blank number; want 0", when, n)
				}
				var got []address.PhoneInput
				if err := r.env.DB.Read(context.Background(), func(tx *db.Tx) error {
					c, err := r.s.GetContact(context.Background(), tx, id)
					got = c.Phones
					return err
				}); err != nil {
					t.Fatalf("%s: GetContact: %v", when, err)
				}
				if !slices.Equal(got, want) {
					t.Fatalf("%s: phones = %+v; want %+v", when, got, want)
				}
			}

			var id int64
			in := ctIn(o, "Sunil", "Mehta", crSunilMobile, true)
			r.must("Create Sunil Mehta with blank phone entries", r.create(&id, crWithBlanks(in, tc.mobile)))
			wantPhones("after CreateContact", id)

			in.MiddleName = "K"
			r.must("Update Sunil Mehta with blank phone entries", r.update(id, crWithBlanks(in, tc.mobile)))
			wantPhones("after UpdateContact", id)
		})
	}
}

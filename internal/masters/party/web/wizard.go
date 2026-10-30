package partyweb

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	mstweb "github.com/vardhan/internaltools/internal/masters/mstkit/web"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// noOwnerYet stands in for the party a draft's rows will belong to, so the pure
// checks run on them: the party has no id until the save, which sets the real
// one. address.Validate only tests that an owner is set.
const noOwnerYet = 1

// draft is the wizard's one save (CD-50): the party and its rows, a row's Line
// being its index plus one.
type draft struct {
	Party     party.Input
	Addresses []address.Input
	Contacts  []address.ContactInput
}

// toDraft is the session's values as a draft.
func toDraft(v map[string]string) (draft, []doctype.FieldError) {
	in, errs := toInput(v)
	d := draft{Party: in}
	for _, row := range mstweb.WizardRows(v, stepAddresses) {
		d.Addresses = append(d.Addresses, addressInput(row))
	}
	for _, row := range mstweb.WizardRows(v, stepContacts) {
		d.Contacts = append(d.Contacts, contactInput(row))
	}
	return d, errs
}

// contactInput is a wizard row: one email and one mobile, as the import.
func contactInput(row map[string]string) address.ContactInput {
	in := address.ContactInput{
		FirstName: row["first_name"], MiddleName: row["middle_name"], LastName: row["last_name"],
		Primary: row["is_primary"] == "1",
	}
	if strings.TrimSpace(row["email"]) != "" {
		in.Emails = []address.EmailInput{{Email: row["email"], Primary: true}}
	}
	if strings.TrimSpace(row["mobile"]) != "" {
		in.Phones = []address.PhoneInput{{Number: row["mobile"], Mobile: true, Primary: true}}
	}
	return in
}

// validateDraft is the rules that need no database, for a step's Next.
func validateDraft(d draft) (draft, error) {
	var es []doctype.FieldError
	_, err := party.Validate(d.Party)
	_ = refusal(&es, 0, err) // pure checks refuse only with field errors
	for i, in := range d.Addresses {
		in.PartyID = noOwnerYet
		_, err := address.Validate(in)
		_ = refusal(&es, i+1, err)
	}
	for i, in := range d.Contacts {
		in.PartyID = noOwnerYet
		_, err := address.ValidateContact(in)
		_ = refusal(&es, i+1, err)
	}
	if len(es) > 0 {
		return d, &mstkit.ValidationError{Fields: es}
	}
	return d, nil
}

// refusal adds a row's field errors to es at line, an address's or a contact's
// list being its one box here. Anything else, a 403 or a failed write, comes
// back to stop the save.
func refusal(es *[]doctype.FieldError, line int, err error) error {
	var he doctype.HTTPError
	if !errors.As(err, &he) || he.Status() != http.StatusUnprocessableEntity || len(he.FieldErrors()) == 0 {
		return err
	}
	for _, e := range he.FieldErrors() {
		e.Line = line
		switch e.Field {
		case "emails":
			e.Field = "email"
		case "phones":
			e.Field = "mobile"
		}
		*es = append(*es, e)
	}
	return nil
}

// wizardSaver is the wizard's Save.
type wizardSaver struct {
	env       mstkit.Env
	parties   *party.Service
	addresses *address.Service
}

// save writes the party, then its addresses, then its contacts, in one write
// (CD-50, CD-94); the party goes first, so CheckCodeFree serialises a double
// submit. A row's refusal doesn't stop the others: every row's errors come back
// together, on their own Line, and the returned error rolls everything back.
func (s wizardSaver) save(ctx context.Context, p auth.Principal, d draft) (mstkit.Saved, error) {
	var saved mstkit.Saved
	var warnings []string
	err := s.env.Write(ctx, p, party.Kind, auth.RightCreate, func(tx *db.Tx) error {
		var err error
		if saved, err = s.parties.CreateTx(ctx, tx, p, d.Party); err != nil {
			return err
		}
		warnings = saved.Warnings
		var es []doctype.FieldError
		for _, i := range billingFirst(d.Addresses) {
			in := d.Addresses[i]
			in.PartyID = saved.ID
			added, err := s.addresses.CreateTx(ctx, tx, p, in)
			if err := refusal(&es, i+1, err); err != nil {
				return err
			}
			warnings = append(warnings, added.Warnings...)
		}
		es = withoutFollowOns(d.Addresses, es)
		for i, in := range d.Contacts {
			in.PartyID = saved.ID
			_, err := s.addresses.CreateContactTx(ctx, tx, p, in)
			if err := refusal(&es, i+1, err); err != nil {
				return err
			}
		}
		if len(es) == 0 {
			return nil
		}
		slices.SortStableFunc(es, func(a, b doctype.FieldError) int { return cmp.Compare(a.Line, b.Line) })
		return &mstkit.ValidationError{Fields: es}
	})
	saved.Warnings = mergeWarnings(warnings)
	return saved, err
}

// billingFirst is the indexes of rows with the billing rows first: a registered
// party's first address must be a billing one (CD-180 D1), whatever the order
// on the page.
func billingFirst(rows []address.Input) []int {
	order := make([]int, len(rows))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		switch {
		case rows[a].IsBilling == rows[b].IsBilling:
			return 0
		case rows[a].IsBilling:
			return -1
		}
		return 1
	})
	return order
}

// withoutFollowOns drops from es, the address rows' errors in save order, those
// that hold only because of an earlier row. A refused billing row was never
// written, so the rows after it look like the party's first: saying D1, or the
// two GSTIN errors of a first billing row, would be false. D1 is about the
// draft, so a row that shows it counts the same and it shows once (CD-180).
// Billing rows save first, so a real error on one has no such row above it.
func withoutFollowOns(rows []address.Input, es []doctype.FieldError) []doctype.FieldError {
	cause := 0 // the Line that explains the rest
	var kept []doctype.FieldError
	for _, e := range es {
		needBilling := e.Message == party.MsgNeedPrimaryBilling
		if cause != 0 && e.Line != cause && (needBilling || e.Message == gstin.MsgNeedGSTIN || e.Message == gstin.MsgUnregisteredGSTIN) {
			continue
		}
		if cause == 0 && (needBilling || rows[e.Line-1].IsBilling) {
			cause = e.Line
		}
		kept = append(kept, e)
	}
	return kept
}

// mergeWarnings is every record's warnings once. The party's same-PAN warning
// and an address's differ only in the address's "; consider adding …", so they
// are one warning, and the longer stays.
func mergeWarnings(all []string) []string {
	key := func(s string) string {
		short, _, _ := strings.Cut(strings.TrimSuffix(s, "."), ";")
		return short
	}
	var out []string
	for _, w := range all {
		i := slices.IndexFunc(out, func(o string) bool { return key(o) == key(w) })
		switch {
		case i < 0:
			out = append(out, w)
		case len(w) > len(out[i]):
			out[i] = w
		}
	}
	return out
}

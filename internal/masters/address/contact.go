package address

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strconv"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/address/dbq"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// ContactDocType is the Contact registry name.
const ContactDocType = "Contact"

// ContactKind is the Contact master. Its table's name column holds the full
// name, which is what messages show.
var ContactKind = mstkit.Kind{DocType: ContactDocType, Label: "Contact", Table: "contact"}

const (
	msgContactName   = "Contact name is required."                                                 // MST-062
	msgEmail         = "Enter a valid email address, e.g. accounts@shreesteel.test."               // MST-063
	msgMobile        = "Enter a 10-digit Indian mobile number starting 6–9, optionally with +91."  // MST-064
	msgOneEmail      = "Only one email can be primary."                                            // MST-065
	msgContactBelong = "That contact belongs to another party. Pick one of this party's contacts." // MST-051
	// CD-190.
	msgOnePhone        = "Only one phone can be primary."
	msgContactEnable   = "Enable this contact before making it primary."
	msgContactMoved    = "The owner of a contact can't be changed."
	msgContactOwner    = "A contact must belong to one party or one branch."
	msgContactNotFound = "Contact not found. Pick one from the list."

	phoneKindMobile   = "mobile"
	phoneKindLandline = "phone"
)

var indianMobile = regexp.MustCompile(`^[6-9][0-9]{9}$`)

func init() {
	mstkit.RegisterMaster(doctype.DocType{
		Name:        ContactDocType,
		Label:       "Contact",
		PluralLabel: "Contacts",
		Table:       ContactKind.Table,
		Importable:  true,
		Fields: []doctype.Field{
			{Name: "party_id", Label: "Party", Column: "party_id", Type: doctype.Link, Link: party.DocType},
			{Name: "branch_id", Label: "Branch", Column: "branch_id", Type: doctype.Link, Link: company.DocType},
			{Name: "name", Label: "Name", Column: "name", Type: doctype.Text, ReadOnly: true, ListPriority: 1}, // full name
			{Name: "first_name", Label: "First name", Column: "first_name", Type: doctype.Text, Required: true},
			{Name: "middle_name", Label: "Middle name", Column: "middle_name", Type: doctype.Text},
			{Name: "last_name", Label: "Last name", Column: "last_name", Type: doctype.Text},
			{Name: "is_primary", Label: "Primary", Column: "is_primary", Type: doctype.Check},
			{Name: "disabled", Label: "Disabled", Column: "disabled", Type: doctype.Check, Filterable: true, ListPriority: 3},
		},
		SearchFields: []string{"name"},
		DefaultSort:  []doctype.SortKey{{Field: "name"}}, // list adds id as the tie-breaker
	}, mstkit.StandardLookup(ContactKind))
}

// Contact is one Contact record with its emails and phones.
type Contact struct {
	ID                              int64
	PartyID, BranchID               int64 // exactly one is set
	FirstName, MiddleName, LastName string
	Emails                          []EmailInput
	Phones                          []PhoneInput
	Primary, Disabled               bool
	Version                         int
}

// EmailInput is one email of a contact.
type EmailInput struct {
	Email   string
	Primary bool
}

// PhoneInput is one phone number of a contact; a mobile number is stored as
// +91 and ten digits (MST-064).
type PhoneInput struct {
	Number          string
	Mobile, Primary bool
}

// ContactInput is what CreateContact and UpdateContact accept. Primary makes
// the contact its owner's primary contact, clearing the previous one
// (MST-066).
type ContactInput struct {
	PartyID, BranchID               int64
	FirstName, MiddleName, LastName string
	Emails                          []EmailInput
	Phones                          []PhoneInput
	Primary                         bool
}

// FullName is "first middle last" without the blank parts (MST-062).
func FullName(c Contact) string {
	var parts []string
	for _, s := range []string{c.FirstName, c.MiddleName, c.LastName} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " ")
}

// NormalizeEmail trims and lower-cases a valid email address; anything else
// is MST-063's error.
func NormalizeEmail(s string) (string, error) {
	s = strings.TrimSpace(s)
	a, err := mail.ParseAddress(s)
	if err != nil || a.Name != "" || a.Address != s || strings.Count(s, "@") != 1 ||
		!strings.Contains(s[strings.Index(s, "@"):], ".") {
		return "", errors.New(msgEmail)
	}
	return strings.ToLower(s), nil
}

// NormalizeMobile stores an Indian mobile number as +91 and ten digits:
// spaces and dashes are dropped, then a +91 or a single leading 0 (MST-064).
func NormalizeMobile(s string) (string, error) {
	s = strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(s))
	if rest, ok := strings.CutPrefix(s, "+91"); ok {
		s = rest
	} else {
		s = strings.TrimPrefix(s, "0")
	}
	if !indianMobile.MatchString(s) {
		return "", errors.New(msgMobile)
	}
	return "+91" + s, nil
}

// ValidateContact normalises in and checks the rules that need no database:
// one owner, a first name, email and mobile formats, and at most one primary
// email and one primary phone. Phone entries with a blank number are dropped.
func ValidateContact(in ContactInput) (ContactInput, error) {
	var es mstkit.Errors
	if (in.PartyID == 0) == (in.BranchID == 0) {
		es.Add("party_id", msgContactOwner)
	}
	in.FirstName, in.MiddleName, in.LastName = strings.TrimSpace(in.FirstName), strings.TrimSpace(in.MiddleName), strings.TrimSpace(in.LastName)
	if in.FirstName == "" {
		es.Add("first_name", msgContactName)
	}
	emails, primaries := make([]EmailInput, len(in.Emails)), 0
	for i, e := range in.Emails {
		addr, err := NormalizeEmail(e.Email)
		if err != nil {
			es.Add("emails", err.Error())
		}
		emails[i] = EmailInput{Email: addr, Primary: e.Primary}
		if e.Primary {
			primaries++
		}
	}
	if primaries > 1 {
		es.Add("emails", msgOneEmail)
	}
	var phones []PhoneInput
	primaries = 0
	for _, ph := range in.Phones {
		number := strings.TrimSpace(ph.Number)
		if number == "" {
			continue // a blank row is dropped, not refused
		}
		if ph.Mobile {
			var err error
			if number, err = NormalizeMobile(number); err != nil {
				es.Add("phones", err.Error())
			}
		}
		phones = append(phones, PhoneInput{Number: number, Mobile: ph.Mobile, Primary: ph.Primary})
		if ph.Primary {
			primaries++
		}
	}
	if primaries > 1 {
		es.Add("phones", msgOnePhone)
	}
	if err := es.Err(); err != nil {
		return ContactInput{}, err
	}
	in.Emails, in.Phones = emails, phones
	return in, nil
}

func contactRef(id int64) *auth.RecordRef { return &auth.RecordRef{DocType: ContactDocType, ID: id} }

// CreateContact adds a contact.
func (s *Service) CreateContact(ctx context.Context, p auth.Principal, in ContactInput) (mstkit.Saved, error) {
	var saved mstkit.Saved
	err := s.env.Write(ctx, p, ContactKind, auth.RightCreate, func(tx *db.Tx) error {
		var err error
		saved, err = s.CreateContactTx(ctx, tx, p, in)
		return err
	})
	return saved, err
}

// CreateContactTx is CreateContact inside tx.
func (s *Service) CreateContactTx(ctx context.Context, tx *db.Tx, p auth.Principal, in ContactInput) (mstkit.Saved, error) {
	if err := s.env.Auth.Can(ctx, p, ContactDocType, auth.RightCreate, nil); err != nil {
		return mstkit.Saved{}, err
	}
	in, err := ValidateContact(in)
	if err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.checkContactOwner(ctx, tx, in); err != nil {
		return mstkit.Saved{}, err
	}
	if in.Primary {
		if err := s.clearOtherPrimaryContacts(ctx, tx, p, in.PartyID, in.BranchID, 0); err != nil {
			return mstkit.Saved{}, err
		}
	}
	now := types.TimestampOf(s.env.Clock.Now()).String()
	c := contactFromInput(in)
	id, err := dbq.New(tx.SQL()).InsertContact(ctx, dbq.InsertContactParams{
		PartyID: nullID(in.PartyID), BranchID: nullID(in.BranchID), Name: FullName(c),
		FirstName: in.FirstName, MiddleName: in.MiddleName, LastName: in.LastName, IsPrimary: flag(in.Primary),
		CreatedAt: now, CreatedBy: userID(p), UpdatedAt: now, UpdatedBy: userID(p),
	})
	if err != nil {
		return mstkit.Saved{}, err
	}
	if err := insertContactLists(ctx, tx, id, in); err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.env.Audit(ctx, tx, p, ContactKind, id, "create", nil, contactAuditFields(c)); err != nil {
		return mstkit.Saved{}, err
	}
	if err := checkContacts(ctx, tx, in.PartyID, in.BranchID); err != nil {
		return mstkit.Saved{}, err
	}
	return mstkit.Saved{ID: id, Version: 1}, nil
}

// UpdateContact replaces the contact's fields, emails and phones.
func (s *Service) UpdateContact(ctx context.Context, p auth.Principal, id int64, version int, in ContactInput) (mstkit.Saved, error) {
	var saved mstkit.Saved
	err := s.env.Write(ctx, p, ContactKind, auth.RightWrite, func(tx *db.Tx) error {
		var err error
		saved, err = s.UpdateContactTx(ctx, tx, p, id, version, in)
		return err
	})
	return saved, err
}

// UpdateContactTx is UpdateContact inside tx, at the version the page posted
// (CD-94). The owner is fixed once saved (CD-190); a blank owner keeps the
// stored one.
func (s *Service) UpdateContactTx(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int, in ContactInput) (mstkit.Saved, error) {
	if err := s.env.Auth.Can(ctx, p, ContactDocType, auth.RightWrite, contactRef(id)); err != nil {
		return mstkit.Saved{}, err
	}
	old, err := s.GetContact(ctx, tx, id)
	if err != nil {
		return mstkit.Saved{}, err
	}
	switch {
	case in.PartyID == 0 && in.BranchID == 0:
		in.PartyID, in.BranchID = old.PartyID, old.BranchID
	case in.PartyID != old.PartyID || in.BranchID != old.BranchID:
		var es mstkit.Errors
		field := "party_id"
		if old.BranchID != 0 {
			field = "branch_id"
		}
		es.Add(field, msgContactMoved)
		return mstkit.Saved{}, es.Err()
	}
	in, err = ValidateContact(in)
	if err != nil {
		return mstkit.Saved{}, err
	}
	if err := mstkit.CheckVersion(ctx, tx, ContactKind, id, version); err != nil {
		return mstkit.Saved{}, err
	}
	if old.Disabled && in.Primary {
		var es mstkit.Errors
		es.Add("disabled", msgContactEnable)
		return mstkit.Saved{}, es.Err()
	}
	if in.Primary {
		if err := s.clearOtherPrimaryContacts(ctx, tx, p, in.PartyID, in.BranchID, id); err != nil {
			return mstkit.Saved{}, err
		}
	}
	c := contactFromInput(in)
	q := dbq.New(tx.SQL())
	if err := q.UpdateContact(ctx, dbq.UpdateContactParams{
		Name: FullName(c), FirstName: in.FirstName, MiddleName: in.MiddleName, LastName: in.LastName, IsPrimary: flag(in.Primary),
		UpdatedAt: types.TimestampOf(s.env.Clock.Now()).String(), UpdatedBy: userID(p), ID: id, Version: int64(version),
	}); err != nil {
		return mstkit.Saved{}, err
	}
	if err := q.DeleteContactEmails(ctx, id); err != nil {
		return mstkit.Saved{}, err
	}
	if err := q.DeleteContactPhones(ctx, id); err != nil {
		return mstkit.Saved{}, err
	}
	if err := insertContactLists(ctx, tx, id, in); err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.env.Audit(ctx, tx, p, ContactKind, id, "save", contactAuditFields(old), contactAuditFields(c)); err != nil {
		return mstkit.Saved{}, err
	}
	if err := checkContacts(ctx, tx, in.PartyID, in.BranchID); err != nil {
		return mstkit.Saved{}, err
	}
	return mstkit.Saved{ID: id, Version: version + 1}, nil
}

// SetContactDisabled disables or enables the contact.
func (s *Service) SetContactDisabled(ctx context.Context, p auth.Principal, id int64, version int, disabled bool) error {
	return s.env.Write(ctx, p, ContactKind, auth.RightWrite, func(tx *db.Tx) error {
		return s.SetContactDisabledTx(ctx, tx, p, id, version, disabled)
	})
}

// SetContactDisabledTx is SetContactDisabled inside tx. Disabling the primary
// contact clears its flag and promotes nothing (CD-190).
func (s *Service) SetContactDisabledTx(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int, disabled bool) error {
	if err := s.env.Auth.Can(ctx, p, ContactDocType, auth.RightWrite, contactRef(id)); err != nil {
		return err
	}
	c, err := s.GetContact(ctx, tx, id)
	if err != nil {
		return err
	}
	if err := s.env.SetDisabledTx(ctx, tx, p, ContactKind, id, version, disabled); err != nil {
		return err
	}
	if disabled && c.Primary {
		if err := s.clearPrimaryContact(ctx, tx, p, id); err != nil {
			return err
		}
	}
	return checkContacts(ctx, tx, c.PartyID, c.BranchID)
}

// DeleteContact deletes an unreferenced contact with its emails and phones.
func (s *Service) DeleteContact(ctx context.Context, p auth.Principal, id int64, version int) error {
	return s.env.Write(ctx, p, ContactKind, auth.RightDelete, func(tx *db.Tx) error {
		return s.DeleteContactTx(ctx, tx, p, id, version)
	})
}

// DeleteContactTx is DeleteContact inside tx. Deleting the primary contact
// promotes nothing (CD-190).
func (s *Service) DeleteContactTx(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int) error {
	if err := s.env.Auth.Can(ctx, p, ContactDocType, auth.RightDelete, contactRef(id)); err != nil {
		return err
	}
	c, err := s.GetContact(ctx, tx, id)
	if err != nil {
		return err
	}
	if err := s.env.DeleteTx(ctx, tx, p, ContactKind, id, version); err != nil {
		return err
	}
	return checkContacts(ctx, tx, c.PartyID, c.BranchID)
}

// SetPrimaryContactTx makes the contact partyID's primary contact, clearing
// the previous one first (MST-066). The contact must be one of the party's
// (MST-051) and enabled (CD-190).
func (s *Service) SetPrimaryContactTx(ctx context.Context, tx *db.Tx, p auth.Principal, partyID, contactID int64) error {
	if err := s.env.Auth.Can(ctx, p, ContactDocType, auth.RightWrite, contactRef(contactID)); err != nil {
		return err
	}
	var es mstkit.Errors
	c, err := s.GetContact(ctx, tx, contactID)
	switch {
	case errors.Is(err, doctype.ErrNotFound):
		es.Add("contact_id", msgContactNotFound)
		return es.Err()
	case err != nil:
		return err
	case c.PartyID != partyID:
		es.Add("contact_id", msgContactBelong)
		return es.Err()
	case c.Disabled:
		es.Add("disabled", msgContactEnable)
		return es.Err()
	case c.Primary:
		return nil
	}
	if err := s.clearOtherPrimaryContacts(ctx, tx, p, partyID, 0, contactID); err != nil {
		return err
	}
	if err := dbq.New(tx.SQL()).SetPrimaryContact(ctx, dbq.SetPrimaryContactParams{
		UpdatedAt: types.TimestampOf(s.env.Clock.Now()).String(), UpdatedBy: userID(p), ID: contactID,
	}); err != nil {
		return err
	}
	if err := s.env.Audit(ctx, tx, p, ContactKind, contactID, "save",
		map[string]string{"is_primary": "false"}, map[string]string{"is_primary": "true"}); err != nil {
		return err
	}
	return checkContacts(ctx, tx, partyID, 0)
}

// GetContact reads the contact with its emails and phones; a missing one is
// doctype.ErrNotFound.
func (s *Service) GetContact(ctx context.Context, tx *db.Tx, id int64) (Contact, error) {
	q := dbq.New(tx.SQL())
	r, err := q.GetContact(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Contact{}, doctype.ErrNotFound
	}
	if err != nil {
		return Contact{}, err
	}
	c := Contact{
		ID: r.ID, PartyID: r.PartyID.Int64, BranchID: r.BranchID.Int64,
		FirstName: r.FirstName, MiddleName: r.MiddleName, LastName: r.LastName,
		Primary: r.IsPrimary != 0, Disabled: r.Disabled != 0, Version: int(r.Version),
	}
	emails, err := q.ContactEmails(ctx, id)
	if err != nil {
		return Contact{}, err
	}
	for _, e := range emails {
		c.Emails = append(c.Emails, EmailInput{Email: e.Email, Primary: e.IsPrimary != 0})
	}
	phones, err := q.ContactPhones(ctx, id)
	if err != nil {
		return Contact{}, err
	}
	for _, ph := range phones {
		c.Phones = append(c.Phones, PhoneInput{Number: ph.Number, Mobile: ph.Kind == phoneKindMobile, Primary: ph.IsPrimary != 0})
	}
	return c, nil
}

// checkContactOwner checks the owner link (CD-170): it exists and, since the
// owner never changes after create, is enabled.
func (s *Service) checkContactOwner(ctx context.Context, tx *db.Tx, in ContactInput) error {
	var es mstkit.Errors
	if in.BranchID != 0 {
		b, err := s.company.Get(ctx, tx, in.BranchID)
		switch {
		case errors.Is(err, doctype.ErrNotFound):
			es.Add("branch_id", "Branch not found. Pick one from the list.")
		case err != nil:
			return err
		case b.Disabled:
			es.Add("branch_id", "Branch "+b.Code+" is disabled. Pick another branch.")
		}
		return es.Err()
	}
	pt, err := s.parties.Get(ctx, tx, in.PartyID)
	switch {
	case errors.Is(err, doctype.ErrNotFound):
		es.Add("party_id", "Party not found. Pick one from the list.")
	case err != nil:
		return err
	case pt.Disabled:
		es.Add("party_id", "Party "+pt.Code+" is disabled. Pick another party.")
	}
	return es.Err()
}

// clearOtherPrimaryContacts clears the primary flag on the owner's primary
// contacts other than id.
func (s *Service) clearOtherPrimaryContacts(ctx context.Context, tx *db.Tx, p auth.Principal, partyID, branchID, id int64) error {
	others, err := dbq.New(tx.SQL()).OtherPrimaryContacts(ctx, dbq.OtherPrimaryContactsParams{
		PartyID: nullID(partyID), BranchID: nullID(branchID), ID: id,
	})
	if err != nil {
		return err
	}
	for _, other := range others {
		if err := s.clearPrimaryContact(ctx, tx, p, other); err != nil {
			return err
		}
	}
	return nil
}

// clearPrimaryContact clears the contact's primary flag and audits it.
func (s *Service) clearPrimaryContact(ctx context.Context, tx *db.Tx, p auth.Principal, id int64) error {
	if err := dbq.New(tx.SQL()).ClearPrimaryContact(ctx, dbq.ClearPrimaryContactParams{
		UpdatedAt: types.TimestampOf(s.env.Clock.Now()).String(), UpdatedBy: userID(p), ID: id,
	}); err != nil {
		return err
	}
	return s.env.Audit(ctx, tx, p, ContactKind, id, "save",
		map[string]string{"is_primary": "true"}, map[string]string{"is_primary": "false"})
}

// checkContacts is the contact post-state check (CD-190): it reads the
// owner's contacts after the change, inside the write tx, and every contact
// write calls it last. No disabled contact is primary, each contact has at
// most one primary email, at most one primary phone and exactly one owner,
// and the owner has at most one primary contact.
func checkContacts(ctx context.Context, tx *db.Tx, partyID, branchID int64) error {
	rows, err := dbq.New(tx.SQL()).OwnerContactFlags(ctx, dbq.OwnerContactFlagsParams{
		PartyID: nullID(partyID), BranchID: nullID(branchID),
	})
	if err != nil {
		return err
	}
	var es mstkit.Errors
	primaries := 0
	for _, r := range rows {
		if r.PartyID.Valid == r.BranchID.Valid {
			return fmt.Errorf("address: contact %d doesn't have exactly one owner", r.ID)
		}
		if r.IsPrimary != 0 {
			primaries++
			if r.Disabled != 0 {
				es.Add("disabled", msgContactEnable)
			}
		}
		if r.PrimaryEmails > 1 {
			es.Add("emails", msgOneEmail)
		}
		if r.PrimaryPhones > 1 {
			es.Add("phones", msgOnePhone)
		}
	}
	if err := es.Err(); err != nil {
		return err
	}
	if primaries > 1 {
		return fmt.Errorf("address: %d primary contacts on party %d, branch %d", primaries, partyID, branchID)
	}
	return nil
}

// insertContactLists saves in's emails and phones on contact id.
func insertContactLists(ctx context.Context, tx *db.Tx, id int64, in ContactInput) error {
	q := dbq.New(tx.SQL())
	for _, e := range in.Emails {
		if err := q.InsertContactEmail(ctx, dbq.InsertContactEmailParams{ContactID: id, Email: e.Email, IsPrimary: flag(e.Primary)}); err != nil {
			return err
		}
	}
	for _, ph := range in.Phones {
		kind := phoneKindLandline
		if ph.Mobile {
			kind = phoneKindMobile
		}
		if err := q.InsertContactPhone(ctx, dbq.InsertContactPhoneParams{ContactID: id, Number: ph.Number, Kind: kind, IsPrimary: flag(ph.Primary)}); err != nil {
			return err
		}
	}
	return nil
}

func contactFromInput(in ContactInput) Contact {
	return Contact{
		PartyID: in.PartyID, BranchID: in.BranchID, FirstName: in.FirstName, MiddleName: in.MiddleName, LastName: in.LastName,
		Emails: in.Emails, Phones: in.Phones, Primary: in.Primary,
	}
}

// contactAuditFields is the contact as audited; emails and phones are listed
// in order, the primary one marked.
func contactAuditFields(c Contact) map[string]string {
	id := func(v int64) string {
		if v == 0 {
			return ""
		}
		return strconv.FormatInt(v, 10)
	}
	var emails, phones []string
	for _, e := range c.Emails {
		emails = append(emails, listEntry(e.Email, e.Primary))
	}
	for _, ph := range c.Phones {
		phones = append(phones, listEntry(ph.Number, ph.Primary))
	}
	return map[string]string{
		"party_id": id(c.PartyID), "branch_id": id(c.BranchID),
		"first_name": c.FirstName, "middle_name": c.MiddleName, "last_name": c.LastName,
		"is_primary": strconv.FormatBool(c.Primary), "emails": strings.Join(emails, ", "), "phones": strings.Join(phones, ", "),
	}
}

func listEntry(s string, primary bool) string {
	if primary {
		return s + " (primary)"
	}
	return s
}

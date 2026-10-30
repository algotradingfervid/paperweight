package allmasters

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// partyImporters is the Parties group's RowFuncs (Task 20).
func partyImporters() map[string]rowFunc {
	return map[string]rowFunc{
		party.DocType:          (*Services).importParty,
		address.DocType:        (*Services).importAddress,
		address.ContactDocType: (*Services).importContact,
	}
}

func (s *Services) importParty(ctx context.Context, tx *db.Tx, p auth.Principal, row map[string]string) (int64, []doctype.FieldError, error) {
	terms, err := cellInt64(row["default_payment_terms_id"])
	if err != nil {
		return 0, nil, err
	}
	saved, err := s.Party.CreateTx(ctx, tx, p, party.Input{
		Code: row["code"], Name: row["name"],
		IsVendor: cellBool(row["is_vendor"]), IsCustomer: cellBool(row["is_customer"]),
		PAN: row["pan"], GSTCategory: gstin.Category(cellSelect(party.DocType, "gst_category", row["gst_category"])),
		DefaultPaymentTermsID: terms,
	})
	return rowResult(ctx, tx, p, row, saved, err, s.Party.SetDisabledTx)
}

func (s *Services) importAddress(ctx context.Context, tx *db.Tx, p auth.Principal, row map[string]string) (int64, []doctype.FieldError, error) {
	partyID, err := cellInt64(row["party_id"])
	if err != nil {
		return 0, nil, err
	}
	saved, err := s.Address.CreateTx(ctx, tx, p, address.Input{
		PartyID: partyID, Title: row["title"], Line1: row["line1"], Line2: row["line2"], City: row["city"],
		StateCode: cellState(row["state_code"]), Country: row["country"], Pincode: row["pincode"], GSTIN: row["gstin"],
		IsBilling: cellBool(row["is_billing"]), IsShipping: cellBool(row["is_shipping"]),
	})
	return rowResult(ctx, tx, p, row, saved, err, s.Address.SetDisabledTx)
}

func (s *Services) importContact(ctx context.Context, tx *db.Tx, p auth.Principal, row map[string]string) (int64, []doctype.FieldError, error) {
	partyID, err := cellInt64(row["party_id"])
	if err != nil {
		return 0, nil, err
	}
	in := address.ContactInput{
		PartyID: partyID, FirstName: row["first_name"], MiddleName: row["middle_name"], LastName: row["last_name"],
		Primary: cellBool(row["is_primary"]),
	}
	if row["email"] != "" {
		in.Emails = []address.EmailInput{{Email: row["email"], Primary: true}}
	}
	if row["mobile"] != "" {
		in.Phones = []address.PhoneInput{{Number: row["mobile"], Mobile: true, Primary: true}}
	}
	saved, err := s.Address.CreateContactTx(ctx, tx, p, in)
	id, errs, err := savedRow(saved, err)
	for i := range errs {
		// csvio finds the cell by Field for its Excel hint.
		switch errs[i].Field {
		case "emails":
			errs[i].Field = "email"
		case "phones":
			errs[i].Field = "mobile"
		}
	}
	return id, errs, err
}

// cellState returns the code of the state cell names by name, in any case. A
// cell naming none comes back unchanged, so the address master refuses it.
func cellState(cell string) string {
	if st, ok := gstin.StateByName(cell); ok {
		return st.Code
	}
	return cell
}

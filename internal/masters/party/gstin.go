package party

import (
	"context"
	"database/sql"
	"errors"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/party/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// ApplyGSTINTx is called by the address package when one of the party's
// addresses gets the normalised, validated GSTIN g, and only after the
// address checks (M5: category, primary billing, other parties) have passed,
// so it never upgrades a category from a non-primary address. A GSTIN whose PAN differs
// from the party's is refused (MST-048). A party without a PAN takes the
// GSTIN's, and an Unregistered party takes the category guessed from it
// (MST-049); the change is audited as a "save" of the party.
func (s *Service) ApplyGSTINTx(ctx context.Context, tx *db.Tx, p auth.Principal, partyID int64, g string) error {
	if g == "" {
		return nil
	}
	old, err := s.Get(ctx, tx, partyID)
	if err != nil {
		return err
	}
	pan := gstin.PANFromGSTIN(g)
	var es mstkit.Errors
	if pan != "" && old.PAN != "" && pan != old.PAN {
		es.Add("gstin", "GSTIN "+g+" belongs to PAN "+pan+", but this party's PAN is "+old.PAN+". Use a separate party for a different business.")
		return es.Err()
	}
	next := old
	if next.PAN == "" {
		next.PAN = pan
	}
	if next.GSTCategory == gstin.Unregistered {
		if next.GSTCategory, err = gstin.GuessCategory(g, "", old.GSTCategory); err != nil {
			es.Add("gstin", err.Error())
			return es.Err()
		}
	}
	if next.PAN == old.PAN && next.GSTCategory == old.GSTCategory {
		return nil
	}
	if err := dbq.New(tx.SQL()).UpdatePANAndCategory(ctx, dbq.UpdatePANAndCategoryParams{
		Pan: next.PAN, GstCategory: string(next.GSTCategory),
		UpdatedAt: types.TimestampOf(s.env.Clock.Now()).String(), UpdatedBy: userID(p), ID: partyID,
	}); err != nil {
		return err
	}
	return s.env.Audit(ctx, tx, p, Kind, partyID, "save", auditFields(old), auditFields(next))
}

// fromAddresses applies the party's addresses to an update (CD-167, M5): a
// posted PAN must be the PAN of every address GSTIN (MST-048 wording; a blank
// one takes the GSTINs' PAN, MST-040); a blank category is guessed from the
// primary billing address's GSTIN, keeping a stored Composition or SEZ the
// GSTIN fits (MST-035, MST-049), else kept as stored; the category must fit
// the primary billing address (M5, MST-034). CheckGST, run last, checks the
// rest of M5 (CD-180).
func (s *Service) fromAddresses(ctx context.Context, tx *db.Tx, id int64, in Input, stored gstin.Category) (Input, error) {
	q := dbq.New(tx.SQL())
	owner := sql.NullInt64{Int64: id, Valid: true}
	gstins, err := q.AddressGSTINs(ctx, owner)
	if err != nil {
		return Input{}, err
	}
	var es mstkit.Errors
	for _, g := range gstins {
		pan := gstin.PANFromGSTIN(g)
		switch {
		case pan == "":
		case in.PAN == "":
			in.PAN = pan
		case pan != in.PAN:
			es.Add("pan", "GSTIN "+g+" belongs to PAN "+pan+", but this party's PAN is "+in.PAN+". Use a separate party for a different business.")
			return Input{}, es.Err()
		}
	}
	primary, err := q.PrimaryBillingAddress(ctx, owner)
	hasPrimary := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Input{}, err
	}
	if in.GSTCategory == "" {
		in.GSTCategory = stored
		if hasPrimary && primary.Gstin != "" {
			if in.GSTCategory, err = gstin.GuessCategory(primary.Gstin, primary.Country, stored); err != nil {
				es.Add("gst_category", err.Error())
				return Input{}, es.Err()
			}
		}
	}
	if hasPrimary {
		if err := gstin.ValidateCategory(in.GSTCategory, primary.Gstin); err != nil {
			es.Add("gst_category", err.Error())
			return Input{}, es.Err()
		}
	}
	return in, nil
}

// Messages of CheckGST: CD-180 D1, and CD-177 (3) for D2.
const (
	msgNeedPrimaryBilling = "A registered party needs a primary billing address with a GSTIN. Add the billing address first."
	msgEnableFirst        = "Enable this address before making it primary."
)

// CheckGST checks M5 on the party and all its addresses as they stand after
// a write, inside that write's tx (CD-180); every address and party write
// calls it last. Its refusal goes on field. The rules: no disabled address
// is primary (D2); an address outside India needs category Overseas
// (MST-047); every address GSTIN fits the category, so an Unregistered party
// has none (MST-034); a Regular, Composition or SEZ party with an address
// has exactly one primary billing address and it has a GSTIN (D1).
func (s *Service) CheckGST(ctx context.Context, tx *db.Tx, partyID int64, field string) error {
	pt, err := s.Get(ctx, tx, partyID)
	if err != nil {
		return err
	}
	rows, err := dbq.New(tx.SQL()).PartyAddressesGST(ctx, sql.NullInt64{Int64: partyID, Valid: true})
	if err != nil {
		return err
	}
	var es mstkit.Errors
	primaries, primaryGSTIN := 0, ""
	for _, a := range rows {
		switch {
		case a.Disabled != 0 && (a.IsPrimaryBilling != 0 || a.IsPrimaryShipping != 0):
			es.Add(field, msgEnableFirst)
			return es.Err()
		case a.Country != "India" && pt.GSTCategory != gstin.Overseas:
			es.Add(field, msgOverseas)
			return es.Err()
		}
		if a.Gstin != "" {
			if err := gstin.ValidateCategory(pt.GSTCategory, a.Gstin); err != nil {
				es.Add(field, err.Error())
				return es.Err()
			}
		}
		if a.IsPrimaryBilling != 0 {
			primaries++
			primaryGSTIN = a.Gstin
		}
	}
	if len(rows) == 0 || pt.GSTCategory == gstin.Unregistered || pt.GSTCategory == gstin.Overseas {
		return nil
	}
	if primaries != 1 || primaryGSTIN == "" {
		es.Add(field, msgNeedPrimaryBilling)
		return es.Err()
	}
	return nil
}

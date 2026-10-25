package address

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/vardhan/internaltools/internal/masters/address/dbq"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// overseasPlace is the GST place-of-supply code for addresses outside India.
const overseasPlace = "96"

// DisplayText is the address as documents snapshot it (MST-068), e.g.
// "Plot 12, MIDC\nAndheri East\nMumbai - 400093\nMaharashtra (27)\nGSTIN: 27AABCS1429B1ZU".
func DisplayText(a Address) string { return strings.Join(lines(a), "\n") }

func lines(a Address) []string {
	var out []string
	for _, l := range []string{a.Line1, a.Line2} {
		if l != "" {
			out = append(out, l)
		}
	}
	city := a.City
	if a.Pincode != "" {
		city += " - " + a.Pincode
	}
	out = append(out, city)
	if st, ok := gstin.StateByCode(a.StateCode); ok && a.Country == india {
		out = append(out, st.Name+" ("+st.Code+")")
	} else if a.Country != india {
		out = append(out, a.Country)
	}
	if a.GSTIN != "" {
		out = append(out, "GSTIN: "+a.GSTIN)
	}
	return out
}

// PlaceOfSupply is the address's GST state code, 96 outside India.
func PlaceOfSupply(a Address) string {
	if a.Country != india {
		return overseasPlace
	}
	return a.StateCode
}

// PartySnapshot is what a document stores about its party and the chosen
// address (D-MST-03). The address must be the party's; addressID 0 gives the
// party's fields only.
func (s *Service) PartySnapshot(ctx context.Context, tx *db.Tx, partyID, addressID int64) (party.Snapshot, error) {
	pt, err := s.parties.Get(ctx, tx, partyID)
	if err != nil {
		return party.Snapshot{}, err
	}
	snap := party.Snapshot{Code: pt.Code, Name: pt.Name, GSTCategory: string(pt.GSTCategory)}
	if addressID == 0 {
		return snap, nil
	}
	a, err := s.Get(ctx, tx, addressID)
	if err != nil {
		return party.Snapshot{}, err
	}
	if a.PartyID != partyID {
		var es mstkit.Errors
		es.Add("address_id", "That address belongs to another party. Pick one of this party's addresses.")
		return party.Snapshot{}, es.Err()
	}
	snap.GSTIN, snap.StateCode, snap.AddressText = a.GSTIN, PlaceOfSupply(a), DisplayText(a)
	if st, ok := gstin.StateByCode(snap.StateCode); ok {
		snap.StateName = st.Name
	}
	return snap, nil
}

// BranchAddressLines is the branch's primary billing address as letterhead
// lines (company.Letterhead.AddressLines, CD-154); nil when it has none.
func (s *Service) BranchAddressLines(ctx context.Context, tx *db.Tx, branchID int64) ([]string, error) {
	r, err := dbq.New(tx.SQL()).PrimaryBillingOfBranch(ctx, nullID(branchID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return lines(fromRow(r)), nil
}

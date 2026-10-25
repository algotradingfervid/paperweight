package party

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/party/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// ApplyGSTINTx is called by the address package when one of the party's
// addresses gets the normalised, validated GSTIN g. A GSTIN whose PAN differs
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

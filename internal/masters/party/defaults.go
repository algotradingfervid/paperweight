package party

import (
	"context"
	"database/sql"
	"errors"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/party/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Defaults are the values a new document takes from its party.
type Defaults struct {
	PaymentTermsID int64 // 0 = none, or the party's terms are disabled (MST-050)
}

// Defaults reads the party's defaults; a missing party is doctype.ErrNotFound.
func (s *Service) Defaults(ctx context.Context, tx *db.Tx, partyID int64) (Defaults, error) {
	r, err := dbq.New(tx.SQL()).GetDefaults(ctx, partyID)
	if errors.Is(err, sql.ErrNoRows) {
		return Defaults{}, doctype.ErrNotFound
	}
	if err != nil {
		return Defaults{}, err
	}
	if r.TermsDisabled != 0 {
		return Defaults{}, nil
	}
	return Defaults{PaymentTermsID: r.DefaultPaymentTermsID.Int64}, nil
}

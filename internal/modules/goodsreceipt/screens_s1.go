package goodsreceipt

import (
	"context"
	"errors"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/dbq"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Receivable reports whether p may receive against PO poID (CD-283): create
// on the GRN and read on the PO through auth.CheckLinkTx, never a master
// link check (CD-312), in one read. A refusal or a missing PO is false with
// no error.
func (s *Service) Receivable(ctx context.Context, p auth.Principal, poID int64) (bool, error) {
	err := s.d.DB.Read(ctx, func(tx *db.Tx) error {
		if err := s.d.Auth.CanTx(ctx, tx, p, DocType, auth.RightCreate, nil); err != nil {
			return err
		}
		return s.d.Auth.CheckLinkTx(ctx, tx, p, purchase.DocType, poID)
	})
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, auth.ErrForbidden), errors.Is(err, doctype.ErrNotFound):
		return false, nil
	}
	return false, err
}

// AllPOChoices is every row POChoices gives with all, unpaged, in one read:
// step 1 filters it for the default list, so a render reads once.
func (s *Service) AllPOChoices(ctx context.Context, p auth.Principal) ([]POChoice, error) {
	pred, err := s.d.Auth.Scope(ctx, p, purchase.DocType, auth.RightRead)
	if err != nil {
		return nil, err
	}
	var out []POChoice
	err = s.d.DB.Read(ctx, func(tx *db.Tx) error {
		readable, err := purchase.ReadableOrders(ctx, tx, pred)
		if err != nil {
			return err
		}
		rows, err := dbq.New(tx.SQL()).G6PickerLines(ctx, "")
		out = choices(rows, readable, true)
		return err
	})
	return out, err
}

// ReceiptFacts is what the first two steps show of a GRN beyond its fields:
// its own snapshot of its PO (CD-217, CD-228) and whether any stored line is
// goods.
type ReceiptFacts struct {
	PONumber, VendorName, SiteName string
	HasGoods                       bool
}

// Facts is GRN id's ReceiptFacts, for p, who must be able to read it.
func (s *Service) Facts(ctx context.Context, p auth.Principal, id int64) (ReceiptFacts, error) {
	var f ReceiptFacts
	err := s.d.DB.Read(ctx, func(tx *db.Tx) error {
		if err := s.d.Auth.CheckLinkTx(ctx, tx, p, DocType, id); err != nil {
			return err
		}
		r, err := dbq.New(tx.SQL()).S1Receipt(ctx, id)
		f = ReceiptFacts{PONumber: r.PoNumber, VendorName: r.VendorName, SiteName: r.SiteName, HasGoods: r.HasGoods}
		return err
	})
	return f, err
}

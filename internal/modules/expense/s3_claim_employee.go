package expense

import (
	"context"
	"errors"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/modules/expense/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// ClaimEmployee is the claimant's name as claim id stores it (its
// snapshot), for the claim page's draft notice (UX §5.7). A claim p can't
// read is missing (CD-245); AUTH builds the record in the read (CD-312).
func (s *Service) ClaimEmployee(ctx context.Context, p auth.Principal, id int64) (string, error) {
	var name string
	err := s.db.Read(ctx, func(tx *db.Tx) error {
		switch err := s.deps.Auth.CheckLinkTx(ctx, tx, p, ClaimDocType, id); {
		case errors.Is(err, auth.ErrForbidden), errors.Is(err, auth.ErrNotFound):
			return doc.Gone()
		case err != nil:
			return err
		}
		c, err := dbq.New(tx.SQL()).E7ClaimPrint(ctx, id)
		name = c.SnapEmployeeName
		return err
	})
	if err != nil {
		return "", err
	}
	return name, nil
}

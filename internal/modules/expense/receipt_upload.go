package expense

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/expense/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Receipt is a receipt uploaded from the expense line page. Reused is true
// when its bytes were already on the claim (FILE refuses them twice), and
// Lines are then the lines holding it, lowest first (UX §5.5, CD-270 Q6).
type Receipt struct {
	File   files.Meta
	Reused bool
	Lines  []int
}

// AttachReceipt attaches f, spooled outside any transaction (CD-29), to
// claim id as name, at the version the page posted (CD-94, CD-183), in one
// db.Write. In order: missing is AUTH's 404, then AUTH's write check on the
// claim (404 when the user can't read it, else its 403), then the version
// (409), then FILE's own checks; the right goes before the version (lessons
// §11). Bytes already on the claim are no refusal: the attached file comes
// back as Reused, nothing written. The claim's version doesn't change
// (FILE-040).
func (s *Service) AttachReceipt(ctx context.Context, p auth.Principal, id int64, version int, name string, f *files.Spooled) (Receipt, error) {
	var out Receipt
	err := s.db.Write(ctx, func(tx *db.Tx) error {
		q := dbq.New(tx.SQL())
		c, err := q.S2ClaimRef(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return auth.ErrNotFound
		}
		if err != nil {
			return err
		}
		ref := &auth.RecordRef{DocType: ClaimDocType, ID: id, CreatedBy: types.UserID(c.CreatedBy), EmployeeID: c.EmployeeID.Int64}
		if err := s.deps.Auth.CanTx(ctx, tx, p, ClaimDocType, auth.RightWrite, ref); err != nil {
			return err
		}
		if int64(version) != c.Version {
			return &doc.Error{Kind: doc.KindStale, Message: fmt.Sprintf(msgPaidStale, naming.Label(c.Number.String, id))}
		}
		attID, err := s.deps.Files.Attach(ctx, tx, p, *ref, name, f)
		if files.KindOf(err) == files.KindDuplicate {
			return s.reused(ctx, tx, q, id, f.SHA256(), &out)
		}
		if err != nil {
			return err
		}
		out.File, err = s.deps.Files.Meta(ctx, tx, attID)
		return err
	})
	return out, err
}

// reused fills out with claim id's attachment holding hash and the lines
// using it, read in tx.
func (s *Service) reused(ctx context.Context, tx *db.Tx, q *dbq.Queries, id int64, hash string, out *Receipt) error {
	metas, err := s.deps.Files.ListTx(ctx, tx, auth.RecordRef{DocType: ClaimDocType, ID: id})
	if err != nil {
		return err
	}
	for _, m := range metas {
		if m.SHA256 == hash {
			out.File, out.Reused = m, true
		}
	}
	lines, err := q.E3ReceiptLines(ctx, id)
	if err != nil {
		return err
	}
	for i, l := range lines {
		if l.ReceiptSha256 == hash {
			out.Lines = append(out.Lines, i+1)
		}
	}
	return nil
}

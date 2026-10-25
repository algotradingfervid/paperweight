package files

import (
	"context"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/files/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// HumanSize shows n bytes as "512 B", "512 KB" (whole KB) or "2.3 MB" (one
// decimal), rounding half up.
func HumanSize(n int64) string {
	switch {
	case n < 1<<10:
		return fmt.Sprintf("%d B", n)
	case n < 1<<20:
		return fmt.Sprintf("%d KB", (n+1<<9)/(1<<10))
	}
	tenths := (n*10 + 1<<19) / (1 << 20)
	return fmt.Sprintf("%d.%d MB", tenths/10, tenths%10)
}

// ListEntry is an attachment's line in a document's list:
// "receipt.jpg · 2.3 MB · Ravi Kulkarni · 24-10-2026 10:30".
func ListEntry(m Meta, uploader string) string {
	return m.Name + " · " + HumanSize(m.Size) + " · " + uploader + " · " + types.FormatIST(m.UploadedAt)
}

// Meta returns attachment id's details read in tx, with no permission check:
// for a caller that has just attached it in the same transaction.
func (s *Store) Meta(ctx context.Context, tx *db.Tx, id int64) (Meta, error) {
	row, err := dbq.New(tx.SQL()).GetAttachment(ctx, id)
	if err != nil {
		return Meta{}, err
	}
	return metaFrom(row)
}

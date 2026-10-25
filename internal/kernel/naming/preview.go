package naming

import (
	"context"

	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Preview shows the number the next approval would get for a document dated
// d. It runs in a read transaction, so it never takes the write lock and
// never uses a number.
func (n *Namer) Preview(ctx context.Context, doctype string, d types.Date) (string, error) {
	if d == (types.Date{}) {
		return "", &Error{Kind: ErrInvalidDate, Msg: msgNoDate}
	}
	var num string
	err := n.db.Read(ctx, func(tx *db.Tx) error {
		s, err := loadSeries(ctx, tx.SQL(), doctype)
		if err != nil {
			return err
		}
		t, err := parseTemplate(s.Template)
		if err != nil {
			return err
		}
		last, err := readLast(ctx, tx.SQL(), doctype, t.prefix(d))
		if err != nil {
			return err
		}
		num = t.render(d, last+1)
		return nil
	})
	return num, err
}

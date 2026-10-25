package naming

import (
	"context"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/naming/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Assign gives the document its number inside the caller's db.Write
// transaction (CD-94), so a failed approval rolls the counter back. Every
// date part comes from the document date d, never the clock.
func (n *Namer) Assign(ctx context.Context, tx *db.Tx, doctype string, docID int64, d types.Date) (string, error) {
	if tx == nil || !tx.Writable() {
		return "", ErrNeedsTx
	}
	if d == (types.Date{}) {
		return "", &Error{Kind: ErrInvalidDate, Msg: msgNoDate}
	}
	s, err := loadSeries(ctx, tx.SQL(), doctype)
	if err != nil {
		return "", err
	}
	t, err := parseTemplate(s.Template)
	if err != nil {
		return "", err
	}
	p := t.prefix(d)
	now := nowText(n.clk)
	c, err := next(ctx, tx, doctype, p, now)
	if err != nil {
		return "", err
	}
	if c > t.maxCounter() {
		return "", &Error{Kind: ErrExhausted, Msg: fmt.Sprintf(msgExhausted, p, t.maxCounter())}
	}
	num := t.render(d, c)
	if err := dbq.New(tx.SQL()).InsertIssued(ctx, dbq.InsertIssuedParams{
		DocType:     doctype,
		DocID:       docID,
		Number:      num,
		Prefix:      p,
		Seq:         c,
		Root:        num,
		AmendmentNo: 0,
		IssuedAt:    now,
	}); err != nil {
		return "", err
	}
	return num, nil
}

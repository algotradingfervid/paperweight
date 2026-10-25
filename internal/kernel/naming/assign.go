package naming

import (
	"context"
	"database/sql"
	"errors"
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
	q := dbq.New(tx.SQL())
	issued, err := q.CountIssuedForDoc(ctx, dbq.CountIssuedForDocParams{DocType: doctype, DocID: docID})
	if err != nil {
		return "", err
	}
	if issued > 0 {
		return "", &Error{Kind: ErrNumberImmutable, Msg: msgImmutable}
	}
	p := t.prefix(d)
	now := nowText(n.clk)
	c, err := next(ctx, tx, doctype, p, t.maxCounter(), now)
	if err != nil {
		return "", err
	}
	num := t.render(d, c)
	// The unique indexes are the backstop if the counter was ever set back.
	if _, err := q.InsertIssuedRoot(ctx, dbq.InsertIssuedRootParams{
		DocType:     doctype,
		DocID:       docID,
		Number:      num,
		Prefix:      p,
		Seq:         c,
		Root:        num,
		AmendmentNo: 0,
		IssuedAt:    now,
	}); errors.Is(err, sql.ErrNoRows) {
		return "", &Error{Kind: ErrDuplicateNumber, Msg: fmt.Sprintf(msgDuplicate, num)}
	} else if err != nil {
		return "", err
	}
	return num, nil
}

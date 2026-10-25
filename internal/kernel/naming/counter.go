package naming

import (
	"context"
	"database/sql"
	"errors"

	"github.com/vardhan/internaltools/internal/kernel/naming/dbq"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// next increments the counter for (doctype, prefix) and returns the new last
// number, creating the row at 1 the first time. It is one UPSERT…RETURNING
// statement inside the caller's db.Write transaction, so there is no
// read-then-write race and a rolled-back approval rolls the counter back too.
func next(ctx context.Context, tx *db.Tx, doctype, prefix string, now string) (int64, error) {
	return dbq.New(tx.SQL()).NextCounter(ctx, dbq.NextCounterParams{
		DocType:   doctype,
		Prefix:    prefix,
		CreatedAt: now,
		UpdatedAt: now,
	})
}

// readLast returns the last number given for (doctype, prefix), or 0 when no
// counter row exists yet.
func readLast(ctx context.Context, q db.DBTX, doctype, prefix string) (int64, error) {
	last, err := dbq.New(q).GetCounterLast(ctx, dbq.GetCounterLastParams{DocType: doctype, Prefix: prefix})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return last, err
}

// nowText formats the clock's instant as a fixed-width UTC timestamp.
func nowText(c clock.Clock) string {
	return c.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

// highestIssued is the highest base number issued for (doctype, prefix), or 0
// when none has been; amendments don't count.
func highestIssued(ctx context.Context, q db.DBTX, doctype, prefix string) (int64, error) {
	return dbq.New(q).HighestIssued(ctx, dbq.HighestIssuedParams{DocType: doctype, Prefix: prefix})
}

// setLast sets the counter for (doctype, prefix) to last, creating the row if
// needed, and returns the counter's id.
func setLast(ctx context.Context, tx *db.Tx, doctype, prefix string, last int64, by sql.NullInt64, now string) (int64, error) {
	return dbq.New(tx.SQL()).SetCounterLast(ctx, dbq.SetCounterLastParams{
		DocType:   doctype,
		Prefix:    prefix,
		Last:      last,
		CreatedAt: now,
		CreatedBy: by,
		UpdatedAt: now,
		UpdatedBy: by,
	})
}

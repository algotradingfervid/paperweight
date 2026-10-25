package naming

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/naming/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// seriesRow is one naming_series row.
type seriesRow struct {
	ID       int64
	DocType  string
	Label    string
	Template string
}

// EnsureSeries seeds the series for doctype with defaultTpl when it has none.
// An existing row is kept unchanged, because admin edits win. System seeding
// writes no audit entry.
func (n *Namer) EnsureSeries(ctx context.Context, doctype, label, defaultTpl string) error {
	if _, err := parseTemplate(defaultTpl); err != nil {
		return err
	}
	now := nowText(n.clk)
	return n.db.Write(ctx, func(tx *db.Tx) error {
		return dbq.New(tx.SQL()).InsertSeriesIfMissing(ctx, dbq.InsertSeriesIfMissingParams{
			DocType:   doctype,
			Label:     label,
			Template:  defaultTpl,
			CreatedAt: now,
			UpdatedAt: now,
		})
	})
}

// loadSeries reads the series for doctype, or ErrNoSeries when there is none.
func loadSeries(ctx context.Context, q db.DBTX, doctype string) (seriesRow, error) {
	s, err := dbq.New(q).GetSeries(ctx, doctype)
	if errors.Is(err, sql.ErrNoRows) {
		return seriesRow{}, &Error{Kind: ErrNoSeries, Msg: fmt.Sprintf(msgNoSeries, doctype)}
	}
	if err != nil {
		return seriesRow{}, err
	}
	return seriesRow{ID: s.ID, DocType: s.DocType, Label: s.Label, Template: s.Template}, nil
}

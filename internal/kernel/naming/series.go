package naming

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/naming/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
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
	t, err := parseTemplate(defaultTpl)
	if err != nil {
		return err
	}
	now := nowText(n.clk)
	return n.db.Write(ctx, func(tx *db.Tx) error {
		_, err := loadSeries(ctx, tx.SQL(), doctype)
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrNoSeries) {
			return err
		}
		if err := checkPrefixFree(ctx, tx.SQL(), doctype, t); err != nil {
			return err
		}
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

// months returns the first of every month from 2000-01 to 2099-12. The FY
// changes on a month boundary, so these dates cover every prefix a template
// can render.
func months() []types.Date {
	ds := make([]types.Date, 0, 1200)
	for y := 2000; y <= 2099; y++ {
		for m := time.January; m <= time.December; m++ {
			ds = append(ds, types.Date{Year: y, Month: m, Day: 1})
		}
	}
	return ds
}

// matchPrefix reports whether t renders prefix for some month.
func matchPrefix(t template, prefix string) bool {
	for _, d := range months() {
		if t.prefix(d) == prefix {
			return true
		}
	}
	return false
}

// checkPrefixFree returns ErrDuplicatePrefix when t renders, for some month,
// the same prefix as another DocType's series, so two series never share a
// counter key's numbers.
func checkPrefixFree(ctx context.Context, q db.DBTX, doctype string, t template) error {
	all, err := dbq.New(q).ListSeries(ctx)
	if err != nil {
		return err
	}
	ds := months()
	for _, s := range all {
		if s.DocType == doctype {
			continue
		}
		o, err := parseTemplate(s.Template)
		if err != nil {
			return err
		}
		for _, d := range ds {
			if t.prefix(d) == o.prefix(d) {
				return &Error{Kind: ErrDuplicatePrefix, Msg: fmt.Sprintf(msgDupPrefix, t.staticPrefix(), s.Label), Field: "template"}
			}
		}
	}
	return nil
}

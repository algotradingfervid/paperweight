package list

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Row is one list row; Values holds each field typed by its FieldType, nil
// for SQL NULL.
type Row struct {
	ID     int64
	Values map[string]any
}

// ScanRow scans the current row of a Build or BuildAll result.
func ScanRow(dt *doctype.DocType, rows *sql.Rows) (Row, error) {
	var fields []doctype.Field
	for _, f := range dt.Fields {
		if !f.OnLine {
			fields = append(fields, f)
		}
	}
	r := Row{Values: make(map[string]any, len(fields))}
	raw := make([]any, len(fields))
	dest := []any{&r.ID}
	for i := range raw {
		dest = append(dest, &raw[i])
	}
	if err := rows.Scan(dest...); err != nil {
		return Row{}, err
	}
	for i, f := range fields {
		v, err := typed(f, raw[i])
		if err != nil {
			return Row{}, fmt.Errorf("list: %s.%s: %w", dt.Name, f.Name, err)
		}
		r.Values[f.Name] = v
	}
	return r, nil
}

// typed converts a scanned SQLite value to f's Go type.
func typed(f doctype.Field, v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	switch x := v.(type) {
	case int64:
		switch f.Type {
		case doctype.Money:
			return types.Money(x), nil
		case doctype.Qty:
			return types.Qty(x), nil
		case doctype.Rate:
			return types.Rate(x), nil
		case doctype.Percent:
			return types.Percent(x), nil
		case doctype.Factor:
			return types.Factor(x), nil
		case doctype.Check:
			return x != 0, nil
		case doctype.Int, doctype.Link:
			return x, nil
		}
	case string:
		switch f.Type {
		case doctype.Text, doctype.Select:
			return x, nil
		case doctype.Date:
			return types.DateFromISO(x)
		case doctype.Timestamp:
			return types.ParseTimestamp(x)
		}
	}
	return nil, fmt.Errorf("unexpected stored value %#v", v)
}

// DefaultFilters hides disabled records: [disabled = 0] when dt has a Check
// field named disabled, else nil.
func DefaultFilters(dt *doctype.DocType) []Filter {
	for _, f := range dt.Fields {
		if f.Name == "disabled" && f.Type == doctype.Check {
			return []Filter{{Field: "disabled", Op: OpEq, Values: []string{"0"}}}
		}
	}
	return nil
}

// Service runs list queries for a principal: read right first, then the
// read scope ANDed into every query.
type Service struct {
	DB   *db.DB
	Auth auth.Service
}

func (s *Service) scope(ctx context.Context, p auth.Principal, dt *doctype.DocType) (auth.Predicate, error) {
	if err := s.Auth.Can(ctx, p, dt.Name, "read", nil); err != nil {
		return auth.Predicate{}, err
	}
	return s.Auth.Scope(ctx, p, dt.Name, "read")
}

// List returns one page of rows and the total matching count.
func (s *Service) List(ctx context.Context, p auth.Principal, dt *doctype.DocType, q Query) (Result[Row], error) {
	scope, err := s.scope(ctx, p, dt)
	if err != nil {
		return Result[Row]{}, err
	}
	rowsQ, rowsArgs, err := Build(dt, q, scope)
	if err != nil {
		return Result[Row]{}, err
	}
	countQ, countArgs, err := BuildCount(dt, q, scope)
	if err != nil {
		return Result[Row]{}, err
	}
	res := Result[Row]{Rows: []Row{}}
	err = s.DB.Read(ctx, func(tx *db.Tx) error {
		if err := tx.SQL().QueryRowContext(ctx, countQ, countArgs...).Scan(&res.Total); err != nil {
			return err
		}
		rows, err := tx.SQL().QueryContext(ctx, rowsQ, rowsArgs...)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			r, err := ScanRow(dt, rows)
			if err != nil {
				return err
			}
			res.Rows = append(res.Rows, r)
		}
		return rows.Err()
	})
	if err != nil {
		return Result[Row]{}, err
	}
	return res, nil
}

// Count returns the number of rows matching q.
func (s *Service) Count(ctx context.Context, p auth.Principal, dt *doctype.DocType, q Query) (int, error) {
	scope, err := s.scope(ctx, p, dt)
	if err != nil {
		return 0, err
	}
	countQ, args, err := BuildCount(dt, q, scope)
	if err != nil {
		return 0, err
	}
	var n int
	err = s.DB.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, countQ, args...).Scan(&n)
	})
	return n, err
}

// Totals sums the named Money fields over the rows matching q.
func (s *Service) Totals(ctx context.Context, p auth.Principal, dt *doctype.DocType, q Query, fields ...string) (map[string]types.Money, error) {
	scope, err := s.scope(ctx, p, dt)
	if err != nil {
		return nil, err
	}
	totalsQ, args, err := BuildTotals(dt, q, scope, fields)
	if err != nil {
		return nil, err
	}
	sums := make([]types.Money, len(fields))
	dest := make([]any, len(fields))
	for i := range sums {
		dest[i] = &sums[i]
	}
	err = s.DB.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, totalsQ, args...).Scan(dest...)
	})
	if err != nil {
		return nil, err
	}
	out := make(map[string]types.Money, len(fields))
	for i, f := range fields {
		out[f] = sums[i]
	}
	return out, nil
}

// Picker lists the first page of a link picker: disabled records hidden,
// the search applied, the default sort.
func (s *Service) Picker(ctx context.Context, p auth.Principal, dt *doctype.DocType, search string, size int) (Result[Row], error) {
	return s.List(ctx, p, dt, Query{Filters: DefaultFilters(dt), Search: search, Page: 1, PageSize: size})
}

package list

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/types"
)

const (
	OpEq         Op = "="
	OpNe         Op = "!="
	OpLt         Op = "<"
	OpLe         Op = "<="
	OpGt         Op = ">"
	OpGe         Op = ">="
	OpContains   Op = "contains"
	OpIn         Op = "in"
	OpNotIn      Op = "not in"
	OpBetween    Op = "between"
	OpOnOrAfter  Op = "on or after"
	OpOnOrBefore Op = "on or before"
	OpIsEmpty    Op = "is empty"
	OpIsNotEmpty Op = "is not empty"
)

// Columns returns the fields shown as list columns: ListPriority > 0, in
// priority order, ties in declaration order (CD-42).
func Columns(dt *doctype.DocType) []doctype.Field {
	var cols []doctype.Field
	for _, f := range dt.Fields {
		if f.ListPriority > 0 {
			cols = append(cols, f)
		}
	}
	slices.SortStableFunc(cols, func(a, b doctype.Field) int { return cmp.Compare(a.ListPriority, b.ListPriority) })
	return cols
}

// BuildCount counts the rows Build would page through.
func BuildCount(dt *doctype.DocType, q Query, scope auth.Predicate) (string, []any, error) {
	where, args, err := whereClause(dt, q, scope)
	if err != nil {
		return "", nil, err
	}
	return "SELECT COUNT(*) FROM " + dt.Table + where, args, nil
}

// BuildTotals sums the Money fields over the rows Build would page through,
// one column per field in order.
func BuildTotals(dt *doctype.DocType, q Query, scope auth.Predicate, fields []string) (string, []any, error) {
	if len(fields) == 0 {
		return "", nil, fmt.Errorf("list: %s totals need at least one field", dt.Name)
	}
	sums := make([]string, len(fields))
	for i, name := range fields {
		j := slices.IndexFunc(dt.Fields, func(f doctype.Field) bool {
			return f.Name == name && f.Type == doctype.Money && !f.OnLine
		})
		if j < 0 {
			return "", nil, fmt.Errorf("list: %s.%s is not a header Money field", dt.Name, name)
		}
		sums[i] = "COALESCE(SUM(" + column(dt, dt.Fields[j]) + "), 0)"
	}
	where, args, err := whereClause(dt, q, scope)
	if err != nil {
		return "", nil, err
	}
	return "SELECT " + strings.Join(sums, ", ") + " FROM " + dt.Table + where, args, nil
}

// column is the qualified SQL column of f; the table is never aliased (CD-16).
func column(dt *doctype.DocType, f doctype.Field) string {
	c := f.Column
	if c == "" {
		c = f.Name
	}
	return dt.Table + "." + c
}

// whereClause is " WHERE …" ANDing the filters, the search and the scope
// predicate, or "" when there is no condition.
func whereClause(dt *doctype.DocType, q Query, scope auth.Predicate) (string, []any, error) {
	var conds []string
	var args []any
	for _, f := range q.Filters {
		c, a, err := filterCond(dt, f)
		if err != nil {
			return "", nil, err
		}
		conds = append(conds, c)
		args = append(args, a...)
	}
	if c, a := searchCond(dt, q.Search); c != "" {
		conds = append(conds, c)
		args = append(args, a...)
	}
	if scope.SQL != "" {
		conds = append(conds, "("+scope.SQL+")")
		args = append(args, scope.Args...)
	}
	if len(conds) == 0 {
		return "", nil, nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args, nil
}

const (
	textOps   = "Use =, contains, in, is empty."
	idOps     = "Use =, in, is empty."
	numberOps = "Use =, <, >, between."
	dateOps   = "Use =, between, on or after, on or before."
)

// allowedOps returns the operators a field type takes and the hint shown
// when another one is used (D-NUM-19).
func allowedOps(t doctype.FieldType) ([]Op, string) {
	switch t {
	case doctype.Text, doctype.Select:
		return []Op{OpEq, OpNe, OpContains, OpIn, OpNotIn, OpIsEmpty, OpIsNotEmpty}, textOps
	case doctype.Link, doctype.Int, doctype.Check:
		return []Op{OpEq, OpNe, OpIn, OpNotIn, OpIsEmpty, OpIsNotEmpty}, idOps
	case doctype.Date, doctype.Timestamp:
		return []Op{OpEq, OpNe, OpLt, OpLe, OpGt, OpGe, OpBetween, OpOnOrAfter, OpOnOrBefore, OpIsEmpty, OpIsNotEmpty}, dateOps
	default: // Money, Qty, Rate, Percent, Factor
		return []Op{OpEq, OpNe, OpLt, OpLe, OpGt, OpGe, OpBetween, OpIn, OpNotIn, OpIsEmpty, OpIsNotEmpty}, numberOps
	}
}

func filterCond(dt *doctype.DocType, f Filter) (string, []any, error) {
	i := slices.IndexFunc(dt.Fields, func(fd doctype.Field) bool { return fd.Name == f.Field && fd.Filterable && !fd.OnLine })
	if i < 0 {
		return "", nil, badQuery(fmt.Sprintf("You can't filter %s by '%s'.", dt.PluralLabel, f.Field))
	}
	fd := dt.Fields[i]
	ops, hint := allowedOps(fd.Type)
	if !slices.Contains(ops, f.Op) {
		return "", nil, badQuery(fmt.Sprintf("'%s' can't be used on %s. %s", f.Op, fd.Label, hint))
	}
	col := column(dt, fd)
	isText := fd.Type == doctype.Text || fd.Type == doctype.Select

	switch f.Op {
	case OpIsEmpty:
		if isText {
			return "(" + col + " IS NULL OR " + col + " = '')", nil, nil
		}
		return col + " IS NULL", nil, nil
	case OpIsNotEmpty:
		if isText {
			return "(" + col + " IS NOT NULL AND " + col + " != '')", nil, nil
		}
		return col + " IS NOT NULL", nil, nil
	case OpBetween:
		if len(f.Values) != 2 {
			return "", nil, badQuery("Choose both a start and an end date.")
		}
	case OpIn, OpNotIn:
	default:
		if len(f.Values) != 1 {
			return "", nil, badQuery("Choose a value for " + fd.Label + ".")
		}
	}

	if fd.Type == doctype.Date || fd.Type == doctype.Timestamp {
		return dateCond(fd, col, f.Op, f.Values)
	}

	args := make([]any, len(f.Values))
	for i, v := range f.Values {
		a, err := parseValue(fd, v)
		if err != nil {
			return "", nil, err
		}
		args[i] = a
	}
	switch f.Op {
	case OpIn, OpNotIn:
		if len(args) == 0 {
			if f.Op == OpIn {
				return "1=0", nil, nil
			}
			return "1=1", nil, nil
		}
		marks := strings.TrimSuffix(strings.Repeat("?, ", len(args)), ", ")
		if f.Op == OpIn {
			return col + " IN (" + marks + ")", args, nil
		}
		return "(" + col + " IS NULL OR " + col + " NOT IN (" + marks + "))", args, nil
	case OpNe:
		return "(" + col + " IS NULL OR " + col + " != ?)", args, nil
	case OpContains:
		return col + ` LIKE ? ESCAPE '\'`, []any{likePattern(args[0].(string))}, nil
	case OpBetween:
		return col + " BETWEEN ? AND ?", args, nil
	default: // = < <= > >=
		return col + " " + string(f.Op) + " ?", args, nil
	}
}

// parseValue turns user text into the bound value for a non-date field.
func parseValue(fd doctype.Field, v string) (any, error) {
	var n int64
	var err error
	switch fd.Type {
	case doctype.Text, doctype.Select:
		return strings.TrimSpace(v), nil
	case doctype.Link, doctype.Int, doctype.Check:
		n, err = strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil || (fd.Type == doctype.Check && n != 0 && n != 1) {
			return nil, badQuery("Choose a value for " + fd.Label + ".")
		}
		return n, nil
	case doctype.Money:
		var m types.Money
		m, err = types.ParseMoney(v)
		n = int64(m)
	case doctype.Qty:
		var q types.Qty
		q, err = types.ParseQty(v)
		n = int64(q)
	case doctype.Rate:
		var r types.Rate
		r, err = types.ParseRate(v)
		n = int64(r)
	case doctype.Percent:
		var p types.Percent
		p, err = types.ParsePercent(v)
		n = int64(p)
	case doctype.Factor:
		var f types.Factor
		f, err = types.ParseFactor(v)
		n = int64(f)
	}
	if err != nil {
		return nil, badQuery(err.Error())
	}
	return n, nil
}

// dateCond compares a Date or Timestamp column against whole days. A
// Timestamp day runs from 00:00 IST to the next 00:00 IST; a Date column
// holds ISO text, so the same half-open ranges work on the ISO dates.
func dateCond(fd doctype.Field, col string, op Op, values []string) (string, []any, error) {
	days := make([]types.Date, len(values))
	for i, v := range values {
		d, err := types.ParseDate(v)
		if err != nil {
			return "", nil, badQuery(err.Error())
		}
		days[i] = d
	}
	start := func(d types.Date) string {
		if fd.Type == doctype.Timestamp {
			return types.ISTDayStart(d).String()
		}
		return d.ISO()
	}
	next := func(d types.Date) string { return start(types.AddDays(d, 1)) }

	d := days[0]
	switch op {
	case OpEq:
		return "(" + col + " >= ? AND " + col + " < ?)", []any{start(d), next(d)}, nil
	case OpNe:
		return "(" + col + " IS NULL OR " + col + " < ? OR " + col + " >= ?)", []any{start(d), next(d)}, nil
	case OpLt:
		return col + " < ?", []any{start(d)}, nil
	case OpLe, OpOnOrBefore:
		return col + " < ?", []any{next(d)}, nil
	case OpGt:
		return col + " >= ?", []any{next(d)}, nil
	case OpGe, OpOnOrAfter:
		return col + " >= ?", []any{start(d)}, nil
	default: // between
		if d.Compare(days[1]) > 0 {
			return "", nil, badQuery("The start date must be on or before the end date.")
		}
		return "(" + col + " >= ? AND " + col + " < ?)", []any{start(d), next(days[1])}, nil
	}
}

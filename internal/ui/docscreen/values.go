package docscreen

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Display turns DOC's plain value of f into display text (CD-33, CD-43):
// NUM's formatters for numbers and dates, "{Code} {Name}" for a master link
// (CD-68) and Info.Display for a document link.
func Display(ctx context.Context, tx *db.Tx, eng doc.Engine, f doctype.Field, plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	switch f.Type {
	case doctype.Money:
		m, err := types.ParseMoney(plain)
		return types.FormatINR(m), err
	case doctype.Qty:
		q, err := types.ParseQty(plain)
		return types.FormatQty(q), err
	case doctype.Rate:
		r, err := types.ParseRate(plain)
		return "₹" + types.FormatRate(r), err
	case doctype.Percent:
		p, err := types.ParsePercent(plain)
		return types.FormatPercent(p), err
	case doctype.Factor:
		x, err := types.ParseFactor(plain)
		return types.FormatFactor(x), err
	case doctype.Date:
		d, err := types.DateFromISO(plain)
		return types.FormatDate(d), err
	case doctype.Timestamp:
		ts, err := types.ParseTimestamp(plain)
		return types.FormatIST(ts.Time()), err
	case doctype.Check:
		if plain == "1" || plain == "true" {
			return "Yes", nil
		}
		return "No", nil
	case doctype.Link:
		return linkDisplay(ctx, tx, eng, f.Link, plain)
	}
	return plain, nil
}

func linkDisplay(ctx context.Context, tx *db.Tx, eng doc.Engine, target, plain string) (string, error) {
	id, err := strconv.ParseInt(plain, 10, 64)
	if err != nil {
		return "", err
	}
	if dt, ok := doctype.Lookup(target); ok && dt.Master != nil {
		mi, found, err := dt.Master.Lookup(ctx, tx, id)
		if err != nil {
			return "", err
		}
		if !found {
			return "", doctype.ErrNotFound
		}
		return mi.Code + " " + mi.Name, nil
	}
	info, err := eng.Info(ctx, tx, target, id)
	return info.Display, err
}

// ToPlain parses typed text for f into DOC's plain form. A parse error is a
// FieldError carrying NUM's message unchanged. Text, Select, Check, Link
// and the other kinds pass through.
func ToPlain(f doctype.Field, typed string) (string, *doctype.FieldError) {
	if strings.TrimSpace(typed) == "" {
		return "", nil
	}
	var (
		plain string
		err   error
	)
	switch f.Type {
	case doctype.Money:
		var v types.Money
		v, err = types.ParseMoney(typed)
		plain = v.Plain()
	case doctype.Qty:
		var v types.Qty
		v, err = types.ParseQty(typed)
		plain = v.Plain()
	case doctype.Rate:
		var v types.Rate
		v, err = types.ParseRate(typed)
		plain = v.Plain()
	case doctype.Percent:
		var v types.Percent
		v, err = types.ParsePercent(typed)
		plain = v.Plain()
	case doctype.Factor:
		var v types.Factor
		v, err = types.ParseFactor(typed)
		plain = v.Plain()
	case doctype.Date:
		var v types.Date
		v, err = types.ParseDate(typed)
		plain = v.ISO()
	default:
		return typed, nil
	}
	if err != nil {
		return "", &doctype.FieldError{Field: f.Name, Message: err.Error()}
	}
	return plain, nil
}

// Problem is an error UI renders itself. Blocked is DOC's refusal of a cancel
// because of linked documents (DOC-052), which shares its status with others.
type Problem struct {
	Status  int
	Message string
	Fields  []doctype.FieldError
	Blocked bool
}

// Classify maps a kernel error through doctype.HTTPError (CD-44) or
// doctype.ErrNotFound (CD-54). Anything else is false: the handler passes
// it to web.ErrorFrom, which answers db.ErrBusy with 503 (CD-76, CD-81).
func Classify(err error) (Problem, bool) {
	var he doctype.HTTPError
	if errors.As(err, &he) {
		var de *doc.Error
		blocked := errors.As(err, &de) && de.Kind == doc.KindBlocked
		return Problem{Status: he.Status(), Message: err.Error(), Fields: he.FieldErrors(), Blocked: blocked}, true
	}
	if errors.Is(err, doctype.ErrNotFound) {
		return Problem{Status: 404, Message: err.Error()}, true
	}
	return Problem{}, false
}

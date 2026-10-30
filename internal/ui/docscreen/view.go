package docscreen

import (
	"context"
	"strconv"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// DocView is one document as its pages show it: display strings, the
// values in input form, and what Extras adds (CD-43, CD-78).
type DocView struct {
	Info           doctype.Info
	Version        int
	Record         auth.RecordRef
	Header         []components.Pair
	Display        map[string]string // header display values by field name
	Inputs         map[string]string
	Lines          []LineView
	GrandTotal     string
	InWords        string
	PrintURL       string // Print's URL from Deps.PrintURL
	Approval       ApprovalView
	ApproveBlocked string
	Origin         Origin
	History        []HistoryRow
	Attachments    []components.Attachment
	Problems       []doctype.FieldError // missing Required values
	DraftProblems  []doctype.FieldError // a draft's stored errors, DOC's text (CD-205)
	Warnings       []string
}

// LineView is one line: its values in input and display form, and its card.
type LineView struct {
	No              int
	Inputs, Display map[string]string
	Card            components.LineCard
}

// View reads document id: the record in one read transaction, then, once p
// may read it, a draft's DOC problems (CD-205), Extras, and, for a pending
// document the user may approve, APR's CheckApprover (CD-52). A missing
// record, or one p may not read, is doctype.ErrNotFound or AUTH's error
// (CD-46, CD-76).
func (d Deps) View(ctx context.Context, p auth.Principal, name string, id int64) (DocView, error) {
	dt, ok := doctype.Lookup(name)
	if !ok {
		return DocView{}, doctype.ErrNotFound
	}
	var v DocView
	err := d.DB.Read(ctx, func(tx *db.Tx) error {
		rec, err := d.Engine.Load(ctx, tx, name, id)
		if err != nil {
			return err
		}
		v, err = d.fromRecord(ctx, tx, dt, rec)
		return err
	})
	if err != nil {
		return DocView{}, err
	}
	if err := d.Auth.Can(ctx, p, name, "read", &v.Record); err != nil {
		return DocView{}, err
	}
	if v.Info.State == doctype.StateDraft {
		err := d.DB.Read(ctx, func(tx *db.Tx) (err error) {
			v.DraftProblems, err = d.Engine.Problems(ctx, tx, name, id)
			return err
		})
		if err != nil {
			return DocView{}, err
		}
	}
	v.PrintURL = d.printURL(name, id)
	if v.Approval, err = d.Extras.Approval(ctx, p, name, id); err != nil {
		return DocView{}, err
	}
	if v.Origin, err = d.Extras.Origin(ctx, p, name, id); err != nil {
		return DocView{}, err
	}
	if v.History, err = d.Extras.Audit(ctx, p, name, id); err != nil {
		return DocView{}, err
	}
	if v.Attachments, err = d.Extras.Attachments(ctx, p, v.Record); err != nil {
		return DocView{}, err
	}
	if v.Info.State == doctype.StatePending && d.Auth.Can(ctx, p, name, "approve", &v.Record) == nil {
		err := d.DB.Read(ctx, func(tx *db.Tx) error { return d.Approval.CheckApprover(ctx, tx, p, name, id) })
		if err != nil {
			v.ApproveBlocked = err.Error() // APR owns the wording (CD-52)
		}
	}
	return v, nil
}

// fromRecord turns rec into display and input strings.
func (d Deps) fromRecord(ctx context.Context, tx *db.Tx, dt *doctype.DocType, rec doc.Record) (DocView, error) {
	i := rec.Info
	v := DocView{
		Info:    i,
		Version: rec.Version,
		Record: auth.RecordRef{
			DocType: i.DocType, ID: i.ID, CreatedBy: i.CreatedBy,
			EmployeeID: i.EmployeeID, ProjectIDs: i.ProjectIDs, SiteIDs: i.SiteIDs,
		},
		Display: map[string]string{},
		Inputs:  map[string]string{},
	}
	for _, f := range dt.Fields {
		if f.OnLine {
			continue
		}
		plain := rec.Values[f.Name]
		if plain == "" {
			if required(f) {
				v.Problems = append(v.Problems, doctype.FieldError{Field: f.Name, Message: f.Label + " is required."})
			}
			continue
		}
		shown, err := Display(ctx, tx, d.Engine, f, plain)
		if err != nil {
			return DocView{}, err
		}
		v.Header = append(v.Header, components.Pair{Label: f.Label, Value: shown, Num: numeric(f)})
		v.Display[f.Name] = shown
		if v.Inputs[f.Name], err = inputValue(f, plain); err != nil {
			return DocView{}, err
		}
		if f.ListPriority == int(components.PFigure) && f.Type == doctype.Money {
			m, err := types.ParseMoney(plain)
			if err != nil {
				return DocView{}, err
			}
			v.GrandTotal = types.FormatINR(m)
			if words, err := print.InWordsINR(m); err == nil {
				v.InWords = words
			}
		}
	}
	for n, line := range rec.Lines {
		lv, problems, err := d.lineView(ctx, tx, dt, n+1, line)
		if err != nil {
			return DocView{}, err
		}
		v.Lines = append(v.Lines, lv)
		v.Problems = append(v.Problems, problems...)
	}
	return v, nil
}

// lineView is line no: every line field in input and display form, and the
// card "{qty} {uom} × {rate}" (CD-61) with the item's master name and code.
func (d Deps) lineView(ctx context.Context, tx *db.Tx, dt *doctype.DocType, no int, line map[string]string) (LineView, []doctype.FieldError, error) {
	lv := LineView{No: no, Inputs: map[string]string{}, Display: map[string]string{}, Card: components.LineCard{No: no}}
	var problems []doctype.FieldError
	var qty, rate string
	for _, f := range dt.Fields {
		if !f.OnLine {
			continue
		}
		plain := line[f.Name]
		if plain == "" {
			if required(f) {
				problems = append(problems, doctype.FieldError{Field: f.Name, Line: no, Message: f.Label + " is required."})
			}
			continue
		}
		shown, err := Display(ctx, tx, d.Engine, f, plain)
		if err != nil {
			return LineView{}, nil, err
		}
		lv.Display[f.Name] = shown
		if lv.Inputs[f.Name], err = inputValue(f, plain); err != nil {
			return LineView{}, nil, err
		}
		switch {
		case f.Type == doctype.Link && lv.Card.Item == "":
			lv.Card.Item, lv.Card.Code, err = itemOf(ctx, tx, f, plain, shown)
			if err != nil {
				return LineView{}, nil, err
			}
		case f.Type == doctype.Qty && qty == "":
			qty = lv.Inputs[f.Name]
		case f.Type == doctype.Rate && rate == "":
			rate = shown
		case f.Type == doctype.Money && lv.Card.Amount == "":
			lv.Card.Amount = shown
		}
	}
	var parts []string
	if qty != "" {
		parts = append(parts, strings.TrimSpace(qty+" "+line["uom"]))
	}
	if rate != "" {
		parts = append(parts, rate)
	}
	lv.Card.QtyRate = strings.Join(parts, " × ")
	return lv, problems, nil
}

// itemOf is a line link's name and code: the master's, else its display.
func itemOf(ctx context.Context, tx *db.Tx, f doctype.Field, plain, shown string) (name, code string, err error) {
	dt, ok := doctype.Lookup(f.Link)
	if !ok || dt.Master == nil {
		return shown, "", nil
	}
	id, err := strconv.ParseInt(plain, 10, 64)
	if err != nil {
		return "", "", err
	}
	mi, _, err := dt.Master.Lookup(ctx, tx, id)
	return mi.Name, mi.Code, err
}

// required matches DOC's check: a Check field is never "missing".
func required(f doctype.Field) bool { return f.Required && f.Type != doctype.Check }

func numeric(f doctype.Field) bool {
	switch f.Type {
	case doctype.Money, doctype.Qty, doctype.Rate, doctype.Percent, doctype.Factor:
		return true
	}
	return false
}

// inputValue is DOC's plain value as a form shows it: numbers grouped
// without ₹ or %, dates dd-mm-yyyy, links as ids, the rest as stored.
func inputValue(f doctype.Field, plain string) (string, error) {
	switch f.Type {
	case doctype.Money:
		m, err := types.ParseMoney(plain)
		return types.FormatMoney(m), err
	case doctype.Qty:
		q, err := types.ParseQty(plain)
		return types.FormatQty(q), err
	case doctype.Rate:
		r, err := types.ParseRate(plain)
		return types.FormatRate(r), err
	case doctype.Percent:
		p, err := types.ParsePercent(plain)
		return strings.TrimSuffix(types.FormatPercent(p), "%"), err
	case doctype.Factor:
		x, err := types.ParseFactor(plain)
		return types.FormatFactor(x), err
	case doctype.Date:
		dt, err := types.DateFromISO(plain)
		return types.FormatDate(dt), err
	}
	return plain, nil
}

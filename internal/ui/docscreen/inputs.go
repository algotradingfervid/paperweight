package docscreen

import (
	"context"
	"io"
	"strconv"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// maxRadios is the most options shown as radios; more is a Select.
const maxRadios = 6

// FieldInput is the form control for f with the element id id, the value in
// input form and an error message (CD-42): Text is a TextInput, or a
// Textarea with a counter past 200 characters; Int a numeric TextInput;
// Check one checkbox; Select radios up to 6 options, else a Select; Link a
// Picker searching /pick/{Link} that shows the chosen master as "{Code}
// {Name}" (CD-68); numbers (Money, Qty, Rate, Percent, Factor) a NumberInput
// checked by the server (CD-48); Date a DateInput with d.Today; Timestamp
// read-only text. MST's master forms reuse it.
func FieldInput(f doctype.Field, id, value, errMsg string, d Deps) templ.Component {
	p := components.FieldProps{ID: id, Name: f.Name, Label: f.Label, Hint: f.Hint, Error: errMsg, Value: value, Optional: !f.Required}
	switch f.Type {
	case doctype.Text:
		if f.MaxLen > 200 {
			return components.Textarea(p, f.MaxLen)
		}
		if f.MaxLen > 0 {
			p.Attrs = templ.Attributes{"maxlength": strconv.Itoa(f.MaxLen)}
		}
		return components.TextInput(p)
	case doctype.Int:
		p.Attrs = templ.Attributes{"inputmode": "numeric"}
		return components.TextInput(p)
	case doctype.Check:
		var checked []string
		if value == "1" || value == "true" {
			checked = []string{"1"}
		}
		return components.Checkboxes(p, []components.Option{{Value: "1", Label: "Yes"}}, checked)
	case doctype.Select:
		opts := make([]components.Option, len(f.Options))
		for i, o := range f.Options {
			opts[i] = components.Option{Value: o, Label: o}
		}
		if len(opts) <= maxRadios {
			return components.Radios(p, opts)
		}
		return components.Select(p, opts)
	case doctype.Link:
		return linkInput(f, p, d)
	case doctype.Money:
		return components.NumberInput(p, components.Money, false)
	case doctype.Qty:
		return components.NumberInput(p, components.Qty, false)
	case doctype.Rate:
		return components.NumberInput(p, components.Rate, false)
	case doctype.Percent:
		return components.NumberInput(p, components.Percent, false)
	case doctype.Factor:
		return components.NumberInput(p, components.Factor, false)
	case doctype.Date:
		dp := components.DateProps{FieldProps: p}
		if d.Today != nil {
			dp.Today, dp.Yesterday = d.Today()
		}
		return components.DateInput(dp)
	case doctype.Timestamp:
		return components.SummaryList([]components.Pair{{Label: f.Label, Value: value}})
	}
	return components.TextInput(p)
}

// linkInput is f's Picker. The chosen id is looked up when it renders, so
// the summary shows the master's "{Code} {Name}" (CD-68).
func linkInput(f doctype.Field, p components.FieldProps, d Deps) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		pp := components.PickerProps{Field: p, SearchURL: "/pick/" + f.Link, HiddenName: f.Name, ListboxID: p.ID + "-list"}
		if id, err := strconv.ParseInt(p.Value, 10, 64); err == nil {
			row := components.PickRow{ID: id, Code: p.Value}
			if dt, ok := doctype.Lookup(f.Link); ok && dt.Master != nil && d.DB != nil {
				err := d.DB.Read(ctx, func(tx *db.Tx) error {
					mi, found, err := dt.Master.Lookup(ctx, tx, id)
					if found {
						row.Code, row.Name = mi.Code, mi.Name
					}
					return err
				})
				if err != nil {
					return err
				}
			}
			pp.Chosen = &row
		}
		return components.Picker(pp).Render(ctx, w)
	})
}

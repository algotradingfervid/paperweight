package docscreen

import (
	"context"
	"errors"
	"io"
	"net/url"
	"strconv"
	"unicode/utf8"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// minSelect is the fewest options shown as a Select; fewer are Segmented,
// or Tiles when a label is longer than maxSegment characters (UI v2 §5.5
// item 8).
const (
	minSelect  = 5
	maxSegment = 12
)

// FieldInput is the form control for f with the element id id, the value in
// input form and an error message (CD-42): Text is a TextInput, or a
// Textarea with a counter past 200 characters; Int a numeric TextInput;
// Check a Switch (on when stored false for an Invert field) posting DOC's
// "true"/"false", or "1"/"0" with d.MasterChecks (CD-322); Select under 5
// options Segmented (Tiles with a label over 12 characters), else a Select; Link a
// Picker searching /pick/{Link} that shows the chosen master as "{Code}
// {Name}" (CD-68); numbers (Money, Qty, Rate, Percent, Factor) a NumberInput
// checked by the server (CD-48), Signed for a Money, Qty or Rate field that is
// neither NonNegative nor Positive (CD-336 (2)); Date a DateInput with
// d.Today; Timestamp read-only text. MST's master forms reuse it.
func FieldInput(f doctype.Field, id, value, errMsg string, d Deps) templ.Component {
	p := components.FieldProps{ID: id, Name: f.Name, Label: f.Label, Hint: f.Hint, Tip: f.Tip, Placeholder: f.Placeholder, Error: errMsg, Value: value, Required: f.Required, Clearable: !f.Required}
	if (f.Type == doctype.Money || f.Type == doctype.Qty || f.Type == doctype.Rate) && !f.NonNegative && !f.Positive {
		p = components.Signed(p)
	}
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
		p.Placeholder = ""
		if f.Invert != "" {
			p.Label = f.Invert
		}
		v := components.SwitchValues{Yes: "true", No: "false"}
		if d.MasterChecks {
			v = components.SwitchValues{Yes: "1", No: "0"}
		}
		return components.Switch(p, f.Invert != "", v)
	case doctype.Select:
		p.Placeholder = ""
		opts := make([]components.Option, len(f.Options))
		for i, o := range f.Options {
			opts[i] = components.Option{Value: o, Label: optionLabel(f, o)}
		}
		if len(opts) >= minSelect {
			return components.Select(p, opts)
		}
		for _, o := range opts {
			if utf8.RuneCountInString(o.Label) > maxSegment {
				return components.Tiles(p, opts)
			}
		}
		return components.Segmented(p, opts)
	case doctype.Link:
		return linkInput(f, p, d)
	case doctype.Money:
		if p.Placeholder == "" {
			p.Placeholder = "0.00" // UI v2 §5.5 item 5
		}
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
		p.Placeholder = "" // a Date always shows dd-mm-yyyy (UI v2 §5.5 item 5)
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

// searchURL is what f's Picker searches; a LinkRole narrows it (CD-218).
func searchURL(f doctype.Field) string {
	if f.LinkRole != "" {
		return "/pick/" + f.Link + "?role=" + url.QueryEscape(f.LinkRole)
	}
	return "/pick/" + f.Link
}

// linkInput is f's Picker. The chosen id is looked up when it renders, so
// the summary shows the master's "{Code} {Name}" (CD-68). A posted id shows that
// only if d.LinkRead lets the user read it; otherwise the raw id, as for an id
// with no record (CD-233). On a document's header it offers recent picks.
func linkInput(f doctype.Field, p components.FieldProps, d Deps) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		recent, err := d.recentPicks(ctx, f)
		if err != nil {
			return err
		}
		pp := components.PickerProps{Field: p, SearchURL: searchURL(f), HiddenName: f.Name, ListboxID: p.ID + "-list", Recent: recent}
		if id, err := strconv.ParseInt(p.Value, 10, 64); err == nil {
			row := components.PickRow{ID: id, Code: p.Value}
			if dt, ok := doctype.Lookup(f.Link); ok && dt.Master != nil && d.DB != nil {
				err := d.DB.Read(ctx, func(tx *db.Tx) error {
					if show, err := d.showsLabel(ctx, tx, dt, p, id); !show {
						return err
					}
					mi, found, err := dt.Master.Lookup(ctx, tx, id)
					if err != nil || !found {
						return err
					}
					row.Code, row.Name = mi.Code, mi.Name
					row.Line, err = d.linkLine(ctx, tx, dt, id)
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

// linkLine is the line of master id of target for the request's user.
func (d Deps) linkLine(ctx context.Context, tx *db.Tx, target *doctype.DocType, id int64) (string, error) {
	if d.LinkLine == nil {
		return "", nil
	}
	user, _ := auth.PrincipalFrom(ctx)
	return d.LinkLine(ctx, tx, user, target, id)
}

// showsLabel reports whether the link in p may show its target's label.
func (d Deps) showsLabel(ctx context.Context, tx *db.Tx, target *doctype.DocType, p components.FieldProps, id int64) (bool, error) {
	if d.stored[p.Name] == p.Value {
		return true, nil
	}
	if d.LinkRead == nil {
		return false, nil
	}
	user, _ := auth.PrincipalFrom(ctx)
	err := d.LinkRead(ctx, tx, user, target, id)
	if errors.Is(err, doctype.ErrNotFound) || errors.Is(err, auth.ErrForbidden) {
		return false, nil
	}
	return err == nil, err
}

package components

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/a-h/templ"
)

// PickRow is one search result of a Picker: code, name and one line that
// tells similar rows apart.
type PickRow struct {
	ID               int64
	Code, Name, Line string
}

// Display is the master's display text, "{Code} {Name}" (CD-68).
func (r PickRow) Display() string { return r.Code + " " + r.Name }

// PickerProps describes a Picker. Field.ID is what an ErrorSummary links to:
// the search input's id, or the picker's own when rendered with a row chosen,
// because the search is hidden then (see pickerInputID). HiddenName is the
// name of the field that carries the chosen ID. Recent are the user's
// recent picks for this field, newest first (UI v2 §5.8); combobox.js shows
// the first MaxRecent that match the typed text above each search's rows.
type PickerProps struct {
	Field                            FieldProps
	SearchURL, HiddenName, ListboxID string
	Chosen                           *PickRow
	Options                          []Option
	Recent                           []PickRow
}

// maxPickRows is the most rows a search shows (design-system §7).
const maxPickRows = 20

// MaxRecent is the most recent picks a Picker shows (UI v2 §5.8).
const MaxRecent = 5

// resultsHeading names a search's group: "All" for the empty search.
func resultsHeading(q string) string {
	if strings.TrimSpace(q) == "" {
		return "All"
	}
	return "Matches"
}

// splitMatch cuts s around the first match of the trimmed q, ignoring case;
// hit is "" when q is blank or absent.
func splitMatch(s, q string) (before, hit, after string) {
	q = strings.TrimSpace(q)
	n := utf8.RuneCountInString(q)
	for i := range s {
		end := i
		for range n {
			_, size := utf8.DecodeRuneInString(s[end:])
			end += size
		}
		if strings.EqualFold(s[i:end], q) {
			return s[:i], s[i:end], s[end:]
		}
	}
	return s, "", ""
}

// pickerSelectField is the Select fallback's field: it posts the chosen ID.
func pickerSelectField(p PickerProps) FieldProps {
	f := p.Field
	if p.HiddenName != "" {
		f.Name = p.HiddenName
	}
	return f
}

// pickerInputID is the search input's id. It gives way to the picker itself
// while a row is chosen, when the search is hidden.
func pickerInputID(p PickerProps) string {
	if p.Chosen != nil {
		return p.Field.ID + "-search"
	}
	return p.Field.ID
}

// pickerField is the field around the picker: its label names the search input.
func pickerField(p PickerProps) FieldProps {
	f := p.Field
	f.labelFor = pickerInputID(p)
	return f
}

// pickerGroupAttrs stand the picker in for its hidden input while a row is
// chosen: it takes Field.ID and tells what is wrong, as the input would.
func pickerGroupAttrs(p PickerProps) templ.Attributes {
	if p.Chosen == nil {
		return nil
	}
	a := templ.Attributes{"id": p.Field.ID, "aria-describedby": describedBy(p.Field)}
	if p.Field.Error != "" {
		a["aria-invalid"] = "true"
	}
	return a
}

// pickerSearchField is the search input's field: it is named "q".
func pickerSearchField(p PickerProps) FieldProps {
	f := p.Field
	f.Name = "q"
	return f
}

func pickerInputAttrs(p PickerProps) templ.Attributes {
	a := controlAttrs(pickerSearchField(p), "input field__input")
	a["id"] = pickerInputID(p)
	a["placeholder"] = "Type to search"
	a["form"] = "" // no form owner: the text goes only with the htmx search, not the form (no q= on GET filter URLs)
	return a
}

func pickerChosen(p PickerProps) (id, text string) {
	if p.Chosen == nil {
		return "", ""
	}
	return strconv.FormatInt(p.Chosen.ID, 10), p.Chosen.Display()
}

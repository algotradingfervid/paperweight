package components

import "strconv"

// PickRow is one search result of a Picker: code, name and one line that
// tells similar rows apart.
type PickRow struct {
	ID               int64
	Code, Name, Line string
}

// Display is the master's display text, "{Code} {Name}" (CD-68).
func (r PickRow) Display() string { return r.Code + " " + r.Name }

// PickerProps describes a Picker. Field.ID is the search input's id;
// HiddenName is the name of the field that carries the chosen ID.
type PickerProps struct {
	Field                            FieldProps
	SearchURL, HiddenName, ListboxID string
	Chosen                           *PickRow
	Options                          []Option
}

// maxPickRows is the most rows a search shows (design-system §7).
const maxPickRows = 20

// pickerSelectField is the Select fallback's field: it posts the chosen ID.
func pickerSelectField(p PickerProps) FieldProps {
	f := p.Field
	if p.HiddenName != "" {
		f.Name = p.HiddenName
	}
	return f
}

// pickerSearchField is the search input's field: it is named "q".
func pickerSearchField(p PickerProps) FieldProps {
	f := p.Field
	f.Name = "q"
	return f
}

func pickerChosen(p PickerProps) (id, text string) {
	if p.Chosen == nil {
		return "", ""
	}
	return strconv.FormatInt(p.Chosen.ID, 10), p.Chosen.Display()
}

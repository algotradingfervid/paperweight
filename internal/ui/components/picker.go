package components

import (
	"cmp"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"unicode"
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

// Monogram is the row's tile: the first letter or digit of the first two
// words of its name; "" with no name, as for a raw id (CD-233, UI v2 §5.8).
func (r PickRow) Monogram() string {
	words := strings.FieldsFunc(r.Name, func(c rune) bool { return !unicode.IsLetter(c) && !unicode.IsMark(c) && !unicode.IsDigit(c) })
	var b strings.Builder
	for _, w := range words[:min(len(words), 2)] {
		c, _ := utf8.DecodeRuneInString(w)
		b.WriteRune(unicode.ToUpper(c))
	}
	return b.String()
}

// PickerProps describes a Picker. Field.ID is what an ErrorSummary links to:
// the search input's id, or the picker's own when rendered with a row chosen,
// because the search is hidden then (see pickerInputID). HiddenName is the
// name of the field that carries the chosen ID. Recent are the user's
// recent picks for this field, newest first (UI v2 §5.8); combobox.js shows
// the first MaxRecent that match the typed text above each search's rows.
// Focus puts autofocus on Change, for a picker a quick-create swaps in.
type PickerProps struct {
	Field                            FieldProps
	SearchURL, HiddenName, ListboxID string
	Chosen                           *PickRow
	Options                          []Option
	Recent                           []PickRow
	Focus                            bool
}

// Noun names a picker's master in its texts: One "vendor", Many "vendors".
type Noun struct{ One, Many string }

// PickNew is a search's create option (UI v2 §5.8, N10): the master's noun
// and the QuickCreate sheet it opens, which the option carries.
type PickNew struct {
	Noun  Noun
	Sheet templ.Component
}

// quickSheetID is the id of the QuickCreate sheet of the picker whose
// listbox is listboxID.
func quickSheetID(listboxID string) string { return listboxID + "-sheet" }

// QuickFormID is the id of that sheet's form, which a refused quick-create
// redraws.
func QuickFormID(listboxID string) string { return quickSheetID(listboxID) + "-form" }

// newLabel is the create option's text: New {noun} "{q}", or New {noun} for
// a blank search.
func newLabel(noun, q string) string {
	if q = strings.TrimSpace(q); q == "" {
		return "New " + noun
	}
	return "New " + noun + " \"" + q + "\""
}

// noMatch is the empty text: No {noun} matches "{q}", or, with nothing typed,
// that the master has nothing to offer yet.
func noMatch(noun, q string) string {
	if q = strings.TrimSpace(q); q == "" {
		return "No " + noun + " to choose from yet."
	}
	return "No " + noun + " matches \"" + q + "\""
}

// emptyLine is the empty state's sentence under noMatch.
func emptyLine(q string) string {
	if strings.TrimSpace(q) == "" {
		return "Add one now. You'll come straight back here."
	}
	return "Check the spelling, or add them now. You'll come straight back here."
}

// countLine is the line over a search's n rows (§5.8), "" with nothing typed:
// more than the rows shown asks for more letters.
func countLine(n int, q string, noun Noun) string {
	q = strings.TrimSpace(q)
	switch {
	case q == "":
		return ""
	case n > MaxPickRows:
		return "More than " + strconv.Itoa(MaxPickRows) + " " + noun.Many + " match \"" + q + "\". Keep typing to narrow."
	case n == 1:
		return "1 " + noun.One + " matches \"" + q + "\"."
	}
	return strconv.Itoa(n) + " " + noun.Many + " match \"" + q + "\"."
}

// pickerVals is the quick slot's hx-vals: what a quick-create posts back so
// the server draws this picker again (PickerFromForm).
func pickerVals(p PickerProps) string {
	flag := func(b bool) string {
		if b {
			return "1"
		}
		return ""
	}
	recent, _ := json.Marshal(p.Recent)     // plain values always marshal
	b, _ := json.Marshal(map[string]string{ // a map of strings always marshals
		"pick_recent": string(recent),
		"pick_id":     p.Field.ID, "pick_label": p.Field.Label, "pick_hint": p.Field.Hint, "pick_tip": p.Field.Tip,
		"pick_placeholder": p.Field.Placeholder, "pick_required": flag(p.Field.Required), "pick_clearable": flag(p.Field.Clearable),
		"pick_search": p.SearchURL, "pick_name": p.HiddenName, "pick_list": p.ListboxID,
	})
	return string(b)
}

// PickerFromForm is the Picker whose quick slot posted v. Recent picks that
// don't parse are dropped: they only show the poster's own list again.
func PickerFromForm(v url.Values) PickerProps {
	var recent []PickRow
	_ = json.Unmarshal([]byte(v.Get("pick_recent")), &recent) // a bad list is no list
	return PickerProps{
		Recent: recent,
		Field: FieldProps{
			ID: v.Get("pick_id"), Label: v.Get("pick_label"), Hint: v.Get("pick_hint"), Tip: v.Get("pick_tip"),
			Placeholder: v.Get("pick_placeholder"), Required: v.Get("pick_required") == "1", Clearable: v.Get("pick_clearable") == "1",
		},
		SearchURL: v.Get("pick_search"), HiddenName: v.Get("pick_name"), ListboxID: v.Get("pick_list"),
	}
}

// MaxPickRows is the most rows a search shows (design-system §7).
const MaxPickRows = 20

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
	a["placeholder"] = cmp.Or(p.Field.Placeholder, "Name or code") // UI v2 §5.8
	a["form"] = ""                                                 // no form owner: the text goes only with the htmx search, not the form (no q= on GET filter URLs)
	return a
}

// pickerChangeAttrs are Change's attributes; Focus autofocuses it.
func pickerChangeAttrs(p PickerProps) templ.Attributes {
	return templ.Attributes{"data-picker-change": true, "autofocus": p.Focus}
}

func pickerChosen(p PickerProps) (id, text, tile, line string) {
	if p.Chosen == nil {
		return "", "", "", ""
	}
	return strconv.FormatInt(p.Chosen.ID, 10), p.Chosen.Display(), p.Chosen.Monogram(), p.Chosen.Line
}

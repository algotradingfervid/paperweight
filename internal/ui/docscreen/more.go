package docscreen

import (
	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// moreBox gathers a card's Field.More inputs for one MoreDetails at its end
// (UI v2 §5.5 item 3). Callers add only the fields they show, so a field the
// module hides is never inside it, named in its summary or opening it.
type moreBox struct {
	labels []string
	inputs []templ.Component
	open   bool
}

// add puts f's input in, as shown with value and errMsg; either opens the
// group (A-10).
func (b *moreBox) add(f doctype.Field, value, errMsg string, in templ.Component) {
	label := f.Label
	if f.Invert != "" {
		label = f.Invert
	}
	b.labels = append(b.labels, label)
	b.inputs = append(b.inputs, in)
	b.open = b.open || errMsg != "" || filled(f, value)
}

// filled reports whether value holds something: a Check stored false holds
// nothing.
func filled(f doctype.Field, value string) bool {
	if f.Type == doctype.Check {
		return value == "true"
	}
	return value != ""
}

// appendTo is inputs followed by b's group, or inputs when b is empty.
func (b moreBox) appendTo(inputs []templ.Component) []templ.Component {
	if len(b.inputs) == 0 {
		return inputs
	}
	return append(inputs, moreGroup(b))
}

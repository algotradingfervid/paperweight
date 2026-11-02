package docscreen

import (
	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// MoreInput is one shown input of a card for GroupMore: the Field it edits
// (its More flag, label, Invert and Type), the value and error it shows, and
// its rendered input. A Check's Value is "true" or "1" when set (a document's
// or a master's form), anything else when not. Callers pass only the inputs they show, so a field the
// module hides is never inside the group, named in its summary or opening it.
type MoreInput struct {
	Field        doctype.Field
	Value, Error string
	Input        templ.Component
}

// MoreGroup is a card's inputs split by GroupMore (UI v2 §5.5 item 3,
// CD-363): Plain shows as it is, in order; Group, with Labels for the
// summary, goes in one MoreDetails after it, open when a value or an error
// is inside (A-10). Group is empty, and the group absent, when no shown
// input is More or when every one is.
type MoreGroup struct {
	Plain, Group []templ.Component
	Labels       []string
	Open         bool
}

// GroupMore splits a card's shown inputs into the plain ones and the More
// group (CD-363).
func GroupMore(ins []MoreInput) MoreGroup {
	var g MoreGroup
	for _, in := range ins {
		if !in.Field.More {
			g.Plain = append(g.Plain, in.Input)
			continue
		}
		label := in.Field.Label
		if in.Field.Invert != "" {
			label = in.Field.Invert
		}
		g.Labels = append(g.Labels, label)
		g.Group = append(g.Group, in.Input)
		g.Open = g.Open || in.Error != "" || filled(in.Field, in.Value)
	}
	if len(g.Plain) == 0 {
		return MoreGroup{Plain: g.Group}
	}
	return g
}

// filled reports whether value holds something. A Check holds something
// only when set: "true" in a document's values, "1" in a master's form; its
// unset "false" or "0" holds nothing.
func filled(f doctype.Field, value string) bool {
	if f.Type == doctype.Check {
		return value == "true" || value == "1"
	}
	return value != ""
}

// Inputs is Plain followed by the More group, when there is one.
func (g MoreGroup) Inputs() []templ.Component {
	if len(g.Group) == 0 {
		return g.Plain
	}
	return append(g.Plain, moreGroup(g))
}
